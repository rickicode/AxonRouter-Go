package openai

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertResponsesRequestToOpenAI(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Hi"}]}
		],
		"instructions": "Be helpful",
		"max_output_tokens": 100,
		"temperature": 0.5,
		"top_p": 0.9
	}`)

	out := convertResponsesRequestToOpenAI("gpt-4o", body, false)

	if got := gjson.GetBytes(out, "model").String(); got != "gpt-4o" {
		t.Errorf("model=%q, want gpt-4o", got)
	}
	if got := gjson.GetBytes(out, "stream").Bool(); got {
		t.Errorf("stream=%v, want false", got)
	}
	if got := gjson.GetBytes(out, "messages.#").Int(); got != 3 {
		t.Errorf("messages length=%d, want 3", got)
	}
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "system" {
		t.Errorf("first message role=%q, want system", got)
	}
	if got := gjson.GetBytes(out, "messages.1.role").String(); got != "user" {
		t.Errorf("second message role=%q, want user", got)
	}
	if got := gjson.GetBytes(out, "messages.2.role").String(); got != "assistant" {
		t.Errorf("third message role=%q, want assistant", got)
	}
	if got := gjson.GetBytes(out, "max_tokens").Int(); got != 100 {
		t.Errorf("max_tokens=%d, want 100", got)
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.5 {
		t.Errorf("temperature=%v, want 0.5", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.9 {
		t.Errorf("top_p=%v, want 0.9", got)
	}
}

func TestConvertResponsesRequestToOpenAI_StringInput(t *testing.T) {
	body := []byte(`{"input":"Say hello"}`)
	out := convertResponsesRequestToOpenAI("gpt-4o", body, true)

	if got := gjson.GetBytes(out, "stream").Bool(); !got {
		t.Errorf("stream=%v, want true", got)
	}
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "user" {
		t.Errorf("role=%q, want user", got)
	}
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != "Say hello" {
		t.Errorf("content=%q, want Say hello", got)
	}
}

func TestConvertResponsesRequestToOpenAI_Tools(t *testing.T) {
	body := []byte(`{
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "weather", "parameters": {"type":"object"}}}],
		"tool_choice": "auto"
	}`)
	out := convertResponsesRequestToOpenAI("gpt-4o", body, false)

	if got := gjson.GetBytes(out, "tools.0.function.name").String(); got != "get_weather" {
		t.Errorf("tool name=%q, want get_weather", got)
	}
	if got := gjson.GetBytes(out, "tool_choice").String(); got != "auto" {
		t.Errorf("tool_choice=%q, want auto", got)
	}
}
