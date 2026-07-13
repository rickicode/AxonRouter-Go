package compression

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplaceImageDataURLs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "inline data URL replaced",
			input:    "See this image: " + strings.ReplaceAll("data:foo/png;base64,ABC123==", "foo", "image"),
			expected: "See this image: [image]",
		},
		{
			name:     "HTTPS URL left untouched",
			input:    "See this image: https://example.com/image.png",
			expected: "See this image: https://example.com/image.png",
		},
		{
			name:     "data URL without [image] marker still replaced",
			input:    strings.ReplaceAll("data:foo/jpeg;base64,/9j/4AAQ", "foo", "image"),
			expected: "[image]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceImageDataURLs(tt.input)
			if got != tt.expected {
				t.Errorf("replaceImageDataURLs(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLiteReplaceImageUrls(t *testing.T) {
	urlFrag := strings.ReplaceAll("data:foo/png;base64,ABC123==", "foo", "image")
	input, _ := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": " hello " + urlFrag + " world "},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": urlFrag}},
			},
			},
		},
	})

	got, stats, err := ApplyLite(input, LiteConfig{CollapseWhitespace: true, ReplaceImageUrls: true})
	if err != nil {
		t.Fatalf("ApplyLite error: %v", err)
	}

	var gotM map[string]any
	if err := json.Unmarshal(got, &gotM); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	messages, _ := gotM["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg, _ := messages[0].(map[string]any)
	parts, _ := msg["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(parts))
	}

	textPart, _ := parts[0].(map[string]any)
	if textPart["text"] != "hello [image] world" {
		t.Errorf("expected text part collapsed to %q, got %q", "hello [image] world", textPart["text"])
	}

	imagePart, _ := parts[1].(map[string]any)
	imageURL, _ := imagePart["image_url"].(map[string]any)
	if imageURL["url"] != "[image]" {
		t.Errorf("expected image_url preserved as %q, got %q", "[image]", imageURL["url"])
	}

	if !contains(stats.TechniquesUsed, "collapse_whitespace") {
		t.Error("expected collapse_whitespace technique")
	}
	if !contains(stats.TechniquesUsed, "replace_image_urls") {
		t.Error("expected replace_image_urls technique")
	}
}

func TestHasTools(t *testing.T) {
	if !HasTools([]byte(`{"tools":[{"type":"function"}]}`)) {
		t.Error("expected HasTools=true for non-empty tools")
	}
	if HasTools([]byte(`{"tools":[]}`)) {
		t.Error("expected HasTools=false for empty tools array")
	}
	if HasTools([]byte(`{"model":"gpt-4"}`)) {
		t.Error("expected HasTools=false when tools absent")
	}
}

func TestHasCacheControl(t *testing.T) {
	if !HasCacheControl([]byte(`{"messages":[{"role":"user","content":"hi","cache_control":{"type":"ephemeral"}}]}`)) {
		t.Error("expected HasCacheControl=true for message-level cache_control")
	}
	if !HasCacheControl([]byte(`{"system":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]}`)) {
		t.Error("expected HasCacheControl=true for system-level cache_control")
	}
	if HasCacheControl([]byte(`{"messages":[{"role":"user","content":"hi"}]}`)) {
		t.Error("expected HasCacheControl=false when cache_control absent")
	}
}
