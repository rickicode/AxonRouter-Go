package admin

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

func TestRecordTestFailure_QuotaFromDisabledBecomesExhausted(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	dbConn.Exec(`CREATE TABLE connections (
		id TEXT PRIMARY KEY,
		status TEXT,
		is_active INTEGER,
		disabled_reason TEXT,
		cooldown_until INTEGER,
		last_error TEXT,
		last_error_code TEXT,
		failure_count INTEGER DEFAULT 0,
		last_failure_at INTEGER,
		last_success_at INTEGER,
		updated_at INTEGER
	)`)

	connID := "cf-1"
	now := time.Now().Unix()
	_, err = dbConn.Exec(`INSERT INTO connections (id, status, is_active, disabled_reason, cooldown_until, failure_count, updated_at)
		VALUES (?, 'disabled', 0, 'unknown', NULL, 2, ?)`, connID, now)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	store := connstate.NewStore()
	store.SeedConnection(connID, "cf", "disabled", 0)
	store.Get(connID).SetStatus(connstate.StatusDisabled, "unknown")

	ex := quota.NewExhaustionCache()
	h := &ConnectionHandler{
		db:         dbConn,
		store:      store,
		exhaustion: ex,
		elig:       connstate.NewEligibilityManager(store),
	}

	until := time.Now().Add(time.Hour)
	det := connstate.ErrorDetection{
		Category:       connstate.ErrorQuota,
		Status:         connstate.StatusQuotaExhausted,
		Message:        "you have used up your daily free allocation of 10,000 neurons",
		CooldownUntil:  &until,
		Scope:          "connection",
	}

	h.recordTestFailure(connID, det)

	cs := store.Get(connID)
	if cs.GetStatus() != connstate.StatusQuotaExhausted {
		t.Errorf("expected quota_exhausted, got %s", cs.GetStatus())
	}

	var dbStatus string
	var isActive int
	dbConn.QueryRow("SELECT status, is_active FROM connections WHERE id=?", connID).Scan(&dbStatus, &isActive)
	if dbStatus != "quota_exhausted" {
		t.Errorf("expected db status quota_exhausted, got %s", dbStatus)
	}
	if isActive != 1 {
		t.Errorf("expected is_active=1, got %d", isActive)
	}
}
