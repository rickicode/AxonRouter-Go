package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// fakeEmbeddingsExecutor records whether its Embeddings method was called and
// satisfies both the standard Executor interface and the EmbeddingsExecutor
// interface used by the /v1/embeddings handler.
type fakeEmbeddingsExecutor struct {
	*executor.BaseExecutor
	called bool
}

func (f *fakeEmbeddingsExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return nil, errors.New("execute should not be called directly for embeddings")
}

func (f *fakeEmbeddingsExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, errors.New("streaming not supported")
}

func (f *fakeEmbeddingsExecutor) Embeddings(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	f.called = true
	return &executor.Response{StatusCode: http.StatusOK, Body: []byte(`{"object":"list","data":[]}`)}, nil
}

func seedProviderAndConnection(t *testing.T, h *Handler, provider, serviceKinds, connID, baseURL string) {
	t.Helper()
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, service_kinds, created_at) VALUES (?, ?, 'openai', ?, ?, 0)`, provider, provider, baseURL, serviceKinds); err != nil {
		t.Fatalf("seed provider_type %q: %v", provider, err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?, ?, 'c1', 'none', 'ready', 1, 0, 0)`, connID, provider); err != nil {
		t.Fatalf("seed connection %q: %v", connID, err)
	}
	h.store.SeedConnection(connID, provider, "ready", 0)
	h.elig.RecomputeAll()
}

func TestEmbeddings_CloudflareSupportedModelRoutes(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()

	fe := &fakeEmbeddingsExecutor{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("cf", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("cf")

	seedProviderAndConnection(t, h, "cf", `["llm","embedding","image"]`, "cf-emb-conn", "http://unused")

	body := []byte(`{"model":"cf/baai/bge-base-en-v1.5","input":"hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Embeddings(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fe.called {
		t.Fatal("expected Embeddings to be called on the executor")
	}
}

func TestEmbeddings_CloudflareUnsupportedModelRejected(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fe := &fakeEmbeddingsExecutor{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("cf", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("cf")

	seedProviderAndConnection(t, h, "cf", `["llm","embedding","image"]`, "cf-emb-conn", "http://unused")

	body := []byte(`{"model":"cf/vendor/unsupported-model","input":"hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Embeddings(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	msg := got["error"].(map[string]any)["message"].(string)
	if msg == "" {
		t.Fatalf("expected error message, got %v", got)
	}
	if fe.called {
		t.Fatal("expected Embeddings not to be called for unsupported model")
	}
}

func TestEmbeddings_OpenAIRoutesWithoutModalityRegistry(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()

	fe := &fakeEmbeddingsExecutor{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm","embedding"]`, "openai-emb-conn", "http://unused")

	body := []byte(`{"model":"openai/text-embedding-3-small","input":"hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Embeddings(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fe.called {
		t.Fatal("expected Embeddings to be called for OpenAI")
	}
}

func TestEmbeddings_ProviderWithoutEmbeddingServiceKindRejected(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fe := &fakeEmbeddingsExecutor{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("nope", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("nope")

	seedProviderAndConnection(t, h, "nope", `["llm"]`, "nope-emb-conn", "http://unused")

	body := []byte(`{"model":"nope/model","input":"hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Embeddings(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if fe.called {
		t.Fatal("expected Embeddings not to be called when provider lacks embedding service kind")
	}
}
