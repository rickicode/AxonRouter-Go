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

// TestAPIKeysAllowedModelsColumn verifies the api_keys.allowed_models migration
// is applied and that re-running migrations is idempotent.
func TestAPIKeysAllowedModelsColumn(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	if !hasColumn(t, d, "api_keys", "allowed_models") {
		t.Fatal("api_keys table missing allowed_models column after migration")
	}

	// Re-running must be idempotent (duplicate column error is ignored).
	if err := RunMigrations(d); err != nil {
		t.Fatalf("re-run migrations failed: %v", err)
	}
}

// TestAPIKeyAllowedModelsList verifies AllowedModelsList JSON-unmarshals the
// stored value and returns nil for empty/null/invalid values.
func TestAPIKeyAllowedModelsList(t *testing.T) {
	// Null/empty values return nil.
	nullKey := APIKey{AllowedModels: sql.NullString{Valid: false}}
	if got := nullKey.AllowedModelsList(); got != nil {
		t.Fatalf("null AllowedModels: got %v, want nil", got)
	}

	emptyKey := APIKey{AllowedModels: sql.NullString{String: "", Valid: true}}
	if got := emptyKey.AllowedModelsList(); got != nil {
		t.Fatalf("empty AllowedModels: got %v, want nil", got)
	}

	// Valid JSON array is parsed.
	validKey := APIKey{AllowedModels: sql.NullString{String: `["gpt-4o","claude-3.5-sonnet"]`, Valid: true}}
	got := validKey.AllowedModelsList()
	want := []string{"gpt-4o", "claude-3.5-sonnet"}
	if len(got) != len(want) {
		t.Fatalf("valid AllowedModels: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllowedModelsList()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Invalid JSON is tolerated and returns nil.
	invalidKey := APIKey{AllowedModels: sql.NullString{String: "not-json", Valid: true}}
	if got := invalidKey.AllowedModelsList(); got != nil {
		t.Fatalf("invalid AllowedModels: got %v, want nil", got)
	}
}

func TestConnectionStatusCollapseMigration(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Seed legacy terminal statuses before migrations run.
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS provider_types (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, format TEXT NOT NULL, base_url TEXT NOT NULL, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('grok-cli','Grok CLI','openai','http://x',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS connections (
		id TEXT PRIMARY KEY,
		provider_type_id TEXT NOT NULL,
		name TEXT NOT NULL,
		auth_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'ready',
		is_active INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	seed := []struct {
		id, status string
		active     int
	}{
		{"conn-auth", "auth_failed", 1},
		{"conn-suspended", "suspended", 1},
		{"conn-balance", "balance_empty", 1},
		{"conn-disabled", "disabled", 0},
	}
	for _, s := range seed {
		if _, err := d.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?, 'grok-cli', ?, 'none', ?, ?, 0, 0)`, s.id, s.id, s.status, s.active); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	// Re-running must be idempotent.
	if err := RunMigrations(d); err != nil {
		t.Fatalf("re-run migrations failed: %v", err)
	}

	// No legacy terminal statuses should remain.
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM connections WHERE status IN ('auth_failed','suspended','balance_empty')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("found %d rows with legacy terminal status", count)
	}

	// Verify disabled_reason mapping.
	cases := map[string]string{
		"conn-auth":      "auth_failed",
		"conn-suspended": "suspended",
		"conn-balance":   "balance_empty",
		"conn-disabled":  "unknown",
	}
	for id, want := range cases {
		var status, reason string
		if err := d.QueryRow(`SELECT status, COALESCE(disabled_reason,'') FROM connections WHERE id = ?`, id).Scan(&status, &reason); err != nil {
			t.Fatalf("scan %s: %v", id, err)
		}
		if status != "disabled" {
			t.Errorf("%s status = %q, want disabled", id, status)
		}
		if reason != want {
			t.Errorf("%s disabled_reason = %q, want %q", id, reason, want)
		}
	}

	// All legacy terminal rows must end up inactive.
	for _, id := range []string{"conn-auth", "conn-suspended", "conn-balance"} {
		var active int
		if err := d.QueryRow(`SELECT is_active FROM connections WHERE id = ?`, id).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active != 0 {
			t.Fatalf("%s is_active = %d, want 0", id, active)
		}
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
	wantBaseURL := "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	if baseURL != wantBaseURL {
		t.Errorf("kiro base_url = %q, want %q", baseURL, wantBaseURL)
	}
}

// TestKiroFormatMigration verifies legacy Kiro rows seeded as "openai" get
// repaired to "kiro" and the base URL is updated to the Kiro IDE gateway endpoint.
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
	wantBaseURL := "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
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
		"claude-sonnet-5":   true,
		"claude-sonnet-4.6": true,
		"claude-haiku-4.5":  true,
		"deepseek-3.2":      true,
		"minimax-m2.5":      true,
		"minimax-m2.1":      true,
		"glm-5":             true,
		"qwen3-coder-next":  true,
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

// TestConnectionsOAuthEmailColumn verifies the oauth_email column and the
// partial unique index used to deduplicate OAuth connections by account.
func TestConnectionsOAuthEmailColumn(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	if !hasColumn(t, d, "connections", "oauth_email") {
		t.Fatal("connections table missing oauth_email column after migration")
	}

	var indexExists bool
	if err := d.QueryRow(`
		SELECT COUNT(*) > 0 FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_connections_oauth_account'
	`).Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if !indexExists {
		t.Fatal("missing partial unique index idx_connections_oauth_account")
	}

	if err := RunMigrations(d); err != nil {
		t.Fatalf("re-run migrations failed: %v", err)
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

// TestPricingSeedPreservesOperatorEdits verifies that model_pricing is seeded
// on first run, and that re-running migrations leaves operator edits intact.
func TestPricingSeedPreservesOperatorEdits(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	// Simulate an operator editing a seeded row via the admin UI/API.
	const modelID = "gpt-4o"
	const editedInput = 9.999
	if _, err := d.Exec(`UPDATE model_pricing SET input_per_1k = ?, updated_at = ? WHERE model_id = ?`, editedInput, 9999, modelID); err != nil {
		t.Fatal(err)
	}

	// Add a custom operator-created row not present in the hardcoded seed.
	const customID = "custom-operator-model"
	if _, err := d.Exec(`INSERT INTO model_pricing (model_id, display_name, input_per_1k, output_per_1k, currency, updated_at) VALUES (?, ?, ?, ?, 'USD', ?)`,
		customID, "Custom Operator Model", 0.123, 0.456, 9999); err != nil {
		t.Fatal(err)
	}

	// Re-running migrations must not wipe the edited or custom rows.
	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	var inPrice float64
	if err := d.QueryRow(`SELECT input_per_1k FROM model_pricing WHERE model_id = ?`, modelID).Scan(&inPrice); err != nil {
		t.Fatal(err)
	}
	if inPrice != editedInput {
		t.Errorf("operator-edited price for %s was reset: got %v, want %v", modelID, inPrice, editedInput)
	}

	var customExists bool
	if err := d.QueryRow(`SELECT COUNT(*) > 0 FROM model_pricing WHERE model_id = ?`, customID).Scan(&customExists); err != nil {
		t.Fatal(err)
	}
	if !customExists {
		t.Errorf("operator-created row %s was deleted on migration re-run", customID)
	}
}
