package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// OAuthTokenWriter persists refreshed OAuth tokens to the connections table
// using a read-then-write CAS pattern. It implements auth.TokenWriter.
type OAuthTokenWriter struct {
	db *sql.DB
	wq *WriteQueue
}

// NewOAuthTokenWriter creates a writer backed by the given database. If wq is
// non-nil, token writes are enqueued asynchronously; otherwise they run
// synchronously against db.
func NewOAuthTokenWriter(database *sql.DB, wq *WriteQueue) *OAuthTokenWriter {
	return &OAuthTokenWriter{db: database, wq: wq}
}

// GetRefreshToken reads the current oauth_refresh_token for a connection.
func (w *OAuthTokenWriter) GetRefreshToken(ctx context.Context, connID string) (string, error) {
	var rt string
	err := w.db.QueryRowContext(ctx, `
		SELECT COALESCE(oauth_refresh_token, '')
		FROM connections
		WHERE id = ?
	`, connID).Scan(&rt)
	return rt, err
}

// SaveTokens persists refreshed tokens for a connection. providerSpecific is
// only written when it is non-empty, so existing provider_specific_data is
// preserved for providers that do not return it during refresh.
func (w *OAuthTokenWriter) SaveTokens(ctx context.Context, connID, accessToken, refreshToken string, expiresAt int64, providerSpecific map[string]string) error {
	now := time.Now().Unix()

	var psdJSON []byte
	if len(providerSpecific) > 0 {
		psdJSON, _ = json.Marshal(providerSpecific)
	}

	update := func(d *sql.DB) error {
		// Use a background context for the actual write; the caller's context
		// may be a request-scoped context that cancels before an async write
		// queue executes the update.
		writeCtx := context.Background()
		if psdJSON != nil {
			_, err := d.ExecContext(writeCtx, `
				UPDATE connections
				SET oauth_token = ?,
				    oauth_refresh_token = ?,
				    oauth_expires_at = ?,
				    provider_specific_data = ?,
				    updated_at = ?
				WHERE id = ?
			`, accessToken, nullString(refreshToken), nullInt64(expiresAt), nullString(string(psdJSON)), now, connID)
			return err
		}
		_, err := d.ExecContext(writeCtx, `
			UPDATE connections
			SET oauth_token = ?,
			    oauth_refresh_token = ?,
			    oauth_expires_at = ?,
			    updated_at = ?
			WHERE id = ?
		`, accessToken, nullString(refreshToken), nullInt64(expiresAt), now, connID)
		return err
	}

	if w.wq != nil {
		w.wq.Enqueue("oauthTokenWriter:saveTokens", update)
		return nil
	}
	return update(w.db)
}

var _ auth.TokenWriter = (*OAuthTokenWriter)(nil)
