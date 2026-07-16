package providers

import (
	"encoding/json"
	"testing"
)

func TestTranslateCloudflareError_ContextLength(t *testing.T) {
	raw := `{"errors":[{"message":"AiError: AiError: {\"object\":\"error\",\"message\":\"Requested token count exceeds the model's maximum context length of 262144 tokens. You requested a total of 263925 tokens: 199925 tokens from the input messages and 64000 tokens for the completion. Please reduce the number of tokens in the input messages or the completion to fit within the limit.\",\"type\":\"BadRequestError\",\"param\":null,\"code\":400} (82a04848-3949-4645-83f0-94050e19f74b)","code":8007}],"success":false,"result":{},"messages":[]}`

	got := TranslateCloudflare(400, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", out["error"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("type=%v, want invalid_request_error", errObj["type"])
	}
	if errObj["code"] != "context_length_exceeded" {
		t.Errorf("code=%v, want context_length_exceeded", errObj["code"])
	}
	if errObj["param"] != nil {
		t.Errorf("param=%v, want nil", errObj["param"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Error("message is empty")
	}
}

func TestTranslateCloudflareError_ModelNotFound(t *testing.T) {
	raw := `{"errors":[{"message":"AiError: {\"object\":\"error\",\"message\":\"model not found\",\"type\":\"NotFoundError\",\"param\":null,\"code\":404}","code":7001}],"success":false,"result":{},"messages":[]}`

	got := TranslateCloudflare(404, []byte(raw))
	if got == nil {
		t.Fatal("expected translated error, got nil")
	}

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid body: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != "model_not_found" {
		t.Errorf("code=%v, want model_not_found", errObj["code"])
	}
	if errObj["type"] != "not_found_error" {
		t.Errorf("type=%v, want not_found_error", errObj["type"])
	}
}

func TestTranslateCloudflareError_PlainContextWindowLimit(t *testing.T) {
	raw := `{"errors":[{"message":"AiError: Ai: The estimated number of input and maximum output tokens (263008) exceeded this model context window limit (262144). (c0a7213e-becc-4e3a-b935-eca2f2391732)","code":5021}],"success":false,"result":{},"messages":[]}`

	got := TranslateCloudflare(413, []byte(raw))
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

func TestTranslateCloudflareError_NonCloudflare(t *testing.T) {
	if got := TranslateCloudflare(400, []byte(`{"error":"plain"}`)); got != nil {
		t.Errorf("expected nil for non-CF body, got %s", string(got))
	}
}
