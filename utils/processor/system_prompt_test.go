package processor

import (
	"strings"
	"sync"
	"testing"

	"github.com/kris-hansen/comanda/utils/config"
	"github.com/kris-hansen/comanda/utils/models"
)

// systemStubProvider implements models.Provider plus the optional
// SystemPrompter and ModelConfigurer capabilities, recording everything it was
// handed so tests can assert on what the processor actually threaded through.
type systemStubProvider struct {
	mu sync.Mutex

	name  string
	model string

	plainPrompts  []string
	systemPrompts []string
	userPrompts   []string
	filePrompts   []string
	fileSystems   []string

	config     models.ModelConfig
	setConfigs []models.ModelConfig
}

func newSystemStubProvider(name, model string) *systemStubProvider {
	return &systemStubProvider{
		name:  name,
		model: model,
		config: models.ModelConfig{
			Temperature: 0.7,
			MaxTokens:   2000,
			TopP:        1.0,
		},
	}
}

func (s *systemStubProvider) Name() string                        { return s.name }
func (s *systemStubProvider) SupportsModel(modelName string) bool { return modelName == s.model }
func (s *systemStubProvider) Configure(apiKey string) error       { return nil }
func (s *systemStubProvider) SetVerbose(bool)                     {}

func (s *systemStubProvider) SendPrompt(model, prompt string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plainPrompts = append(s.plainPrompts, prompt)
	return "stub response", nil
}

func (s *systemStubProvider) SendPromptWithFile(model, prompt string, file models.FileInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plainPrompts = append(s.plainPrompts, prompt)
	return "stub response", nil
}

func (s *systemStubProvider) SendPromptWithSystem(model, system, prompt string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemPrompts = append(s.systemPrompts, system)
	s.userPrompts = append(s.userPrompts, prompt)
	return "stub response", nil
}

func (s *systemStubProvider) SendPromptWithFileAndSystem(model, system, prompt string, file models.FileInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileSystems = append(s.fileSystems, system)
	s.filePrompts = append(s.filePrompts, prompt)
	return "stub response", nil
}

func (s *systemStubProvider) SetConfig(cfg models.ModelConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
	s.setConfigs = append(s.setConfigs, cfg)
}

func (s *systemStubProvider) GetConfig() models.ModelConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *systemStubProvider) snapshot() ([]string, []string, []string, []models.ModelConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.systemPrompts...),
		append([]string(nil), s.userPrompts...),
		append([]string(nil), s.plainPrompts...),
		append([]models.ModelConfig(nil), s.setConfigs...)
}

// plainStubProvider implements only models.Provider — no SystemPrompter, no
// ModelConfigurer — to exercise the fallback paths.
type plainStubProvider struct {
	mu      sync.Mutex
	name    string
	model   string
	prompts []string
}

func newPlainStubProvider(name, model string) *plainStubProvider {
	return &plainStubProvider{name: name, model: model}
}

func (s *plainStubProvider) Name() string                        { return s.name }
func (s *plainStubProvider) SupportsModel(modelName string) bool { return modelName == s.model }
func (s *plainStubProvider) Configure(apiKey string) error       { return nil }
func (s *plainStubProvider) SetVerbose(bool)                     {}

func (s *plainStubProvider) SendPrompt(model, prompt string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, prompt)
	return "plain response", nil
}

func (s *plainStubProvider) SendPromptWithFile(model, prompt string, file models.FileInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, prompt)
	return "plain response", nil
}

func (s *plainStubProvider) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.prompts...)
}

func emptyEnvConfig() *config.EnvConfig {
	return &config.EnvConfig{Providers: map[string]*config.Provider{}}
}

func runStepWithProvider(t *testing.T, provider models.Provider, stepConfig StepConfig) {
	t.Helper()

	dsl := &DSLConfig{
		Steps: []Step{{Name: "step_one", Config: stepConfig}},
	}
	p := NewProcessor(dsl, emptyEnvConfig(), createTestServerConfig(), false, "")
	p.SetProvider(provider)

	if err := p.Process(); err != nil {
		t.Fatalf("Process() failed: %v", err)
	}
}

// TestStepInstructionsUseSystemPrompter is the headline behavior: a standard
// (non-responses) step with instructions must reach the provider as a system
// prompt, not glued onto the user prompt.
func TestStepInstructionsUseSystemPrompter(t *testing.T) {
	stub := newSystemStubProvider("sysstub", "sys-model")

	runStepWithProvider(t, stub, StepConfig{
		Input:        []string{"NA"},
		Model:        []string{"sys-model"},
		Action:       []string{"summarize the input"},
		Output:       []string{"STDOUT"},
		Instructions: "You are a terse assistant.",
	})

	systems, users, plains, _ := stub.snapshot()

	if len(systems) != 1 {
		t.Fatalf("SendPromptWithSystem call count = %d, want 1 (system prompt was not threaded through)", len(systems))
	}
	if systems[0] != "You are a terse assistant." {
		t.Errorf("system prompt = %q, want %q", systems[0], "You are a terse assistant.")
	}
	if len(plains) != 0 {
		t.Errorf("plain SendPrompt was used %d time(s); the SystemPrompter path should have been taken", len(plains))
	}
	if len(users) != 1 || users[0] == "" {
		t.Fatalf("user prompt not passed through: %v", users)
	}
	// The instructions must NOT also be smuggled into the user prompt.
	if got := users[0]; len(got) >= len("You are a terse assistant.") &&
		strings.Contains(got, "You are a terse assistant.") {
		t.Errorf("system prompt leaked into the user prompt: %q", got)
	}
}

// TestStepWithoutInstructionsUsesPlainSendPrompt guards against the system path
// firing when no instructions are set.
func TestStepWithoutInstructionsUsesPlainSendPrompt(t *testing.T) {
	stub := newSystemStubProvider("sysstub", "sys-model")

	runStepWithProvider(t, stub, StepConfig{
		Input:  []string{"NA"},
		Model:  []string{"sys-model"},
		Action: []string{"summarize the input"},
		Output: []string{"STDOUT"},
	})

	systems, _, plains, _ := stub.snapshot()

	if len(systems) != 0 {
		t.Errorf("SendPromptWithSystem called %d time(s) with no instructions set, want 0", len(systems))
	}
	if len(plains) != 1 {
		t.Errorf("SendPrompt call count = %d, want 1", len(plains))
	}
}

// TestStepModelConfigAppliedViaModelConfigurer checks temperature and
// max_output_tokens reach the provider, and that unset fields keep their
// existing values rather than collapsing to zero.
func TestStepModelConfigAppliedViaModelConfigurer(t *testing.T) {
	stub := newSystemStubProvider("sysstub", "sys-model")

	runStepWithProvider(t, stub, StepConfig{
		Input:           []string{"NA"},
		Model:           []string{"sys-model"},
		Action:          []string{"do the thing"},
		Output:          []string{"STDOUT"},
		Temperature:     0.2,
		MaxOutputTokens: 512,
	})

	_, _, _, configs := stub.snapshot()

	if len(configs) != 1 {
		t.Fatalf("SetConfig call count = %d, want 1", len(configs))
	}
	got := configs[0]
	if got.Temperature != 0.2 {
		t.Errorf("Temperature = %v, want 0.2", got.Temperature)
	}
	if got.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d, want 512", got.MaxTokens)
	}
	if got.MaxCompletionTokens != 512 {
		t.Errorf("MaxCompletionTokens = %d, want 512", got.MaxCompletionTokens)
	}
	// TopP was not set on the step, so the provider's existing 1.0 must survive.
	if got.TopP != 1.0 {
		t.Errorf("TopP = %v, want the provider default 1.0 to be preserved", got.TopP)
	}
}

// TestStepModelConfigNotAppliedWhenUnset ensures we do not clobber provider
// defaults on every single step.
func TestStepModelConfigNotAppliedWhenUnset(t *testing.T) {
	stub := newSystemStubProvider("sysstub", "sys-model")

	runStepWithProvider(t, stub, StepConfig{
		Input:  []string{"NA"},
		Model:  []string{"sys-model"},
		Action: []string{"do the thing"},
		Output: []string{"STDOUT"},
	})

	_, _, _, configs := stub.snapshot()
	if len(configs) != 0 {
		t.Errorf("SetConfig called %d time(s) for a step with no generation settings, want 0", len(configs))
	}
}

// TestInstructionsFallBackForPlainProvider covers providers that implement
// neither optional interface: instructions still have to reach the model, and
// the step must not fail.
func TestInstructionsFallBackForPlainProvider(t *testing.T) {
	stub := newPlainStubProvider("plainstub", "plain-model")

	runStepWithProvider(t, stub, StepConfig{
		Input:           []string{"NA"},
		Model:           []string{"plain-model"},
		Action:          []string{"do the thing"},
		Output:          []string{"STDOUT"},
		Instructions:    "You are a terse assistant.",
		Temperature:     0.2,
		MaxOutputTokens: 512,
	})

	prompts := stub.recorded()
	if len(prompts) != 1 {
		t.Fatalf("SendPrompt call count = %d, want 1", len(prompts))
	}
	if !strings.Contains(prompts[0], "You are a terse assistant.") {
		t.Errorf("instructions were dropped for a provider without SystemPrompter: %q", prompts[0])
	}
	if !strings.Contains(prompts[0], "do the thing") {
		t.Errorf("action text missing from prompt: %q", prompts[0])
	}
}
