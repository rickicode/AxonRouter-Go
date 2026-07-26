package v1

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

func TestImagesEdits_JSONBodyRoutesToImageExecutor(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fg := &fakeImageGenerator{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, fg)
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm","image"]`, "openai-img-edit-conn", "http://unused")

	body := []byte(`{"model":"openai/dall-e-2","prompt":"make it blue","image":"data:image/png;base64,abc"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImagesEdits(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fg.called {
		t.Fatal("expected Images executor to be called")
	}
}

func TestImagesEdits_MultipartBuildsJSON(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fg := &fakeImageGenerator{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, fg)
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm","image"]`, "openai-img-edit-multi-conn", "http://unused")

	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("model", "openai/dall-e-2")
	_ = mw.WriteField("prompt", "add a hat")
	fw, _ := mw.CreateFormFile("image", "face.png")
	_, _ = fw.Write([]byte{0x89, 0x50, 0x4e, 0x47}) // minimal PNG magic
	mw.Close()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &b)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())

	h.ImagesEdits(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fg.called {
		t.Fatal("expected Images executor to be called for multipart request")
	}
}

func TestImagesEdits_MissingPrompt(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	body := []byte(`{"model":"openai/dall-e-2","image":"data:image/png;base64,abc"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImagesEdits(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
