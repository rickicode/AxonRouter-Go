package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

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

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	store := connstate.NewStore()
	store.SeedConnection("conn-1", "test", "ready", 0)
	mgr := auth.NewManager()
	return &Handler{
		db:         openTestDB(t),
		store:      store,
		elig:       connstate.NewEligibilityManager(store),
		authMgr:    mgr,
		exhaustion: quota.NewExhaustionCache(),
		providerCfg: providercfg.NewManager(t.TempDir()),
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

func TestWriteUpstreamClientError_WritesTranslatedError(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{"error":{"message":"context too long","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	upErr := &executor.UpstreamError{StatusCode: http.StatusBadRequest, Body: body}

	if !h.writeUpstreamClientError(c, upErr, nil, "cf", "@cf/meta/llama-3.3-70b", time.Now(), false) {
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

	if h.writeUpstreamClientError(c, upErr, nil, "cf", "model", time.Now(), false) {
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
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_value, created_at) VALUES ('test-key-1', 'test-key', 'sk-test', 0)`); err != nil {
			t.Fatalf("seed api_key: %v", err)
		}
		if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('test-key-1', 0, 0)`); err != nil {
			t.Fatalf("seed api_key_usage: %v", err)
		}

		// Create a gin test context with api_key_id set.
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
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
		h.streamResponse(c, result, conn, "test", "test-model",
			executor.FormatOpenAI, executor.FormatOpenAI,
			dummyReq, dummyReq,
			func(err error) []byte { return []byte(err.Error()) },
			time.Now(),
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
