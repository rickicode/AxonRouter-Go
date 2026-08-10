package cursor

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func createTestStateVSCDB(t *testing.T, accessToken, refreshToken, email, signUpType string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	insert := func(key, val string) {
		quoted, _ := json.Marshal(val)
		if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, key, string(quoted)); err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
	}
	insert("cursorAuth/accessToken", accessToken)
	insert("cursorAuth/refreshToken", refreshToken)
	insert("cursorAuth/cachedEmail", email)
	insert("cursorAuth/cachedSignUpType", signUpType)
	return path
}

func TestDiscover_Found(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiZXhwIjo5OTk5OTk5OTk5fQ.test"
	path := createTestStateVSCDB(t, token, "rt", "user@example.com", "Google")

	auth, err := Discover(context.Background(), SearchRoots{StatePaths: []string{path}})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if auth.AccessToken != token {
		t.Errorf("access token mismatch")
	}
	if auth.RefreshToken != "rt" {
		t.Errorf("refresh token mismatch")
	}
	if auth.Email != "user@example.com" {
		t.Errorf("email mismatch")
	}
	if auth.SignUpType != "Google" {
		t.Errorf("sign up type mismatch")
	}
}

func TestDiscover_NotFound(t *testing.T) {
	_, err := Discover(context.Background(), SearchRoots{StatePaths: []string{"/nonexistent/state.vscdb"}})
	if err == nil {
		t.Fatal("expected error when state file missing")
	}
	de, ok := err.(*DiscoveryError)
	if !ok {
		t.Fatalf("expected DiscoveryError, got %T", err)
	}
	if len(de.TriedPaths) != 1 {
		t.Errorf("expected 1 tried path, got %d", len(de.TriedPaths))
	}
}

func TestDiscover_MissingAccessToken(t *testing.T) {
	path := createTestStateVSCDB(t, "", "rt", "", "")
	_, err := Discover(context.Background(), SearchRoots{StatePaths: []string{path}})
	if err == nil {
		t.Fatal("expected error when access token empty")
	}
}

func TestValidateToken_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/usage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("bad authorization header: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"gpt-4":{"numRequests":1,"maxRequestUsage":500},"startOfMonth":"2026-07-01T00:00:00.000Z"}`)
	}))
	defer ts.Close()

	oldURL := upstreamUsageURL
	upstreamUsageURL = ts.URL + "/auth/usage"
	defer func() { upstreamUsageURL = oldURL }()

	res, err := ValidateToken(context.Background(), http.DefaultClient, "test-token")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.OK {
		t.Error("expected OK")
	}
	if res.UsageMonthStart != "2026-07-01T00:00:00.000Z" {
		t.Errorf("usage month start = %q", res.UsageMonthStart)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer ts.Close()

	oldURL := upstreamUsageURL
	upstreamUsageURL = ts.URL + "/auth/usage"
	defer func() { upstreamUsageURL = oldURL }()

	if _, err := ValidateToken(context.Background(), http.DefaultClient, "bad"); err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestValidateToken_Format(t *testing.T) {
	// ValidateToken should reject empty token.
	if _, err := ValidateToken(context.Background(), http.DefaultClient, ""); err == nil {
		t.Error("expected error for empty token")
	}
}

func TestExpiresAt(t *testing.T) {
	fixed := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	// Build a minimal JWT with exp claim.
	header := base64URL(`{"alg":"none","typ":"JWT"}`)
	payload := base64URL(fmt.Sprintf(`{"sub":"x","exp":%d}`, fixed.Unix()))
	token := header + "." + payload + "."

	got := ExpiresAt(token)
	if !got.Equal(fixed) {
		t.Errorf("expires at = %v, want %v", got, fixed)
	}

	if !ExpiresAt("not-a-jwt").IsZero() {
		t.Error("invalid JWT should return zero time")
	}
}

func TestHashedEmail(t *testing.T) {
	h1 := HashedEmail("A@Example.com")
	h2 := HashedEmail("a@example.com")
	if h1 != h2 {
		t.Error("email hashing should be case-insensitive")
	}
	if HashedEmail("") != "" {
		t.Error("empty email should produce empty hash")
	}
}

func base64URL(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
