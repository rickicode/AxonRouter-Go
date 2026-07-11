package providers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTranslateGemini_InvalidArgument(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"Request payload size exceeds the limit: 20971520 bytes.","status":"INVALID_ARGUMENT"}}`)
	got := TranslateGemini(http.StatusBadRequest, body)
	assertGeminiCode(t, got, "bad_request")
	assertGeminiType(t, got, "invalid_request_error")
	assertGeminiMessage(t, got, "Request payload size")
}

func TestTranslateGemini_ContextLength(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"The maximum context length was exceeded.","status":"INVALID_ARGUMENT"}}`)
	got := TranslateGemini(http.StatusBadRequest, body)
	assertGeminiCode(t, got, "context_length_exceeded")
}

func TestTranslateGemini_RateLimit(t *testing.T) {
	body := []byte(`{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`)
	got := TranslateGemini(http.StatusTooManyRequests, body)
	assertGeminiCode(t, got, "rate_limit_exceeded")
	assertGeminiType(t, got, "rate_limit_error")
}

func TestTranslateGemini_Unauthenticated(t *testing.T) {
	body := []byte(`{"error":{"code":401,"message":"API key not valid","status":"UNAUTHENTICATED"}}`)
	got := TranslateGemini(http.StatusUnauthorized, body)
	assertGeminiCode(t, got, "invalid_api_key")
	assertGeminiType(t, got, "authentication_error")
}

func assertGeminiCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := out["error"].(map[string]any)["code"]; got != want {
		t.Errorf("code=%v, want %v", got, want)
	}
}

func assertGeminiType(t *testing.T, body []byte, want string) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := out["error"].(map[string]any)["type"]; got != want {
		t.Errorf("type=%v, want %v", got, want)
	}
}

func assertGeminiMessage(t *testing.T, body []byte, want string) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	msg := out["error"].(map[string]any)["message"].(string)
	if !strings.Contains(msg, want) {
		t.Errorf("message=%q, want containing %q", msg, want)
	}
}
