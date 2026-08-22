package gemini

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToGemini_DataURLStripsPrefix(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,ABC123"}}]}]}`)
	out := convertOpenAIRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "image/png" {
		t.Fatalf("mime type = %q, want image/png", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "ABC123" {
		t.Fatalf("data = %q, want raw base64 without data URL prefix", got)
	}
}

func TestConvertOpenAIRequestToGemini_RemoteURLUsesFileData(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`)
	out := convertOpenAIRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.fileData.fileUri").String(); got != "https://example.com/image.png" {
		t.Fatalf("file URI = %q, want remote image URL", got)
	}
	if gjson.GetBytes(out, "contents.0.parts.0.inlineData").Exists() {
		t.Fatal("remote URL must not be sent as inline base64 data")
	}
}
