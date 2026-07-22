package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	db "github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

func withFakeKiroExecutor(t *testing.T, fake executor.Executor) {
	t.Helper()
	reg := executor.GetRegistry()
	oldExec, oldFormat, ok := reg.Get("kiro")
	reg.Register("kiro", executor.FormatOpenAI, fake)
	t.Cleanup(func() {
		if ok {
			reg.Register("kiro", oldFormat, oldExec)
		} else {
			reg.Unregister("kiro")
		}
	})
}

type fakeTestExecutor struct {
	mu      sync.Mutex
	calls   []*executor.Request
	results []fakeTestResult
	idx     int
}

type fakeTestResult struct {
	err    error
	stream *executor.StreamResult
}

func (f *fakeTestExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	_, err := f.next(req)
	return nil, err
}

func (f *fakeTestExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return f.next(req)
}

func (f *fakeTestExecutor) next(req *executor.Request) (*executor.StreamResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.idx >= len(f.results) {
		return nil, errors.New("no more fake results")
	}
	r := f.results[f.idx]
	f.idx++
	return r.stream, r.err
}

func (f *fakeTestExecutor) lastAccessToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1].AccessToken
}

func successStreamResult() *executor.StreamResult {
	ch := make(chan executor.StreamChunk, 1)
	ch <- executor.StreamChunk{}
	close(ch)
	return &executor.StreamResult{Chunks: ch, StatusCode: http.StatusOK}
}

type fakeKiroOAuthService struct {
	mu         sync.Mutex
	calls      int
	creds      *auth.Credentials
	newAccess  string
	newRefresh string
	err        error
}

func (f *fakeKiroOAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (f *fakeKiroOAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, nil
}

func (f *fakeKiroOAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.creds = creds
	if f.err != nil {
		return nil, f.err
	}
	return &auth.Credentials{
		AccessToken:  f.newAccess,
		RefreshToken: f.newRefresh,
		ExpiresAt:    time.Now().Add(time.Hour),
	}, nil
}

func (f *fakeKiroOAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

func (f *fakeKiroOAuthService) refreshCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func seedKiroOAuthConnection(t *testing.T, database *sql.DB, id, accessToken, refreshToken string, expiresAt int64) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('kiro','Kiro','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`UPDATE provider_types SET format = 'openai' WHERE id = 'kiro'`); err != nil {
		t.Fatalf("update provider_type format: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, oauth_token, oauth_refresh_token, oauth_expires_at, created_at, updated_at) VALUES (?, 'kiro', 'k1', 'oauth', 'ready', 1, ?, ?, ?, ?, ?)`,
		id, accessToken, refreshToken, expiresAt, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
}

func newAuthManagerWithKiro(t *testing.T, database *sql.DB, svc auth.OAuthService) *auth.Manager {
	t.Helper()
	mgr := auth.NewManagerWithWriter(db.NewOAuthTokenWriter(database, nil))
	mgr.RegisterService(auth.ProviderType("kiro"), svc)
	return mgr
}

// TestIsRefreshableTestError classifies auth and provider-specific errors that
// should trigger an OAuth token refresh during TestConnection.
func TestIsRefreshableTestError(t *testing.T) {
	cases := []struct {
		name       string
		providerID string
		err        error
		det        connstate.ErrorDetection
		want       bool
	}{
		{
			name: "auth category",
			err:  errors.New("invalid key"),
			det:  connstate.ErrorDetection{Category: connstate.ErrorAuth},
			want: true,
		},
		{
			name:       "upstream 401",
			providerID: "openai",
			err:        &executor.UpstreamError{StatusCode: http.StatusUnauthorized, Body: []byte("unauthorized")},
			det:        connstate.ErrorDetection{Category: connstate.ErrorUnknown},
			want:       true,
		},
		{
			name:       "upstream 403",
			providerID: "openai",
			err:        &executor.UpstreamError{StatusCode: http.StatusForbidden, Body: []byte("forbidden")},
			det:        connstate.ErrorDetection{Category: connstate.ErrorUnknown},
			want:       true,
		},
		{
			name:       "kiro 400 improperly formed request",
			providerID: "kiro",
			err: &executor.UpstreamError{
				StatusCode: http.StatusBadRequest,
				Body:       []byte(`{"message":"Improperly formed request.","reason":"REQUEST_BODY_INVALID"}`),
			},
			det:  connstate.ErrorDetection{Category: connstate.ErrorUnknown},
			want: true,
		},
		{
			name:       "kiro 400 other body is not refreshable",
			providerID: "kiro",
			err: &executor.UpstreamError{
				StatusCode: http.StatusBadRequest,
				Body:       []byte(`{"message":"Bad request"}`),
			},
			det:  connstate.ErrorDetection{Category: connstate.ErrorUnknown},
			want: false,
		},
		{
			name:       "non-auth 400",
			providerID: "openai",
			err:        &executor.UpstreamError{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":"bad request"}`)},
			det:        connstate.ErrorDetection{Category: connstate.ErrorUnknown},
			want:       false,
		},
		{
			name:       "rate limit 429 does not refresh",
			providerID: "openai",
			err:        &executor.UpstreamError{StatusCode: http.StatusTooManyRequests, Body: []byte("rate limited")},
			det:        connstate.ErrorDetection{Category: connstate.ErrorRateLimit},
			want:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isRefreshableTestError(tc.providerID, tc.err, tc.det)
			if got != tc.want {
				t.Fatalf("isRefreshableTestError(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTestConnection_ProactiveRefresh verifies that an OAuth token which is
// near/past expiry is refreshed before the test request, the new token is
// persisted, and the upstream call uses the refreshed access token.
func TestTestConnection_ProactiveRefresh(t *testing.T) {
	fake := &fakeTestExecutor{results: []fakeTestResult{{stream: successStreamResult()}}}
	withFakeKiroExecutor(t, fake)

	database := newConnectionHandlerTestDB(t)
	expiresPast := time.Now().Add(-time.Hour).Unix()
	seedKiroOAuthConnection(t, database, "kiro-conn-1", "old-access", "old-refresh", expiresPast)

	svc := &fakeKiroOAuthService{newAccess: "new-access", newRefresh: "new-refresh"}
	h := newConnectionHandlerForTest(t, database, executor.GetRegistry())
	h.authMgr = newAuthManagerWithKiro(t, database, svc)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/kiro-conn-1/test", nil)
	c.Params = gin.Params{{Key: "id", Value: "kiro-conn-1"}}
	h.TestConnection(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp["status"]; got != "ok" {
		t.Fatalf("status=%v, want ok", got)
	}

	var access, refresh string
	var expiry int64
	row := database.QueryRow(`SELECT oauth_token, oauth_refresh_token, oauth_expires_at FROM connections WHERE id='kiro-conn-1'`)
	if err := row.Scan(&access, &refresh, &expiry); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if access != "new-access" {
		t.Fatalf("oauth_token = %q, want new-access", access)
	}
	if refresh != "new-refresh" {
		t.Fatalf("oauth_refresh_token = %q, want new-refresh", refresh)
	}
	if expiry == 0 {
		t.Fatal("oauth_expires_at should be persisted")
	}
	if fake.lastAccessToken() != "new-access" {
		t.Fatalf("upstream access token = %q, want new-access", fake.lastAccessToken())
	}
	if svc.refreshCalls() != 1 {
		t.Fatalf("refresh calls = %d, want 1", svc.refreshCalls())
	}
}

// TestTestConnection_ReactiveRefreshKiro400 verifies that a Kiro 400 response
// indicating an invalid session triggers a token refresh and a single retry.
func TestTestConnection_ReactiveRefreshKiro400(t *testing.T) {
	fake := &fakeTestExecutor{results: []fakeTestResult{
		{err: &executor.UpstreamError{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"message":"Improperly formed request.","reason":"REQUEST_BODY_INVALID"}`),
			RawBody:    []byte(`{"message":"Improperly formed request.","reason":"REQUEST_BODY_INVALID"}`),
		}},
		{stream: successStreamResult()},
	}}
	withFakeKiroExecutor(t, fake)

	database := newConnectionHandlerTestDB(t)
	expiresFuture := time.Now().Add(time.Hour).Unix()
	seedKiroOAuthConnection(t, database, "kiro-conn-2", "old-access", "old-refresh", expiresFuture)

	svc := &fakeKiroOAuthService{newAccess: "new-access", newRefresh: "new-refresh"}
	h := newConnectionHandlerForTest(t, database, executor.GetRegistry())
	h.authMgr = newAuthManagerWithKiro(t, database, svc)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/kiro-conn-2/test", nil)
	c.Params = gin.Params{{Key: "id", Value: "kiro-conn-2"}}
	h.TestConnection(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp["status"]; got != "ok" {
		t.Fatalf("status=%v, want ok", got)
	}
	if svc.refreshCalls() != 1 {
		t.Fatalf("refresh calls = %d, want 1", svc.refreshCalls())
	}
	if fake.lastAccessToken() != "new-access" {
		t.Fatalf("retry access token = %q, want new-access", fake.lastAccessToken())
	}

	var access string
	row := database.QueryRow(`SELECT oauth_token FROM connections WHERE id='kiro-conn-2'`)
	if err := row.Scan(&access); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if access != "new-access" {
		t.Fatalf("persisted token = %q, want new-access", access)
	}
}

// TestTestConnection_RefreshStillFails verifies that when a refresh succeeds but
// the second upstream attempt still fails, the final result is failed.
func TestTestConnection_RefreshStillFails(t *testing.T) {
	fake := &fakeTestExecutor{results: []fakeTestResult{
		{err: &executor.UpstreamError{StatusCode: http.StatusUnauthorized, Body: []byte("unauthorized")}},
		{err: &executor.UpstreamError{StatusCode: http.StatusForbidden, Body: []byte("still forbidden")}},
	}}
	withFakeKiroExecutor(t, fake)

	database := newConnectionHandlerTestDB(t)
	expiresFuture := time.Now().Add(time.Hour).Unix()
	seedKiroOAuthConnection(t, database, "kiro-conn-3", "old-access", "old-refresh", expiresFuture)

	svc := &fakeKiroOAuthService{newAccess: "new-access", newRefresh: "new-refresh"}
	h := newConnectionHandlerForTest(t, database, executor.GetRegistry())
	h.authMgr = newAuthManagerWithKiro(t, database, svc)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/kiro-conn-3/test", nil)
	c.Params = gin.Params{{Key: "id", Value: "kiro-conn-3"}}
	h.TestConnection(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp["status"]; got != "failed" {
		t.Fatalf("status=%v, want failed", got)
	}
	if svc.refreshCalls() != 1 {
		t.Fatalf("refresh calls = %d, want 1", svc.refreshCalls())
	}

	var status string
	row := database.QueryRow(`SELECT status FROM connections WHERE id='kiro-conn-3'`)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "auth_failed" {
		t.Fatalf("status = %q, want auth_failed", status)
	}
}

// TestTestConnection_NonAuthErrorDoesNotRefresh verifies that non-auth errors
// such as rate limits do not trigger a token refresh.
func TestTestConnection_NonAuthErrorDoesNotRefresh(t *testing.T) {
	fake := &fakeTestExecutor{results: []fakeTestResult{
		{err: &executor.UpstreamError{StatusCode: http.StatusTooManyRequests, Body: []byte("rate limited")}},
	}}
	withFakeKiroExecutor(t, fake)

	database := newConnectionHandlerTestDB(t)
	expiresFuture := time.Now().Add(time.Hour).Unix()
	seedKiroOAuthConnection(t, database, "kiro-conn-4", "old-access", "old-refresh", expiresFuture)

	svc := &fakeKiroOAuthService{newAccess: "new-access", newRefresh: "new-refresh"}
	h := newConnectionHandlerForTest(t, database, executor.GetRegistry())
	h.authMgr = newAuthManagerWithKiro(t, database, svc)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/kiro-conn-4/test", nil)
	c.Params = gin.Params{{Key: "id", Value: "kiro-conn-4"}}
	h.TestConnection(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp["status"]; got != "failed" {
		t.Fatalf("status=%v, want failed", got)
	}
	if svc.refreshCalls() != 0 {
		t.Fatalf("refresh calls = %d, want 0", svc.refreshCalls())
	}

	var access string
	row := database.QueryRow(`SELECT oauth_token FROM connections WHERE id='kiro-conn-4'`)
	if err := row.Scan(&access); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if access != "old-access" {
		t.Fatalf("oauth_token = %q, want old-access", access)
	}
}
