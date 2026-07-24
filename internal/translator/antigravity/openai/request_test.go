package openai

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToAntigravity_ObfuscatesUserText(t *testing.T) {
	// Isolate config.Get so tests do not touch the real data directory.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AXONROUTER_DIR", "")

	input := []byte(`{
		"model": "gemini-2.5-pro",
		"messages": [
			{"role": "system", "content": "Talk about Cursor"},
			{"role": "user", "content": "I use Cursor IDE"},
			{"role": "assistant", "content": "I use Cursor IDE"}
		]
	}`)

	out := convertOpenAIRequestToAntigravity("gemini-2.5-pro", input, false)

	userText := gjson.GetBytes(out, "request.contents.0.parts.0.text").String()
	if !strings.Contains(userText, "\u200d") {
		t.Errorf("expected user text to be obfuscated, got %q", userText)
	}
	if userText == "I use Cursor IDE" {
		t.Errorf("user text was left unobfuscated")
	}

	sysText := gjson.GetBytes(out, "request.systemInstruction.parts.0.text").String()
	if strings.Contains(sysText, "\u200d") {
		t.Errorf("expected system instruction to stay unobfuscated, got %q", sysText)
	}

	assistantText := gjson.GetBytes(out, "request.contents.1.parts.0.text").String()
	if strings.Contains(assistantText, "\u200d") {
		t.Errorf("expected assistant text to stay unobfuscated, got %q", assistantText)
	}
}

func TestConvertOpenAIRequestToAntigravity_ObfuscatesUserContentArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AXONROUTER_DIR", "")

	input := []byte(`{
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "opencode is nice"},
				{"type": "text", "text": "plain text"}
			]}
		]
	}`)

	out := convertOpenAIRequestToAntigravity("gemini-2.5-pro", input, false)

	text0 := gjson.GetBytes(out, "request.contents.0.parts.0.text").String()
	if !strings.Contains(text0, "\u200d") {
		t.Errorf("expected first user text part to be obfuscated, got %q", text0)
	}

	text1 := gjson.GetBytes(out, "request.contents.0.parts.1.text").String()
	if strings.Contains(text1, "\u200d") {
		t.Errorf("expected second user text part to stay unobfuscated, got %q", text1)
	}
}
