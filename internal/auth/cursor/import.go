// Package cursor discovers and validates Cursor IDE credentials from its
// VS Code: SQLite state file (state.vscdb) without intercepting traffic.
package cursor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DiscoverFunc is the signature used by handlers to inject discovery logic in tests.
type DiscoverFunc func(ctx context.Context, roots SearchRoots) (*DiscoveredAuth, error)

var upstreamUsageURL = "https://api2.cursor.sh/auth/usage"

// UpstreamUsageURL returns the current Cursor validation URL (used by tests).
func UpstreamUsageURL() string { return upstreamUsageURL }

// SetUpstreamUsageURL overrides the Cursor validation URL (used by tests).
func SetUpstreamUsageURL(url string) string {
	prev := upstreamUsageURL
	upstreamUsageURL = url
	return prev
}

// DiscoveredAuth holds Cursor auth state extracted from a local state.vscdb.
type DiscoveredAuth struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Email        string `json:"email,omitempty"`
	SignUpType   string `json:"sign_up_type,omitempty"`
	Source       string `json:"source"`
	TriedPaths   []string
}

// SearchRoots groups filesystem locations used to locate the Cursor state DB.
type SearchRoots struct {
	StatePaths []string
}

// DefaultSearchRoots returns the platform-specific default paths for the Cursor
// VS Code: globalStorage SQLite database.
func DefaultSearchRoots() SearchRoots {
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	var paths []string
	if home != "" {
		paths = append(paths, filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"))
		paths = append(paths, filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"))
	}
	if appData != "" {
		paths = append(paths, filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb"))
	} else if home != "" {
		paths = append(paths, filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb"))
	}
	return SearchRoots{StatePaths: paths}
}

// DiscoveryError reports that discovery failed but the caller may want to fall
// back to a manual guide instead of treating the error as fatal.
type DiscoveryError struct {
	TriedPaths []string
	Message    string
}

func (e *DiscoveryError) Error() string { return e.Message }

// Discover searches local Cursor state files for an access token.
func Discover(ctx context.Context, roots SearchRoots) (*DiscoveredAuth, error) {
	return discoverWithSearchRoots(ctx, roots)
}

func discoverWithSearchRoots(ctx context.Context, roots SearchRoots) (*DiscoveredAuth, error) {
	var tried []string
	for _, p := range roots.StatePaths {
		tried = append(tried, p)
		auth, err := readStateVSCDB(p)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("Cursor state read failed for %s: %v", p, err)
			}
			continue
		}
		if auth != nil && strings.TrimSpace(auth.AccessToken) != "" {
			auth.Source = p
			auth.TriedPaths = tried
			return auth, nil
		}
	}

	return nil, &DiscoveryError{
		TriedPaths: tried,
		Message:    "Cursor IDE state file not found or does not contain an access token. Log in to Cursor IDE first, then retry.",
	}
}

// readStateVSCDB opens the Cursor SQLite state file read-only and extracts the
// auth fields used by the Cursor IDE.
func readStateVSCDB(path string) (*DiscoveredAuth, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=query_only(1)&_pragma=journal_mode(OFF)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return nil, err
	}

	accessToken := queryItemTable(db, "cursorAuth/accessToken")
	if accessToken == "" {
		return nil, nil
	}
	return &DiscoveredAuth{
		AccessToken:  accessToken,
		RefreshToken: queryItemTable(db, "cursorAuth/refreshToken"),
		Email:        queryItemTable(db, "cursorAuth/cachedEmail"),
		SignUpType:   queryItemTable(db, "cursorAuth/cachedSignUpType"),
	}, nil
}

func queryItemTable(db *sql.DB, key string) string {
	var value string
	err := db.QueryRow("SELECT value FROM ItemTable WHERE key = ?", key).Scan(&value)
	if err != nil {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Field values are stored as quoted JSON strings; unwrap them when possible.
	var unquoted string
	if err := json.Unmarshal([]byte(value), &unquoted); err == nil {
		return strings.TrimSpace(unquoted)
	}
	return value
}

// ValidationResult is returned after validating a token with Cursor's upstream API.
type ValidationResult struct {
	OK bool `json:"ok"`
	// UsageMonthStart is the ISO timestamp returned by /auth/usage when valid.
	UsageMonthStart string `json:"usage_month_start,omitempty"`
}

// ValidateToken calls Cursor's upstream /auth/usage endpoint to verify the token
// is alive. It never logs the raw token.
func ValidateToken(ctx context.Context, client *http.Client, accessToken string) (*ValidationResult, error) {
	if accessToken == "" {
		return nil, errors.New("access token is empty")
	}
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamUsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cursor usage request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	log.Printf("Cursor token validation status=%d duration=%s token_hash=%s", resp.StatusCode, time.Since(start).Round(time.Millisecond), hashToken(accessToken))

	if resp.StatusCode == http.StatusOK {
		res := &ValidationResult{OK: true}
		var data map[string]any
		if err := json.Unmarshal(body, &data); err == nil {
			if v, ok := data["startOfMonth"].(string); ok {
				res.UsageMonthStart = v
			}
		}
		return res, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("cursor token is invalid or expired")
	}
	return nil, fmt.Errorf("cursor validation returned %d: %s", resp.StatusCode, string(body))
}

// HashedEmail returns a stable SHA-256 hash of the email suitable for logging.
func HashedEmail(email string) string {
	if email == "" {
		return ""
	}
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(h[:])
}

// hashToken returns a short opaque hash of the token for logs.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])[:16]
}

// ExpiresAt parses the exp claim of a JWT-style access token. Cursor access
// tokens are JWTs, so we can derive the expiry without making a network call.
func ExpiresAt(accessToken string) time.Time {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}
	}
	raw, ok := claims["exp"]
	if !ok {
		return time.Time{}
	}
	var exp int64
	switch v := raw.(type) {
	case float64:
		exp = int64(v)
	case string:
		e, _ := strconv.ParseInt(v, 10, 64)
		exp = e
	}
	if exp <= 0 {
		return time.Time{}
	}
	return time.Unix(exp, 0)
}
