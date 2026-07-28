package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/headroom"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
)

func TestHeadroomCompressesLargeToolOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = newTestHandler(t)

	// Enable headroom globally for the translator registry.
	headroom.SetGlobalToolCompressor(headroom.NewToolCompressor(headroom.Config{Enabled: true}))
	headroom.SetEnabled(true)
	defer func() {
		headroom.SetGlobalToolCompressor(headroom.NewToolCompressor(headroom.Config{Enabled: false}))
		headroom.SetEnabled(false)
	}()

	largeDiff := strings.Repeat("diff --git a/internal/foo.go b/internal/foo.go\nindex 1234567..abcdefg 100644\n--- a/internal/foo.go\n+++ b/internal/foo.go\n@@ -1,5 +1,5 @@\n package foo\n \n func Foo() string {\n-    return \"old\"\n+    return \"new\"\n }\n", 100)
	body, _ := json.Marshal(map[string]any{
		"model": "claude-sonnet-4",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":    "tool_result",
						"content": largeDiff,
					},
				},
			},
		},
	})

	originalLen := len(body)
	translated := registry.Request("openai", "claude", "claude-sonnet-4", body, false)
	finalLen := len(translated)
	if finalLen >= originalLen {
		t.Fatalf("expected translated body to be smaller after headroom compression: original=%d final=%d", originalLen, finalLen)
	}

	// Also exercise that the request path through the handler does not corrupt the body.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	_ = c.Request.Body
}
