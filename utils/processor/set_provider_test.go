package processor

import (
	"sync"
	"testing"

	"github.com/kris-hansen/comanda/utils/config"
	"github.com/kris-hansen/comanda/utils/models"
)

// stubProvider is a models.Provider that serves a model name no registry or
// DetectProvider implementation knows about. It records what it was asked to
// do so tests can assert the processor actually routed through it.
type stubProvider struct {
	mu             sync.Mutex
	name           string
	model          string
	configureCalls []string
	promptedModels []string
	promptedTexts  []string
	response       string
}

func newStubProvider(name, model string) *stubProvider {
	return &stubProvider{name: name, model: model, response: "stub response"}
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) SupportsModel(modelName string) bool { return modelName == s.model }

func (s *stubProvider) Configure(apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configureCalls = append(s.configureCalls, apiKey)
	return nil
}

func (s *stubProvider) SendPrompt(model, prompt string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptedModels = append(s.promptedModels, model)
	s.promptedTexts = append(s.promptedTexts, prompt)
	return s.response, nil
}

func (s *stubProvider) SendPromptWithFile(model, prompt string, file models.FileInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptedModels = append(s.promptedModels, model)
	s.promptedTexts = append(s.promptedTexts, prompt)
	return s.response, nil
}

func (s *stubProvider) SetVerbose(bool) {}

func (s *stubProvider) prompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.promptedTexts...)
}

func (s *stubProvider) configureCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.configureCalls)
}

// TestSetProviderServesStepWithoutEnvConfigEntry is the core embedding case: a
// caller hands the processor an already-configured provider and runs a
// workflow whose model exists in no envConfig provider entry and in no
// registry.
func TestSetProviderServesStepWithoutEnvConfigEntry(t *testing.T) {
	stub := newStubProvider("embedded-stub", "stub-model-x")

	dsl := &DSLConfig{
		Steps: []Step{
			{
				Name: "step_one",
				Config: StepConfig{
					Input:  []string{"NA"},
					Model:  []string{"stub-model-x"},
					Action: []string{"do the thing"},
					Output: []string{"STDOUT"},
				},
			},
		},
	}

	// envConfig deliberately has no entry for the stub provider or its model.
	envConfig := &config.EnvConfig{Providers: map[string]*config.Provider{}}

	p := NewProcessor(dsl, envConfig, createTestServerConfig(), false, "")
	p.SetProvider(stub)

	if err := p.Process(); err != nil {
		t.Fatalf("Process() with SetProvider-registered stub failed: %v", err)
	}

	prompts := stub.prompts()
	if len(prompts) != 1 {
		t.Fatalf("stub provider prompt count = %d, want 1 (provider was not used for the step)", len(prompts))
	}
	if got := stub.promptedModels[0]; got != "stub-model-x" {
		t.Errorf("stub received model %q, want %q", got, "stub-model-x")
	}

	// The caller already configured it; the processor must not re-Configure.
	if n := stub.configureCallCount(); n != 0 {
		t.Errorf("Configure() called %d times on a pre-configured provider, want 0", n)
	}
}

// TestSetProviderRegistersUnderProviderName checks the map bookkeeping so a
// later SetProvider call for the same name replaces rather than duplicates.
func TestSetProviderRegistersUnderProviderName(t *testing.T) {
	p := NewProcessor(&DSLConfig{}, createTestEnvConfig(), createTestServerConfig(), false, "")

	first := newStubProvider("embedded-stub", "stub-model-x")
	p.SetProvider(first)

	if got := p.providers["embedded-stub"]; got != models.Provider(first) {
		t.Fatalf("provider registered under name %q = %v, want the stub", "embedded-stub", got)
	}
	if !p.preConfiguredNames["embedded-stub"] {
		t.Error("preConfiguredNames missing the SetProvider-registered name")
	}

	second := newStubProvider("embedded-stub", "stub-model-y")
	p.SetProvider(second)

	if got := p.providers["embedded-stub"]; got != models.Provider(second) {
		t.Error("second SetProvider call did not replace the first for the same provider name")
	}
	if len(p.providers) != 1 {
		t.Errorf("providers map size = %d, want 1 after re-registering the same name", len(p.providers))
	}
}

// TestConfigureProvidersSkipsPreConfigured guards the short-circuit: a stub
// with no envConfig entry would otherwise fail configureProviders with
// "unknown provider" or "missing API key".
func TestConfigureProvidersSkipsPreConfigured(t *testing.T) {
	stub := newStubProvider("embedded-stub", "stub-model-x")

	envConfig := &config.EnvConfig{Providers: map[string]*config.Provider{}}
	p := NewProcessor(&DSLConfig{}, envConfig, createTestServerConfig(), false, "")
	p.SetProvider(stub)

	if err := p.configureProviders(); err != nil {
		t.Fatalf("configureProviders() with a pre-configured provider failed: %v", err)
	}
	if n := stub.configureCallCount(); n != 0 {
		t.Errorf("Configure() called %d times, want 0 for a SetProvider-registered provider", n)
	}
}

// TestGetProviderForModelPrefersPreConfigured ensures the lookup path used by
// generate/responses steps also resolves the injected provider.
func TestGetProviderForModelPrefersPreConfigured(t *testing.T) {
	stub := newStubProvider("embedded-stub", "stub-model-x")

	envConfig := &config.EnvConfig{Providers: map[string]*config.Provider{}}
	p := NewProcessor(&DSLConfig{}, envConfig, createTestServerConfig(), false, "")
	p.SetProvider(stub)

	got, err := p.getProviderForModel("stub-model-x")
	if err != nil {
		t.Fatalf("getProviderForModel() error: %v", err)
	}
	if got != models.Provider(stub) {
		t.Errorf("getProviderForModel() = %v, want the SetProvider-registered stub", got)
	}
}
