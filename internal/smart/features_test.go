package smart

import (
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/models"
)

func TestExtractFeatures_TextOnly(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello world this is a test"}]}`)
	f := ExtractFeatures(body)
	if f.NumMessages != 1 {
		t.Errorf("NumMessages = %d, want 1", f.NumMessages)
	}
	if f.HasImage || f.HasAudio || f.HasVideo || f.HasPDF {
		t.Error("expected no special capabilities")
	}
	if f.Score <= 0 {
		t.Errorf("expected positive score, got %v", f.Score)
	}
}

func TestExtractFeatures_Image(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)
	f := ExtractFeatures(body)
	if !f.HasImage {
		t.Error("expected HasImage=true")
	}
	if f.Score < 0.1 {
		t.Errorf("image score too low: %v", f.Score)
	}
}

func TestExtractFeatures_Audio(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"x","format":"mp3"}}]}]}`)
	f := ExtractFeatures(body)
	if !f.HasAudio {
		t.Error("expected HasAudio=true")
	}
}

func TestExtractFeatures_Video(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"inlineData":{"mimeType":"video/mp4","data":"abc"}}]}]}`)
	f := ExtractFeatures(body)
	if !f.HasVideo {
		t.Error("expected HasVideo=true")
	}
}

func TestExtractFeatures_Tools(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","function":{"name":"get_weather"}}],"messages":[{"role":"assistant","tool_calls":[{"id":"1","function":{"name":"get_weather"}}]},{"role":"tool","tool_call_id":"1","content":"ok"}]}`)
	f := ExtractFeatures(body)
	if f.ToolCount != 1 {
		t.Errorf("ToolCount = %d, want 1", f.ToolCount)
	}
	if f.ToolCallCount != 1 {
		t.Errorf("ToolCallCount = %d, want 1", f.ToolCallCount)
	}
}

func TestExtractFeatures_Reasoning(t *testing.T) {
	body := []byte(`{"reasoning_effort":"high","messages":[{"role":"user","content":"think hard"}]}`)
	f := ExtractFeatures(body)
	if f.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want high", f.ReasoningEffort)
	}
}

func TestExtractFeatures_CodeHints(t *testing.T) {
	codeBlock := strings.Join([]string{"```go", "package main", "```"}, "\n")
	bodyStr := `{"messages":[{"role":"user","content":"look at this code\n` + codeBlock + `\n"}]}`
	body := []byte(bodyStr)
	f := ExtractFeatures(body)
	if !f.HasCodeHint {
		t.Error("expected HasCodeHint=true")
	}
	if f.LanguageHint != "go" {
		t.Errorf("LanguageHint = %q, want go", f.LanguageHint)
	}
}

func TestExtractFeatures_TotalTokensEstimation(t *testing.T) {
	body := []byte(`{"max_tokens":2000,"messages":[{"role":"user","content":"` + strings.Repeat("a", 8000) + `"}]}`)
	f := ExtractFeatures(body)
	if f.TotalTokens < 0 {
		t.Errorf("TotalTokens = %d, want non-negative", f.TotalTokens)
	}
}

func TestExtractCapabilityFeatures(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"file","file":{"filename":"doc.pdf","mime_type":"application/pdf"}}]}]}`)
	caps := ExtractCapabilityFeatures(body)
	if !caps.PDF {
		t.Error("expected PDF=true")
	}
}

func TestIsVirtualModel(t *testing.T) {
	cases := []struct {
		m    string
		want bool
	}{
		{"smart/auto", true},
		{"smart/auto-fast", true},
		{"smart/auto-quality", true},
		{"smart/economy", false},
		{"openai/gpt-4o", false},
	}
	for _, c := range cases {
		if got := IsVirtualModel(c.m); got != c.want {
			t.Errorf("IsVirtualModel(%q) = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestVirtualModelRegistry(t *testing.T) {
	r := NewRegistry(nil)
	vm, ok := r.Get(ModelAuto)
	if !ok || vm == nil {
		t.Fatal("expected default virtual model")
	}
	if !vm.Enabled {
		t.Error("expected default enabled")
	}
	list := r.List()
	if len(list) != 3 {
		t.Errorf("expected 3 defaults, got %d", len(list))
	}
}

func TestVirtualModelRegistry_UpsertRoundTrip(t *testing.T) {
	r := NewRegistry(nil)
	updated, err := r.Upsert(&VirtualModel{
		ID:         ModelAuto,
		Candidates: []string{"openai/gpt-4o"},
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(updated.Candidates) != 1 || updated.Enabled {
		t.Errorf("unexpected state: %+v", updated)
	}
	current, _ := r.Get(ModelAuto)
	if current.Enabled {
		t.Error("expected disabled after upsert")
	}
}

func TestCapabilityMatches(t *testing.T) {
	req := models.ModelCapabilities{Vision: true}
	caps := models.ModelCapabilities{Vision: true}
	if !capabilityMatches(caps, req) {
		t.Error("expected match")
	}
	req2 := models.ModelCapabilities{Tools: true}
	if capabilityMatches(models.ModelCapabilities{}, req2) {
		t.Error("expected no match")
	}
}

func TestSplitProviderModel(t *testing.T) {
	p, m, ok := splitProviderModel("openai/gpt-4o")
	if !ok || p != "openai" || m != "gpt-4o" {
		t.Errorf("got %q/%q/%v", p, m, ok)
	}
	_, _, ok2 := splitProviderModel("gpt-4o")
	if ok2 {
		t.Error("expected no split")
	}
}
