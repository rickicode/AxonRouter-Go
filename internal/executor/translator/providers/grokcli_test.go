package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranslateGrokCLI_403PermissionDenied_DoubleEncoded(t *testing.T) {
	// Real-world Grok 403 where error.message is itself a JSON string that
	// contains both "permission-denied" and "insufficient_quota". The
	// translator must classify this as a permission/auth error, not quota.
	raw := `{"error":{"message":"{\"code\":\"permission-denied\",\"error\":\"Access to the chat endpoint is denied. Please ensure you're using the correct credentials. If you believe this is a mistake, please log into console.x.ai and update the permissions, or contact support.\",\"type\":\"permission_error\",\"code\":\"insufficient_quota\"}","type":"permission_error","code":"insufficient_quota"}}`

	got := TranslateGrokCLI(403, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)

	if errObj["type"] != "permission_error" {
		t.Errorf("type=%v, want permission_error", errObj["type"])
	}
	if errObj["code"] != "permission_error" {
		t.Errorf("code=%v, want permission_error", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Error("message is empty")
	}
	if !strings.Contains(msg, "Access to the chat endpoint is denied") {
		t.Errorf("message did not preserve upstream text: %q", msg)
	}
}

func TestTranslateGrokCLI_402SpendingLimit(t *testing.T) {
	raw := `{"error":{"message":"Your account has reached its spending limit.","type":"billing_error","code":"spending_limit"}}`

	got := TranslateGrokCLI(402, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

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
}

func TestTranslateGrokCLI_429RateLimit(t *testing.T) {
	raw := `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error","code":"rate_limit_exceeded"}}`

	got := TranslateGrokCLI(429, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

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

func TestTranslateGrokCLI_401Unauthorized(t *testing.T) {
	raw := `{"error":{"message":"Invalid authentication","type":"authentication_error","code":"invalid_api_key"}}`

	got := TranslateGrokCLI(401, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

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

func TestTranslateGrokCLI_ContextLength(t *testing.T) {
	raw := `{"error":{"message":"This model's maximum context length is 8192 tokens, however you requested 10000 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`

	got := TranslateGrokCLI(400, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

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


