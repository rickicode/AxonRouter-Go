package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestAPIKeysExpiresAtColumn verifies the api_keys.expires_at migration is
// applied and that re-running migrations is idempotent.
func TestAPIKeysExpiresAtColumn(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	if !hasColumn(t, d, "api_keys", "expires_at") {
		t.Fatal("api_keys table missing expires_at column after migration")
	}

	// Re-running must be idempotent (duplicate column error is ignored).
	if err := RunMigrations(d); err != nil {
		t.Fatalf("re-run migrations failed: %v", err)
	}
}

func hasColumn(t *testing.T, d *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := d.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

// TestValidateSeedPricing enforces the user's hard rule on the pricing seed:
// no duplicate model IDs and no $0 (free-tier) rows.
func TestValidateSeedPricing(t *testing.T) {
	// OK case mirrors the real seed shape (price > 0, unique IDs).
	ok := []struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{
		{"gpt-4o", "GPT-4o", 0.0025, 0.01, 0, 0, 0},
		{"claude-3-opus", "Claude 3 Opus", 0.015, 0.075, 0, 0, 0},
	}
	if err := validateSeedPricing(ok); err != nil {
		t.Fatalf("expected valid seed, got error: %v", err)
	}

	// Duplicate ID must be rejected.
	dup := append([]struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{}, ok...)
	dup = append(dup, struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{"gpt-4o", "GPT-4o dup", 0.0025, 0.01, 0, 0, 0})
	if err := validateSeedPricing(dup); err == nil {
		t.Fatal("expected duplicate model_id error, got nil")
	}

	// $0 input+output (free-tier without real price) must be rejected.
	zero := []struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{
		{"some-free-model", "Free Model", 0, 0, 0, 0, 0},
	}
	if err := validateSeedPricing(zero); err == nil {
		t.Fatal("expected $0 free-tier error, got nil")
	}
}
