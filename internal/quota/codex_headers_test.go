package quota

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	_ "modernc.org/sqlite"
)

func TestParseCodexQuotaHeaders_CodexDualWindow(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-5h-usage", "12000")
	h.Set("x-codex-5h-limit", "100000")
	h.Set("x-codex-5h-reset-at", "2026-07-13T18:00:00Z")
	h.Set("x-codex-7d-usage", "240000")
	h.Set("x-codex-7d-limit", "1000000")
	h.Set("x-codex-7d-reset-at", "2026-07-20T18:00:00Z")

	qs := ParseCodexQuotaHeaders(h)
	if len(qs) != 2 {
		t.Fatalf("expected 2 quotas, got %d", len(qs))
	}
	five, seven := qs[0], qs[1]
	if five.Name != "5h Window" || five.Used != 12000 || five.Total != 100000 {
		t.Errorf("5h mismatch: %+v", five)
	}
	if five.RemainingPct < 87 || five.RemainingPct > 89 {
		t.Errorf("expected ~88%% remaining, got %.2f", five.RemainingPct)
	}
	if seven.Name != "7d Window" || seven.Used != 240000 || seven.Total != 1000000 {
		t.Errorf("7d mismatch: %+v", seven)
	}
	if seven.ResetAt != "2026-07-20T18:00:00Z" {
		t.Errorf("unexpected 7d reset: %s", seven.ResetAt)
	}
}

func TestParseCodexQuotaHeaders_5hOnly(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-5h-usage", "50000")
	h.Set("x-codex-5h-limit", "100000")
	qs := ParseCodexQuotaHeaders(h)
	if len(qs) != 1 || qs[0].Name != "5h Window" {
		t.Fatalf("expected single 5h quota, got %+v", qs)
	}
	if qs[0].RemainingPct != 50 {
		t.Errorf("expected 50%%, got %.2f", qs[0].RemainingPct)
	}
}

func TestParseCodexQuotaHeaders_GenericFallback(t *testing.T) {
	h := http.Header{}
	h.Set("X-Ratelimit-Limit-Requests", "100")
	h.Set("X-Ratelimit-Remaining-Requests", "90")
	h.Set("X-Ratelimit-Reset-Requests", "2026-07-13T18:00:00Z")

	qs := ParseCodexQuotaHeaders(h)
	if len(qs) != 1 || qs[0].Name != "RateLimit requests" {
		t.Fatalf("expected generic fallback, got %+v", qs)
	}
	if qs[0].Used != 10 || qs[0].Total != 100 || qs[0].RemainingPct != 90 {
		t.Errorf("unexpected values: %+v", qs[0])
	}
}

func TestParseCodexQuotaHeaders_Empty(t *testing.T) {
	if qs := ParseCodexQuotaHeaders(http.Header{}); len(qs) != 0 {
		t.Fatalf("expected 0 quotas, got %d", len(qs))
	}
}

func TestSnapshotFromHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-5h-usage", "1")
	h.Set("x-codex-5h-limit", "2")
	h.Set("x-codex-5h-reset-at", "2026-07-13T18:00:00Z")
	h.Set("x-codex-7d-usage", "3")
	h.Set("x-codex-7d-limit", "4")
	h.Set("x-codex-7d-reset-at", "2026-07-20T18:00:00Z")

	s := SnapshotFromHeaders(h)
	if s == nil {
		t.Fatal("expected snapshot")
	}
	if s.Usage5h != 1 || s.Limit5h != 2 || s.Usage7d != 3 || s.Limit7d != 4 {
		t.Errorf("snapshot values mismatch: %+v", s)
	}
	if s.ResetAt7d != "2026-07-20T18:00:00Z" {
		t.Errorf("unexpected 7d reset: %s", s.ResetAt7d)
	}
}

func TestSaveCodexHeaderQuota_MergesExisting(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`CREATE TABLE IF NOT EXISTS quota_cache (
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
	connID := "conn-123"

	// Seed existing cache with a session window from /wham/usage.
	db.Exec(`INSERT INTO quota_cache (id, connection_id, provider_type_id, connection_name, plan, quotas, status, error, fetched_at, updated_at)
		VALUES (?, ?, 'cx', 'Test', 'plus', ?, 'ok', '', 1, 1)`,
		connID, connID, `[{"name":"Session","used":10,"total":100,"remaining_pct":90,"scope":"codex"}]`)

	h := http.Header{}
	h.Set("x-codex-5h-usage", "12000")
	h.Set("x-codex-5h-limit", "100000")
	h.Set("x-codex-5h-reset-at", "2026-07-13T18:00:00Z")

	SaveCodexHeaderQuota(db, connID, "cx", "Test", "plus", h)

	var raw string
	if err := db.QueryRow(`SELECT quotas FROM quota_cache WHERE id = ?`, connID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var items []QuotaItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 merged quotas, got %d: %s", len(items), raw)
	}
	hasSession := false
	has5h := false
	for _, q := range items {
		if q.Name == "Session" {
			hasSession = true
		}
		if q.Name == "5h Window" {
			has5h = true
		}
	}
	if !hasSession || !has5h {
		t.Errorf("missing merged window: session=%v 5h=%v; raw=%s", hasSession, has5h, raw)
	}
}
