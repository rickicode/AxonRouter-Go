package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

func TestLoggingIncludesClientInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previous := logging.Logger.Load()
	var buf bytes.Buffer
	logging.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { logging.SetLogger(previous) })

	router := gin.New()
	router.Use(Logging())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/test?x=1", nil)
	req.Header.Set("User-Agent", "axon-test/1.0")
	req.RemoteAddr = "203.0.113.10:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	out := buf.String()
	if !strings.Contains(out, `"msg":"http"`) {
		t.Fatalf("missing http log output:\n%s", out)
	}
	if !strings.Contains(out, `"client_ip":"203.0.113.10"`) {
		t.Fatalf("missing client_ip in log output:\n%s", out)
	}
	if !strings.Contains(out, `"user_agent":"axon-test/1.0"`) {
		t.Fatalf("missing user_agent in log output:\n%s", out)
	}
}
