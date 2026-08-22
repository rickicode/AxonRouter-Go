package openai

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiRequestToOpenAI_InlineImageBecomesDataURL(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"ABC123"}}]}]}`)
	out := convertGeminiRequestToOpenAI("gpt-test", body, false)
	if got := gjson.GetBytes(out, "messages.0.content.0.type").String(); got != "image_url" {
		t.Fatalf("content type = %q, want image_url", got)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.image_url.url").String(); got != "data:image/png;base64,ABC123" {
		t.Fatalf("image URL = %q, want data URL", got)
	}
}

func TestConvertGeminiRequestToOpenAI_FileImagePreservesURL(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"fileData":{"mimeType":"image/jpeg","fileUri":"https://example.com/image.jpg"}}]}]}`)
	out := convertGeminiRequestToOpenAI("gpt-test", body, false)
	if got := gjson.GetBytes(out, "messages.0.content.0.image_url.url").String(); got != "https://example.com/image.jpg" {
		t.Fatalf("image URL = %q, want remote URL", got)
	}
}
