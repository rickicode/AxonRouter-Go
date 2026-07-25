package common

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSSEEventData(t *testing.T) {
	event := SSEEventData("response.completed", []byte(`{"done":true}`))
	want := "event: response.completed\ndata: {\"done\":true}"
	if string(event) != want {
		t.Errorf("SSEEventData = %q, want %q", event, want)
	}
}

func TestAppendSSEEventString(t *testing.T) {
	out := AppendSSEEventString(nil, "response.output_text.delta", `{"delta":"hi"}`, 2)
	want := "event: response.output_text.delta\ndata: {\"delta\":\"hi\"}\n\n"
	if string(out) != want {
		t.Errorf("AppendSSEEventString = %q, want %q", out, want)
	}
}

func TestAppendSSEEventBytes(t *testing.T) {
	out := AppendSSEEventBytes(nil, "response.output_text.delta", []byte(`{"delta":"hi"}`), 2)
	want := "event: response.output_text.delta\ndata: {\"delta\":\"hi\"}\n\n"
	if string(out) != want {
		t.Errorf("AppendSSEEventBytes = %q, want %q", out, want)
	}
}

func TestJoinRawArray(t *testing.T) {
	items := [][]byte{[]byte(`"a"`), []byte(`"b"`)}
	out := JoinRawArray(items)
	if string(out) != `["a","b"]` {
		t.Errorf("JoinRawArray = %q", out)
	}
	if string(JoinRawArray(nil)) != "[]" {
		t.Errorf("JoinRawArray(nil) = %q", JoinRawArray(nil))
	}
}

func TestSetRawArrayItems(t *testing.T) {
	data := []byte(`{"items":[]}`)
	got := SetRawArrayItems(data, "items", [][]byte{[]byte(`{"id":"1"}`)})
	if !bytes.Equal(got, []byte(`{"items":[{"id":"1"}]}`)) {
		t.Errorf("SetRawArrayItems = %q", got)
	}
}

func TestAttachCacheControl(t *testing.T) {
	src := gjson.ParseBytes([]byte(`{"name":"tool","cache_control":{"type":"ephemeral"}}`))
	dst := []byte(`{"name":"tool"}`)
	got := AttachCacheControl(dst, src)
	if gjson.GetBytes(got, "cache_control.type").String() != "ephemeral" {
		t.Errorf("AttachCacheControl did not attach cache_control: %s", got)
	}

	// Missing cache_control leaves dst unchanged.
	srcNoCC := gjson.ParseBytes([]byte(`{"name":"tool"}`))
	got2 := AttachCacheControl(dst, srcNoCC)
	if gjson.GetBytes(got2, "cache_control").Exists() {
		t.Errorf("AttachCacheControl should not attach missing cache_control")
	}
}

func TestAttachMessageCacheControl_ArrayContent(t *testing.T) {
	msg := []byte(`{"role":"user","content":[{"type":"text","text":"hello"}]}`)
	src := gjson.ParseBytes([]byte(`{"cache_control":{"type":"ephemeral"}}`))
	got := AttachMessageCacheControl(msg, src)
	if gjson.GetBytes(got, "content.0.cache_control.type").String() != "ephemeral" {
		t.Errorf("AttachMessageCacheControl did not attach to last content block: %s", got)
	}

	// Existing part-level cache_control wins.
	msgWithCC := []byte(`{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`)
	got2 := AttachMessageCacheControl(msgWithCC, src)
	if gjson.GetBytes(got2, "content.0.cache_control.ttl").String() != "1h" {
		t.Errorf("AttachMessageCacheControl should preserve existing cache_control: %s", got2)
	}
}

func TestAttachMessageCacheControl_StringContent(t *testing.T) {
	msg := []byte(`{"role":"user","content":"hello"}`)
	src := gjson.ParseBytes([]byte(`{"cache_control":{"type":"ephemeral"}}`))
	got := AttachMessageCacheControl(msg, src)
	if gjson.GetBytes(got, "content.0.type").String() != "text" {
		t.Errorf("AttachMessageCacheControl did not promote string content to array: %s", got)
	}
	if gjson.GetBytes(got, "content.0.text").String() != "hello" {
		t.Errorf("AttachMessageCacheControl lost text: %s", got)
	}
	if gjson.GetBytes(got, "content.0.cache_control.type").String() != "ephemeral" {
		t.Errorf("AttachMessageCacheControl did not attach cache_control: %s", got)
	}
}

func TestAttachMessageCacheControl_NoCacheControl(t *testing.T) {
	msg := []byte(`{"role":"user","content":"hello"}`)
	src := gjson.ParseBytes([]byte(`{}`))
	got := AttachMessageCacheControl(msg, src)
	if !bytes.Equal(got, msg) {
		t.Errorf("AttachMessageCacheControl changed msg when no cache_control: %s", got)
	}
}

func TestGeminiTokenCountJSON(t *testing.T) {
	out := GeminiTokenCountJSON(42)
	if gjson.GetBytes(out, "totalTokens").Int() != 42 {
		t.Errorf("GeminiTokenCountJSON totalTokens = %s", out)
	}
	if gjson.GetBytes(out, "promptTokensDetails.0.tokenCount").Int() != 42 {
		t.Errorf("GeminiTokenCountJSON tokenCount = %s", out)
	}
}

func TestClaudeInputTokensJSON(t *testing.T) {
	out := ClaudeInputTokensJSON(7)
	if gjson.GetBytes(out, "input_tokens").Int() != 7 {
		t.Errorf("ClaudeInputTokensJSON = %s", out)
	}
}

func TestNewRawArrayItems(t *testing.T) {
	if NewRawArrayItems(0) != nil {
		t.Error("NewRawArrayItems(0) should return nil")
	}
	items := NewRawArrayItems(3)
	if cap(items) != 3 {
		t.Errorf("NewRawArrayItems cap = %d, want 3", cap(items))
	}
}
