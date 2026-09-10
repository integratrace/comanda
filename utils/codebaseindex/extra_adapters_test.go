package codebaseindex

import (
	"os"
	"path/filepath"
	"testing"
)

// stubAdapter is a minimal in-process Adapter an embedder could supply via
// Config.ExtraAdapters.
type stubAdapter struct {
	name string
	exts []string
}

func (a *stubAdapter) Name() string                 { return a.name }
func (a *stubAdapter) DetectionFiles() []string     { return []string{a.name + ".manifest"} }
func (a *stubAdapter) FileExtensions() []string     { return a.exts }
func (a *stubAdapter) IgnoreDirs() []string         { return nil }
func (a *stubAdapter) IgnoreGlobs() []string        { return nil }
func (a *stubAdapter) EntrypointPatterns() []string { return nil }
func (a *stubAdapter) ConfigPatterns() []string     { return nil }

func (a *stubAdapter) ExtractSymbols(path string, content []byte) (*SymbolInfo, error) {
	return &SymbolInfo{}, nil
}

func (a *stubAdapter) ScoreFile(path string, depth int, isEntrypoint, isConfig bool) int {
	return 0
}

func newTestConfig(t *testing.T, root string) *Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Root = root
	return cfg
}

func TestNewManagerRegistersExtraAdapters(t *testing.T) {
	root := t.TempDir()

	cfg := newTestConfig(t, root)
	cfg.ExtraAdapters = []Adapter{
		&stubAdapter{name: "acme-dsl", exts: []string{".acme"}},
		&stubAdapter{name: "widget", exts: []string{".widget"}},
	}

	m, err := NewManager(cfg, false)
	if err != nil {
		t.Fatalf("NewManager with ExtraAdapters failed: %v", err)
	}

	for _, name := range []string{"acme-dsl", "widget"} {
		if _, ok := m.registry.Get(name); !ok {
			t.Errorf("extra adapter %q was not registered", name)
		}
	}

	// Built-ins must survive alongside the injected ones.
	if _, ok := m.registry.Get("go"); !ok {
		t.Error("registering extra adapters dropped the built-in go adapter")
	}
}

func TestNewManagerRejectsExtraAdapterCollisions(t *testing.T) {
	tests := []struct {
		name    string
		adapter Adapter
	}{
		{name: "collides with built-in", adapter: &stubAdapter{name: "go", exts: []string{".go"}}},
		{name: "collides with another built-in", adapter: &stubAdapter{name: "python", exts: []string{".py"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestConfig(t, t.TempDir())
			cfg.ExtraAdapters = []Adapter{tt.adapter}

			if _, err := NewManager(cfg, false); err == nil {
				t.Fatalf("NewManager accepted an extra adapter colliding with %q, want an error", tt.adapter.Name())
			}
		})
	}
}

func TestNewManagerRejectsDuplicateExtraAdapters(t *testing.T) {
	cfg := newTestConfig(t, t.TempDir())
	cfg.ExtraAdapters = []Adapter{
		&stubAdapter{name: "acme-dsl", exts: []string{".acme"}},
		&stubAdapter{name: "acme-dsl", exts: []string{".acme2"}},
	}

	if _, err := NewManager(cfg, false); err == nil {
		t.Fatal("NewManager accepted two extra adapters with the same name, want an error")
	}
}

func TestNewManagerRejectsNilAndUnnamedExtraAdapters(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		cfg := newTestConfig(t, t.TempDir())
		cfg.ExtraAdapters = []Adapter{nil}
		if _, err := NewManager(cfg, false); err == nil {
			t.Fatal("NewManager accepted a nil extra adapter, want an error")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		cfg := newTestConfig(t, t.TempDir())
		cfg.ExtraAdapters = []Adapter{&stubAdapter{name: "", exts: []string{".x"}}}
		if _, err := NewManager(cfg, false); err == nil {
			t.Fatal("NewManager accepted an unnamed extra adapter, want an error")
		}
	})
}

// TestRegistryAllIsDeterministic runs the ordering repeatedly: map iteration
// order is randomized per range statement, so a single comparison would pass by
// luck.
func TestRegistryAllIsDeterministic(t *testing.T) {
	build := func() []string {
		r := NewRegistry()
		r.Register(&stubAdapter{name: "acme-dsl", exts: []string{".acme"}})
		r.Register(&stubAdapter{name: "zeta", exts: []string{".zeta"}})
		names := make([]string, 0)
		for _, a := range r.All() {
			names = append(names, a.Name())
		}
		return names
	}

	want := build()
	if len(want) == 0 {
		t.Fatal("registry returned no adapters")
	}
	for i := 0; i < 20; i++ {
		got := build()
		if len(got) != len(want) {
			t.Fatalf("iteration %d: adapter count = %d, want %d", i, len(got), len(want))
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: All() order = %v, want %v", i, got, want)
			}
		}
	}

	// And it must actually be name-sorted, not merely stable.
	for i := 1; i < len(want); i++ {
		if want[i-1] > want[i] {
			t.Errorf("All() is not name-sorted: %v", want)
			break
		}
	}
}

// TestRegistryDetectIsDeterministic is the case that actually changes indexing
// output: Detect feeds the first-match extension routing in scan.go.
func TestRegistryDetectIsDeterministic(t *testing.T) {
	root := t.TempDir()

	// A repo that trips several adapters at once, so ordering is observable.
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/x\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "package.json"), "{}\n")
	writeFile(t, filepath.Join(root, "index.js"), "console.log(1)\n")
	writeFile(t, filepath.Join(root, "requirements.txt"), "requests\n")
	writeFile(t, filepath.Join(root, "app.py"), "print(1)\n")

	build := func() []string {
		r := NewRegistry()
		r.Register(&stubAdapter{name: "acme-dsl", exts: []string{".acme"}})
		names := make([]string, 0)
		for _, a := range r.Detect(root) {
			names = append(names, a.Name())
		}
		return names
	}

	want := build()
	if len(want) < 2 {
		t.Fatalf("expected multiple adapters detected for the fixture repo, got %v", want)
	}

	for i := 0; i < 20; i++ {
		got := build()
		if len(got) != len(want) {
			t.Fatalf("iteration %d: detected count = %d (%v), want %d (%v)", i, len(got), got, len(want), want)
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: Detect() order = %v, want %v", i, got, want)
			}
		}
	}

	for i := 1; i < len(want); i++ {
		if want[i-1] > want[i] {
			t.Errorf("Detect() is not name-sorted: %v", want)
			break
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
