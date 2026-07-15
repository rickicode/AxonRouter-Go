package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func makeTestJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := map[string]any{"exp": exp}
	payloadBytes, _ := json.Marshal(payload)
	return header + "." + base64.RawURLEncoding.EncodeToString(payloadBytes) + "."
}

func TestMimocodeExecutor_URLs(t *testing.T) {
	tests := []struct {
		base          string
		wantBootstrap string
		wantChat      string
	}{
		{
			base:          "https://api.xiaomimimo.com/api/free-ai/openai",
			wantBootstrap: "https://api.xiaomimimo.com/api/free-ai/bootstrap",
			wantChat:      "https://api.xiaomimimo.com/api/free-ai/openai/chat",
		},
		{
			base:          "https://api.xiaomimimo.com/openai",
			wantBootstrap: "https://api.xiaomimimo.com/api/free-ai/bootstrap",
			wantChat:      "https://api.xiaomimimo.com/openai/chat",
		},
	}
	for _, tc := range tests {
		t.Run(tc.base, func(t *testing.T) {
			if got := bootstrapURLFromBase(tc.base); got != tc.wantBootstrap {
				t.Errorf("bootstrapURLFromBase(%q) = %q, want %q", tc.base, got, tc.wantBootstrap)
			}
			if got := chatURLFromBase(tc.base); got != tc.wantChat {
				t.Errorf("chatURLFromBase(%q) = %q, want %q", tc.base, got, tc.wantChat)
			}
		})
	}
}

func TestMimocodeExecutor_Execute(t *testing.T) {
	var bootstrapCalls atomic.Int32
	var chatCalls atomic.Int32
	var lastHeaders http.Header
	var lastBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/free-ai/bootstrap"):
			bootstrapCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			jwt := makeTestJWT(time.Now().Add(time.Hour).Unix())
			json.NewEncoder(w).Encode(map[string]string{"jwt": jwt})
		case strings.HasSuffix(r.URL.Path, "/openai/chat"):
			chatCalls.Add(1)
			lastHeaders = r.Header.Clone()
			lastBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test",
				"model":   "mimo-auto",
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "ok"}}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	exec := NewMimocodeExecutor(NewBaseExecutor())
	ctx := context.Background()
	body, _ := json.Marshal(map[string]any{
		"model": "mimocode/mimo-v2-pro",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	})
	req := &Request{
		BaseURL:              ts.URL + "/api/free-ai/openai",
		Provider:             "mimocode",
		Model:                "mimocode/mimo-v2-pro",
		Body:                 body,
		ProviderSpecificData: map[string]string{"fingerprint": "fp-test-1"},
	}

	resp, err := exec.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if chatCalls.Load() != 1 {
		t.Errorf("chat calls = %d, want 1", chatCalls.Load())
	}
	if bootstrapCalls.Load() != 1 {
		t.Errorf("bootstrap calls = %d, want 1", bootstrapCalls.Load())
	}

	auth := lastHeaders.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || auth == "Bearer " {
		t.Errorf("Authorization header missing or empty bearer: %q", auth)
	}
	if got := lastHeaders.Get("X-Mimo-Source"); got != mimocodeSource {
		t.Errorf("X-Mimo-Source = %q, want %q", got, mimocodeSource)
	}
	ua := lastHeaders.Get("User-Agent")
	if !strings.Contains(ua, "Chrome") {
		t.Errorf("User-Agent missing Chrome signature: %q", ua)
	}

	var gotBody map[string]any
	if err := json.Unmarshal(lastBody, &gotBody); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if gotBody["model"] != "mimo-auto" {
		t.Errorf("model = %v, want mimo-auto", gotBody["model"])
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("messages missing or empty: %v", gotBody["messages"])
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), mimocodeSystemMarker) {
		t.Errorf("first message not marker system: %v", first)
	}
}

func TestMimocodeExecutor_Execute_RetriesAuthOnce(t *testing.T) {
	var bootstrapCalls atomic.Int32
	var chatCalls atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/free-ai/bootstrap") {
			bootstrapCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			jwt := makeTestJWT(time.Now().Add(time.Hour).Unix())
			json.NewEncoder(w).Encode(map[string]string{"jwt": jwt})
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/openai/chat") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
			return
		}
		chatCalls.Add(1)
		if chatCalls.Load() == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid jwt"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "ok", "choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "ok"}}}})
	}))
	defer ts.Close()

	exec := NewMimocodeExecutor(NewBaseExecutor())
	req := &Request{
		BaseURL:              ts.URL + "/api/free-ai/openai",
		Provider:             "mimocode",
		Model:                "mimocode/mimo-auto",
		Body:                 mustJSON(map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}),
		ProviderSpecificData: map[string]string{"fingerprint": "fp-auth-retry"},
	}

	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if bootstrapCalls.Load() != 2 {
		t.Errorf("bootstrap calls = %d, want 2", bootstrapCalls.Load())
	}
	if chatCalls.Load() != 2 {
		t.Errorf("chat calls = %d, want 2", chatCalls.Load())
	}
}

func TestMimocodeExecutor_Execute_JwtCacheReuse(t *testing.T) {
	var bootstrapCalls atomic.Int32
	var chatCalls atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/free-ai/bootstrap") {
			bootstrapCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			jwt := makeTestJWT(time.Now().Add(time.Hour).Unix())
			json.NewEncoder(w).Encode(map[string]string{"jwt": jwt})
			return
		}
		chatCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "ok"})
	}))
	defer ts.Close()

	exec := NewMimocodeExecutor(NewBaseExecutor())
	for i := 0; i < 2; i++ {
		_, err := exec.Execute(context.Background(), &Request{
			BaseURL:              ts.URL + "/api/free-ai/openai",
			Provider:             "mimocode",
			Model:                "mimocode/mimo-auto",
			Body:                 mustJSON(map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}),
			ProviderSpecificData: map[string]string{"fingerprint": "fp-cache"},
		})
		if err != nil {
			t.Fatalf("Execute #%d error: %v", i, err)
		}
	}
	if bootstrapCalls.Load() != 1 {
		t.Errorf("bootstrap calls = %d, want 1", bootstrapCalls.Load())
	}
	if chatCalls.Load() != 2 {
		t.Errorf("chat calls = %d, want 2", chatCalls.Load())
	}
}

func TestMimocodeExecutor_ExecuteStream(t *testing.T) {
	var bootstrapCalls atomic.Int32
	var chatHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/free-ai/bootstrap") {
			bootstrapCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"jwt": makeTestJWT(time.Now().Add(time.Hour).Unix())})
			return
		}
		chatHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	exec := NewMimocodeExecutor(NewBaseExecutor())
	req := &Request{
		BaseURL:              ts.URL + "/api/free-ai/openai",
		Provider:             "mimocode",
		Model:                "mimocode/mimo-v2-pro",
		Body:                 mustJSON(map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}),
		ProviderSpecificData: map[string]string{"fingerprint": "fp-stream"},
		StreamConfig:         &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}

	result, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.StatusCode)
	}
	if bootstrapCalls.Load() != 1 {
		t.Errorf("bootstrap calls = %d, want 1", bootstrapCalls.Load())
	}
	if got := chatHeaders.Get("X-Mimo-Source"); got != mimocodeSource {
		t.Errorf("X-Mimo-Source = %q, want %q", got, mimocodeSource)
	}
	if auth := chatHeaders.Get("Authorization"); auth == "" {
		t.Errorf("Authorization header missing")
	}
}
