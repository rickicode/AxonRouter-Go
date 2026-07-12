package admin

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

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

func newConnectionHandlerForTest(t *testing.T, database *sql.DB) *ConnectionHandler {
	t.Helper()
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	return &ConnectionHandler{
		db:         database,
		store:      store,
		elig:       elig,
		exhaustion: quota.NewExhaustionCache(),
	}
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
	h := newConnectionHandlerForTest(t, database)
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
	h := newConnectionHandlerForTest(t, database)
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
	h := newConnectionHandlerForTest(t, database)
	seedConnection(t, database, "conn-1")

	det := connstate.ErrorDetection{
		Category: connstate.ErrorAuth,
		Message:  "Invalid API key",
		Status:   connstate.StatusAuthFailed,
	}
	h.recordTestFailure("conn-1", det)

	var status string
	var cooldownU sql.NullInt64
	var failureCount int
	row := database.QueryRow(`SELECT status, cooldown_until, failure_count FROM connections WHERE id='conn-1'`)
	if err := row.Scan(&status, &cooldownU, &failureCount); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "auth_failed" {
		t.Fatalf("status = %q, want auth_failed", status)
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
