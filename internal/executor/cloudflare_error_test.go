package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestTranslateCloudflareError_ContextLength(t *testing.T) {
	raw := `{"errors":[{"message":"AiError: AiError: {\"object\":\"error\",\"message\":\"Requested token count exceeds the model's maximum context length of 262144 tokens. You requested a total of 263925 tokens: 199925 tokens from the input messages and 64000 tokens for the completion. Please reduce the number of tokens in the input messages or the completion to fit within the limit.\",\"type\":\"BadRequestError\",\"param\":null,\"code\":400} (82a04848-3949-4645-83f0-94050e19f74b)","code":8007}],"success":false,"result":{},"messages":[]}`

	got := translateCloudflareError(400, []byte(raw))
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

func TestToCloudflareUpstreamError_Stream(t *testing.T) {
	cfBody := `{"errors":[{"message":"AiError: AiError: {\"object\":\"error\",\"message\":\"Requested token count exceeds the model's maximum context length of 262144 tokens.\",\"type\":\"BadRequestError\",\"param\":null,\"code\":400} (uuid)","code":8007}],"success":false,"result":{},"messages":[]}`
	rawErr := fmt.Errorf("stream error 400: %s", cfBody)

	ue := toCloudflareUpstreamError(rawErr)
	if ue == nil {
		t.Fatal("expected UpstreamError, got nil")
	}
	if ue.StatusCode != 400 {
		t.Errorf("status=%d, want 400", ue.StatusCode)
	}
	if ue.Error() == "" {
		t.Error("UpstreamError.Error() is empty")
	}

	var out map[string]any
	if err := json.Unmarshal(ue.Body, &out); err != nil {
		t.Fatalf("invalid body json: %v", err)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != "context_length_exceeded" {
		t.Errorf("code=%v, want context_length_exceeded", errObj["code"])
	}
}

func TestToCloudflareUpstreamError_NonCloudflare(t *testing.T) {
	rawErr := errors.New("stream error 502: upstream gateway timeout")
	if ue := toCloudflareUpstreamError(rawErr); ue != nil {
		t.Errorf("expected nil, got %v", ue)
	}
}

func TestToCloudflareUpstreamError_OpenAIErrorPrefix(t *testing.T) {
	cfBody := `{"errors":[{"message":"AiError: {\"object\":\"error\",\"message\":\"model not found\",\"type\":\"NotFoundError\",\"param\":null,\"code\":404}","code":7001}],"success":false,"result":{},"messages":[]}`
	rawErr := fmt.Errorf("openai error 404: %s", cfBody)

	ue := toCloudflareUpstreamError(rawErr)
	if ue == nil {
		t.Fatal("expected UpstreamError, got nil")
	}
	if ue.StatusCode != 404 {
		t.Errorf("status=%d, want 404", ue.StatusCode)
	}

	var out map[string]any
	if err := json.Unmarshal(ue.Body, &out); err != nil {
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
