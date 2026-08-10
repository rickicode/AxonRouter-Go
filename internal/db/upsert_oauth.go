package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// UpsertOAuthConnection persists an OAuth connection, deduplicating by
// provider_type_id + oauth_email when accountKey is non-empty. If a matching
// row exists, its tokens and metadata are refreshed, status is reset to ready,
// and is_active is set to 1; otherwise a new connection is inserted.
func UpsertOAuthConnection(ctx context.Context, db *sql.DB, providerID, accountKey, connName, accessToken, refreshToken string, expiresAt int64, providerSpecificData sql.NullString, now int64) (string, bool, error) {
	if accountKey != "" {
		var existingID string
		err := db.QueryRowContext(ctx, `
			SELECT id FROM connections
			WHERE provider_type_id = ? AND oauth_email = ? AND auth_type = 'oauth'
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, providerID, accountKey).Scan(&existingID)
		if err != nil && err != sql.ErrNoRows {
			return "", false, err
		}
		if err == nil {
			if _, err := db.ExecContext(ctx, `
			UPDATE connections
			SET oauth_token = ?,
			    oauth_refresh_token = ?,
			    oauth_expires_at = ?,
			    provider_specific_data = ?,
			    name = ?,
			    status = 'ready',
			    disabled_reason = NULL,
			    is_active = 1,
			    updated_at = ?
			WHERE id = ?
		`, accessToken, refreshToken, nullInt64(expiresAt), providerSpecificData, connName, now, existingID); err != nil {
				return "", false, err
			}
			return existingID, false, nil
		}
	}

	connID := uuid.New().String()
	_, err := db.ExecContext(ctx, `
		INSERT INTO connections (
			id, provider_type_id, name, auth_type, oauth_email,
			oauth_token, oauth_refresh_token, oauth_expires_at,
			provider_specific_data, status, is_active, created_at, updated_at
		) VALUES (?, ?, ?, 'oauth', ?, ?, ?, ?, ?, 'ready', 1, ?, ?)
	`, connID, providerID, connName, nullString(accountKey), accessToken, refreshToken, nullInt64(expiresAt), providerSpecificData, now, now)
	if err != nil {
		return "", false, err
	}
	return connID, true, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt64(n int64) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}
