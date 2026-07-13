package providers

import (
	"encoding/json"
	"testing"
)

func TestTranslateCodexUsageLimitReached(t *testing.T) {
	raw := []byte(`{"error":{"type":"usage_limit_reached","message":"Usage limit reached","resets_at":1893456000,"resets_in_seconds":3600}}`)
	got := TranslateCodex(429, raw)
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("type=%v, want rate_limit_error", errObj["type"])
	}
	if errObj["code"] != "insufficient_quota" {
		t.Errorf("code=%v, want insufficient_quota", errObj["code"])
	}
	if retryAfter, ok := errObj["retry_after"].(float64); !ok || retryAfter <= 0 {
		t.Errorf("retry_after=%v, want positive number", errObj["retry_after"])
	}
}

func TestTranslateCodexModelAtCapacity(t *testing.T) {
	raw := []byte(`{"error":{"type":"server_error","message":"The selected model is at capacity, please retry later"}}`)
	got := TranslateCodex(503, raw)
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("type=%v, want rate_limit_error", errObj["type"])
	}
	if errObj["code"] != "rate_limit_exceeded" {
		t.Errorf("code=%v, want rate_limit_exceeded", errObj["code"])
	}
}

func TestTranslateCodexContextLength(t *testing.T) {
	raw := []byte(`{"error":{"type":"invalid_request_error","message":"context length exceeded"}}`)
	got := TranslateCodex(400, raw)
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("type=%v, want invalid_request_error", errObj["type"])
	}
	if errObj["code"] != "context_length_exceeded" {
		t.Errorf("code=%v, want context_length_exceeded", errObj["code"])
	}
}

func TestTranslateCodexAuthFailed(t *testing.T) {
	raw := []byte(`{"error":{"type":"authentication_error","message":"invalid or expired token"}}`)
	got := TranslateCodex(401, raw)
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["type"] != "authentication_error" {
		t.Errorf("type=%v, want authentication_error", errObj["type"])
	}
	if errObj["code"] != "invalid_api_key" {
		t.Errorf("code=%v, want invalid_api_key", errObj["code"])
	}
}

func TestTranslateCodexInvalidResponse(t *testing.T) {
	raw := []byte(`{"error":{"type":"invalid_request_error","message":"previous_response_not_found"}}`)
	got := TranslateCodex(400, raw)
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != "bad_request" {
		t.Errorf("code=%v, want bad_request", errObj["code"])
	}
}
