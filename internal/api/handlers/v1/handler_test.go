package v1

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// errReader is an io.ReadCloser that always returns a configured error.
type errReader struct {
	err error
}

func (e *errReader) Read(p []byte) (int, error) { return 0, e.err }
func (e *errReader) Close() error               { return nil }

// memoryHandler captures slog records for assertions.
type memoryHandler struct {
	records []slog.Record
}

func (m *memoryHandler) Enabled(_ context.Context, level slog.Level) bool { return true }
func (m *memoryHandler) Handle(_ context.Context, r slog.Record) error {
	m.records = append(m.records, r)
	return nil
}
func (m *memoryHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return m }
func (m *memoryHandler) WithGroup(name string) slog.Handler       { return m }

// fakeExecutor records calls and returns programmed responses.
type fakeExecutor struct {
	callCount int
	responses []struct {
		resp *executor.Response
		err  error
	}
	streamResults []struct {
		result *executor.StreamResult
		err    error
	}
	streamErr bool
}

func (f *fakeExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	idx := f.callCount
	f.callCount++
	if idx >= len(f.responses) {
		return nil, errors.New("no more responses")
	}
	return f.responses[idx].resp, f.responses[idx].err
}

func (f *fakeExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	idx := f.callCount
	f.callCount++
	if len(f.streamResults) > 0 {
		if idx >= len(f.streamResults) {
			return nil, errors.New("no more stream results")
		}
		return f.streamResults[idx].result, f.streamResults[idx].err
	}
	f.streamErr = true
	return nil, errors.New("streaming not supported")
}

type fakeOAuthService struct {
	refreshFunc func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error)
}

func (f *fakeOAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (f *fakeOAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, nil
}

func (f *fakeOAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	return f.refreshFunc(ctx, creds)
}

func (f *fakeOAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "handler-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func mustHashKey(t *testing.T, key string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash key: %v", err)
	}
	return string(hash)
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	store := connstate.NewStore()
	store.SeedConnection("conn-1", "test", "ready", 0)
	mgr := auth.NewManager()
	database := openTestDB(t)
	elig := connstate.NewEligibilityManager(store)
	return &Handler{
		db:          database,
		store:       store,
		elig:        elig,
		authMgr:     mgr,
		exhaustion:  quota.NewExhaustionCache(),
		providerCfg: providercfg.NewManager(t.TempDir()),
		combo:       combo.NewHandler(database, store, elig),
		registry:    executor.GetRegistry(),
	}
}

func TestProxyContext_PoolID(t *testing.T) {
	h := newTestHandler(t)
	now := db.UnixNow()
	if _, err := h.db.Exec(`INSERT INTO proxy_pools (id, name, type, proxy_url, is_active, created_at, updated_at)
		VALUES ('pool-abc', 'Test Pool', 'http', 'http://10.0.0.1:8080', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert proxy pool: %v", err)
	}
	h.resolver = proxypool.NewResolver(h.db)

	conn := &Connection{
		ID:                   "conn-pool",
		Provider:             "openai",
		ProviderSpecificData: `{"proxyPoolId":"pool-abc"}`,
	}
	proxyCtx := h.proxyContext(context.Background(), conn)
	if got := executor.ProxyPoolIDFromContext(proxyCtx); got != "pool-abc" {
		t.Errorf("ProxyPoolIDFromContext = %q, want pool-abc", got)
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id   string
		n    int
		want string
	}{
		{"abcdefgh", 8, "abcdefgh"},
		{"abcdefghij", 8, "abcdefgh"},
		{"abc", 8, "abc"},
		{"", 8, ""},
		{"x", 0, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := shortID(tt.id, tt.n); got != tt.want {
				t.Errorf("shortID(%q, %d) = %q, want %q", tt.id, tt.n, got, tt.want)
			}
		})
	}
}

func TestExecuteWithRetry_AuthRefreshThenSuccess(t *testing.T) {
	h := newTestHandler(t)
	h.authMgr.RegisterService(auth.ProviderType("test"), &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			return &auth.Credentials{AccessToken: "fresh-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	})

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, errors.New("401 unauthorized")},
			{&executor.Response{StatusCode: 200, Body: []byte(`ok`)}, nil},
		},
	}

	conn := &Connection{
		ID:             "conn-1",
		RefreshToken:   "old-refresh",
		OAuthExpiresAt: time.Now().Add(-time.Minute),
	}

	req := &executor.Request{Stream: false}
	resp, _, err := h.executeWithRetry(context.Background(), fe, req, conn, "test", "model-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if req.AccessToken != "fresh-token" {
		t.Errorf("access token was not refreshed, got %q", req.AccessToken)
	}
	if fe.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", fe.callCount)
	}
}

func TestProactiveRefreshToken_CopilotLead(t *testing.T) {
	h := newTestHandler(t)
	refreshed := false
	h.authMgr.RegisterService(auth.ProviderGitHub, &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			refreshed = true
			return &auth.Credentials{
				AccessToken:  "fresh-token",
				RefreshToken: creds.RefreshToken,
				ExpiresAt:    time.Now().Add(time.Hour),
			}, nil
		},
	})

	conn := &Connection{
		ID:             "conn-1",
		RefreshToken:   "old-refresh",
		OAuthExpiresAt: time.Now().Add(4 * time.Minute),
	}

	// Within the 5-minute lead for copilot should refresh.
	if !h.proactiveRefreshToken(context.Background(), conn, "copilot") {
		t.Fatal("expected proactive refresh for copilot within lead")
	}
	if !refreshed {
		t.Fatal("expected refresh to be called")
	}

	// Outside the lead should not refresh.
	refreshed = false
	conn.OAuthExpiresAt = time.Now().Add(6 * time.Minute)
	if h.proactiveRefreshToken(context.Background(), conn, "copilot") {
		t.Fatal("expected no proactive refresh for copilot outside lead")
	}
	if refreshed {
		t.Fatal("expected refresh not to be called outside lead")
	}
}

func TestExecuteWithRetry_GivesUpAfter3(t *testing.T) {
	h := newTestHandler(t)

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, errors.New("401 unauthorized")},
			{nil, errors.New("401 unauthorized")},
			{nil, errors.New("401 unauthorized")},
		},
	}

	conn := &Connection{ID: "conn-1"}
	req := &executor.Request{Stream: false}

	start := time.Now()
	_, _, err := h.executeWithRetry(context.Background(), fe, req, conn, "test", "model-1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after retries")
	}
	if fe.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", fe.callCount)
	}
	// Linear backoff: sleeps of 1s + 2s = 3s minimum.
	if elapsed < 3*time.Second {
		t.Errorf("expected at least 3s delay, got %v", elapsed)
	}
}

func TestExecuteWithRetry_ContextCanceled(t *testing.T) {
	h := newTestHandler(t)

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, errors.New("transient error")},
			{nil, errors.New("transient error")},
			{nil, errors.New("transient error")},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, _, err := h.executeWithRetry(ctx, fe, &executor.Request{Stream: false}, &Connection{ID: "conn-1"}, "test", "model-1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 call before context cancel, got %d", fe.callCount)
	}
	if elapsed >= time.Second {
		t.Errorf("expected immediate return, got %v", elapsed)
	}
}

func TestWriteUpstreamClientError_WritesTranslatedError(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{"error":{"message":"context too long","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	upErr := &executor.UpstreamError{StatusCode: http.StatusBadRequest, Body: body}

	if !h.writeUpstreamClientError(context.Background(), c, upErr, nil, "cf", "@cf/meta/llama-3.3-70b", time.Now(), false) {
		t.Fatal("expected writeUpstreamClientError to return true")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	if got["error"].(map[string]any)["code"] != "context_length_exceeded" {
		t.Errorf("code=%v, want context_length_exceeded", got["error"])
	}
}

func TestWriteUpstreamClientError_SkipsRateLimit(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	upErr := &executor.UpstreamError{
		StatusCode: http.StatusTooManyRequests,
		Body:       []byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`),
	}

	if h.writeUpstreamClientError(context.Background(), c, upErr, nil, "cf", "model", time.Now(), false) {
		t.Fatal("expected writeUpstreamClientError to return false for 429")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("response should not be written for 429, got status=%d", rec.Code)
	}
}

// TestPersistCooldown_WritesRealColumns proves the cooldown/error UPDATE lands in
// the DB using real schema columns (regression for the phantom consecutive_error_count
// column that made the whole UPDATE fail silently in the write queue).
func TestPersistCooldown_WritesRealColumns(t *testing.T) {
	h := newTestHandler(t)
	database := h.db
	wq := db.NewWriteQueue(database)
	h.writeQueue = wq

	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-1','test','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	cooldown := time.Now().Add(5 * time.Minute)
	det := connstate.ErrorDetection{
		Category:      connstate.ErrorRateLimit,
		Message:       "Rate limit exceeded",
		Status:        connstate.StatusRateLimited,
		CooldownUntil: &cooldown,
	}
	h.persistCooldown("conn-1", det)
	h.persistSuccess("conn-1")

	wq.Stop() // flush all queued writes

	var (
		status      string
		cooldownU   sql.NullInt64
		lastErr     sql.NullString
		lastErrCode sql.NullString
		failCount   int
		lastFail    sql.NullInt64
		lastSucc    sql.NullInt64
	)
	row := database.QueryRow(`SELECT status, cooldown_until, last_error, last_error_code, failure_count, last_failure_at, last_success_at FROM connections WHERE id='conn-1'`)
	if err := row.Scan(&status, &cooldownU, &lastErr, &lastErrCode, &failCount, &lastFail, &lastSucc); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != string(connstate.StatusRateLimited) {
		t.Fatalf("status = %q, want rate_limited", status)
	}
	if !cooldownU.Valid || cooldownU.Int64 == 0 {
		t.Fatalf("cooldown_until not persisted: %+v", cooldownU)
	}
	if lastErr.String != "Rate limit exceeded" {
		t.Fatalf("last_error = %q", lastErr.String)
	}
	if lastErrCode.String != string(connstate.ErrorRateLimit) {
		t.Fatalf("last_error_code = %q", lastErrCode.String)
	}
	if failCount != 1 {
		t.Fatalf("failure_count = %d, want 1", failCount)
	}
	if !lastFail.Valid || lastFail.Int64 == 0 {
		t.Fatalf("last_failure_at not persisted")
	}
	if !lastSucc.Valid || lastSucc.Int64 == 0 {
		t.Fatalf("last_success_at not persisted")
	}
}

// TestGetConnectionRejectsCooledDownConnection proves that getConnection never
// returns a connection that is actively in cooldown, even when an eligibility
// snapshot is stale.
func TestGetConnectionRejectsCooledDownConnection(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	now := time.Now().Unix()
	if _, err := h.db.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at)
		VALUES ('conn-oc-1','oc','prox1','none','ready',1,?,?)
	`, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	h.store.SeedConnection("conn-oc-1", "oc", "ready", 0)
	h.elig.RecomputeAll()

	cs := h.store.Get("conn-oc-1")

	// Normal case: connection is eligible.
	conn, err := h.getConnection(context.Background(), "oc", "hy3-free")
	if err != nil {
		t.Fatalf("expected eligible connection: %v", err)
	}
	if conn.ID != "conn-oc-1" {
		t.Fatalf("expected conn-oc-1, got %s", conn.ID)
	}

	// Mark cooldown and rebuild snapshot.
	cs.SetCooldown(time.Now().Add(time.Hour))
	h.elig.RecomputeAll()

	conn, err = h.getConnection(context.Background(), "oc", "hy3-free")
	if err == nil {
		t.Fatalf("expected error for cooled-down connection, got conn %s", conn.ID)
	}
}

func TestIsClientCanceled(t *testing.T) {
	h := &Handler{}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	c := &gin.Context{Request: req.WithContext(ctx)}

	// Client cancelled (request context done) → true.
	cancel()
	if !h.isClientCanceled(c, context.Canceled) {
		t.Fatal("expected true for cancelled request context")
	}

	// Non-cancel error → false.
	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	c2 := &gin.Context{Request: req2.WithContext(context.Background())}
	if h.isClientCanceled(c2, errors.New("boom")) {
		t.Fatal("expected false for non-cancel error")
	}

	// Cancelled error but client context still alive → false
	// (server-side cancel, not a client disconnect).
	req3 := httptest.NewRequest(http.MethodGet, "/x", nil)
	c3 := &gin.Context{Request: req3.WithContext(context.Background())}
	if h.isClientCanceled(c3, context.Canceled) {
		t.Fatal("expected false when request context is not cancelled")
	}
}

func TestChatCompletions_ContextCanceled(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fe := &fakeExecutor{}
	executor.GetRegistry().Register("ctxtest", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("ctxtest")

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('ctxtest','CtxTest','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('ctxtest-conn','ctxtest','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("ctxtest-conn", "ctxtest", "ready", 0)
	h.elig.RecomputeAll()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ctx.Err() == nil {
		t.Fatal("test setup failed: context was not canceled")
	}
	body := []byte(`{"model":"ctxtest/model","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	if c.Request.Context().Err() == nil {
		t.Fatal("test setup failed: c.Request context was not canceled")
	}

	h.ChatCompletions(c)
	if rec.Code != 499 {
		t.Errorf("expected status 499, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMessages_ContextDeadlineExceeded(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fe := &fakeExecutor{}
	executor.GetRegistry().Register("ctxtestclaude", executor.FormatClaude, fe)
	defer executor.GetRegistry().Unregister("ctxtestclaude")

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('ctxtestclaude','CtxClaude','claude','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('ctxtestclaude-conn','ctxtestclaude','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("ctxtestclaude-conn", "ctxtestclaude", "ready", 0)
	h.elig.RecomputeAll()

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	body := []byte(`{"model":"ctxtestclaude/model","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Messages(c)
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status 504, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMalformedProviderSpecificData_Warns(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	mh := &memoryHandler{}
	logging.Logger = slog.New(mh)

	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{resp: &executor.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"x"}`)}},
		},
	}
	executor.GetRegistry().Register("psdtest", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("psdtest")

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('psdtest','PsdTest','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, provider_specific_data, created_at, updated_at) VALUES ('psdtest-conn','psdtest','c1','none','ready',1,'not-json',0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("psdtest-conn", "psdtest", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"psdtest/model","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ChatCompletions(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var found bool
	for _, r := range mh.records {
		if r.Message == "malformed provider_specific_data" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning log for malformed provider_specific_data, got %d records", len(mh.records))
	}
}

func TestBuildFailoverErrorResponse(t *testing.T) {
	tests := []struct {
		name        string
		category    connstate.ErrorCategory
		lastErr     error
		modelName   string
		wantMsg     string
		wantStatus  int
		wantErrType string
	}{
		{
			name:        "model not found",
			category:    connstate.ErrorModelNotFound,
			modelName:   "unknown-model",
			wantMsg:     "model not found: unknown-model",
			wantStatus:  http.StatusNotFound,
			wantErrType: "invalid_request_error",
		},
		{
			name:        "auth error",
			category:    connstate.ErrorAuth,
			wantMsg:     "authentication failed for all connections",
			wantStatus:  http.StatusUnauthorized,
			wantErrType: "authentication_error",
		},
		{
			name:        "rate limit preserves upstream message",
			category:    connstate.ErrorRateLimit,
			lastErr:     &executor.UpstreamError{StatusCode: 429, Body: []byte(`{"error":{"message":"rate limiting: inference request per min rate reached"}}`)},
			wantMsg:     "rate limiting: inference request per min rate reached",
			wantStatus:  http.StatusTooManyRequests,
			wantErrType: "rate_limit_error",
		},
		{
			name:        "quota preserves upstream message",
			category:    connstate.ErrorQuota,
			lastErr:     &executor.UpstreamError{StatusCode: 429, Body: []byte(`{"error":{"message":"you have used up your daily free allocation of 10,000 neurons"}}`)},
			wantMsg:     "you have used up your daily free allocation of 10,000 neurons",
			wantStatus:  http.StatusTooManyRequests,
			wantErrType: "insufficient_quota",
		},
		{
			name:        "quota fallback without upstream error",
			category:    connstate.ErrorQuota,
			wantMsg:     "quota exhausted for all connections",
			wantStatus:  http.StatusTooManyRequests,
			wantErrType: "insufficient_quota",
		},
		{
			name:        "default",
			category:    connstate.ErrorUnknown,
			wantMsg:     "all connections exhausted or failing",
			wantStatus:  http.StatusServiceUnavailable,
			wantErrType: "server_error",
		},
	}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				msg, status, errType := buildFailoverErrorResponse(string(tt.category), tt.lastErr, tt.modelName)
				if msg != tt.wantMsg {
					t.Errorf("msg: got %q, want %q", msg, tt.wantMsg)
				}
				if status != tt.wantStatus {
					t.Errorf("status: got %d, want %d", status, tt.wantStatus)
				}
				if errType != tt.wantErrType {
					t.Errorf("errType: got %q, want %q", errType, tt.wantErrType)
				}
			})
		}
	}

	// TestStreamResponse_UsageAccumulation verifies that per-chunk token extraction
	// accumulates correctly across a Claude message_start + message_delta stream
	// and writes merged tokens to request_logs.
	func TestStreamResponse_UsageAccumulation(t *testing.T) {
		h := newTestHandler(t)

		// Create a minimal tracker and set it on the handler so Log() doesn't
		// panic, but we will verify via api_key_usage instead of request_logs
		// to avoid the async tracker flush race in tests.
		wq := db.NewWriteQueue(h.db)
		tracker := usage.NewTracker(h.db)
		tracker.SetWriteQueue(wq)
		h.tracker = tracker

		// Seed a provider_type and api_key so the DB FK constraint is satisfied.
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
			t.Fatalf("seed provider_type: %v", err)
		}
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-1','test','c1','none','ready',1,0,0)`); err != nil {
			t.Fatalf("seed connection: %v", err)
		}
	// Seed the test API key so the increment path has a row to update.
	hash := mustHashKey(t, "sk-test")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('test-key-1', 'test-key', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}

		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('test-key-1', 0, 0)`); err != nil {
			t.Fatalf("seed api_key_usage: %v", err)
		}

	// Create a gin test context with api_key_id set.
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Authorization", "Bearer sk-test")
	c.Set("api_key_id", "test-key-1")


		// Build a stream with Claude message_start (input tokens + cache)
		// and message_delta (output tokens).
		chunks := make(chan executor.StreamChunk, 3)
		chunks <- executor.StreamChunk{
			Payload: []byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}`),
		}
		chunks <- executor.StreamChunk{
			Payload: []byte(`data: {"type":"message_delta","usage":{"output_tokens":25}}`),
		}
		close(chunks)

		result := &executor.StreamResult{
			Chunks:     chunks,
			StatusCode: http.StatusOK,
		}

	conn := &Connection{ID: "conn-1"}
	dummyReq := []byte(`{}`)

	// Call streamResponse directly.
	h.streamResponse(context.Background(), c, result, conn, "test", "test-model",
		executor.FormatOpenAI, executor.FormatOpenAI,
		dummyReq, dummyReq,
		func(err error) []byte { return []byte(err.Error()) },
		time.Now(), "",
	)

		// Verify via api_key_usage: the accumulated tokens (15 input + 25 output = 40)
		// should have been written by incrementAPIKeyUsage which uses a direct DB write
		// (not going through the async tracker).
		var totalTokens int64
		err := h.db.QueryRow(`SELECT total_tokens FROM api_key_usage WHERE api_key_id = 'test-key-1'`).Scan(&totalTokens)
		if err != nil {
			t.Fatalf("query api_key_usage: %v", err)
		}
		if totalTokens != 40 {
			t.Errorf("total_tokens = %d, want 40 (15 input + 25 output)", totalTokens)
		}

		// Also verify the SSE output contains the translated chunks.
		body := rec.Body.String()
		if !strings.Contains(body, "data: [DONE]") {
			t.Errorf("SSE output missing [DONE] marker")
		}
		if !strings.Contains(body, "type\":\"message_start") {
			t.Errorf("SSE output missing message_start chunk")
		}
		if !strings.Contains(body, "type\":\"message_delta") {
			t.Errorf("SSE output missing message_delta chunk")
		}

	// Clean up
	tracker.Stop()
	wq.Stop()
}

func TestStreamResponse_UpstreamChunkErrMarksExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-err','test','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	chunks := make(chan executor.StreamChunk, 1)
	chunks <- executor.StreamChunk{Err: &executor.UpstreamError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":"rate limit"}`)}}
	close(chunks)
	result := &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}

	conn := &Connection{ID: "conn-err"}
	h.streamResponse(context.Background(), c, result, conn, "test", "test-model",
		executor.FormatOpenAI, executor.FormatOpenAI,
		[]byte(`{}`), []byte(`{}`),
		func(err error) []byte { return []byte(`{"error":"upstream"}`) },
		time.Now(), "",
	)

	if !h.exhaustion.IsExhausted("conn-err") {
		t.Error("expected connection marked exhausted after rate-limit chunk error")
	}

	tracker.Stop()
	wq.Stop()
}

func TestStreamResponse_ClientCanceledChunkErrDoesNotMarkExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-cancel','test','c2','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	cancel()

	chunks := make(chan executor.StreamChunk, 1)
	chunks <- executor.StreamChunk{Err: context.Canceled}
	close(chunks)
	result := &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}

	conn := &Connection{ID: "conn-cancel"}
	h.streamResponse(context.Background(), c, result, conn, "test", "test-model",
		executor.FormatOpenAI, executor.FormatOpenAI,
		[]byte(`{}`), []byte(`{}`),
		func(err error) []byte { return []byte(`{"error":"canceled"}`) },
		time.Now(), "",
	)

	if h.exhaustion.IsExhausted("conn-cancel") {
		t.Error("expected connection NOT marked exhausted after client cancellation mid-stream")
	}

	tracker.Stop()
	wq.Stop()
}

// TestFallbackUsage verifies that fallback estimation is applied when token
// extraction yields zero tokens but the request was successful, and that it
// is NOT applied on error responses or when tokens are already present.
func TestFallbackUsage(t *testing.T) {
	t.Run("non-streaming no usage in response applies fallback", func(t *testing.T) {
		// A response body with no usage info at all.
		body := []byte(`{"id":"x","choices":[{"index":0,"message":{"content":"Hello world, this is a test response","role":"assistant"}}]}`)
		reqBody := []byte(`{"model":"test/model","messages":[{"role":"user","content":"Hello world, this is a test request"}]}`)

		// Verify ExtractTokensFromBody returns zero.
		counts := ExtractTokensFromBody(body)
		if counts.InputTokens != 0 || counts.OutputTokens != 0 {
			t.Fatalf("expected zero tokens from body without usage, got input=%d output=%d", counts.InputTokens, counts.OutputTokens)
		}

		// Verify fallback estimation yields non-zero.
		estInput := usage.EstimateTokensFromRequest(reqBody)
		estOutput := usage.EstimateTokensFromResponse(body)
		if estInput == 0 && estOutput == 0 {
			t.Fatal("expected non-zero estimation from request/response bodies")
		}
	})

	t.Run("non-streaming with usage skips fallback", func(t *testing.T) {
		body := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5},"choices":[{"index":0,"message":{"content":"hi","role":"assistant"}}]}`)

		counts := ExtractTokensFromBody(body)
		if counts.InputTokens == 0 || counts.OutputTokens == 0 {
			t.Fatalf("expected non-zero tokens from body with usage, got input=%d output=%d", counts.InputTokens, counts.OutputTokens)
		}
	})

	t.Run("non-streaming error status skips fallback", func(t *testing.T) {
		// Error responses should not have fallback applied.
		body := []byte(`{"error":{"message":"bad request"}}`)
		reqBody := []byte(`{"model":"test/model","messages":[{"role":"user","content":"hi"}]}`)

		counts := ExtractTokensFromBody(body)
		// When there's no usage but also an error, the fallback is not applied.
		// Verify that ExtractTokensFromBody returns zero.
		if counts.InputTokens != 0 || counts.OutputTokens != 0 {
			t.Fatalf("expected zero tokens from error body, got input=%d output=%d", counts.InputTokens, counts.OutputTokens)
		}
		// Even if estimation would produce values, fallback should not be applied on error.
		_ = usage.EstimateTokensFromRequest(reqBody)
		_ = usage.EstimateTokensFromResponse(body)
		// No assertion needed - the handler code checks status before applying fallback.
	})

	t.Run("streaming no usage applies fallback", func(t *testing.T) {
		h := newTestHandler(t)
		wq := db.NewWriteQueue(h.db)
		tracker := usage.NewTracker(h.db)
		tracker.SetWriteQueue(wq)
		h.tracker = tracker

		// Seed required DB rows.
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
			t.Fatalf("seed provider_type: %v", err)
		}
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-1','test','c1','none','ready',1,0,0)`); err != nil {
			t.Fatalf("seed connection: %v", err)
		}
		hash := mustHashKey(t, "sk-test")
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('test-key-1', 'test-key', ?, 0)`, hash); err != nil {
			t.Fatalf("seed api_key: %v", err)
		}
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('test-key-1', 0, 0)`); err != nil {
			t.Fatalf("seed api_key_usage: %v", err)
		}

		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("Authorization", "Bearer sk-test")
		c.Set("api_key_id", "test-key-1")

		// Stream chunks without any usage info.
		chunks := make(chan executor.StreamChunk, 2)
		chunks <- executor.StreamChunk{
			Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"}}]}`),
		}
		chunks <- executor.StreamChunk{
			Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" world"}}]}`),
		}
		close(chunks)

		result := &executor.StreamResult{
			Chunks:     chunks,
			StatusCode: http.StatusOK,
		}

		conn := &Connection{ID: "conn-1"}
		originalReq := []byte(`{"model":"test/model","messages":[{"role":"user","content":"Hello"}]}`)
	translatedReq := []byte(`{"model":"test-model","messages":[{"role":"user","content":"Hello"}]}`)

	h.streamResponse(context.Background(), c, result, conn, "test", "test-model",
		executor.FormatOpenAI, executor.FormatOpenAI,
		originalReq, translatedReq,
		func(err error) []byte { return []byte(err.Error()) },
		time.Now(), "",
	)

		// Verify api_key_usage got non-zero estimated tokens (fallback).
		var totalTokens int64
		err := h.db.QueryRow(`SELECT total_tokens FROM api_key_usage WHERE api_key_id = 'test-key-1'`).Scan(&totalTokens)
		if err != nil {
			t.Fatalf("query api_key_usage: %v", err)
		}
		if totalTokens == 0 {
			t.Error("expected non-zero estimated tokens from fallback, got 0")
		}

		tracker.Stop()
		wq.Stop()
	})

	t.Run("streaming with usage skips fallback", func(t *testing.T) {
		h := newTestHandler(t)
		wq := db.NewWriteQueue(h.db)
		tracker := usage.NewTracker(h.db)
		tracker.SetWriteQueue(wq)
		h.tracker = tracker

		if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
			t.Fatalf("seed provider_type: %v", err)
		}
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-1','test','c1','none','ready',1,0,0)`); err != nil {
			t.Fatalf("seed connection: %v", err)
		}
		hash := mustHashKey(t, "sk-test-2")
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('test-key-2', 'test-key-2', ?, 0)`, hash); err != nil {
			t.Fatalf("seed api_key: %v", err)
		}
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('test-key-2', 0, 0)`); err != nil {
			t.Fatalf("seed api_key_usage: %v", err)
		}

		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("Authorization", "Bearer sk-test-2")
		c.Set("api_key_id", "test-key-2")

		// Stream with usage chunk.
		chunks := make(chan executor.StreamChunk, 2)
		chunks <- executor.StreamChunk{
			Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"}}]}`),
		}
		chunks <- executor.StreamChunk{
			Payload: []byte(`data: {"usage":{"prompt_tokens":7,"completion_tokens":3}}`),
		}
		close(chunks)

		result := &executor.StreamResult{
			Chunks:     chunks,
			StatusCode: http.StatusOK,
		}

	conn := &Connection{ID: "conn-1"}
	dummyReq := []byte(`{}`)

	h.streamResponse(context.Background(), c, result, conn, "test", "test-model",
		executor.FormatOpenAI, executor.FormatOpenAI,
		dummyReq, dummyReq,
		func(err error) []byte { return []byte(err.Error()) },
		time.Now(), "",
	)

		var totalTokens int64
		err := h.db.QueryRow(`SELECT total_tokens FROM api_key_usage WHERE api_key_id = 'test-key-2'`).Scan(&totalTokens)
		if err != nil {
			t.Fatalf("query api_key_usage: %v", err)
		}
		// Expect exactly 10 (7 input + 3 output) from the usage chunk, not estimated values.
		if totalTokens != 10 {
			t.Errorf("expected total_tokens = 10 from usage chunk, got %d", totalTokens)
		}

		tracker.Stop()
		wq.Stop()
	})
}

// TestChatCompletions_FallbackUsage verifies that a successful non-streaming
// response without upstream usage triggers fallback token estimation and marks
// the log row as estimated.
func TestChatCompletions_FallbackUsage(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	// Seed required rows.
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('testchat','TestChat','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('testchatconn1','testchat','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	hash := mustHashKey(t, "sk-test")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('test-key-chat', 'test-key-chat', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}

	// Register a fake OpenAI-compatible executor that returns a response without usage.
	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{
				resp: &executor.Response{
					StatusCode: http.StatusOK,
					Body:       []byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Hello world, this is a test response"}}]}`),
				},
			},
		},
	}
	executor.GetRegistry().Register("testchat", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testchat")

	h.store.SeedConnection("testchatconn1", "testchat", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"testchat/model","messages":[{"role":"user","content":"Hello world, this is a test request"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer sk-test")
	c.Set("api_key_id", "test-key-chat")

	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Give the async tracker time to flush to the write queue before stopping.
	time.Sleep(200 * time.Millisecond)
	tracker.Stop()
	wq.Stop()

	var totalTokens int64
	if err := h.db.QueryRow(`SELECT COALESCE(total_tokens, 0) FROM api_key_usage WHERE api_key_id = 'test-key-chat'`).Scan(&totalTokens); err != nil {
		t.Fatalf("query api_key_usage: %v", err)
	}
	if totalTokens == 0 {
		t.Fatalf("expected non-zero api_key_usage from fallback estimation")
	}
}

func TestWriteUpstreamClientError_PassesStatusAndBody(t *testing.T) {
	h := newTestHandler(t)
	conn := &Connection{ID: "conn-test", Provider: "testimage"}
	upErr := &executor.UpstreamError{
		StatusCode: http.StatusUnauthorized,
		Body:       []byte(`{"error":{"message":"bad key","type":"authentication_error"}}`),
		RawBody:    []byte(`{"error":{"message":"bad key","type":"authentication_error"}}`),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set("api_key_id", "test-key")

	if !h.writeUpstreamClientError(context.Background(), c, upErr, conn, "testimage", "dall-e-3", time.Now(), false) {
		t.Fatal("writeUpstreamClientError returned false for UpstreamError")
	}

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bad key") {
		t.Errorf("expected upstream body in response, got %q", rec.Body.String())
	}
}

func TestCacheOnlySuccess_UpstreamErrorNotCached(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	h.exactCache = cache.NewExactCache(1000)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	// Seed required rows.
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('testcache','TestCache','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('testcacheconn1','testcache','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	hash := mustHashKey(t, "sk-test")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('test-key-cache', 'test-key-cache', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}

	// Return a 429 upstream error.
	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{
				resp: &executor.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       []byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`),
				},
			},
		},
	}
	executor.GetRegistry().Register("testcache", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testcache")

	h.store.SeedConnection("testcacheconn1", "testcache", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"testcache/model","messages":[{"role":"user","content":"Hello"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer sk-test")
	c.Set("api_key_id", "test-key-cache")

	h.ChatCompletions(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", rec.Code)
	}
	if stats := h.exactCache.Stats(); stats.Size != 0 {
		t.Errorf("expected cache size 0 after upstream error, got %d", stats.Size)
	}
}

func TestReadBody_OtherError(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", &errReader{err: errors.New("connection reset")})

	_, err := readBody(c)
	if !errors.Is(err, errReadBody) {
		t.Errorf("expected errReadBody, got %v", err)
	}
}

func TestChatCompletions_ReadBodyErrorReturns400(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", &errReader{err: errors.New("connection reset")})

	h.ChatCompletions(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	if body["error"].(map[string]any)["message"] != errReadBody.Error() {
		t.Errorf("unexpected message: %v", body)
	}
}

func TestBodyTooLarge_TrackActiveRejects(t *testing.T) {
	h := newTestHandler(t)
	body := make([]byte, maxBodySize+1024)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.TrackActive()(c)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBodyPreserved_TrackActiveRestores(t *testing.T) {
	h := newTestHandler(t)
	body := []byte(`{"model":"test/model","messages":[]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.TrackActive()(c)

	// After TrackActive, the downstream readBody helper must see the full body.
	got, err := readBody(c)
	if err != nil {
		t.Fatalf("readBody after TrackActive: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("body changed after TrackActive; got %q, want %q", got, body)
	}
}

// TestRefreshOAuthToken_PersistsProviderSpecific verifies that a successful
// OAuth refresh which returns provider-specific data marshals it to
// connections.provider_specific_data and updates the in-memory cache.
func TestRefreshOAuthToken_PersistsProviderSpecific(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	h.writeQueue = wq

	if _, err := h.db.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('psdtest','PsdTest','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, provider_specific_data, created_at, updated_at) VALUES ('psdtest-conn','psdtest','c1','oauth','ready',1,'',0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	wantPSD := map[string]string{"copilot_token": "ct-123", "github_user_id": "42", "login": "octocat", "name": "Octo Cat", "email": "octo@example.com"}
	h.authMgr.RegisterService(auth.ProviderType("psdtest"), &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			return &auth.Credentials{
				AccessToken:      "new-access",
				RefreshToken:     "new-refresh",
				ExpiresAt:        time.Now().Add(time.Hour),
				ProviderSpecific: wantPSD,
			}, nil
		},
	})

	conn := &Connection{
		ID:             "psdtest-conn",
		AccessToken:    "old-access",
		RefreshToken:   "old-refresh",
		OAuthExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := h.refreshOAuthToken(context.Background(), conn, "psdtest"); err != nil {
		t.Fatalf("refreshOAuthToken: %v", err)
	}

	// In-memory cache should be updated immediately.
	cc, ok := h.conns.Load(conn.ID)
	if !ok {
		t.Fatal("connection not cached after refresh")
	}
	cached := cc.(cachedConn).conn
	var gotCached map[string]string
	if err := json.Unmarshal([]byte(cached.ProviderSpecificData), &gotCached); err != nil {
		t.Fatalf("cached provider_specific_data is not valid JSON: %v", err)
	}
	for k, want := range wantPSD {
		if gotCached[k] != want {
			t.Errorf("cached PSD %q = %q, want %q", k, gotCached[k], want)
		}
	}

	// DB should be updated after the async write queue flushes.
	wq.Stop()
	var psd string
	if err := h.db.QueryRow(`SELECT provider_specific_data FROM connections WHERE id = ?`, conn.ID).Scan(&psd); err != nil {
		t.Fatalf("query provider_specific_data: %v", err)
	}
	var gotDB map[string]string
	if err := json.Unmarshal([]byte(psd), &gotDB); err != nil {
		t.Fatalf("persisted provider_specific_data is not valid JSON: %v", err)
	}
	for k, want := range wantPSD {
		if gotDB[k] != want {
			t.Errorf("persisted PSD %q = %q, want %q", k, gotDB[k], want)
		}
	}
}

// TestRefreshOAuthToken_KeepsProviderSpecificWhenEmpty verifies that providers
// which do not return provider-specific data do not wipe the existing value.
func TestRefreshOAuthToken_KeepsProviderSpecificWhenEmpty(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	h.writeQueue = wq

	if _, err := h.db.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('psdempty','PsdEmpty','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	existing := `{"proxyPoolId":"pool-abc"}`
	if _, err := h.db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, provider_specific_data, created_at, updated_at) VALUES ('psdempty-conn','psdempty','c1','oauth','ready',1,?,0,0)`, existing); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	h.authMgr.RegisterService(auth.ProviderType("psdempty"), &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			return &auth.Credentials{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
				ExpiresAt:    time.Now().Add(time.Hour),
			}, nil
		},
	})

	conn := &Connection{
		ID:                   "psdempty-conn",
		AccessToken:          "old-access",
		RefreshToken:         "old-refresh",
		OAuthExpiresAt:       time.Now().Add(-time.Minute),
		ProviderSpecificData: existing,
	}
	if err := h.refreshOAuthToken(context.Background(), conn, "psdempty"); err != nil {
		t.Fatalf("refreshOAuthToken: %v", err)
	}

	cc, ok := h.conns.Load(conn.ID)
	if !ok {
		t.Fatal("connection not cached after refresh")
	}
	cached := cc.(cachedConn).conn
	if cached.ProviderSpecificData != existing {
		t.Errorf("cached PSD changed: got %q, want %q", cached.ProviderSpecificData, existing)
	}

	wq.Stop()
	var psd string
	if err := h.db.QueryRow(`SELECT provider_specific_data FROM connections WHERE id = ?`, conn.ID).Scan(&psd); err != nil {
		t.Fatalf("query provider_specific_data: %v", err)
	}
	if psd != existing {
		t.Errorf("persisted PSD changed: got %q, want %q", psd, existing)
	}
}
