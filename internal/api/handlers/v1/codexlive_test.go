package v1

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api/handlers/v1/codexlivestore"
)

// setupCodexLiveTest seeds a Codex connection and returns a handler with a live
// session store ready for sideband tests.
func setupCodexLiveTest(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(t)
	now := time.Now().Unix()
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('cx','Codex','openai-responses','https://chatgpt.com/backend-api/codex',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, oauth_token, provider_specific_data, created_at, updated_at) VALUES ('cx-live-conn','cx','Codex Live','oauth','ready',1,'live-access-token','{"accountId":"live-account"}',?,?)`, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("cx-live-conn", "cx", "ready", 0)
	h.elig.RecomputeAll()
	return h
}

func TestCodexLive_ForwardsToUpstreamAndStoresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/realtime/calls" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("intent") != "quicksilver" {
			t.Errorf("intent = %q", r.URL.Query().Get("intent"))
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer live-access-token" {
			t.Errorf("Authorization = %q", auth)
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("missing User-Agent")
		}
		if origin := r.Header.Get("Origin"); origin != "https://chatgpt.com" {
			t.Errorf("Origin = %q", origin)
		}
		if account := r.Header.Get("Chatgpt-Account-Id"); account != "live-account" {
			t.Errorf("Chatgpt-Account-Id = %q", account)
		}
		w.Header().Set("Location", "/v1/live/call-abc123")
		w.Header().Set("Content-Type", "application/sdp")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0\r\n"))
	}))
	defer upstream.Close()

	h.codexLiveHTTPClient = upstream.Client()
	body := []byte(`{"model":"cx/gpt-live-1-codex","sdp":"v=0"}`)
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/live", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Alpha", "quicksilver=v2")
	req.Header.Set("Originator", "Codex Desktop")

	// Patch the upstream URL via a RoundTripper so the hardcoded production URL
	// resolves to the test server.
	h.codexLiveHTTPClient.Transport = &codexLiveTestTransport{base: upstream.URL}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	h.CodexLive(c)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/v1/live/call-abc123" {
		t.Errorf("Location = %q", loc)
	}
	if _, ok, _ := h.codexLiveSessions.Get(context.Background(), "call-abc123"); !ok {
		t.Fatal("live session was not stored")
	}
}

func TestCodexLive_WithoutModelUsesDefaultAndReachesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)

	seen := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	h.codexLiveHTTPClient = upstream.Client()
	h.codexLiveHTTPClient.Transport = &codexLiveTestTransport{base: upstream.URL}

	body := []byte(`{"sdp":"v=0"}`)
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/live", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	h.CodexLive(c)

	if !seen {
		t.Fatal("upstream was not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCodexLive_MultipartBodyRewrittenToJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)

	var upstreamBody string
	var upstreamContentType string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		upstreamContentType = r.Header.Get("Content-Type")
		w.Header().Set("Location", "/v1/live/call-multi")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0"))
	}))
	defer upstream.Close()

	h.codexLiveHTTPClient = upstream.Client()
	h.codexLiveHTTPClient.Transport = &codexLiveTestTransport{base: upstream.URL}

	const boundary = "live-boundary"
	parts := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"sdp\"\r\n\r\n" +
		"v=0\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"session\"\r\n\r\n" +
		`{"model":"cx/gpt-live-1-codex","instructions":"hi"}` + "\r\n" +
		"--" + boundary + "--\r\n"
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/live", strings.NewReader(parts))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	h.CodexLive(c)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if upstreamContentType != "application/json" {
		t.Errorf("upstream Content-Type = %q, want application/json", upstreamContentType)
	}
	if !strings.Contains(upstreamBody, `"sdp":"v=0"`) {
		t.Errorf("upstream body missing sdp: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `"instructions":"hi"`) {
		t.Errorf("upstream body missing session data: %s", upstreamBody)
	}
}

func TestCodexLive_ModelNotAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)
	body := []byte(`{"model":"cx/gpt-live-1-codex"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/live", body, map[string]struct{}{"openai": {}})

	h.CodexLive(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if !strings.Contains(rec.Body.String(), "model not allowed for this API key") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestCodexLive_NoConnectionAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler(t)
	body := []byte(`{"model":"cx/gpt-live-1-codex"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/live", body, nil)

	h.CodexLive(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestCodexLiveSideband_RelaysBidirectionally(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.AcceptOptions{InsecureSkipVerify: true}
		conn, err := websocket.Accept(w, r, &upgrader)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
		ctx := context.Background()
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		_ = conn.Write(ctx, typ, append([]byte("echo:"), data...))
	}))
	defer upstream.Close()

	sidebandBase := "ws" + strings.TrimPrefix(upstream.URL, "http") + "/v1"
	h.codexLiveSidebandBaseURL = sidebandBase
	h.codexLiveSessions.Put(context.Background(), codexlivestore.Session{CallID: "call-relay", ConnID: "cx-live-conn", ConnToken: "live-access-token", Model: "cx/gpt-live-1-codex"})

	ginEngine := gin.New()
	ginEngine.GET("/v1/live/:call_id", h.CodexLiveSideband)
	downstream := httptest.NewServer(ginEngine)
	defer downstream.Close()

	wsURL := "ws" + strings.TrimPrefix(downstream.URL, "http") + "/v1/live/call-relay"
	client, resp, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{})
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("dial sideband: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	ctx := context.Background()
	if err := client.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		_ = client.Close(websocket.StatusInternalError, "write failed")
		t.Fatalf("write: %v", err)
	}
	typ, data, err := client.Read(ctx)
	if err != nil {
		_ = client.Close(websocket.StatusInternalError, "read failed")
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText || string(data) != "echo:ping" {
		_ = client.Close(websocket.StatusInternalError, "unexpected message")
		t.Fatalf("message = %q %q", typ, string(data))
	}
	_ = client.Close(websocket.StatusNormalClosure, "done")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok, _ := h.codexLiveSessions.Get(context.Background(), "call-relay"); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sideband session was not cleaned up")
}

func TestCodexLiveSideband_SessionNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/live/missing-call", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = gin.Params{{Key: "call_id", Value: "missing-call"}}

	h.CodexLiveSideband(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestExtractCodexLiveCallID(t *testing.T) {
	cases := []struct {
		location string
		want     string
	}{
		{"call-1", "call-1"},
		{"/v1/live/call-2", "call-2"},
		{"/v1/realtime/calls/call-3", "call-3"},
		{"/v1/realtime?intent=quicksilver&call_id=call-4", "call-4"},
		{"/other/call-5", ""},
	}
	for _, tc := range cases {
		t.Run(tc.location, func(t *testing.T) {
			if got := extractCodexLiveCallID(tc.location); got != tc.want {
				t.Errorf("extractCodexLiveCallID(%q) = %q, want %q", tc.location, got, tc.want)
			}
		})
	}
}

// codexLiveTestTransport rewrites the hardcoded production Codex upstream URL to
// the test server address.
type codexLiveTestTransport struct {
	base string
}

func (t *codexLiveTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.base, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

func TestCodexLiveSideband_PersistentSQLiteSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)

	// Replace in-memory store with SQLite persistence using the test DB.
	dbStore, err := codexlivestore.New(codexlivestore.Options{Provider: codexlivestore.ProviderSQLite, TTL: 5 * time.Minute, DB: h.db})
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	h.codexLiveSessions = dbStore

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.AcceptOptions{InsecureSkipVerify: true}
		conn, err := websocket.Accept(w, r, &upgrader)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
		ctx := context.Background()
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		_ = conn.Write(ctx, typ, append([]byte("echo:"), data...))
	}))
	defer upstream.Close()

	sidebandBase := "ws" + strings.TrimPrefix(upstream.URL, "http") + "/v1"
	h.codexLiveSidebandBaseURL = sidebandBase
	if err := dbStore.Put(context.Background(), codexlivestore.Session{CallID: "call-persist", ConnID: "cx-live-conn", ConnToken: "live-access-token", Model: "cx/gpt-live-1-codex"}); err != nil {
		t.Fatalf("put persistent session: %v", err)
	}

	ginEngine := gin.New()
	ginEngine.GET("/v1/live/:call_id", h.CodexLiveSideband)
	downstream := httptest.NewServer(ginEngine)
	defer downstream.Close()

	wsURL := "ws" + strings.TrimPrefix(downstream.URL, "http") + "/v1/live/call-persist"
	client, resp, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{})
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("dial sideband: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	ctx := context.Background()
	if err := client.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		_ = client.Close(websocket.StatusInternalError, "write failed")
		t.Fatalf("write: %v", err)
	}
	typ, data, err := client.Read(ctx)
	if err != nil {
		_ = client.Close(websocket.StatusInternalError, "read failed")
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText || string(data) != "echo:ping" {
		t.Fatalf("message = %q %q", typ, string(data))
	}
	_ = client.Close(websocket.StatusNormalClosure, "done")
}

func TestCodexLive_StoresSessionInSQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupCodexLiveTest(t)
	dbStore, err := codexlivestore.New(codexlivestore.Options{Provider: codexlivestore.ProviderSQLite, TTL: 5 * time.Minute, DB: h.db})
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	h.codexLiveSessions = dbStore

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/v1/live/call-db")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0"))
	}))
	defer upstream.Close()
	h.codexLiveHTTPClient = upstream.Client()
	h.codexLiveHTTPClient.Transport = &codexLiveTestTransport{base: upstream.URL}

	body := []byte(`{"model":"cx/gpt-live-1-codex","sdp":"v=0"}`)
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/live", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	h.CodexLive(c)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	got, ok, err := dbStore.Get(context.Background(), "call-db")
	if err != nil {
		t.Fatalf("get from sqlite: %v", err)
	}
	if !ok || got.ConnID != "cx-live-conn" {
		t.Fatalf("expected persisted session connID=cx-live-conn, got ok=%v connID=%q", ok, got.ConnID)
	}
}
