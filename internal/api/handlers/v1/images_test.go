package v1

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// fakeImageGenerator records whether its Images method was called and satisfies
// both the standard Executor interface and the executor.ImageGenerator interface.
type fakeImageGenerator struct {
	*executor.BaseExecutor
	called bool
}

func (f *fakeImageGenerator) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return nil, nil
}

func (f *fakeImageGenerator) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, nil
}

func (f *fakeImageGenerator) Images(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	f.called = true
	return &executor.Response{StatusCode: http.StatusOK, Body: []byte(`{"created":1,"data":[]}`)}, nil
}

func TestImages_CloudflareModelRoutes(t *testing.T) {
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

	fg := &fakeImageGenerator{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("cf", executor.FormatOpenAI, fg)
	defer executor.GetRegistry().Unregister("cf")

	seedProviderAndConnection(t, h, "cf", `["llm","embedding","image"]`, "cf-img-conn", "http://unused")

	body := []byte(`{"model":"cf/black-forest-labs/flux-1-schnell","prompt":"a cat"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Images(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fg.called {
		t.Fatal("expected Images to be called on the executor")
	}
}

func TestImages_CloudflareWithoutImageServiceKindRejected(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fg := &fakeImageGenerator{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("cf", executor.FormatOpenAI, fg)
	defer executor.GetRegistry().Unregister("cf")

	seedProviderAndConnection(t, h, "cf", `["llm","embedding","image"]`, "cf-img-conn", "http://unused")
	// Migration already seeds `cf` with the `image` service kind; strip it to test the gate.
	if _, err := h.db.Exec(`UPDATE provider_types SET service_kinds = '["llm","embedding"]' WHERE id = 'cf'`); err != nil {
		t.Fatalf("update cf service_kinds: %v", err)
	}

	body := []byte(`{"model":"cf/black-forest-labs/flux-1-schnell","prompt":"a cat"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Images(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if fg.called {
		t.Fatal("expected Images not to be called when provider lacks image service kind")
	}
}
