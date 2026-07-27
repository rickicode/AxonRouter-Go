package executor

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
)

func init() {
	logging.Init("text")
	// Tests use httptest which binds to localhost; override validation.
	validateURL = func(string) error { return nil }
}

func cfTestServer(t *testing.T, handler http.HandlerFunc) (cf *CloudflareExecutor, ts *httptest.Server) {
	ts = httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	t.Setenv("CLOUDFLARE_BASE_URL", ts.URL)
	base := NewBaseExecutor()
	cf = NewCloudflareExecutor(NewOpenAIExecutor(base))
	return
}

func TestCloudflareExecutor_Classification(t *testing.T) {
	var calledPath string
	var receivedBody []byte
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":[{"label":"POSITIVE","score":0.98}]}`)
	})

	req := &Request{
		Model:                "cf/huggingface/distilbert-sst-2-int8",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId":"test-account"},
		Body: mustJSON(map[string]any{
			"model": "cf/huggingface/distilbert-sst-2-int8",
			"text":  "I love this product",
		}),
	}
	resp, err := cf.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if calledPath != "/client/v4/accounts/test-account/ai/run/@cf/huggingface/distilbert-sst-2-int8" {
		t.Fatalf("expected native classification path, got %s", calledPath)
	}
	if gjson.GetBytes(receivedBody, "text").String() != "I love this product" {
		t.Fatalf("expected text forwarded, got %s", string(receivedBody))
	}
	if gjson.GetBytes(resp.Body, "object").String() != "classification" {
		t.Fatalf("expected object=classification, got %s", resp.Body)
	}
	if gjson.GetBytes(resp.Body, "results.0.label").String() != "POSITIVE" {
		t.Fatalf("expected POSITIVE label, got %s", resp.Body)
	}
	if gjson.GetBytes(resp.Body, "results.0.score").Float() != 0.98 {
		t.Fatalf("expected score 0.98, got %s", resp.Body)
	}
}

func TestCloudflareExecutor_Rerank(t *testing.T) {
	var calledPath string
	var receivedBody []byte
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":[{"index":1,"score":0.95},{"index":0,"score":0.12}]}`)
	})

	req := &Request{
		Model:                "cf/baai/bge-reranker-base",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId":"test-account"},
		Body: mustJSON(map[string]any{
			"model":    "cf/baai/bge-reranker-base",
			"query":    "hello world",
			"contexts": []string{"foo", "bar"},
		}),
	}
	resp, err := cf.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if calledPath != "/client/v4/accounts/test-account/ai/run/@cf/baai/bge-reranker-base" {
		t.Fatalf("expected native rerank path, got %s", calledPath)
	}
	if !gjson.GetBytes(receivedBody, "query").Exists() {
		t.Fatalf("expected query in upstream body, got %s", receivedBody)
	}
	if len(gjson.GetBytes(receivedBody, "contexts").Array()) != 2 {
		t.Fatalf("expected 2 contexts, got %s", receivedBody)
	}
	if gjson.GetBytes(resp.Body, "object").String() != "list" {
		t.Fatalf("expected object=list, got %s", resp.Body)
	}
	d := gjson.GetBytes(resp.Body, "data")
	if !d.IsArray() || len(d.Array()) != 2 {
		t.Fatalf("expected 2 rerank results, got %s", resp.Body)
	}
	first := d.Array()[0].Map()
	if first["index"].Int() != 1 {
		t.Fatalf("expected first result index 1, got %v", first["index"].Value())
	}
	if first["text"].String() != "bar" {
		t.Fatalf("expected first result text 'bar', got %v", first["text"].Value())
	}
	if first["score"].Float() != 0.95 {
		t.Fatalf("expected first score 0.95, got %v", first["score"].Value())
	}
}

func TestCloudflareExecutor_ImageClassification(t *testing.T) {
	var calledPath string
	var receivedBody []byte
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":[{"label":"cat","score":0.91,"box":{"x1":1,"y1":2,"x2":3,"y2":4}}]}`)
	})

	imagePayload := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("fake-image"))
	req := &Request{
		Model:                "cf/microsoft/resnet-50",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId":"test-account"},
		Body: mustJSON(map[string]any{
			"model": "cf/microsoft/resnet-50",
			"image": imagePayload,
		}),
	}
	resp, err := cf.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if calledPath != "/client/v4/accounts/test-account/ai/run/@cf/microsoft/resnet-50" {
		t.Fatalf("expected native image-classification path, got %s", calledPath)
	}
	upstreamImage := gjson.GetBytes(receivedBody, "image").String()
	if !strings.Contains(upstreamImage, "ZmFrZS1pbWFnZQ") {
		t.Fatalf("expected image forwarded, got %s", receivedBody)
	}
	if gjson.GetBytes(resp.Body, "object").String() != "list" {
		t.Fatalf("expected object=list, got %s", resp.Body)
	}
	if gjson.GetBytes(resp.Body, "data.0.label").String() != "cat" {
		t.Fatalf("expected cat label, got %s", resp.Body)
	}
	if gjson.GetBytes(resp.Body, "data.0.box.x1").Int() != 1 {
		t.Fatalf("expected box x1=1, got %s", resp.Body)
	}
}

func TestCloudflareExecutor_ImageClassification_URL(t *testing.T) {
	var calledPath string
	var receivedBody []byte
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"result":[{"label":"dog","score":0.87}]}`)
	})

	req := &Request{
		Model:                "cf/facebook/detr-resnet-50",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId":"test-account"},
		Body: mustJSON(map[string]any{
			"model":     "cf/facebook/detr-resnet-50",
			"image_url": "https://example.com/pic.jpg",
		}),
	}
	resp, err := cf.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if calledPath != "/client/v4/accounts/test-account/ai/run/@cf/facebook/detr-resnet-50" {
		t.Fatalf("expected native object-detection path, got %s", calledPath)
	}
	if gjson.GetBytes(receivedBody, "image").String() != "https://example.com/pic.jpg" {
		t.Fatalf("expected image URL forwarded, got %s", receivedBody)
	}
	if gjson.GetBytes(resp.Body, "data.0.label").String() != "dog" {
		t.Fatalf("expected dog label, got %s", resp.Body)
	}
}

func TestCloudflareExecutor_Classification_MissingText(t *testing.T) {
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := &Request{
		Model:                "cf/huggingface/distilbert-sst-2-int8",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId":"test-account"},
		Body: mustJSON(map[string]any{
			"model": "cf/huggingface/distilbert-sst-2-int8",
		}),
	}
	_, err := cf.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing text")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok || upErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 upstream error, got %v", err)
	}
}

func TestCloudflareExecutor_Rerank_MissingContexts(t *testing.T) {
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := &Request{
		Model:                "cf/baai/bge-reranker-base",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId":"test-account"},
		Body: mustJSON(map[string]any{
			"model": "cf/baai/bge-reranker-base",
			"query": "hello",
		}),
	}
	_, err := cf.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing contexts")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok || upErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 upstream error, got %v", err)
	}
}

func TestCloudflareExecutor_NativeRun_MissingAccountID(t *testing.T) {
	os.Unsetenv("CLOUDFLARE_ACCOUNT_ID")
	cf, _ := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := &Request{
		Model:                "cf/huggingface/distilbert-sst-2-int8",
		BaseURL:              "unused",
		ProviderSpecificData: map[string]string{},
		Body: mustJSON(map[string]any{
			"model": "cf/huggingface/distilbert-sst-2-int8",
			"text":  "ok",
		}),
	}
	_, err := cf.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing account id")
	}
}

func TestCloudflareExecutor_StandardChatNotIntercepted(t *testing.T) {
	var calledPath string
	cf, ts := cfTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	})
	// Override base URL back to test server for the standard chat path too.
	t.Setenv("CLOUDFLARE_BASE_URL", ts.URL)

	req := &Request{
		Model:                "cf/meta/llama-3.2-1b-instruct",
		BaseURL:              ts.URL + "/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId": "test-account"},
		Body: mustJSON(map[string]any{
			"model":    "cf/meta/llama-3.2-1b-instruct",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		}),
	}
	_, err := cf.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if calledPath != "/v1/chat/completions" {
		t.Fatalf("expected standard chat path, got %s", calledPath)
	}
}
