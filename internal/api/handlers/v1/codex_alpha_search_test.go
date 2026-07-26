package v1

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCodexAlphaSearch_RequiresModelOrDefaults(t *testing.T) {
	h := newTestHandler(t)

	body := []byte(`{"prompt":"find examples"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CodexAlphaSearch(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no connection), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCodexAlphaSearch_RequiresCodexProvider(t *testing.T) {
	h := newTestHandler(t)

	body := []byte(`{"model":"openai/gpt-4o","prompt":"find examples"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CodexAlphaSearch(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
