package codex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// rawAuthShape captures the common fields found in both a bare access-token
// payload and the Codex CLI auth.json file.
type rawAuthShape struct {
	// Bare access-token style.
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`

	// CLI auth.json style.
	Token     string `json:"token"`
	RToken    string `json:"refreshToken"`
	ExpAt     any    `json:"expiresAt"`
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
}

// ImportCredentials persists a raw access-token or Codex CLI auth.json blob as
// a ready Codex OAuth connection. It returns the new connection ID.
func ImportCredentials(ctx context.Context, db *sql.DB, raw []byte) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database not configured")
	}
	var shape rawAuthShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}

	accessToken := firstNonEmpty(shape.AccessToken, shape.Token)
	if accessToken == "" {
		return "", fmt.Errorf("missing access_token/token")
	}
	refreshToken := firstNonEmpty(shape.RefreshToken, shape.RToken)
	idToken := shape.IDToken

	expiresAt := parseExpires(shape.ExpiresAt, shape.ExpiresIn, shape.ExpAt)
	if expiresAt.IsZero() {
		// Default to 55 minutes from now if no expiry is provided, matching
		// a typical Codex access-token lifetime.
		expiresAt = time.Now().Add(55 * time.Minute)
	}

	// Derive account identity from the ID token or explicit fields.
	accountID, email := extractTokenClaims(idToken)
	if accountID == "" {
		accountID = shape.AccountID
	}
	if email == "" {
		email = shape.Email
	}

	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at)
		VALUES ('cx', 'OpenAI Codex', 'openai-responses', 'https://api.openai.com', ?)
	`, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("ensure provider type: %w", err)
	}

	connID := uuid.New().String()
	connName := email
	if connName == "" {
		if accountID != "" {
			connName = "Codex " + accountID
		} else {
			connName = "Codex Imported"
		}
	}
	psd := map[string]any{}
	if accountID != "" {
		psd["account_id"] = accountID
	}
	if email != "" {
		psd["email"] = email
	}
	var psdJSON sql.NullString
	if len(psd) > 0 {
		b, _ := json.Marshal(psd)
		psdJSON = sql.NullString{String: string(b), Valid: true}
	}
	var refreshNull sql.NullString
	if refreshToken != "" {
		refreshNull = sql.NullString{String: refreshToken, Valid: true}
	}
	now := time.Now().Unix()
	_, err := db.ExecContext(ctx, `
		INSERT INTO connections (
			id, provider_type_id, name, auth_type,
			oauth_token, oauth_refresh_token, oauth_expires_at,
			provider_specific_data, status, is_active, created_at, updated_at
		) VALUES (?, ?, ?, 'oauth', ?, ?, ?, ?, 'ready', 1, ?, ?)
	`, connID, "cx", connName, accessToken, refreshNull, expiresAt.Unix(), psdJSON, now, now)
	if err != nil {
		return "", fmt.Errorf("insert connection: %w", err)
	}
	return connID, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseExpires(expiresAtSec int64, expiresIn int64, expiresAtRaw any) time.Time {
	if expiresIn > 0 {
		return time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	if expiresAtSec > 0 {
		return time.Unix(expiresAtSec, 0)
	}
	switch v := expiresAtRaw.(type) {
	case float64:
		return time.Unix(int64(v), 0)
	case int64:
		return time.Unix(v, 0)
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
