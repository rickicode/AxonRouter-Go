package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newUpsertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "upsert.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestUpsertOAuthConnection_InsertsNewConnection(t *testing.T) {
	d := newUpsertTestDB(t)
	now := time.Now().Unix()

	id, created, err := UpsertOAuthConnection(context.Background(), d, "cx", "new@example.com", "New", "at-1", "rt-1", now+3600, sql.NullString{String: `{"k":"v"}`, Valid: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected created=true for new account")
	}
	if id == "" {
		t.Fatal("expected connection id")
	}

	var email, name, accessTok, refreshTok string
	var expiresAt int64
	var psd string
	if err := d.QueryRow(`
		SELECT oauth_email, name, COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0), COALESCE(provider_specific_data,'')
		FROM connections WHERE id = ?
	`, id).Scan(&email, &name, &accessTok, &refreshTok, &expiresAt, &psd); err != nil {
		t.Fatal(err)
	}
	if email != "new@example.com" {
		t.Errorf("oauth_email = %q, want new@example.com", email)
	}
	if name != "New" {
		t.Errorf("name = %q, want New", name)
	}
	if accessTok != "at-1" {
		t.Errorf("access token = %q", accessTok)
	}
	if refreshTok != "rt-1" {
		t.Errorf("refresh token = %q", refreshTok)
	}
	if expiresAt == 0 {
		t.Error("expected non-zero expires_at")
	}
	if psd == "" {
		t.Error("expected provider_specific_data")
	}
}

func TestUpsertOAuthConnection_UpdatesExistingConnection(t *testing.T) {
	d := newUpsertTestDB(t)
	now := time.Now().Unix()

	firstID, _, err := UpsertOAuthConnection(context.Background(), d, "cx", "same@example.com", "Same", "at-old", "rt-old", now+3600, sql.NullString{}, now)
	if err != nil {
		t.Fatal(err)
	}

	now = time.Now().Unix()
	secondID, created, err := UpsertOAuthConnection(context.Background(), d, "cx", "same@example.com", "Renamed", "at-new", "rt-new", now+7200, sql.NullString{String: `{"updated":true}`, Valid: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected created=false for duplicate account")
	}
	if secondID != firstID {
		t.Fatalf("expected same connection id, got %q and %q", firstID, secondID)
	}

	var accessTok, refreshTok, psd, status string
	var isActive int
	if err := d.QueryRow(`
		SELECT COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(provider_specific_data,''), status, is_active
		FROM connections WHERE id = ?
	`, secondID).Scan(&accessTok, &refreshTok, &psd, &status, &isActive); err != nil {
		t.Fatal(err)
	}
	if accessTok != "at-new" {
		t.Errorf("access token = %q, want at-new", accessTok)
	}
	if refreshTok != "rt-new" {
		t.Errorf("refresh token = %q, want rt-new", refreshTok)
	}
	if psd != `{"updated":true}` {
		t.Errorf("provider_specific_data = %q", psd)
	}
	if status != "ready" {
		t.Errorf("status = %q, want ready", status)
	}
	if isActive != 1 {
		t.Errorf("is_active = %d, want 1", isActive)
	}
}

func TestUpsertOAuthConnection_DifferentProvidersAreNotDuplicates(t *testing.T) {
	d := newUpsertTestDB(t)
	now := time.Now().Unix()

	_, _, err := UpsertOAuthConnection(context.Background(), d, "cx", "shared@example.com", "Codex", "at-1", "rt-1", now+3600, sql.NullString{}, now)
	if err != nil {
		t.Fatal(err)
	}
	id2, created, err := UpsertOAuthConnection(context.Background(), d, "ag", "shared@example.com", "Antigravity", "at-2", "rt-2", now+3600, sql.NullString{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected separate connection for different provider")
	}

	var providerID string
	if err := d.QueryRow("SELECT provider_type_id FROM connections WHERE id = ?", id2).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if providerID != "ag" {
		t.Errorf("provider_type_id = %q, want ag", providerID)
	}
}

func TestUpsertOAuthConnection_EmptyAccountKeyAlwaysInserts(t *testing.T) {
	d := newUpsertTestDB(t)
	now := time.Now().Unix()

	id1, created1, err := UpsertOAuthConnection(context.Background(), d, "grok-cli", "", "NoAccount1", "at-1", "rt-1", now+3600, sql.NullString{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created1 {
		t.Fatal("expected created=true when no account key")
	}
	id2, created2, err := UpsertOAuthConnection(context.Background(), d, "grok-cli", "", "NoAccount2", "at-2", "rt-2", now+3600, sql.NullString{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created2 {
		t.Fatal("expected another insert when no account key")
	}
	if id1 == id2 {
		t.Fatal("expected distinct ids when no account key")
	}
}
