package v1

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// fakeVideoRouteExecutor records whether its Execute method was called.
type fakeVideoRouteExecutor struct {
	*executor.BaseExecutor
	called bool
	body   []byte
}

func (f *fakeVideoRouteExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	f.called = true
	body := f.body
	if body == nil {
		body = []byte(`{"video":{"id":"vid_123","url":"http://example.com/video.mp4"}}`)
	}
	return &executor.Response{StatusCode: http.StatusOK, Body: body}, nil
}

func (f *fakeVideoRouteExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, nil
}

func TestVideosCreate_DefaultModel(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fv := &fakeVideoRouteExecutor{BaseExecutor: executor.NewBaseExecutor()}
	h.videoExecutorFactory = func() executor.Executor { return fv }

	seedProviderAndConnection(t, h, "xai", `["llm","video"]`, "xai-video-conn", "http://unused")

	body := []byte(`{"prompt":"a cat walking"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VideosCreate(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fv.called {
		t.Fatal("expected video executor to be called")
	}
}

func TestVideosGenerations_RequiresModelOrDefaults(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fv := &fakeVideoRouteExecutor{BaseExecutor: executor.NewBaseExecutor()}
	h.videoExecutorFactory = func() executor.Executor { return fv }

	seedProviderAndConnection(t, h, "xai", `["llm","video"]`, "xai-video-gen-conn", "http://unused")

	body := []byte(`{"prompt":"a dog running"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.VideosGenerations(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOpenAIVideosCreate_MapsSoraToXAI(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fv := &fakeVideoRouteExecutor{BaseExecutor: executor.NewBaseExecutor()}
	h.videoExecutorFactory = func() executor.Executor { return fv }

	seedProviderAndConnection(t, h, "xai", `["llm","video"]`, "xai-video-openai-conn", "http://unused")

	body := []byte(`{"model":"sora-2","prompt":"a robot dancing"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.OpenAIVideosCreate(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
