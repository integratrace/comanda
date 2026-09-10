package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// Compile-time proof that the providers advertised as system-prompt capable
// actually satisfy the optional interfaces.
var (
	_ SystemPrompter  = (*AnthropicProvider)(nil)
	_ SystemPrompter  = (*BedrockProvider)(nil)
	_ ModelConfigurer = (*AnthropicProvider)(nil)
	_ ModelConfigurer = (*BedrockProvider)(nil)
	_ ModelConfigurer = (*OpenAIProvider)(nil)
)

// TestAnthropicRequestOmitsEmptySystem checks the wire format: no system field
// when there is no system prompt, and a top-level one when there is.
func TestAnthropicRequestOmitsEmptySystem(t *testing.T) {
	withoutSystem, err := json.Marshal(anthropicRequest{Model: "m", MaxTokens: 10})
	if err != nil {
		t.Fatalf("marshal without system: %v", err)
	}
	if got := string(withoutSystem); strings.Contains(got, `"system"`) {
		t.Errorf("empty system prompt was serialized: %s", got)
	}

	withSystem, err := json.Marshal(anthropicRequest{Model: "m", MaxTokens: 10, System: "be terse"})
	if err != nil {
		t.Fatalf("marshal with system: %v", err)
	}
	if got := string(withSystem); !strings.Contains(got, `"system":"be terse"`) {
		t.Errorf("system prompt missing from request body: %s", got)
	}
}

// TestSystemContentBlocksNilForEmpty covers the Bedrock Converse mapping.
func TestSystemContentBlocksNilForEmpty(t *testing.T) {
	if blocks := systemContentBlocks(""); blocks != nil {
		t.Errorf("systemContentBlocks(\"\") = %v, want nil so the field is omitted", blocks)
	}
	if blocks := systemContentBlocks("be terse"); len(blocks) != 1 {
		t.Fatalf("systemContentBlocks(%q) returned %d blocks, want 1", "be terse", len(blocks))
	}
}
