package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/rickicode/AxonRouter-Go/internal/executor/translator/providers"
)

func TestDoStreamRequest_TranslatesUpstreamError(t *testing.T) {
	translator.Register("bedrock", translator.Func(providers.TranslateOpenAICompatible))

	orig := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = orig }()

	upstreamBody := `{"error":{"code":"validation_error","message":"This model's maximum context length is 202752 tokens. However, you requested too many.","type":"invalid_request_error"}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(upstreamBody))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	base.FetchTimeout = 2 * time.Second
	_, err := base.DoStreamRequestWithConfig(ContextWithProvider(context.Background(), "bedrock"), "POST", ts.URL, nil, nil, nil)
	if err == nil {
		t.Fatal("expected upstream error")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected *UpstreamError, got %T", err)
	}
	if string(upErr.RawBody) != upstreamBody {
		t.Fatalf("RawBody should be upstream body, got %s", string(upErr.RawBody))
	}

	var parsed translator.OpenAIError
	if err := json.Unmarshal(upErr.Body, &parsed); err != nil {
		t.Fatalf("translated body is not valid JSON: %v\n%s", err, string(upErr.Body))
	}
	if parsed.Error.Code != "context_length_exceeded" {
		t.Errorf("code = %q, want context_length_exceeded", parsed.Error.Code)
	}
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", parsed.Error.Type)
	}
}
