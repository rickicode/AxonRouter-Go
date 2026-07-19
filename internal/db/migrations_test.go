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

// TestKiroProviderTypeFormat verifies Kiro is seeded with the "kiro" format
// (not the legacy "openai" value) and the canonical AWS upstream base URL.
func TestKiroProviderTypeFormat(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	var format, baseURL string
	if err := d.QueryRow("SELECT format, base_url FROM provider_types WHERE id = 'kiro'").Scan(&format, &baseURL); err != nil {
		t.Fatal(err)
	}
	if format != "kiro" {
		t.Errorf("kiro format = %q, want 'kiro'", format)
	}
	wantBaseURL := "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	if baseURL != wantBaseURL {
		t.Errorf("kiro base_url = %q, want %q", baseURL, wantBaseURL)
	}
}

// TestKiroFormatMigration verifies legacy Kiro rows seeded as "openai" get
// repaired to "kiro" and the base URL is updated to the AWS endpoint.
func TestKiroFormatMigration(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Create the schema and seed the canonical Kiro row.
	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	// Simulate a legacy row from an older build.
	if _, err := d.Exec(`UPDATE provider_types SET format = 'openai', base_url = 'https://api.kiro.ai/v1' WHERE id = 'kiro'`); err != nil {
		t.Fatal(err)
	}

	// Re-run migrations: the repair step must fix the legacy row.
	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	var format, baseURL string
	if err := d.QueryRow("SELECT format, base_url FROM provider_types WHERE id = 'kiro'").Scan(&format, &baseURL); err != nil {
		t.Fatal(err)
	}
	if format != "kiro" {
		t.Errorf("kiro format after migration = %q, want 'kiro'", format)
	}
	wantBaseURL := "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	if baseURL != wantBaseURL {
		t.Errorf("kiro base_url after migration = %q, want %q", baseURL, wantBaseURL)
	}
}

// TestKiroPricingSeed verifies the Kiro verified base model IDs are seeded in
// model_pricing with non-zero input/output prices.
func TestKiroPricingSeed(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"claude-sonnet-5":    true,
		"claude-sonnet-4.6":  true,
		"claude-haiku-4.5":   true,
		"deepseek-3.2":       true,
		"minimax-m2.5":       true,
		"minimax-m2.1":       true,
		"glm-5":              true,
		"qwen3-coder-next":   true,
	}

	rows, err := d.Query("SELECT model_id, display_name, input_per_1k, output_per_1k FROM model_pricing WHERE model_id IN ('claude-sonnet-5','claude-sonnet-4.6','claude-haiku-4.5','deepseek-3.2','minimax-m2.5','minimax-m2.1','glm-5','qwen3-coder-next')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, name string
		var inPrice, outPrice float64
		if err := rows.Scan(&id, &name, &inPrice, &outPrice); err != nil {
			t.Fatal(err)
		}
		if name == "" {
			t.Errorf("%s: display_name is empty", id)
		}
		if inPrice == 0 || outPrice == 0 {
			t.Errorf("%s: input_per_1k=%v output_per_1k=%v, both must be non-zero", id, inPrice, outPrice)
		}
		delete(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for id := range want {
			missing = append(missing, id)
		}
		t.Errorf("missing Kiro pricing rows: %v", missing)
	}
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
