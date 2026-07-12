package codex

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// TokenStore manages Codex OAuth credentials with the connections table as the
// source of truth. It no longer writes to codex_tokens.json; the file is kept
// only as a non-authoritative legacy fallback and is never read.
type TokenStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	oauth  *OAuthService
	legacy legacyTokenFile // optional fallback path, ignored at runtime
}

// legacyTokenFile preserves the old file path signature for callers that may
// still construct NewTokenStore(storageDir, oauth). It is not used as source of
// truth.
type legacyTokenFile struct {
	filePath string
}

// NewTokenStore creates a new Codex token store backed by the database. The
// storageDir argument is retained for compatibility but the store only uses db.
func NewTokenStore(db *sql.DB, storageDir string, oauth *OAuthService) *TokenStore {
	return &TokenStore{
		db:    db,
		oauth: oauth,
	}
}

// Get returns credentials for a connection, refreshing the access token if it
// has expired. connID is the connection UUID from the connections table.
func (ts *TokenStore) Get(ctx context.Context, connID string) (*auth.Credentials, error) {
	if ts.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	creds, err := ts.readCredentials(ctx, connID)
	if err != nil {
		return nil, err
	}
	if creds.HasValidToken() {
		return creds, nil
	}
	// Refresh using retries for transient failures.
	newCreds, err := ts.oauth.RefreshTokenWithRetry(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	if err := ts.Store(ctx, connID, newCreds); err != nil {
		// Log but don't fail: the token is still usable in memory.
		fmt.Printf("warning: failed to persist refreshed token for %s: %v\n", connID, err)
	}
	return newCreds, nil
}

// Store persists credentials for a connection in the database.
func (ts *TokenStore) Store(ctx context.Context, connID string, creds *auth.Credentials) error {
	if ts.db == nil {
		return fmt.Errorf("database not configured")
	}
	var refresh sql.NullString
	if creds.RefreshToken != "" {
		refresh = sql.NullString{String: creds.RefreshToken, Valid: true}
	}
	expiresAt := creds.ExpiresAt.Unix()
	_, err := ts.db.ExecContext(ctx, `
		UPDATE connections
		SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, updated_at = ?
		WHERE id = ?
	`, creds.AccessToken, refresh, expiresAt, time.Now().Unix(), connID)
	return err
}

// Load is a no-op kept for API compatibility. The database is the source of
// truth and credentials are loaded lazily via Get.
func (ts *TokenStore) Load() error { return nil }

// Save is a no-op kept for API compatibility.
func (ts *TokenStore) Save() error { return nil }

// List returns the active OAuth connection IDs for the Codex provider.
func (ts *TokenStore) List(ctx context.Context) ([]string, error) {
	if ts.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	rows, err := ts.db.QueryContext(ctx, `
		SELECT id FROM connections
		WHERE provider_type_id = 'cx' AND auth_type = 'oauth' AND is_active = 1
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Remove deactivates a connection instead of deleting it, matching the rest of
// the admin surface.
func (ts *TokenStore) Remove(ctx context.Context, connID string) error {
	if ts.db == nil {
		return fmt.Errorf("database not configured")
	}
	_, err := ts.db.ExecContext(ctx, `
		UPDATE connections SET is_active = 0, updated_at = ? WHERE id = ?
	`, time.Now().Unix(), connID)
	return err
}

// RefreshAll refreshes expired tokens for all active Codex connections.
func (ts *TokenStore) RefreshAll(ctx context.Context) error {
	ids, err := ts.List(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		creds, err := ts.readCredentials(ctx, id)
		if err != nil {
			fmt.Printf("warning: failed to read credentials for %s: %v\n", id, err)
			continue
		}
		if creds.HasValidToken() || creds.RefreshToken == "" {
			continue
		}
		if _, err := ts.Get(ctx, id); err != nil {
			fmt.Printf("warning: refresh failed for %s: %v\n", id, err)
		}
	}
	return nil
}

// StartRefreshLoop starts a background goroutine that refreshes tokens.
func (ts *TokenStore) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ts.RefreshAll(ctx)
			}
		}
	}()
}

func (ts *TokenStore) readCredentials(ctx context.Context, connID string) (*auth.Credentials, error) {
	var accessToken, refreshToken string
	var expiresAt int64
	var psd sql.NullString
	err := ts.db.QueryRowContext(ctx, `
		SELECT COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0), COALESCE(provider_specific_data,'')
		FROM connections WHERE id = ?
	`, connID).Scan(&accessToken, &refreshToken, &expiresAt, &psd)
	if err != nil {
		return nil, err
	}
	var accountID string
	if psd.Valid && psd.String != "" {
		accountID = extractAccountIDFromPSD(psd.String)
	}
	return &auth.Credentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Unix(expiresAt, 0),
		AccountID:    accountID,
	}, nil
}

func extractAccountIDFromPSD(raw string) string {
	// ProviderSpecificData may contain a JWT id_token or account_id. Try the
	// simplest JSON path first; token parsing is handled by the JWT helper.
	return "" // placeholder, real extraction lives in import/JWT helpers
}
