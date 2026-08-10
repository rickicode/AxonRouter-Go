package quota

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	_ "modernc.org/sqlite"
)

func newCacheTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE connections (
		id TEXT PRIMARY KEY,
		status TEXT,
		is_active INTEGER,
		disabled_reason TEXT,
		updated_at INTEGER
	)`)
	db.Exec(`CREATE TABLE quota_cache (
		id TEXT PRIMARY KEY,
		connection_id TEXT,
		provider_type_id TEXT,
		connection_name TEXT,
		plan TEXT,
		quotas TEXT,
		status TEXT,
		error TEXT,
		fetched_at INTEGER,
		updated_at INTEGER
	)`)
	return db
}

func TestUpdateConnectionQuotaStatus_DisablesOnAuthError(t *testing.T) {
	db := newCacheTestDB(t)
	defer db.Close()

	store := connstate.NewStore()
	exhaustion := NewExhaustionCache()
	connID := "conn-auth"

	db.Exec(`INSERT INTO connections (id, status, updated_at) VALUES (?, 'ready', 0)`, connID)
	store.GetOrCreate(connID).SetStatus(connstate.StatusReady, "")

	changed := false
	UpdateConnectionQuotaStatus(db, store, exhaustion, connID, nil, "token expired or access denied (HTTP 403)", &changed)

	if !changed {
		t.Error("expected status change to be recorded")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM connections WHERE id = ?`, connID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "disabled" {
		t.Errorf("expected disabled, got %s", status)
	}
	if store.Get(connID).GetStatus() != connstate.Status("disabled") {
		t.Errorf("expected connstate disabled, got %s", store.Get(connID).GetStatus())
	}
}

func TestNextProviderResets(t *testing.T) {
	db := newCacheTestDB(t)
	defer db.Close()

	// cx has two reset windows; the earliest future one should win.
	now := time.Now()
	earliestReset := now.Add(24 * time.Hour).Format(time.RFC3339)
	cxQuotas := fmt.Sprintf(`[{"name":"5h","used":1,"total":10,"remaining_pct":90,"reset_at":%q},{"name":"7d","used":1,"total":100,"remaining_pct":99,"reset_at":%q}]`,
		earliestReset, now.Add(7*24*time.Hour).Format(time.RFC3339))
	// ag only has a past reset.
	agQuotas := fmt.Sprintf(`[{"name":"daily","used":5,"total":10,"remaining_pct":50,"reset_at":%q}]`,
		now.Add(-24*time.Hour).Format(time.RFC3339))

	nowUnix := now.Unix()
	db.Exec(`INSERT INTO quota_cache (id, connection_id, provider_type_id, connection_name, plan, quotas, status, fetched_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cx-1", "conn-cx", "cx", "Codex 1", "plus", cxQuotas, "ok", nowUnix, nowUnix)
	db.Exec(`INSERT INTO quota_cache (id, connection_id, provider_type_id, connection_name, plan, quotas, status, fetched_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ag-1", "conn-ag", "ag", "AG 1", "pro", agQuotas, "ok", nowUnix, nowUnix)

	resets, err := NextProviderResets(db)
	if err != nil {
		t.Fatalf("NextProviderResets: %v", err)
	}

	if _, ok := resets["ag"]; ok {
		t.Errorf("expected no future reset for ag, got %v", resets["ag"])
	}
	cxReset, ok := resets["cx"]
	if !ok {
		t.Fatalf("expected future reset for cx")
	}
	if !strings.Contains(cxReset, earliestReset) {
		t.Errorf("expected earliest future reset, got %s", cxReset)
	}
}

func TestUpdateConnectionQuotaStatus_KeepsTerminalState(t *testing.T) {
	db := newCacheTestDB(t)
	defer db.Close()

	store := connstate.NewStore()
	exhaustion := NewExhaustionCache()
	connID := "conn-terminal"

	db.Exec(`INSERT INTO connections (id, status, updated_at) VALUES (?, 'disabled', 0)`, connID)
	store.GetOrCreate(connID).SetStatus(connstate.StatusDisabled, "")

	changed := false
	UpdateConnectionQuotaStatus(db, store, exhaustion, connID, nil, "no projectId for retrieveUserQuota", &changed)

	if changed {
		t.Error("expected no status change for terminal state")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM connections WHERE id = ?`, connID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "disabled" {
		t.Errorf("expected disabled unchanged, got %s", status)
	}
}
