package v1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator"
)

// fakeJWT builds a trivial base64url JWT-like token with the given claims.
func fakeJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	cb, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(cb)
	return header + "." + payload + "."
}

// TestGrokCLI_EndToEnd seeds a grok-cli OAuth connection, mocks the upstream
// Grok CLI Responses SSE endpoint with httptest, and verifies that a streaming
// POST to /v1/chat/completions returns OpenAI Chat Completions SSE frames.
func TestGrokCLI_EndToEnd(t *testing.T) {
	logging.Init("text")

	// The base executor blocks private/localhost URLs. Allow httptest servers.
	restore := executor.SetValidateURLForTest(func(string) error { return nil })
	defer restore()

	h := newTestHandler(t)

	// Register only the Grok CLI executor so the test does not pollute the
	// global registry with unrelated providers.
	base := executor.NewBaseExecutor()
	executor.GetRegistry().Register("grok-cli", executor.FormatGrokCLI, executor.NewGrokCLIExecutor(base))

	accessToken := fakeJWT(map[string]any{
		"sub":   "grok-cli-sub-123",
		"email": "test@example.com",
	})

	var gotMethod, gotContentType, gotAuth string
	var gotBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

	created, _ := json.Marshal(map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id": "resp_grok_123",
			"model": "grok-build",
		},
	})
		writeSSE(w, flusher, created)

		delta, _ := json.Marshal(map[string]any{
			"type":  "response.output_text.delta",
			"delta": "Hello from Grok CLI",
		})
		writeSSE(w, flusher, delta)

	completed, _ := json.Marshal(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp_grok_123",
			"model": "grok-build",
			"status": "completed",
			"output": []any{},
			"usage": map[string]any{
				"input_tokens":  5,
				"output_tokens": 4,
				"total_tokens":  9,
			},
		},
	})
		writeSSE(w, flusher, completed)
	}))
	defer upstream.Close()

	now := time.Now().Unix()
	if _, err := h.db.Exec(`UPDATE provider_types SET base_url = ? WHERE id = 'grok-cli'`, upstream.URL); err != nil {
		t.Fatalf("update grok-cli provider_type base_url: %v", err)
	}
	if _, err := h.db.Exec(
		`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, oauth_token, provider_specific_data, status, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"grok-cli-conn", "grok-cli", "Grok CLI Test", "oauth", accessToken,
		`{"sub":"grok-cli-sub-123","email":"test@example.com"}`,
		"ready", 1, now, now,
	); err != nil {
		t.Fatalf("seed grok-cli connection: %v", err)
	}
	h.store.SeedConnection("grok-cli-conn", "grok-cli", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"grok-cli/grok-build","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %q", got)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("upstream method=%q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("upstream Content-Type=%q, want application/json", gotContentType)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("upstream Authorization=%q, want Bearer ...", gotAuth)
	}
	if !strings.Contains(string(gotBody), `"input"`) {
		t.Errorf("upstream body missing input items: %s", gotBody)
	}

	resp := rec.Body.String()
	if !strings.Contains(resp, "data: [DONE]") {
		t.Errorf("SSE output missing [DONE] marker; body:\n%s", resp)
	}
	if !strings.Contains(resp, `"object":"chat.completion.chunk"`) {
		t.Errorf("SSE output missing chat.completion.chunk; body:\n%s", resp)
	}
	if !strings.Contains(resp, `"content":"Hello from Grok CLI"`) {
		t.Errorf("SSE output missing translated content; body:\n%s", resp)
	}
	if !strings.Contains(resp, `"finish_reason":"stop"`) {
		t.Errorf("SSE output missing finish_reason=stop; body:\n%s", resp)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, data []byte) {
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}
