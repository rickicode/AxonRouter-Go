package smart

import (
	"testing"
)

func TestExtractFeatures_TextOnly(t *testing.T) {
	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"Hello world"}]}`)
	fv := ExtractFeatures(body)

	if fv.TotalTokens == 0 {
		t.Error("expected non-zero token estimate")
	}
	if fv.MessageCount != 2 {
		t.Errorf("message count = %d, want 2", fv.MessageCount)
	}
	if fv.HasImages || fv.HasAudio || fv.HasVideo || fv.HasPDF {
		t.Error("plain text request should not trigger media flags")
	}
}

func TestExtractFeatures_ToolsAndReasoning(t *testing.T) {
	body := []byte(`{
		"model": "smart/auto",
		"tools": [{"type": "function"}, {"type": "function"}],
		"reasoning_effort": "high",
		"messages": [{"role": "user", "content": "think deeply"}]
	}`)
	fv := ExtractFeatures(body)

	if fv.ToolCount != 2 {
		t.Errorf("tool count = %d, want 2", fv.ToolCount)
	}
	if !fv.Reasoning {
		t.Error("expected reasoning flag set")
	}
	if fv.ReasoningEffort != "high" {
		t.Errorf("reasoning effort = %q, want high", fv.ReasoningEffort)
	}
}

func TestExtractFeatures_ImagePart(t *testing.T) {
	body := []byte(`{
		"model": "smart/auto",
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "describe this"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,abc"}}
			]
		}]
	}`)
	fv := ExtractFeatures(body)

	if !fv.HasImages {
		t.Error("expected image flag set")
	}
}

func TestExtractFeatures_CodeHint(t *testing.T) {
	body := []byte("{\"messages\":[{\"role\":\"user\",\"content\":\"Fix this:\\n```go\\nfmt.Println(\\\"hi\\\")\\n```\"}]}")
	fv := ExtractFeatures(body)

	if !fv.CodeHint {
		t.Error("expected code hint flag set")
	}
	if fv.CodeLanguage != "go" {
		t.Errorf("code language = %q, want go", fv.CodeLanguage)
	}
}

func TestExtractFeatures_GeminiPDF(t *testing.T) {
	body := []byte(`{
		"contents": [{
			"parts": [{
				"fileData": {"mimeType": "application/pdf", "fileUri": "gs://bucket/doc.pdf"}
			}]
		}]
	}`)
	fv := ExtractFeatures(body)

	if !fv.HasPDF {
		t.Error("expected PDF flag set for Gemini fileData")
	}
}
