package quota

import (
	"database/sql"
	"testing"

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

func TestUpdateConnectionQuotaStatus_KeepsTerminalState(t *testing.T) {
	db := newCacheTestDB(t)
	defer db.Close()

	store := connstate.NewStore()
	exhaustion := NewExhaustionCache()
	connID := "conn-terminal"

	db.Exec(`INSERT INTO connections (id, status, updated_at) VALUES (?, 'suspended', 0)`, connID)
	store.GetOrCreate(connID).SetStatus(connstate.StatusSuspended, "")

	changed := false
	UpdateConnectionQuotaStatus(db, store, exhaustion, connID, nil, "no projectId for retrieveUserQuota", &changed)

	if changed {
		t.Error("expected no status change for terminal state")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM connections WHERE id = ?`, connID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "suspended" {
		t.Errorf("expected suspended unchanged, got %s", status)
	}
}
