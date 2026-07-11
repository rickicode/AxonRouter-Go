package executor

import (
	"encoding/json"
	"net/http"
	"testing"
)

func init() {
	RegisterDefaults()
}

func TestUpstreamError_TranslateClaude(t *testing.T) {
	raw := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens (max: 200000)"}}`)
	upErr := &UpstreamError{
		StatusCode: http.StatusBadRequest,
		Body:       raw,
		RawBody:    raw,
	}
	upErr.TranslateErrorBody("claude")

	var out map[string]any
	if err := json.Unmarshal(upErr.Body, &out); err != nil {
		t.Fatalf("invalid body: %v", err)
	}
	if got := out["error"].(map[string]any)["code"]; got != "context_length_exceeded" {
		t.Errorf("code=%v, want context_length_exceeded", got)
	}
}

func TestUpstreamError_PassthroughWhenNoTranslator(t *testing.T) {
	raw := []byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
	upErr := &UpstreamError{
		StatusCode: http.StatusTooManyRequests,
		Body:       raw,
		RawBody:    raw,
	}
	upErr.TranslateErrorBody("openai")

	if string(upErr.Body) != string(raw) {
		t.Errorf("body was modified without translator: %s", string(upErr.Body))
	}
}
