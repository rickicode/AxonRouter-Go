package kiro

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestValidateDiscoveredCredential(t *testing.T) {
	t.Run("valid AWS token", func(t *testing.T) {
		res := &AutoImportResult{Found: true, AccessToken: "at", RefreshToken: "aorAAAAAGvalid"}
		if err := ValidateDiscoveredCredential(res); err != nil {
			t.Fatalf("expected valid: %v", err)
		}
	})
	t.Run("missing refresh token", func(t *testing.T) {
		res := &AutoImportResult{Found: true, AccessToken: "at"}
		if err := ValidateDiscoveredCredential(res); err == nil || !strings.Contains(err.Error(), "refresh token") {
			t.Fatalf("expected refresh token error, got %v", err)
		}
	})
	t.Run("missing access token", func(t *testing.T) {
		res := &AutoImportResult{Found: true, RefreshToken: "aorAAAAAGvalid"}
		if err := ValidateDiscoveredCredential(res); err == nil || !strings.Contains(err.Error(), "access token") {
			t.Fatalf("expected access token error, got %v", err)
		}
	})
	t.Run("external_idp without token endpoint", func(t *testing.T) {
		res := &AutoImportResult{Found: true, AccessToken: "at", RefreshToken: "rt", AuthMethod: "external_idp"}
		if err := ValidateDiscoveredCredential(res); err == nil {
			t.Fatal("expected validation error for external_idp")
		}
	})
	t.Run("external_idp with allowed endpoint", func(t *testing.T) {
		res := &AutoImportResult{
			Found:         true,
			AccessToken:   "at",
			RefreshToken:  "rt",
			AuthMethod:    "external_idp",
			TokenEndpoint: "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
			Scopes:        "openid",
		}
		if err := ValidateDiscoveredCredential(res); err != nil {
			t.Fatalf("expected valid: %v", err)
		}
	})
}

func TestDiscoverKiroCliSQLite_auth_kv(t *testing.T) {
	dbPath := createTestSQLite(t)
	res, err := discoverKiroCliSQLite([]string{dbPath})
	if err != nil {
		t.Fatalf("discovery error: %v", err)
	}
	if res == nil || !res.Found {
		t.Fatal("expected discovery to find credentials")
	}
	if res.Source != "kiro-cli-sqlite" {
		t.Fatalf("source = %q", res.Source)
	}
	if res.RefreshToken != "aorAAAAAGrefresh" {
		t.Fatalf("refresh token = %q", res.RefreshToken)
	}
	if res.AccessToken != "access-123" {
		t.Fatalf("access token = %q", res.AccessToken)
	}
	if res.Region != "us-west-2" {
		t.Fatalf("region = %q", res.Region)
	}
	if res.ProfileArn != "arn:aws:codewhisperer:us-west-2:123:profile/test" {
		t.Fatalf("profileArn = %q", res.ProfileArn)
	}
	if res.ClientID != "client-id-123" {
		t.Fatalf("clientId = %q", res.ClientID)
	}
	if res.ClientSecret != "client-secret-456" {
		t.Fatalf("clientSecret = %q", res.ClientSecret)
	}
	if res.AuthMethod != "import" {
		t.Fatalf("authMethod = %q", res.AuthMethod)
	}
}

func TestDiscoverKiroCliSQLite_ItemTable(t *testing.T) {
	dbPath := createTestSQLiteWithTable(t, "ItemTable")
	res, err := discoverKiroCliSQLite([]string{dbPath})
	if err != nil {
		t.Fatalf("discovery error: %v", err)
	}
	if res == nil || !res.Found {
		t.Fatal("expected discovery to find credentials")
	}
	if res.RefreshToken != "aorAAAAAGitem" {
		t.Fatalf("refresh token = %q", res.RefreshToken)
	}
}

func TestDiscoverAwsSsoCache_AWSToken(t *testing.T) {
	cacheDir := t.TempDir()
	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "profile.json")
	writeJSON(t, profilePath, map[string]any{
		"arn": "arn:aws:codewhisperer:eu-west-1:123:profile/sso",
	})

	tokenBody := map[string]any{
		"refreshToken": "aorAAAAAGssotoken",
		"accessToken":  "sso-access",
		"region":       "eu-west-1",
		"authMethod":   "idc",
		"clientIdHash": "hash123",
	}
	writeJSON(t, filepath.Join(cacheDir, "kiro-auth-token.json"), tokenBody)
	writeJSON(t, filepath.Join(cacheDir, "hash123.json"), map[string]any{
		"clientId":     "reg-client-id",
		"clientSecret": "reg-client-secret",
	})

	res, err := discoverAwsSsoCache(cacheDir, []string{profilePath})
	if err != nil {
		t.Fatalf("discovery error: %v", err)
	}
	if res == nil || !res.Found {
		t.Fatal("expected discovery to find credentials")
	}
	if res.Source != "kiro-auth-token.json" {
		t.Fatalf("source = %q", res.Source)
	}
	if res.RefreshToken != "aorAAAAAGssotoken" {
		t.Fatalf("refresh token = %q", res.RefreshToken)
	}
	if res.AuthMethod != "idc" {
		t.Fatalf("authMethod = %q", res.AuthMethod)
	}
	if res.Region != "eu-west-1" {
		t.Fatalf("region = %q", res.Region)
	}
	if res.ProfileArn != "arn:aws:codewhisperer:eu-west-1:123:profile/sso" {
		t.Fatalf("profileArn = %q", res.ProfileArn)
	}
	if res.ClientID != "reg-client-id" {
		t.Fatalf("clientId = %q", res.ClientID)
	}
}

func TestDiscoverAwsSsoCache_ExternalIDP(t *testing.T) {
	cacheDir := t.TempDir()
	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "profile.json")
	writeJSON(t, profilePath, map[string]any{
		"arn": "arn:aws:codewhisperer:us-east-1:123:profile/ext",
	})

	tokenBody := map[string]any{
		"refreshToken":  "org-refresh-token",
		"accessToken":   "org-access-token",
		"authMethod":    "external_idp",
		"clientId":      "org-client-id",
		"tokenEndpoint": "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		"scopes":        "api://client/codewhisperer:conversations offline_access",
	}
	writeJSON(t, filepath.Join(cacheDir, "kiro-auth-token.json"), tokenBody)

	res, err := discoverAwsSsoCache(cacheDir, []string{profilePath})
	if err != nil {
		t.Fatalf("discovery error: %v", err)
	}
	if res == nil || !res.Found {
		t.Fatal("expected discovery to find credentials")
	}
	if res.AuthMethod != "external_idp" {
		t.Fatalf("authMethod = %q", res.AuthMethod)
	}
	if res.TokenEndpoint == "" {
		t.Fatal("expected token endpoint")
	}
	if res.Scopes == "" {
		t.Fatal("expected scopes")
	}
	if res.ProfileArn == "" {
		t.Fatal("expected profileArn from IDE profile.json")
	}
}

func TestAutoImportWithSearchRoots_NotFound(t *testing.T) {
	roots := searchRoots{sqlitePaths: []string{}, awsSsoDir: ""}
	res, err := autoImportWithSearchRoots(t.Context(), roots)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Found {
		t.Fatal("expected not found")
	}
	if res.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestAutoImportWithSearchRoots_Found(t *testing.T) {
	dbPath := createTestSQLite(t)
	roots := searchRoots{sqlitePaths: []string{dbPath}, awsSsoDir: ""}
	res, err := autoImportWithSearchRoots(t.Context(), roots)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Found {
		t.Fatalf("expected found, got %+v", res)
	}
}

func TestParseExpiresAt(t *testing.T) {
	now := time.Now().Unix()
	if parseExpiresAt("") <= now {
		t.Fatal("empty expiry should default to future")
	}
	if parseExpiresAt("2030-01-01T00:00:00Z") != time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatal("RFC3339 parsing failed")
	}
	if parseExpiresAt("1893456000000") != time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatal("millisecond timestamp parsing failed")
	}
}

func createTestSQLite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mustExec(t, db, "CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)")
	mustExec(t, db, "CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)")
	mustExec(t, db, "CREATE TABLE storage (key TEXT PRIMARY KEY, value TEXT)")

	insertJSON(t, db, "auth_kv", "kirocli:oidc:token", map[string]any{
		"access_token":  "access-123",
		"refresh_token": "aorAAAAAGrefresh",
		"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
		"region":        "us-west-2",
	})
	insertJSON(t, db, "auth_kv", "kirocli:oidc:device-registration", map[string]any{
		"client_id":     "client-id-123",
		"client_secret": "client-secret-456",
	})
	insertJSON(t, db, "auth_kv", "api.codewhisperer.profile", map[string]any{
		"arn": "arn:aws:codewhisperer:us-west-2:123:profile/test",
	})
	return path
}

func createTestSQLiteWithTable(t *testing.T, table string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mustExec(t, db, "CREATE TABLE "+table+" (key TEXT PRIMARY KEY, value TEXT)")
	insertJSON(t, db, table, "kiro:auth:token", map[string]any{
		"access_token":  "access-item",
		"refresh_token": "aorAAAAAGitem",
		"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	return path
}

func insertJSON(t *testing.T, db *sql.DB, table, key string, value map[string]any) {
	t.Helper()
	b, err := marshalJSON(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	mustExec(t, db, "INSERT INTO "+table+" (key, value) VALUES (?, ?)", key, string(b))
}

func mustExec(t *testing.T, db *sql.DB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func writeJSON(t *testing.T, path string, value map[string]any) {
	t.Helper()
	b, err := marshalJSON(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func marshalJSON(v map[string]any) ([]byte, error) {
	return json.Marshal(v)
}
