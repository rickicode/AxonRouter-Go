package v1

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
)

func TestStreamOptions_InjectsWhenAllConditionsMet(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/chat/completions")
	if !gjson.GetBytes(result, "stream_options.include_usage").Bool() {
		t.Error("expected stream_options.include_usage to be true")
	}
	if gjson.GetBytes(result, "model").String() != "gpt-4" {
		t.Error("expected model to remain unchanged")
	}
}

func TestStreamOptions_DoesNotInjectWhenNonStreaming(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":false,"stream_options":{"include_usage":true}}`)
	result := sanitizeStreamOptions(body, false, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/chat/completions")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be removed for non-streaming request")
	}
}

func TestStreamOptions_DoesNotInjectWhenClientNotOpenAI(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatClaude, executor.FormatOpenAI, "/v1/chat/completions")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be removed for non-OpenAI client")
	}
}

func TestStreamOptions_DoesNotInjectWhenProviderNotOpenAI(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatClaude, "/v1/chat/completions")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be removed for non-OpenAI provider")
	}
}

func TestStreamOptions_DoesNotInjectWhenPathNotChatCompletions(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/messages")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be removed for non-chat path")
	}
}

func TestStreamOptions_StripsExistingStreamOptionsWhenConditionsNotMet(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":true,"stream_options":{"include_usage":true}}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatClaude, "/v1/chat/completions")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be stripped")
	}
}

func TestStreamOptions_PreservesBodyWhenNoStreamOptionsAndConditionsNotMet(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatClaude, executor.FormatClaude, "/v1/messages")
	if string(result) != string(body) {
		t.Error("expected body to be unchanged when no stream_options and conditions not met")
	}
}

func TestStreamOptions_HandlesPathSuffix(t *testing.T) {
	// Path should match on suffix "/chat/completions"
	body := []byte(`{"model":"gpt-4","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/chat/completions")
	if !gjson.GetBytes(result, "stream_options.include_usage").Bool() {
		t.Error("expected stream_options.include_usage to be true for path ending with /chat/completions")
	}
}

func TestStreamOptions_ResponsesAPINoStreamOptions(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":true,"stream_options":{"include_usage":true}}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAIResponses, executor.FormatOpenAIResponses, "/v1/responses")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be stripped for Responses API")
	}
}

func TestStreamOptions_StripsStreamOptionsForNonStreamingOpenAIChat(t *testing.T) {
	body := []byte(`{"model":"gpt-4","stream":false,"stream_options":{"include_usage":true}}`)
	result := sanitizeStreamOptions(body, false, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/chat/completions")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options to be removed for non-streaming")
	}
}

func TestStreamOptions_PreservesInjectForStreamingResponsesAPINotMatch(t *testing.T) {
	// For Responses API, should NOT inject even if streaming
	body := []byte(`{"model":"gpt-4","stream":true}`)
	result := sanitizeStreamOptions(body, true, executor.FormatOpenAIResponses, executor.FormatOpenAI, "/v1/responses")
	if gjson.GetBytes(result, "stream_options").Exists() {
		t.Error("expected stream_options not to exist for Responses API")
	}
}
