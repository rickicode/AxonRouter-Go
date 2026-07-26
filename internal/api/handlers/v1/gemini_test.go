package v1

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/tidwall/gjson"
)

func setupGeminiTest(t *testing.T) (*Handler, func()) {
	logging.Init("text")
	executor.RegisterDefaults()
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	connID := "gemini-conn1"
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?,'gemini','c1','apikey','ready',1,0,0)`, connID); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection(connID, "gemini", "ready", 0)
	h.elig.RecomputeAll()

	return h, func() {}
}

// fakeGeminiExecutor implements executor.Executor and executor.TokenCounter for tests.
type fakeGeminiExecutor struct {
	fakeExecutor
	countTokensResp *executor.Response
	countTokensErr  error
	lastCountTokens *executor.Request
}

func (f *fakeGeminiExecutor) CountTokens(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	f.lastCountTokens = req
	return f.countTokensResp, f.countTokensErr
}

func registerFakeGemini(t *testing.T, h *Handler, exec executor.Executor) func() {
	t.Helper()
	reg := executor.GetRegistry()
	origExec, origFormat, hadOriginal := reg.Get("gemini")
	reg.Register("gemini", executor.FormatGemini, exec)
	return func() {
		if hadOriginal {
			reg.Register("gemini", origFormat, origExec)
		} else {
			reg.Unregister("gemini")
		}
	}
}

func TestGeminiModels_ReturnsGeminiCatalog(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	rec, c := jsonRequestWithAllowedModels(t, http.MethodGet, "/v1beta/models", nil, nil)
	h.GeminiModels(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !gjson.GetBytes(rec.Body.Bytes(), "models").IsArray() {
		t.Fatalf("expected models array, got %s", rec.Body.String())
	}
	found := false
	for _, m := range gjson.GetBytes(rec.Body.Bytes(), "models").Array() {
		if m.Get("name").String() == "models/gemini-2.5-pro" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected model models/gemini-2.5-pro in %s", rec.Body.String())
	}
}

func TestGeminiGetHandler_ReturnsSingleModel(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	rec, c := jsonRequestWithAllowedModels(t, http.MethodGet, "/v1beta/models/gemini-2.5-pro", nil, nil)
	c.Params = gin.Params{{Key: "action", Value: "/gemini-2.5-pro"}}
	h.GeminiGetHandler(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gjson.GetBytes(rec.Body.Bytes(), "name").String() != "models/gemini-2.5-pro" {
		t.Fatalf("unexpected model name: %s", rec.Body.String())
	}
}

func TestGeminiHandler_GenerateContent(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi there"}]},` +
							`"finishReason":"STOP"}],"usageMetadata":` +
							`{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`),
					},
				},
			},
		},
	}
	defer registerFakeGemini(t, h, fe)()

	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", body, nil)
	c.Params = gin.Params{{Key: "action", Value: "/gemini-2.5-pro:generateContent"}}
	h.GeminiHandler(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hi there"`)) {
		t.Fatalf("expected Gemini response passthrough, got %s", rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request")
	}
	if fe.lastReq.Model != "gemini-2.5-pro" {
		t.Errorf("model=%q, want gemini-2.5-pro", fe.lastReq.Model)
	}
}

func TestGeminiHandler_CountTokens(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	fe := &fakeGeminiExecutor{
		countTokensResp: &executor.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"totalTokens":42}`),
		},
	}
	defer registerFakeGemini(t, h, fe)()

	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", body, nil)
	c.Params = gin.Params{{Key: "action", Value: "/gemini-2.5-pro:countTokens"}}
	h.GeminiHandler(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gjson.GetBytes(rec.Body.Bytes(), "totalTokens").Int() != 42 {
		t.Fatalf("expected totalTokens 42, got %s", rec.Body.String())
	}
	if fe.lastCountTokens == nil {
		t.Fatalf("expected countTokens call")
	}
}

func TestGeminiHandler_UpstreamErrorPassedThrough(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	upBody := []byte(`{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}`)
	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusBadRequest, Body: upBody}},
		},
	}
	defer registerFakeGemini(t, h, fe)()

	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", body, nil)
	c.Params = gin.Params{{Key: "action", Value: "/gemini-2.5-pro:generateContent"}}
	h.GeminiHandler(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`API key not valid`)) {
		t.Errorf("expected upstream error body, got %s", rec.Body.String())
	}
}

func TestInteractions_ModelTarget_ConvertsAndResponds(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},` +
							`"finishReason":"STOP"}],"usageMetadata":` +
							`{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3},"modelVersion":"gemini-2.5-pro"}`),
					},
				},
			},
		},
	}
	defer registerFakeGemini(t, h, fe)()

	body := []byte(`{"target":{"model":"models/gemini-2.5-pro"},"input":"hello"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1beta/interactions", body, nil)
	h.Interactions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gjson.GetBytes(rec.Body.Bytes(), "object").String() != "interaction" {
		t.Fatalf("expected interaction object, got %s", rec.Body.String())
	}
	if !gjson.GetBytes(rec.Body.Bytes(), "steps").IsArray() {
		t.Fatalf("expected steps array, got %s", rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request")
	}
	if !gjson.GetBytes(fe.lastReq.Body, "contents").IsArray() {
		t.Fatalf("expected converted Gemini contents, got %s", fe.lastReq.Body)
	}
}

func TestInteractions_AgentTarget_Rejected(t *testing.T) {
	h, cleanup := setupGeminiTest(t)
	defer cleanup()

	body := []byte(`{"target":{"agent":"my-agent"},"input":"hello"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1beta/interactions", body, nil)
	h.Interactions(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`agent interactions are not supported yet`)) {
		t.Fatalf("expected agent rejection, got %s", rec.Body.String())
	}
}
