package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

type capturedRequest struct {
	path   string
	header http.Header
	body   []byte
}

var (
	copilotAuthCalls atomic.Int32
	lastCopilotReq   capturedRequest
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

	copilotAuthCalls.Store(0)
	lastCopilotReq = capturedRequest{}

	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		copilotAuthCalls.Add(1)
		if r.URL.Path != "/copilot_internal/v2/token" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "token ") {
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
		body, _ := io.ReadAll(r.Body)
		lastCopilotReq.path = r.URL.Path
		lastCopilotReq.header = r.Header.Clone()
		lastCopilotReq.body = body

		model := gjson.GetBytes(body, "model").String()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions", "/responses", "/v1/responses":
			json.NewEncoder(w).Encode(map[string]any{
				"object": "chat.completion",
				"model":  model,
				"choices": []map[string]any{{"message": map[string]any{
					"role":    "assistant",
					"content": "ok",
				}}},
			})
		case "/models":
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

func TestCopilotExecutor_PrefersAccessToken(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider:    "copilot",
		AccessToken: "oauth-token",
		APIKey:      "old-api-key",
		Body:        []byte(`{"model":"copilot/gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if copilotAuthCalls.Load() != 1 {
		t.Fatalf("auth calls = %d, want 1", copilotAuthCalls.Load())
	}
	if lastCopilotReq.header.Get("Authorization") != "Bearer test-copilot-token" {
		t.Fatalf("authorization = %q, want Bearer test-copilot-token", lastCopilotReq.header.Get("Authorization"))
	}
}

func TestCopilotExecutor_APIKeyFallbackForMigration(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "legacy-token",
		Body:     []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if copilotAuthCalls.Load() != 1 {
		t.Fatalf("auth calls = %d, want 1", copilotAuthCalls.Load())
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

func TestCopilotExecutor_UsesPSDCopilotTokenWhenNotNearExpiry(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "any",
		ProviderSpecificData: map[string]string{
			"copilotToken":          "cached-copilot-token",
			"copilotTokenExpiresAt": strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
		},
		Body: []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if copilotAuthCalls.Load() != 0 {
		t.Fatalf("auth calls = %d, want 0", copilotAuthCalls.Load())
	}
	if lastCopilotReq.header.Get("Authorization") != "Bearer cached-copilot-token" {
		t.Fatalf("authorization = %q, want Bearer cached-copilot-token", lastCopilotReq.header.Get("Authorization"))
	}
}

func TestCopilotExecutor_RefreshesPSDCopilotTokenNearExpiry(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		APIKey:   "any",
		ProviderSpecificData: map[string]string{
			"copilotToken":          "cached-copilot-token",
			"copilotTokenExpiresAt": strconv.FormatInt(time.Now().Add(30*time.Second).Unix(), 10),
		},
		Body: []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if copilotAuthCalls.Load() != 1 {
		t.Fatalf("auth calls = %d, want 1", copilotAuthCalls.Load())
	}
	if lastCopilotReq.header.Get("Authorization") != "Bearer test-copilot-token" {
		t.Fatalf("authorization = %q, want Bearer test-copilot-token", lastCopilotReq.header.Get("Authorization"))
	}
}

func TestCopilotExecutor_HeadersUseOmniRouteConstants(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider:    "copilot",
		AccessToken: "tok",
		Body:        []byte(`{"model":"copilot/gpt-4o","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	want := map[string]string{
		"Editor-Version":                      "vscode/1.126.0",
		"Editor-Plugin-Version":               "copilot-chat/0.54.0",
		"User-Agent":                          "GitHubCopilotChat/0.54.0",
		"Copilot-Integration-Id":              "vscode-chat",
		"Openai-Intent":                       "conversation-panel",
		"X-Github-Api-Version":                "2026-06-01",
		"X-Vscode-User-Agent-Library-Version": "electron-fetch",
	}
	for k, v := range want {
		if got := lastCopilotReq.header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestCopilotExecutor_ForwardsXInitiator(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	for _, tc := range []struct {
		header string
		want   string
	}{
		{"agent", "agent"},
		{"user", "user"},
		{"other", "user"},
	} {
		_, err := exec.Execute(context.Background(), &Request{
			Provider:    "copilot",
			AccessToken: "tok",
			Headers:     map[string]string{"x-initiator": tc.header},
			Body:        []byte(`{"model":"copilot/gpt-4o","messages":[{"role":"user"}]}`),
		})
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}
		if got := lastCopilotReq.header.Get("X-Initiator"); got != tc.want {
			t.Errorf("x-initiator=%q => X-Initiator = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestCopilotHeaders_DetectsInitiator(t *testing.T) {
	exec := NewCopilotExecutor(NewBaseExecutor())
	h := exec.copilotHeaders("tok", []byte(`{"messages":[{"role":"user"},{"role":"assistant"}]}`), nil, false)
	if h["X-Initiator"] != "agent" {
		t.Errorf("X-Initiator = %q, want agent", h["X-Initiator"])
	}

	h = exec.copilotHeaders("tok", []byte(`{"messages":[{"role":"user"}]}`), nil, false)
	if h["X-Initiator"] != "user" {
		t.Errorf("X-Initiator = %q, want user", h["X-Initiator"])
	}
}

func TestCopilotExecutor_RoutesCodexToResponses(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	for _, model := range []string{"copilot/gpt-5.3-codex", "copilot/copilot-codex-latest"} {
		_, err := exec.Execute(context.Background(), &Request{
			Provider:    "copilot",
			AccessToken: "tok",
			Body:        []byte(`{"model":"` + model + `","messages":[{"role":"user"}]}`),
		})
		if err != nil {
			t.Fatalf("execute failed for %s: %v", model, err)
		}
		if !strings.HasSuffix(lastCopilotReq.path, "/responses") {
			t.Errorf("model %s routed to %q, want /responses", model, lastCopilotReq.path)
		}
	}
}

func TestCopilotExecutor_NeverRoutesClaudeOrGeminiToResponses(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	for _, model := range []string{"copilot/claude-3.5-sonnet", "copilot/gemini-2.0-flash"} {
		_, err := exec.Execute(context.Background(), &Request{
			Provider:    "copilot",
			AccessToken: "tok",
			Body:        []byte(`{"model":"` + model + `","messages":[{"role":"user"}]}`),
		})
		if err != nil {
			t.Fatalf("execute failed for %s: %v", model, err)
		}
		if lastCopilotReq.path != "/chat/completions" {
			t.Errorf("model %s routed to %q, want /chat/completions", model, lastCopilotReq.path)
		}
	}
}

func TestCopilotExecutor_StripTemperatureOnGpt54(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider:    "copilot",
		AccessToken: "tok",
		Body:        []byte(`{"model":"copilot/gpt-5.4","messages":[{"role":"user"}],"temperature":0.7}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if gjson.GetBytes(lastCopilotReq.body, "temperature").Exists() {
		t.Errorf("temperature should be stripped for gpt-5.4")
	}
}

func TestCopilotExecutor_CapsToolsTo128(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	tools := make([]map[string]any, 150)
	for i := 0; i < 150; i++ {
		tools[i] = map[string]any{"type": "function", "function": map[string]any{"name": strconv.Itoa(i)}}
	}
	body, _ := json.Marshal(map[string]any{
		"model":    "copilot/gpt-4o",
		"messages": []map[string]any{{"role": "user"}},
		"tools":    tools,
	})
	_, err := exec.Execute(context.Background(), &Request{
		Provider:    "copilot",
		AccessToken: "tok",
		Body:        body,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := int64(len(gjson.GetBytes(lastCopilotReq.body, "tools").Array())); got != 128 {
		t.Errorf("tools count = %d, want 128", got)
	}
}

func TestCopilotExecutor_SanitizesMessageContentParts(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider:    "copilot",
		AccessToken: "tok",
		Body:        []byte(`{"model":"copilot/gpt-4o","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"tool_use","name":"x"},{"type":"text","text":"visible"}]},{"role":"user","content":[{"type":"text","text":"hi"}]}]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	parts := gjson.GetBytes(lastCopilotReq.body, "messages.0.content").Array()
	for _, p := range parts {
		if p.Get("type").String() != "text" {
			t.Errorf("unexpected part type %q", p.Get("type").String())
		}
	}
	if len(parts) != 3 {
		t.Errorf("content parts = %d, want 3", len(parts))
	}
}

func TestCopilotExecutor_DropsTrailingAssistantPrefill(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		AccessToken: "tok",
		Body: []byte(`{"model":"copilot/gpt-4o","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"prefill"}]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	count := len(gjson.GetBytes(lastCopilotReq.body, "messages").Array())
	if count != 1 {
		t.Errorf("messages count = %d, want 1", count)
	}
}

func TestCopilotExecutor_StripsThinkingForClaude(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	for _, model := range []string{"copilot/claude-opus-4.5", "copilot/claude-sonnet-4.5"} {
		_, err := exec.Execute(context.Background(), &Request{
			Provider: "copilot",
			AccessToken: "tok",
			Body: []byte(`{"model":"` + model + `","messages":[{"role":"user"}],"thinking":{"type":"adaptive"},"reasoning_effort":"high"}`),
		})
		if err != nil {
			t.Fatalf("execute failed for %s: %v", model, err)
		}
		if gjson.GetBytes(lastCopilotReq.body, "thinking").Exists() {
			t.Errorf("thinking should be stripped for %s", model)
		}
		if gjson.GetBytes(lastCopilotReq.body, "reasoning_effort").Exists() {
			t.Errorf("reasoning_effort should be stripped for %s", model)
		}
	}
}

func TestCopilotExecutor_KeepsThinkingForClaude46(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		AccessToken: "tok",
		Body: []byte(`{"model":"copilot/claude-sonnet-4.6","messages":[{"role":"user"}],"thinking":{"type":"adaptive"},"reasoning_effort":"high"}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !gjson.GetBytes(lastCopilotReq.body, "thinking").Exists() {
		t.Errorf("thinking should be kept for claude-sonnet-4.6")
	}
	if !gjson.GetBytes(lastCopilotReq.body, "reasoning_effort").Exists() {
		t.Errorf("reasoning_effort should be kept for claude-sonnet-4.6")
	}
}

func TestCopilotExecutor_StripsTemperatureForClaudeOpus4(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		AccessToken: "tok",
		Body: []byte(`{"model":"copilot/claude-opus-4.5","messages":[{"role":"user"}],"temperature":0.7}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if gjson.GetBytes(lastCopilotReq.body, "temperature").Exists() {
		t.Errorf("temperature should be stripped for claude-opus-4")
	}
}

func TestCopilotExecutor_InjectsResponseFormatForClaude(t *testing.T) {
	_, _, exec, cleanup := execWithCopilotTransport(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"model": "copilot/claude-sonnet-4.5",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"response_format": map[string]any{"type": "json_object"},
	})
	_, err := exec.Execute(context.Background(), &Request{
		Provider: "copilot",
		AccessToken: "tok",
		Body: body,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if gjson.GetBytes(lastCopilotReq.body, "response_format").Exists() {
		t.Errorf("response_format should be removed for Claude on Copilot")
	}
	content := gjson.GetBytes(lastCopilotReq.body, "messages.0.content").String()
	if !strings.Contains(content, "Respond only with valid JSON") {
		t.Errorf("system instruction not injected, got %q", content)
	}
}
