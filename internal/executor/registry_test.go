package executor

import "testing"

func TestRegistry_NewOpenAICompatibleProviders(t *testing.T) {
	RegisterDefaults()
	want := []string{
		"glm", "minimax", "kimi", "mistral",
		"cerebras", "together", "fireworks",
		"novita", "lambda", "pollinations",
		"codebuddy", "nanobanana", "topaz", "puter", "comfyui",
	}
	for _, prefix := range want {
		exec, format, ok := GetRegistry().Get(prefix)
		if !ok {
			t.Errorf("provider %q not registered", prefix)
			continue
		}
		if format != FormatOpenAI {
			t.Errorf("provider %q format = %q, want %q", prefix, format, FormatOpenAI)
		}
		if exec == nil {
			t.Errorf("provider %q has nil executor", prefix)
		}
	}
}

func TestRegistry_OpenAIResponsesProviders(t *testing.T) {
	RegisterDefaults()
	want := []string{"cx", "qwencloud"}
	for _, prefix := range want {
		exec, format, ok := GetRegistry().Get(prefix)
		if !ok {
			t.Errorf("provider %q not registered", prefix)
			continue
		}
		if format != FormatOpenAIResponses {
			t.Errorf("provider %q format = %q, want %q", prefix, format, FormatOpenAIResponses)
		}
		if exec == nil {
			t.Errorf("provider %q has nil executor", prefix)
		}
	}
}

func TestRegistry_GetByModel_NewProviders(t *testing.T) {
	RegisterDefaults()
	cases := []struct {
		model      string
		wantPrefix string
		wantModel  string
		wantFormat ProviderFormat
	}{
		{"glm/glm-4", "glm", "glm-4", FormatOpenAI},
		{"minimax/minimax-m2.5", "minimax", "minimax-m2.5", FormatOpenAI},
		{"kimi/kimi-k2", "kimi", "kimi-k2", FormatOpenAI},
		{"mistral/mistral-large-latest", "mistral", "mistral-large-latest", FormatOpenAI},
		{"cerebras/gpt-oss-120b", "cerebras", "gpt-oss-120b", FormatOpenAI},
		{"codebuddy/glm-5.0", "codebuddy", "glm-5.0", FormatOpenAI},
		{"qwencloud/qwen3.7-plus", "qwencloud", "qwen3.7-plus", FormatOpenAIResponses},
		{"zenmux/openai/gpt-5.6-luna", "zenmux", "openai/gpt-5.6-luna", FormatOpenAI},
		{"zenmux/anthropic/claude-opus-5", "zenmux", "anthropic/claude-opus-5", FormatOpenAI},
		{"zenmux-free/z-ai/glm-5.2", "zenmux-free", "z-ai/glm-5.2", FormatOpenAI},
		{"nanobanana/nanobanana-default", "nanobanana", "nanobanana-default", FormatOpenAI},
		{"topaz/topaz-default", "topaz", "topaz-default", FormatOpenAI},
		{"puter/puter-default", "puter", "puter-default", FormatOpenAI},
		{"comfyui/comfyui-workflow", "comfyui", "comfyui-workflow", FormatOpenAI},
	}
	for _, c := range cases {
		exec, format, model, err := GetRegistry().GetByModel(c.model)
		if err != nil {
			t.Errorf("GetByModel(%q) unexpected error: %v", c.model, err)
			continue
		}
		if exec == nil {
			t.Errorf("GetByModel(%q) returned nil executor", c.model)
		}
		if format != c.wantFormat {
			t.Errorf("GetByModel(%q) format = %q, want %q", c.model, format, c.wantFormat)
		}
		if model != c.wantModel {
			t.Errorf("GetByModel(%q) model = %q, want %q", c.model, model, c.wantModel)
		}
	}
}
