package providers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTranslateAntigravity_UsesGeminiEnvelope(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"The maximum context length was exceeded.","status":"INVALID_ARGUMENT"}}`)
	got := TranslateAntigravity(http.StatusBadRequest, body)

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != "context_length_exceeded" {
		t.Errorf("code=%v, want context_length_exceeded", errObj["code"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("type=%v, want invalid_request_error", errObj["type"])
	}
	msg := errObj["message"].(string)
	if !strings.Contains(msg, "context length") {
		t.Errorf("message=%q, want containing 'context length'", msg)
	}
}
