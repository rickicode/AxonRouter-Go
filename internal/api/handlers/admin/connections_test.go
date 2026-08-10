package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/background"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

func jsonBodyConn(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func newConnectionHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "connection-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func newConnectionHandlerForTest(t *testing.T, database *sql.DB, registry *executor.Registry) *ConnectionHandler {
	t.Helper()
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	return &ConnectionHandler{
		db:           database,
		store:        store,
		elig:         elig,
		exhaustion:   quota.NewExhaustionCache(),
		registry:     registry,
		lifecycleMgr: background.NewLifecycleManager(database, 60),
	}
}

func init() {
	logging.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	executor.RegisterDefaults()
	_ = executor.SetValidateURLForTest(func(string) error { return nil })
}

func seedConnection(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?,'test','c1','none','ready',1,?,?)`, id, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
}

// TestRecordTestSuccess_ClearsCooldownAndExhaustion proves that a successful
// admin TestConnection resets the connection to ready and clears in-memory
// exhaustion so routing can reuse it immediately.
func TestRecordTestSuccess_ClearsCooldownAndExhaustion(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, nil)
	seedConnection(t, database, "conn-1")

	// Pre-condition: mark connection exhausted and rate-limited inDB.
	now := time.Now().Unix()
	if _, err := database.Exec(`UPDATE connections SET status='rate_limited', cooldown_until=?, last_error='Rate limit exceeded', last_error_code='rate_limit', failure_count=5 WHERE id='conn-1'`, now+300); err != nil {
		t.Fatalf("setup cooldown: %v", err)
	}
	h.exhaustion.MarkExhausted("conn-1", 5*time.Minute)

	h.recordTestSuccess("conn-1")

	var status string
	var cooldown sql.NullInt64
	var lastError sql.NullString
	var failureCount int
	row := database.QueryRow(`SELECT status, cooldown_until, last_error, failure_count FROM connections WHERE id='conn-1'`)
	if err := row.Scan(&status, &cooldown, &lastError, &failureCount); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "ready" {
		t.Fatalf("status = %q, want ready", status)
	}
	if cooldown.Valid {
		t.Fatalf("cooldown_until should be null, got %v", cooldown)
	}
	if lastError.Valid {
		t.Fatalf("last_error should be null, got %v", lastError)
	}
	if failureCount != 0 {
		t.Fatalf("failure_count = %d, want 0", failureCount)
	}
	if h.exhaustion.IsExhausted("conn-1") {
		t.Fatal("exhaustion cache should be cleared after successful test")
	}
	if cs := h.store.Get("conn-1"); cs == nil || cs.Status != connstate.StatusReady {
		t.Fatalf("in-memory status should be ready, got %v", cs)
	}
}

// TestRecordTestFailure_RateLimit persists cooldown and exhaustion like the
// real proxy path does, so the dashboard reflects the failure.
func TestRecordTestFailure_RateLimit(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, nil)
	seedConnection(t, database, "conn-1")

	cooldown := time.Now().Add(5 * time.Minute)
	det := connstate.ErrorDetection{
		Category:      connstate.ErrorRateLimit,
		Message:       "Rate limit exceeded",
		Status:        connstate.StatusRateLimited,
		CooldownUntil: &cooldown,
	}
	h.recordTestFailure("conn-1", det)

	var status string
	var cooldownU sql.NullInt64
	var lastError sql.NullString
	var failureCount int
	row := database.QueryRow(`SELECT status, cooldown_until, last_error, failure_count FROM connections WHERE id='conn-1'`)
	if err := row.Scan(&status, &cooldownU, &lastError, &failureCount); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "rate_limited" {
		t.Fatalf("status = %q, want rate_limited", status)
	}
	if !cooldownU.Valid || cooldownU.Int64 == 0 {
		t.Fatalf("cooldown_until not persisted: %+v", cooldownU)
	}
	if lastError.String != "Rate limit exceeded" {
		t.Fatalf("last_error = %q", lastError.String)
	}
	if failureCount != 1 {
		t.Fatalf("failure_count = %d, want 1", failureCount)
	}
	if !h.exhaustion.IsExhausted("conn-1") {
		t.Fatal("exhaustion cache should be marked for rate-limit failure")
	}
}

// TestRecordTestFailure_Auth persists status/error without a cooldown window.
func TestRecordTestFailure_Auth(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, nil)
	seedConnection(t, database, "conn-1")

	det := connstate.ErrorDetection{
		Category: connstate.ErrorAuth,
		Message:  "Invalid API key",
		Status:   connstate.StatusDisabled,
	}
	h.recordTestFailure("conn-1", det)

	var status string
	var reason string
	var isActive int
	var cooldownU sql.NullInt64
	var failureCount int
	row := database.QueryRow(`SELECT status, COALESCE(disabled_reason,''), is_active, cooldown_until, failure_count FROM connections WHERE id='conn-1'`)
	if err := row.Scan(&status, &reason, &isActive, &cooldownU, &failureCount); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "disabled" {
		t.Fatalf("status = %q, want disabled", status)
	}
	if reason != "auth_failed" {
		t.Fatalf("disabled_reason = %q, want auth_failed", reason)
	}
	if isActive != 0 {
		t.Fatalf("is_active = %d, want 0", isActive)
	}
	if cooldownU.Valid {
		t.Fatalf("cooldown_until should be null for auth failure, got %v", cooldownU)
	}
	if failureCount != 1 {
		t.Fatalf("failure_count = %d, want 1", failureCount)
	}
	if h.exhaustion.IsExhausted("conn-1") {
		t.Fatal("auth failure should not mark connection exhausted")
	}
}

// TestBulkUpdate_ThroughWriteQueue verifies BulkUpdate routes its write through
// db.WriteQueue and that both the DB row and the in-memory store reflect the
// change after the queued write commits.
func TestBulkUpdate_ThroughWriteQueue(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	wq := db.NewWriteQueue(database)
	defer wq.Stop()

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := &ConnectionHandler{
		db:         database,
		store:      store,
		elig:       elig,
		exhaustion: quota.NewExhaustionCache(),
		writeQueue: wq,
	}
	seedConnection(t, database, "conn-1")
	h.store.UpdateStatus("conn-1", connstate.StatusReady)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/connections/bulk-update", jsonBodyConn(t, map[string]any{
		"ids":    []any{"conn-1"},
		"action": "disable",
	}))
	h.BulkUpdate(c)

	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdate status = %d, body=%s", w.Code, w.Body.String())
	}

	var isActive string
	row := database.QueryRow(`SELECT is_active FROM connections WHERE id='conn-1'`)
	if err := row.Scan(&isActive); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if isActive != "0" {
		t.Fatalf("is_active = %q, want 0 after disable", isActive)
	}
	if cs := h.store.Get("conn-1"); cs == nil || cs.Status != connstate.StatusDisabled {
		t.Fatalf("in-memory status should be disabled, got %v", cs)
	}
}

func seedGrokCLIConnection(t *testing.T, database *sql.DB, id, baseURL string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('grok-cli','Grok CLI','grok-cli',?,0)`, baseURL); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`UPDATE provider_types SET base_url = ? WHERE id = 'grok-cli'`, baseURL); err != nil {
		t.Fatalf("update provider_type base_url: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, oauth_token, created_at, updated_at) VALUES (?, 'grok-cli', 'grok', 'oauth', 'ready', 1, 'grok-at-123', ?, ?)`, id, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
}

// TestTestConnection_GrokCLI_402SoftSuccess proves that a Grok CLI connection
// test which receives HTTP 402 (credits/quota exhausted) is treated as a soft
// success: the connection status stays ready and the response explains auth is
// valid but credits/quota are exhausted.
func TestTestConnection_GrokCLI_402SoftSuccess(t *testing.T) {
	errBody, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "Spending limit reached"}})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write(errBody)
	}))
	defer ts.Close()

	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, executor.GetRegistry())
	seedGrokCLIConnection(t, database, "grok-conn-1", ts.URL)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/grok-conn-1/test", nil)
	c.Params = gin.Params{{Key: "id", Value: "grok-conn-1"}}
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
	msg, _ := resp["message"].(string)
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "auth") || !strings.Contains(lower, "credit") && !strings.Contains(lower, "quota") {
		t.Fatalf("message does not explain auth success and exhausted credits/quota: %q", msg)
	}

	var status string
	row := database.QueryRow(`SELECT status FROM connections WHERE id='grok-conn-1'`)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "ready" {
		t.Fatalf("status=%q, want ready", status)
	}
	if cs := h.store.Get("grok-conn-1"); cs == nil || cs.Status != connstate.StatusReady {
		t.Fatalf("in-memory status should be ready, got %v", cs)
	}
}

// TestTestConnection_GrokCLI_401StillFails verifies that 401/403 auth errors
// are NOT masked as soft success and still mark the connection failed.
func TestTestConnection_GrokCLI_401StillFails(t *testing.T) {
	errBody, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "Unauthorized"}})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(errBody)
	}))
	defer ts.Close()

	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, executor.GetRegistry())
	seedGrokCLIConnection(t, database, "grok-conn-auth", ts.URL)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/grok-conn-auth/test", nil)
	c.Params = gin.Params{{Key: "id", Value: "grok-conn-auth"}}
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

	var status, reason string
	row := database.QueryRow(`SELECT status, COALESCE(disabled_reason,'') FROM connections WHERE id='grok-conn-auth'`)
	if err := row.Scan(&status, &reason); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "disabled" {
		t.Fatalf("status=%q, want disabled", status)
	}
	if reason != "auth_failed" {
		t.Fatalf("disabled_reason=%q, want auth_failed", reason)
	}
}

// TestList_IncludesDisabledConnections verifies that disabled (inactive) rows
// are included in the default list so the provider detail page stays consistent
// with the provider card counts.
func TestList_IncludesDisabledConnections(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, nil)
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('testp','Test','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-ready','testp','Ready','none','ready',1,?,?)`, now, now); err != nil {
		t.Fatalf("seed ready: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, disabled_reason, created_at, updated_at) VALUES ('conn-disabled','testp','Disabled','none','disabled',0,'auth_failed',?,?)`, now, now); err != nil {
		t.Fatalf("seed disabled: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/providers/testp/connections", nil)
	c.Params = gin.Params{{Key: "id", Value: "testp"}}
	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data       []map[string]any `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Pagination.Total != 2 {
		t.Fatalf("total = %d, want 2", body.Pagination.Total)
	}
	ids := make(map[string]bool)
	for _, conn := range body.Data {
		ids[conn["id"].(string)] = true
	}
	if !ids["conn-ready"] {
		t.Fatal("missing conn-ready")
	}
	if !ids["conn-disabled"] {
		t.Fatal("missing conn-disabled")
	}
}

// qoderMockExecutor records whether ExecuteStream was called and returns a
// trivial successful stream so the connection test can complete after validation.
type qoderMockExecutor struct {
	called bool
}

func (m *qoderMockExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *qoderMockExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	m.called = true
	ch := make(chan executor.StreamChunk, 1)
	ch <- executor.StreamChunk{Payload: []byte(`{"ok":true}`)}
	close(ch)
	return &executor.StreamResult{Chunks: ch, StatusCode: http.StatusOK}, nil
}

func registerQoderMockExecutor(t *testing.T, mock *qoderMockExecutor) {
	t.Helper()
	reg := executor.GetRegistry()
	origExec, origFormat, _ := reg.Get("qoder")
	reg.Register("qoder", executor.FormatQoder, mock)
	t.Cleanup(func() {
		if origExec != nil {
			reg.Register("qoder", origFormat, origExec)
		}
	})
}

func seedQoderConnection(t *testing.T, database *sql.DB, id, authType, apiKey, accessToken, psd string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('qoder','Qoder','qoder','',?)`, now); err != nil {
		t.Fatalf("seed provider_type qoder: %v", err)
	}
	psdValue := psd
	if psdValue == "" {
		psdValue = "{}"
	}
	if _, err := database.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, oauth_token, provider_specific_data, status, is_active, created_at, updated_at)
		VALUES (?, 'qoder', ?, ?, ?, ?, ?, 'ready', 1, ?, ?)
	`, id, id, authType, apiKey, accessToken, psdValue, now, now); err != nil {
		t.Fatalf("seed qoder connection: %v", err)
	}
}

func qoderValidationTestServer(t *testing.T, exchangeStatus, apiStatus int, exchangeCalled *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/jobToken/exchange":
			if exchangeCalled != nil {
				*exchangeCalled = true
			}
			w.Header().Set("Content-Type", "application/json")
			if exchangeStatus != http.StatusOK {
				w.WriteHeader(exchangeStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"job_token": "jt-exchanged"})
		case "/compatible-mode/v1/models":
			w.Header().Set("Content-Type", "application/json")
			if apiStatus != http.StatusOK {
				w.WriteHeader(apiStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestTestConnection_QoderValidation covers Qoder-specific pre-flight checks:
// PAT exchange, API token DashScope validation, auth failures, and provider errors.
func TestTestConnection_QoderValidation(t *testing.T) {
	quota.ClearQoderJobTokenCache()
	t.Cleanup(quota.ClearQoderJobTokenCache)

	cases := []struct {
		name        string
		apiKey      string
		wantStatus  string
		wantReason  string
		wantMockHit bool
		exchangeSC  int
		apiSC       int
	}{
		{
			name:        "pat_exchange_success",
			apiKey:      "pt-good-token",
			wantStatus:  "ok",
			wantMockHit: true,
			exchangeSC:  http.StatusOK,
		},
		{
			name:        "pat_exchange_401_marks_auth_failed",
			apiKey:      "pt-bad-token",
			wantStatus:  "failed",
			wantReason:  "auth_failed",
			wantMockHit: false,
			exchangeSC:  http.StatusUnauthorized,
		},
		{
			name:        "api_token_models_success",
			apiKey:      "ak-good-token",
			wantStatus:  "ok",
			wantMockHit: true,
			apiSC:       http.StatusOK,
		},
		{
			name:        "api_token_403_marks_auth_failed",
			apiKey:      "ak-bad-token",
			wantStatus:  "failed",
			wantReason:  "auth_failed",
			wantMockHit: false,
			apiSC:       http.StatusForbidden,
		},
		{
			name:        "api_token_503_marks_provider_error",
			apiKey:      "ak-err-token",
			wantStatus:  "failed",
			wantReason:  "provider_error",
			wantMockHit: false,
			apiSC:       http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var exchangeCalled bool
			ts := qoderValidationTestServer(t, tc.exchangeSC, tc.apiSC, &exchangeCalled)
			defer ts.Close()
			t.Setenv("QODER_JOB_TOKEN_EXCHANGE_URL", ts.URL+"/api/v1/jobToken/exchange")
			t.Setenv("QODER_MODELS_VALIDATION_URL", ts.URL+"/compatible-mode/v1/models")

			database := newConnectionHandlerTestDB(t)
			mock := &qoderMockExecutor{}
			registerQoderMockExecutor(t, mock)
			h := newConnectionHandlerForTest(t, database, executor.GetRegistry())

			connID := "qoder-conn-" + tc.name
			seedQoderConnection(t, database, connID, "api_key", tc.apiKey, "", "{}")

			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/admin/connections/"+connID+"/test", nil)
			c.Params = gin.Params{{Key: "id", Value: connID}}
			h.TestConnection(c)

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp["status"] != tc.wantStatus {
				t.Errorf("response status=%v, want %v", resp["status"], tc.wantStatus)
			}
			if mock.called != tc.wantMockHit {
				t.Errorf("executor called=%v, want %v", mock.called, tc.wantMockHit)
			}

			var status, reason string
			row := database.QueryRow(`SELECT status, COALESCE(disabled_reason,'') FROM connections WHERE id = ?`, connID)
			if err := row.Scan(&status, &reason); err != nil {
				t.Fatalf("scan db status: %v", err)
			}
			if tc.wantStatus == "ok" && status != "ready" {
				t.Errorf("db status=%q, want ready", status)
			}
			if tc.wantStatus == "failed" {
				if status != "disabled" {
					t.Errorf("db status=%q, want disabled", status)
				}
				if reason != tc.wantReason {
					t.Errorf("disabled_reason=%q, want %q", reason, tc.wantReason)
				}
				// Token must never leak into error responses.
				body := w.Body.String()
				if strings.Contains(body, tc.apiKey) {
					t.Errorf("response body leaked token: %s", body)
				}
			}
			if strings.HasPrefix(tc.apiKey, "pt-") && tc.exchangeSC != 0 {
				if !exchangeCalled {
					t.Errorf("PAT exchange endpoint was not called")
				}
			}
		})
	}
}
