package translator

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
	"github.com/tidwall/gjson"
)

// registrySanityTest exercises the side-effect registrations performed by
// init.go. It makes the global translator registry deterministic by asserting
// the exact (clientFormat, providerFormat) mappings that should exist after all
// translator packages are imported.
func TestRegistrySanity(t *testing.T) {
	expectRequest := []struct {
		from types.Format
		to   types.Format
	}{
		{types.FormatOpenAI, types.FormatClaude},
		{types.FormatOpenAI, types.FormatGemini},
		{types.FormatOpenAI, types.FormatCodexResponses},
		{types.FormatOpenAI, types.FormatAntigravity},
		{types.FormatOpenAI, types.FormatKiro},
		{types.FormatOpenAI, types.FormatGrokCLI},

		{types.FormatClaude, types.FormatOpenAI},
		{types.FormatClaude, types.FormatGemini},
		{types.FormatClaude, types.FormatAntigravity},
		{types.FormatClaude, types.FormatKiro},

		{types.FormatGemini, types.FormatOpenAI},
		{types.FormatGemini, types.FormatClaude},

		{types.FormatCodexResponses, types.FormatClaude},
		{types.FormatCodexResponses, types.FormatGemini},

		{types.FormatAntigravity, types.FormatOpenAI},
		{types.FormatAntigravity, types.FormatGemini},
	}

	expectResponse := []struct {
		from types.Format
		to   types.Format
	}{
		{types.FormatOpenAI, types.FormatOpenAI},
		{types.FormatOpenAI, types.FormatClaude},
		{types.FormatOpenAI, types.FormatGemini},
		{types.FormatOpenAI, types.FormatCodexResponses},
		{types.FormatOpenAI, types.FormatAntigravity},
		{types.FormatOpenAI, types.FormatKiro},
		{types.FormatOpenAI, types.FormatGrokCLI},

		{types.FormatClaude, types.FormatOpenAI},
		{types.FormatClaude, types.FormatGemini},
		{types.FormatClaude, types.FormatCodexResponses},

		{types.FormatGemini, types.FormatOpenAI},
		{types.FormatGemini, types.FormatClaude},
		{types.FormatGemini, types.FormatCodexResponses},

		{types.FormatCodexResponses, types.FormatOpenAI},
		{types.FormatCodexResponses, types.FormatClaude},
		{types.FormatCodexResponses, types.FormatGemini},

		{types.FormatAntigravity, types.FormatOpenAI},
		{types.FormatAntigravity, types.FormatClaude},
		{types.FormatAntigravity, types.FormatGemini},

		{types.FormatKiro, types.FormatClaude},
		{types.FormatGrokCLI, types.FormatOpenAI},
	}

	r := registry.Default()
	for _, p := range expectRequest {
		if !r.HasRequestTransformer(p.from, p.to) {
			t.Errorf("expected request transformer %s -> %s to be registered", p.from, p.to)
		}
	}
	for _, p := range expectResponse {
		if !r.HasResponseTransformer(p.from, p.to) {
			t.Errorf("expected response transformer %s -> %s to be registered", p.from, p.to)
		}
	}
}

// TestCodexPathDoesNotLeakRejectedFields ensures that the (openai,
// openai-responses) request transform registered by init.go uses the Codex-safe
// implementation from openai/codex_responses + codex/responses, not the generic
// openai/openai_responses translator that would forward fields rejected by the
// Codex upstream.
func TestCodexPathDoesNotLeakRejectedFields(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"hi"}],"max_tokens":100,"temperature":0.5}`)
	out := registry.Request(
		string(types.FormatOpenAI),
		string(types.FormatCodexResponses),
		"cx/gpt-5.4",
		req,
		false,
	)
	for _, field := range []string{"max_tokens", "temperature"} {
		if gjson.GetBytes(out, field).Exists() {
			t.Errorf("Codex-bound request leaked rejected field %q", field)
		}
	}
}
