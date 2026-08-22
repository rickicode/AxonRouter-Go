package gemini

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToGemini_URLImageUsesFileData(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]}`)
	out := convertClaudeRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.fileData.fileUri").String(); got != "https://example.com/image.png" {
		t.Fatalf("file URI = %q, want remote image URL", got)
	}
}

func TestConvertClaudeRequestToGemini_Base64ImagePreservesData(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/webp","data":"ABC123"}}]}]}`)
	out := convertClaudeRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "image/webp" {
		t.Fatalf("mime type = %q, want image/webp", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "ABC123" {
		t.Fatalf("data = %q, want ABC123", got)
	}
}
