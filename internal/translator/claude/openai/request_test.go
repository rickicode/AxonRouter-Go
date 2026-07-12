package openai

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestToolResultEmittedAsRoleTool(t *testing.T) {
	body := []byte(`{
		"model": "openai/gpt-4o-mini",
		"max_tokens": 200,
		"messages": [
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "42"},
				{"type": "text", "text": "what is 42?"}
			]}
		]
	}`)
	out := ConvertClaudeRequestToOpenAI("gpt-4o-mini", body, false)
	root := gjson.ParseBytes(out)

	var sawToolRole, sawToolResultInUser bool
	root.Get("messages").ForEach(func(_, m gjson.Result) bool {
		role := m.Get("role").String()
		if role == "tool" {
			sawToolRole = true
			if m.Get("tool_call_id").String() != "toolu_1" {
				t.Errorf("tool_call_id = %q, want toolu_1", m.Get("tool_call_id").String())
			}
			if m.Get("content").String() != "42" {
				t.Errorf("tool content = %q, want 42", m.Get("content").String())
			}
		}
		if role == "user" {
			m.Get("content").ForEach(func(_, p gjson.Result) bool {
				if p.Get("type").String() == "tool_result" {
					sawToolResultInUser = true
				}
				return true
			})
		}
		return true
	})
	if !sawToolRole {
		t.Error("expected a standalone role:tool message, got none")
	}
	if sawToolResultInUser {
		t.Error("tool_result part must not appear inside the user message")
	}
}

func TestToolChoiceAnyAndObject(t *testing.T) {
	anyBody := []byte(`{"model":"m","messages":[],"tool_choice":"any"}`)
	out := ConvertClaudeRequestToOpenAI("m", anyBody, false)
	if v := gjson.ParseBytes(out).Get("tool_choice").String(); v != "required" {
		t.Errorf("tool_choice any -> %q, want required", v)
	}

	objBody := []byte(`{"model":"m","messages":[],"tool_choice":{"type":"tool","name":"calc"}}`)
	out = ConvertClaudeRequestToOpenAI("m", objBody, false)
	tc := gjson.ParseBytes(out).Get("tool_choice")
	if tc.Get("type").String() != "function" {
		t.Errorf("tool_choice object type = %q, want function", tc.Get("type").String())
	}
	if tc.Get("function.name").String() != "calc" {
		t.Errorf("tool_choice function.name = %q, want calc", tc.Get("function.name").String())
	}
}

func TestArraySystemJoined(t *testing.T) {
	body := []byte(`{
		"model":"m","messages":[],
		"system":[{"type":"text","text":"You are a bot"},{"type":"text","text":"Be concise"}]
	}`)
	out := ConvertClaudeRequestToOpenAI("m", body, false)
	sys := gjson.ParseBytes(out).Get("messages.0")
	if sys.Get("role").String() != "system" {
		t.Fatalf("first message role = %q, want system", sys.Get("role").String())
	}
	if got := sys.Get("content").String(); got != "You are a bot\nBe concise" {
		t.Errorf("system content = %q, want joined text", got)
	}
}

func TestBase64Image(t *testing.T) {
	body := []byte(`{
		"model":"m","messages":[{"role":"user","content":[
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"ABCDEF"}}
		]}]
	}`)
	out := ConvertClaudeRequestToOpenAI("m", body, false)
	url := gjson.ParseBytes(out).Get("messages.0.content.0.image_url.url").String()
	want := "data:image/png;base64,ABCDEF"
	if url != want {
		t.Errorf("image url = %q, want %q", url, want)
	}
}

func TestThinkingEnabledMapsToReasoningHigh(t *testing.T) {
	body := []byte(`{"model":"m","messages":[],"thinking":{"type":"enabled"}}`)
	out := ConvertClaudeRequestToOpenAI("m", body, false)
	if v := gjson.ParseBytes(out).Get("reasoning_effort").String(); v != "high" {
		t.Errorf("reasoning_effort = %q, want high", v)
	}

	disabled := []byte(`{"model":"m","messages":[],"thinking":{"type":"disabled"}}`)
	out = ConvertClaudeRequestToOpenAI("m", disabled, false)
	if v := gjson.ParseBytes(out).Get("reasoning_effort").String(); v != "none" {
		t.Errorf("reasoning_effort (disabled) = %q, want none", v)
	}
}

func TestToolUseBecomesToolCalls(t *testing.T) {
	body := []byte(`{
		"model":"m","messages":[{"role":"assistant","content":[
			{"type":"tool_use","id":"toolu_x","name":"calc","input":{"a":1}}
		]}]
	}`)
	out := ConvertClaudeRequestToOpenAI("m", body, false)
	root := gjson.ParseBytes(out)
	m := root.Get("messages.0")
	if m.Get("role").String() != "assistant" {
		t.Fatalf("role = %q, want assistant", m.Get("role").String())
	}
	tc := m.Get("tool_calls.0")
	if tc.Get("id").String() != "toolu_x" {
		t.Errorf("tool_calls.0.id = %q, want toolu_x", tc.Get("id").String())
	}
	if tc.Get("type").String() != "function" {
		t.Errorf("tool_calls.0.type = %q, want function", tc.Get("type").String())
	}
	if tc.Get("function.name").String() != "calc" {
		t.Errorf("tool_calls.0.function.name = %q, want calc", tc.Get("function.name").String())
	}
}

func TestAssistantThinkingPartDropped(t *testing.T) {
	body := []byte(`{
		"model":"m","messages":[{"role":"assistant","content":[
			{"type":"thinking","thinking":"hmm"},
			{"type":"text","text":"ok"}
		]}]
	}`)
	out := ConvertClaudeRequestToOpenAI("m", body, false)
	root := gjson.ParseBytes(out)
	// content should be the text only (no thinking mapped)
	if root.Get("messages.0.content.0.type").String() != "text" {
		t.Errorf("expected text part, got %q", root.Get("messages.0.content.0.type").String())
	}
}
