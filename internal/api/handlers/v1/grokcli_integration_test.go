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
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
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

// TestGrokCLI_EndToEnd_NonStream verifies that a non-streaming POST to
// /v1/chat/completions returns a standard OpenAI chat.completion object.
func TestGrokCLI_EndToEnd_NonStream(t *testing.T) {
	logging.Init("text")

	restore := executor.SetValidateURLForTest(func(string) error { return nil })
	defer restore()

	h := newTestHandler(t)

	base := executor.NewBaseExecutor()
	executor.GetRegistry().Register("grok-cli", executor.FormatGrokCLI, executor.NewGrokCLIExecutor(base))

	accessToken := fakeJWT(map[string]any{
		"sub":   "grok-cli-sub-123",
		"email": "test@example.com",
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		created, _ := json.Marshal(map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":    "resp_grok_123",
				"model": "grok-build",
			},
		})
		writeSSE(w, flusher, created)

		itemDone, _ := json.Marshal(map[string]any{
			"type":         "response.output_item.done",
			"output_index": 0,
			"item": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": "Hello from Grok CLI",
					},
				},
			},
		})
		writeSSE(w, flusher, itemDone)

		completed, _ := json.Marshal(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     "resp_grok_123",
				"model":  "grok-build",
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
		"grok-cli-conn-nonstream", "grok-cli", "Grok CLI Non-stream Test", "oauth", accessToken,
		`{"sub":"grok-cli-sub-123","email":"test@example.com"}`,
		"ready", 1, now, now,
	); err != nil {
		t.Fatalf("seed grok-cli connection: %v", err)
	}
	h.store.SeedConnection("grok-cli-conn-nonstream", "grok-cli", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"grok-cli/grok-build","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected application/json, got %q", got)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v\nbody: %s", err, rec.Body.String())
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %v", resp["object"])
	}
	if resp["model"] != "grok-build" {
		t.Errorf("expected model=grok-build, got %v", resp["model"])
	}

	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("expected choices array, got %v", resp["choices"])
	}
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason=stop, got %v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if message["content"] != "Hello from Grok CLI" {
		t.Errorf("expected content='Hello from Grok CLI', got %v", message["content"])
	}
}

// TestGrokCLI_EndToEnd_FailoverAuthFailed proves that a 403 permission-denied
// response from one Grok CLI account only disables that account (auth_failed)
// and the request fails over to a second healthy account.
func TestGrokCLI_EndToEnd_FailoverAuthFailed(t *testing.T) {
	logging.Init("text")

	restore := executor.SetValidateURLForTest(func(string) error { return nil })
	defer restore()

	h := newTestHandler(t)

	base := executor.NewBaseExecutor()
	executor.GetRegistry().Register("grok-cli", executor.FormatGrokCLI, executor.NewGrokCLIExecutor(base))

	invalidToken := fakeJWT(map[string]any{"sub": "invalid-sub", "email": "invalid@example.com"})
	validToken := fakeJWT(map[string]any{"sub": "valid-sub", "email": "valid@example.com"})

	var invalidCalls, validCalls int

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch {
		case strings.Contains(auth, invalidToken):
			invalidCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			body := `{"error":{"message":"{\"code\":\"permission-denied\",\"error\":\"Access to the chat endpoint is denied. Please ensure you're using the correct credentials.\",\"type\":\"permission_error\",\"code\":\"insufficient_quota\"}","type":"permission_error","code":"insufficient_quota"}}`
			w.Write([]byte(body))
		case strings.Contains(auth, validToken):
			validCalls++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("response writer does not support flushing")
			}
			completed, _ := json.Marshal(map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":     "resp_grok_ok",
					"model":  "grok-build",
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
		default:
			t.Fatalf("unexpected token: %s", auth)
		}
	}))
	defer upstream.Close()

	now := time.Now().Unix()
	if _, err := h.db.Exec(`UPDATE provider_types SET base_url = ? WHERE id = 'grok-cli'`, upstream.URL); err != nil {
		t.Fatalf("update grok-cli provider_type base_url: %v", err)
	}
	// Remove any grok-cli connections left by other tests in this package so
	// routing is deterministic with only our two seeded accounts.
	if _, err := h.db.Exec(`DELETE FROM connections WHERE provider_type_id = 'grok-cli'`); err != nil {
		t.Fatalf("clean grok-cli connections: %v", err)
	}
	for _, tc := range []struct {
		id, token, status string
	}{
		{"grok-cli-invalid", invalidToken, "ready"},
		{"grok-cli-valid", validToken, "ready"},
	} {
		if _, err := h.db.Exec(
			`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, oauth_token, provider_specific_data, status, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tc.id, "grok-cli", tc.id, "oauth", tc.token,
			`{"sub":"`+tc.id+`-sub","email":"`+tc.id+`@example.com"}`,
			tc.status, 1, now, now,
		); err != nil {
			t.Fatalf("seed %s: %v", tc.id, err)
		}
		h.store.SeedConnection(tc.id, "grok-cli", tc.status, 0)
	}
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
	if invalidCalls != 1 {
		t.Errorf("expected exactly one upstream call with invalid token, got %d", invalidCalls)
	}
	if validCalls != 1 {
		t.Errorf("expected exactly one upstream call with valid token, got %d", validCalls)
	}

	invalidCS := h.store.Get("grok-cli-invalid")
	if invalidCS == nil {
		t.Fatal("missing invalid connection state")
	}
	if invalidCS.GetStatus() != connstate.StatusAuthFailed {
		t.Errorf("invalid conn status=%v, want auth_failed", invalidCS.GetStatus())
	}

	validCS := h.store.Get("grok-cli-valid")
	if validCS == nil {
		t.Fatal("missing valid connection state")
	}
	if validCS.GetStatus() != connstate.StatusReady {
		t.Errorf("valid conn status=%v, want ready", validCS.GetStatus())
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, data []byte) {
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}
