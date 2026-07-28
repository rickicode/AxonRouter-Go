package multiagentv2

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/config"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/codex/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/codex/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
	"github.com/tidwall/gjson"
)

var codexDesktopUA = "Codex Desktop/0.146.0-alpha.3 (Mac OS 26.5.2; arm64)"
var codexTUIUA = "codex-tui/0.145.0 (Mac OS 26.5.2; arm64)"

func TestIsCodexMultiAgentClient(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{"Codex Desktop", codexDesktopUA, true},
		{"codex-tui", codexTUIUA, true},
		{"curl", "curl/8.7.1", false},
		{"embedded token", "proxy Codex Desktop/0.146.0", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCodexMultiAgentClient(tt.ua); got != tt.want {
				t.Fatalf("IsCodexMultiAgentClient(%q) = %v, want %v", tt.ua, got, tt.want)
			}
		})
	}
}

func TestOptimizeRequestDisabledLeavesPayloadUnchanged(t *testing.T) {
	payload := []byte(`{"tools":[{"type":"function","name":"spawn_agent","description":"Spawns an agent."}]}`)
	headers := http.Header{"User-Agent": []string{codexDesktopUA}}
	cfg := &config.Config{}

	got, optimized := OptimizeRequest(context.Background(), headers, payload, cfg)
	if optimized {
		t.Fatal("optimization should be disabled")
	}
	if string(got) != string(payload) {
		t.Fatalf("payload changed when disabled: %s", got)
	}
}

func TestOptimizeRequestRewritesSpawnAgent(t *testing.T) {
	payload := []byte(`{
		"tools":[{
			"type":"namespace",
			"name":"collaboration",
			"tools":[{
				"type":"function",
				"name":"spawn_agent",
				"description":"Spawns an agent.",
				"parameters":{"type":"object","properties":{"message":{"type":"string","encrypted":true}}}
			}]
		}]
	}`)
	headers := http.Header{"User-Agent": []string{codexDesktopUA}}
	cfg := &config.Config{Codex: config.CodexConfig{OptimizeMultiAgentV2: true}}

	got, optimized := OptimizeRequest(context.Background(), headers, payload, cfg)
	if !optimized {
		t.Fatal("expected namespace optimization")
	}
	if ns := gjson.GetBytes(got, "tools.0.name").String(); ns != codexOptimizedCollaborationNamespace {
		t.Fatalf("namespace = %q, want %q", ns, codexOptimizedCollaborationNamespace)
	}
	if encrypted := gjson.GetBytes(got, "tools.0.tools.0.parameters.properties.message.encrypted"); encrypted.Exists() {
		t.Fatal("spawn_agent message encrypted marker was not removed")
	}
	description := gjson.GetBytes(got, "tools.0.tools.0.description").String()
	if !strings.Contains(description, codexSpawnAgentModelsHeading) {
		t.Fatalf("description missing model list: %s", description)
	}
}

func TestOptimizeRequestNormalizesAgentMessageContent(t *testing.T) {
	payload := []byte(`{"input":[{"type":"agent_message","content":[{"type":"input_text","text":"Payload:"},{"type":"encrypted_content","encrypted_content":"delegated task"}]}]}`)
	headers := http.Header{"User-Agent": []string{codexDesktopUA}}
	cfg := &config.Config{Codex: config.CodexConfig{OptimizeMultiAgentV2: true}}

	got, optimized := OptimizeRequest(context.Background(), headers, payload, cfg)
	if optimized {
		t.Fatal("no spawn_agent, so namespace should not be optimized")
	}
	if messageType := gjson.GetBytes(got, "input.0.type").String(); messageType != "agent_message" {
		t.Fatalf("outer type = %q, want agent_message", messageType)
	}
	if text := gjson.GetBytes(got, "input.0.content.1.text").String(); text != "delegated task" {
		t.Fatalf("content text = %q, want delegated task", text)
	}
	if encrypted := gjson.GetBytes(got, "input.0.content.1.encrypted_content"); encrypted.Exists() {
		t.Fatal("encrypted_content was preserved")
	}
}

func TestNormalizeInputConvertsAgentMessageToUserMessage(t *testing.T) {
	payload := []byte(`{"input":[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`)
	headers := http.Header{"User-Agent": []string{codexDesktopUA}}
	cfg := &config.Config{Codex: config.CodexConfig{OptimizeMultiAgentV2: true}}

	got := NormalizeInput(context.Background(), headers, payload, cfg)
	if messageType := gjson.GetBytes(got, "input.0.type").String(); messageType != "message" {
		t.Fatalf("type = %q, want message", messageType)
	}
	if role := gjson.GetBytes(got, "input.0.role").String(); role != "user" {
		t.Fatalf("role = %q, want user", role)
	}
	if text := gjson.GetBytes(got, "input.0.content.0.text").String(); text != "task" {
		t.Fatalf("content text = %q, want task", text)
	}
}

func TestNormalizeInputLeavesPayloadWhenDisabled(t *testing.T) {
	payload := []byte(`{"input":[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`)

	tests := []struct {
		name string
		cfg  *config.Config
		ua   string
	}{
		{"disabled", &config.Config{}, codexDesktopUA},
		{"unrelated client", &config.Config{Codex: config.CodexConfig{OptimizeMultiAgentV2: true}}, "curl/8.7.1"},
		{"nil config", nil, codexDesktopUA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{"User-Agent": []string{tt.ua}}
			got := NormalizeInput(context.Background(), headers, payload, tt.cfg)
			if string(got) != string(payload) {
				t.Fatalf("payload changed unexpectedly: %s", got)
			}
		})
	}
}

func TestTranslateRequestNormalizesForNonResponsesTargets(t *testing.T) {
	payload := []byte(`{"model":"test-model","input":[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`)
	headers := http.Header{"User-Agent": []string{codexDesktopUA}}
	cfg := &config.Config{Codex: config.CodexConfig{OptimizeMultiAgentV2: true}}

	tests := []struct {
		target string
		path   string
	}{
		{string(types.FormatOpenAI), "messages.0.content.0.text"},
		{string(types.FormatClaude), "messages.0.content.0.text"},
		{string(types.FormatGemini), "contents.0.parts.0.text"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := TranslateRequest(context.Background(), headers, cfg, types.FormatCodexResponses, types.Format(tt.target), "test-model", payload, false)
			if value := gjson.GetBytes(got, tt.path).String(); value != "task" {
				t.Fatalf("%s = %q, want task; output=%s", tt.path, value, got)
			}
		})
	}
}

func TestTranslateRequestPreservesAgentMessageForResponsesTarget(t *testing.T) {
	payload := []byte(`{"input":[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`)
	headers := http.Header{"User-Agent": []string{codexDesktopUA}}
	cfg := &config.Config{Codex: config.CodexConfig{OptimizeMultiAgentV2: true}}

	got := TranslateRequest(context.Background(), headers, cfg, types.FormatCodexResponses, types.FormatCodexResponses, "test-model", payload, false)
	if messageType := gjson.GetBytes(got, "input.0.type").String(); messageType != "agent_message" {
		t.Fatalf("type = %q, want agent_message for same-format passthrough", messageType)
	}
}

func TestRestoreResponseRewritesNamespace(t *testing.T) {
	payload := []byte(`{
		"type":"response.completed",
		"response":{
			"output":[
				{"type":"function_call","name":"spawn_agent","namespace":"collaboration-optimize"},
				{"type":"function_call","name":"collaboration-optimize__send_message"}
			],
			"tools":[{"type":"namespace","name":"collaboration-optimize"}]
		}
	}`)

	got := RestoreResponse(payload, true)
	if ns := gjson.GetBytes(got, "response.output.0.namespace").String(); ns != codexCollaborationNamespace {
		t.Fatalf("function namespace = %q, want collaboration", ns)
	}
	if name := gjson.GetBytes(got, "response.output.1.name").String(); name != "collaboration__send_message" {
		t.Fatalf("qualified name = %q, want collaboration__send_message", name)
	}
	if name := gjson.GetBytes(got, "response.tools.0.name").String(); name != codexCollaborationNamespace {
		t.Fatalf("namespace tool name = %q, want collaboration", name)
	}

	unchanged := RestoreResponse(payload, false)
	if string(unchanged) != string(payload) {
		t.Fatalf("RestoreResponse with optimized=false changed payload: %s", unchanged)
	}
}

func TestCodexSpawnAgentModelsFromSources(t *testing.T) {
	catalog := []byte(`{"models":[
		{"slug":"model-template","display_name":"Template","description":"Template model.","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}],"service_tiers":[{"id":"priority"}],"priority":1},
		{"slug":"gpt-5.5","display_name":"Default","description":"Default model.","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"}],"service_tiers":[{"id":"priority"}],"priority":2}
	]}`)
	available := []map[string]any{
		{"id": "custom-model", "display_name": "Custom", "description": "Catalog description."},
		{"id": "model-template"},
	}
	lookup := func(modelID string) *modelInfo {
		if modelID != "custom-model" {
			return nil
		}
		return &modelInfo{Description: "Dynamic model.", Thinking: &thinkingSupport{Levels: []string{"none", "low", "medium", "high"}}}
	}

	models := codexSpawnAgentModelsFromSources(available, catalog, lookup)
	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2", len(models))
	}
	if models[0].id != "model-template" || models[0].description != "Template model." || models[0].defaultReasoningEffort != "low" {
		t.Fatalf("template model = %+v", models[0])
	}
	custom := models[1]
	if custom.id != "custom-model" || custom.description != "Dynamic model." {
		t.Fatalf("custom model = %+v", custom)
	}
	if got := strings.Join(custom.reasoningEfforts, ","); got != "none,low,medium,high" {
		t.Fatalf("custom reasoning efforts = %q", got)
	}
	if custom.defaultReasoningEffort != "medium" {
		t.Fatalf("custom default reasoning effort = %q, want medium", custom.defaultReasoningEffort)
	}
}
