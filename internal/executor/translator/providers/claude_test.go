package providers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTranslateClaude_ContextLength(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 210000 tokens (max: 200000)"}}`)
	got := TranslateClaude(http.StatusBadRequest, body)
	assertCode(t, got, "context_length_exceeded")
	assertType(t, got, "invalid_request_error")
	assertMessage(t, got, "prompt is too long")
}

func TestTranslateClaude_Auth(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	got := TranslateClaude(http.StatusUnauthorized, body)
	assertCode(t, got, "invalid_api_key")
	assertType(t, got, "authentication_error")
}

func TestTranslateClaude_RateLimit(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	got := TranslateClaude(http.StatusTooManyRequests, body)
	assertCode(t, got, "rate_limit_exceeded")
	assertType(t, got, "rate_limit_error")
}

func assertCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := out["error"].(map[string]any)["code"]; got != want {
		t.Errorf("code=%v, want %v", got, want)
	}
}

func assertType(t *testing.T, body []byte, want string) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := out["error"].(map[string]any)["type"]; got != want {
		t.Errorf("type=%v, want %v", got, want)
	}
}

func assertMessage(t *testing.T, body []byte, want string) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	msg := out["error"].(map[string]any)["message"].(string)
	if msg == "" || !strings.Contains(msg, want) {
		t.Errorf("message=%q, want containing %q", msg, want)
	}
}
