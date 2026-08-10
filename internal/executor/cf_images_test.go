package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func init() {
	// Tests use httptest which binds to localhost; override validation.
	validateURL = func(string) error { return nil }
}

func TestCloudflareImageGenerator_RoutesToNativeRunEndpoint(t *testing.T) {
	var calledPath string
	var receivedBody []byte
	var auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		auth = r.Header.Get("Authorization")
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":{"image":"aW1hZ2VkYXRh","name":"test.png"},"success":true}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	gen := NewCloudflareImageGenerator(base)
	req := &Request{
		Model:                "@cf/black-forest-labs/flux-1-schnell",
		APIKey:               "test-token",
		BaseURL:              ts.URL + "/accounts/{accountId}",
		Provider:             "cf",
		ProviderSpecificData: map[string]string{"accountId": "acc123"},
		Body:                 mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat"}),
	}
	resp, err := gen.Images(context.Background(), req)
	if err != nil {
		t.Fatalf("Images error: %v", err)
	}
	if calledPath != "/accounts/acc123/ai/run/@cf/black-forest-labs/flux-1-schnell" {
		t.Fatalf("expected native CF run path, got %s", calledPath)
	}
	if auth != "Bearer test-token" {
		t.Fatalf("expected bearer token, got %s", auth)
	}
	var upstream map[string]any
	if err := json.Unmarshal(receivedBody, &upstream); err != nil {
		t.Fatalf("unmarshal upstream body: %v", err)
	}
	if upstream["prompt"] != "a cat" {
		t.Fatalf("expected prompt 'a cat', got %v", upstream["prompt"])
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if !gjson.GetBytes(resp.Body, "data.0.url").Exists() {
		t.Fatalf("expected data.0.url in response, got %s", string(resp.Body))
	}
	if !strings.HasPrefix(gjson.GetBytes(resp.Body, "data.0.url").String(), "data:image/png;base64,") {
		t.Fatalf("expected data URL prefix, got %s", gjson.GetBytes(resp.Body, "data.0.url").String())
	}
}

func TestCloudflareImageGenerator_B64ResponseFormat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":{"image":"aW1hZ2VkYXRh","name":"test.png"},"success":true}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	gen := NewCloudflareImageGenerator(base)
	req := &Request{
		Model:                "@cf/black-forest-labs/flux-1-schnell",
		APIKey:               "test-token",
		BaseURL:              ts.URL + "/accounts/{accountId}",
		Provider:             "cf",
		ProviderSpecificData: map[string]string{"accountId": "acc123"},
		Body:                 mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat", "response_format": "b64_json"}),
	}
	resp, err := gen.Images(context.Background(), req)
	if err != nil {
		t.Fatalf("Images error: %v", err)
	}
	b64 := gjson.GetBytes(resp.Body, "data.0.b64_json").String()
	if b64 != "aW1hZ2VkYXRh" {
		t.Fatalf("expected b64 payload, got %q", b64)
	}
}

func TestCloudflareImageGenerator_FallsBackToEnvAccountID(t *testing.T) {
	os.Setenv("CLOUDFLARE_ACCOUNT_ID", "env-acc")
	defer os.Unsetenv("CLOUDFLARE_ACCOUNT_ID")

	var calledPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":{"image":"aW1hZ2VkYXRh","name":"test.png"},"success":true}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	gen := NewCloudflareImageGenerator(base)
	req := &Request{
		Model:    "cf/black-forest-labs/flux-1-schnell",
		APIKey:   "test-token",
		BaseURL:  ts.URL + "/accounts/{accountId}",
		Provider: "cf",
		Body:     mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat"}),
	}
	_, err := gen.Images(context.Background(), req)
	if err != nil {
		t.Fatalf("Images error: %v", err)
	}
	if !strings.Contains(calledPath, "/env-acc/") {
		t.Fatalf("expected env account id in path, got %s", calledPath)
	}
}

func TestCloudflareImageGenerator_RequiresPrompt(t *testing.T) {
	base := NewBaseExecutor()
	gen := NewCloudflareImageGenerator(base)
	req := &Request{
		Model:                "@cf/black-forest-labs/flux-1-schnell",
		APIKey:               "test-token",
		Provider:             "cf",
		ProviderSpecificData: map[string]string{"accountId": "acc123"},
		Body:                 mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell"}),
	}
	_, err := gen.Images(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestCloudflareImageGenerator_RequiresAccountID(t *testing.T) {
	os.Unsetenv("CLOUDFLARE_ACCOUNT_ID")
	base := NewBaseExecutor()
	gen := NewCloudflareImageGenerator(base)
	req := &Request{
		Model:    "@cf/black-forest-labs/flux-1-schnell",
		APIKey:   "test-token",
		Provider: "cf",
		Body:     mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat"}),
	}
	_, err := gen.Images(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing account id")
	}
}

func TestCloudflareImageGenerator_TranslatesUpstreamErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"errors":[{"message":"prompt too long","code":400}]}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	gen := NewCloudflareImageGenerator(base)
	req := &Request{
		Model:                "@cf/black-forest-labs/flux-1-schnell",
		APIKey:               "test-token",
		BaseURL:              ts.URL + "/accounts/{accountId}",
		Provider:             "cf",
		ProviderSpecificData: map[string]string{"accountId": "acc123"},
		Body:                 mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat"}),
	}
	_, err := gen.Images(context.Background(), req)
	if err == nil {
		t.Fatal("expected upstream error")
	}
	var upErr *UpstreamError
	if !errors.As(err, &upErr) {
		t.Fatalf("expected *UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, upErr.StatusCode)
	}
}

func TestCloudflareImageGenerator_CfExecutorImagesInterface(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":{"image":"aW1hZ2VkYXRh","name":"test.png"},"success":true}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cf := NewCloudflareExecutor(NewOpenAIExecutor(base))
	req := &Request{
		Model:                "cf/black-forest-labs/flux-1-schnell",
		APIKey:               "test-token",
		BaseURL:              ts.URL + "/accounts/{accountId}",
		Provider:             "cf",
		ProviderSpecificData: map[string]string{"accountId": "acc123"},
		Body:                 mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat"}),
	}
	resp, err := cf.Images(context.Background(), req)
	if err != nil {
		t.Fatalf("CloudflareExecutor.Images error: %v", err)
	}
	if !gjson.GetBytes(resp.Body, "data").Exists() {
		t.Fatalf("expected data in response, got %s", string(resp.Body))
	}
}
