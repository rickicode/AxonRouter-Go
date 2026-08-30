package codex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbpkg "github.com/rickicode/AxonRouter-Go/internal/db"
)

// rawAuthShape captures the common fields found in both a bare access-token
// payload and the Codex CLI auth.json file.
type rawAuthShape struct {
	// Bare access-token style.
	AccessToken      string `json:"access_token"`
	AccessTokenCamel string `json:"accessToken"`
	RefreshToken     string `json:"refresh_token"`
	IDToken          string `json:"id_token"`
	IDTokenCamel     string `json:"idToken"`
	ExpiresIn        int64  `json:"expires_in"`
	ExpiresInCamel   int64  `json:"expiresIn"`
	ExpiresAt        int64  `json:"expires_at"`

	// CLI auth.json style.
	Token                 string         `json:"token"`
	RToken                string         `json:"refreshToken"`
	ExpAt                 any            `json:"expiresAt"`
	AccountID             string         `json:"accountId"`
	Email                 string         `json:"email"`
	ProviderSpecificData  map[string]any `json:"providerSpecificData"`
	ProviderSpecificSnake map[string]any `json:"provider_specific_data"`
}

// ImportCredentials persists a raw access-token or Codex CLI auth.json blob as
// a ready Codex OAuth connection. It returns the new connection ID.
func ImportCredentials(ctx context.Context, database *sql.DB, raw []byte) (string, error) {
	if database == nil {
		return "", fmt.Errorf("database not configured")
	}
	var shape rawAuthShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}

	accessToken := firstNonEmpty(shape.AccessToken, shape.AccessTokenCamel, shape.Token)
	if accessToken == "" {
		return "", fmt.Errorf("missing access_token/token")
	}
	refreshToken := firstNonEmpty(shape.RefreshToken, shape.RToken)
	idToken := firstNonEmpty(shape.IDToken, shape.IDTokenCamel)

	expiresAt := parseExpires(shape.ExpiresAt, firstPositive(shape.ExpiresIn, shape.ExpiresInCamel), shape.ExpAt)
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
	providerSpecificData := make(map[string]any, len(shape.ProviderSpecificData)+len(shape.ProviderSpecificSnake))
	for key, value := range shape.ProviderSpecificSnake {
		providerSpecificData[key] = value
	}
	for key, value := range shape.ProviderSpecificData {
		providerSpecificData[key] = value
	}
	if accountID == "" {
		accountID = stringValue(providerSpecificData["account_id"])
	}
	if accountID == "" {
		accountID = stringValue(providerSpecificData["accountId"])
	}
	if accountID == "" {
		accountID = stringValue(providerSpecificData["chatgptAccountId"])
	}
	if email == "" {
		email = shape.Email
	}

	if _, err := database.ExecContext(ctx, `
		INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at)
		VALUES ('cx', 'OpenAI Codex', 'openai-responses', 'https://api.openai.com', ?)
	`, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("ensure provider type: %w", err)
	}

	connName := email
	if connName == "" {
		if accountID != "" {
			connName = "Codex " + accountID
		} else {
			connName = "Codex Imported"
		}
	}
	psd := make(map[string]any, len(providerSpecificData)+2)
	for key, value := range providerSpecificData {
		psd[key] = value
	}
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
	accountKey := email
	if accountKey == "" {
		accountKey = accountID
	}

	now := time.Now().Unix()
	connID, _, err := dbpkg.UpsertOAuthConnection(ctx, database, "cx", accountKey, connName, accessToken, refreshToken, expiresAt.Unix(), psdJSON, now)
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

func firstPositive(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func stringValue(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
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
