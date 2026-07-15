package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

type copilotTestTransport struct {
	authHost string
	apiHost  string
}

func (rt *copilotTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	if host == "api.github.com" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.authHost
	} else if host == "api.githubcopilot.com" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.apiHost
	}
	return http.DefaultTransport.RoundTrip(req)
}

func execWithCopilotTransport(t *testing.T) (*httptest.Server, *httptest.Server, *CopilotExecutor, func()) {
	t.Helper()

	callCount := make(map[string]int)

	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount["auth"]++
		if r.URL.Path != "/copilot_internal/v2/token" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "token test-oauth" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"token":      "test-copilot-token",
			"expires_at": 9999999999,
			"endpoints":  map[string]any{"api": "https://api.githubcopilot.com"},
		})
	}))

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			if r.Header.Get("Authorization") != "Bearer test-copilot-token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			body, _ := io.ReadAll(r.Body)
			model := gjson.GetBytes(body, "model").String()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"object": "chat.completion",
				"model":  model,
				"choices": []map[string]any{{"message": map[string]any{
					"role":    "assistant",
					"content": "ok",
				}}},
			})
		case "/models":
			if r.Header.Get("Authorization") != "Bearer test-copilot-token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"id": "gpt-4o"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))

	authURL, _ := url.Parse(auth.URL)
	apiURL, _ := url.Parse(api.URL)
	rt := &copilotTestTransport{authHost: authURL.Host, apiHost: apiURL.Host}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt

	exec := NewCopilotExecutor(NewBaseExecutor())
	exec.Client.Transport = rt
	exec.streamBase.Transport = rt

	return auth, api, exec, func() {
		http.DefaultClient.Transport = old
		auth.Close()
		api.Close()
	}
}

func TestCopilotExecutor_ExchangesTokenAndStripsModelPrefix(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	resp, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "test-oauth",
		Body:     []byte(`{"model":"copilot/gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	model := gjson.GetBytes(resp.Body, "model").String()
	if model != "gpt-4o" {
		t.Errorf("upstream model = %q, want gpt-4o", model)
	}
}

func TestCopilotExecutor_CachesToken(t *testing.T) {
	auth, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, _ = exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "test-oauth",
		Body:     []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})
	_, _ = exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "test-oauth",
		Body:     []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})

	if len(exec.tokens) != 1 {
		t.Errorf("expected 1 cached token, got %d", len(exec.tokens))
	}
	_ = auth
}

func TestCopilotExecutor_ErrorsWithoutOAuthToken(t *testing.T) {
	exec := NewCopilotExecutor(NewBaseExecutor())
	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "",
		Body:     []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})
	if err == nil {
		t.Fatal("expected error without oauth token")
	}
	if !strings.Contains(err.Error(), "missing OAuth token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopilotExecutor_ModelsRequiresAuth(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	resp, err := exec.Models(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "test-oauth",
	})
	if err != nil {
		t.Fatalf("models failed: %v", err)
	}
	id := gjson.GetBytes(resp.Body, "data.0.id").String()
	if id != "gpt-4o" {
		t.Errorf("models response id = %q, want gpt-4o", id)
	}
}

func TestCopilotExecutor_UnsupportedMethods(t *testing.T) {
	exec := NewCopilotExecutor(NewBaseExecutor())
	req := &Request{Provider: "copilot", APIKey: "tok"}

	if _, err := exec.Embeddings(context.Background(), req); err == nil || !strings.Contains(err.Error(), "embeddings endpoint not supported") {
		t.Errorf("Embeddings error = %v, want unsupported embeddings", err)
	}
	if _, err := exec.Images(context.Background(), req); err == nil || !strings.Contains(err.Error(), "images endpoint not supported") {
		t.Errorf("Images error = %v, want unsupported images", err)
	}
	if _, err := exec.Responses(context.Background(), req); err == nil || !strings.Contains(err.Error(), "responses endpoint not supported") {
		t.Errorf("Responses error = %v, want unsupported responses", err)
	}
	if _, err := exec.ResponsesStream(context.Background(), req); err == nil || !strings.Contains(err.Error(), "responses endpoint not supported") {
		t.Errorf("ResponsesStream error = %v, want unsupported responses", err)
	}
}

func TestCopilotExecutor_DefaultsExpiresAtToOneHour(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"token":      "test-copilot-token",
			"expires_at": 0,
			"endpoints":  map[string]any{"api": "https://api.githubcopilot.com"},
		})
	}))
	defer auth.Close()

	authURL, _ := url.Parse(auth.URL)
	rt := &copilotTestTransport{authHost: authURL.Host, apiHost: authURL.Host}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt
	defer func() { http.DefaultClient.Transport = old }()

	exec := NewCopilotExecutor(NewBaseExecutor())
	exec.Client.Transport = rt

	tok, err := exec.fetchToken("test-oauth")
	if err != nil {
		t.Fatalf("fetchToken failed: %v", err)
	}
	if tok.ExpiresAt <= time.Now().Unix() || tok.ExpiresAt > time.Now().Unix()+7200 {
		t.Errorf("ExpiresAt = %d, expected roughly now+3600", tok.ExpiresAt)
	}
}

func TestCopilotOAuthToken_CachesResult(t *testing.T) {
	resetCopilotOAuthTokenCache()
	defer resetCopilotOAuthTokenCache()

	tmp := t.TempDir()
	oldXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer os.Setenv("XDG_CONFIG_HOME", oldXDG)

	configDir := filepath.Join(tmp, "github-copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "hosts.json")
	if err := os.WriteFile(path, []byte(`{"github.com":{"oauth_token":"cached-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	first := loadCopilotOAuthToken()
	if first != "cached-token" {
		t.Fatalf("first load = %q, want cached-token", first)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second := loadCopilotOAuthToken()
	if second != "cached-token" {
		t.Fatalf("second load after deleting file = %q, want cached-token (cache miss)", second)
	}

	resetCopilotOAuthTokenCache()
	third := loadCopilotOAuthToken()
	if third != "" {
		t.Fatalf("after reset = %q, want empty", third)
	}
}

func TestCopilotHeaders_DetectsInitiator(t *testing.T) {
	exec := NewCopilotExecutor(NewBaseExecutor())
	h := exec.copilotHeaders("tok", []byte(`{"messages":[{"role":"user"},{"role":"assistant"}]}`))
	if h["X-Initiator"] != "agent" {
		t.Errorf("X-Initiator = %q, want agent", h["X-Initiator"])
	}

	h = exec.copilotHeaders("tok", []byte(`{"messages":[{"role":"user"}]}`))
	if h["X-Initiator"] != "user" {
		t.Errorf("X-Initiator = %q, want user", h["X-Initiator"])
	}
}
