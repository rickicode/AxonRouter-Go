package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

func TestRequestID_AttachesClientIPAndUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	var gotIP, gotUA string
	router.GET("/test", func(c *gin.Context) {
		gotIP = executor.ClientIPFromContext(c.Request.Context())
		gotUA = executor.UserAgentFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotIP != "192.168.1.100" {
		t.Errorf("client IP = %q, want 192.168.1.100", gotIP)
	}
	if gotUA != "test-agent/1.0" {
		t.Errorf("user agent = %q, want test-agent/1.0", gotUA)
	}
}
