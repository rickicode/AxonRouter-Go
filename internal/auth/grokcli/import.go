package grokcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbpkg "github.com/rickicode/AxonRouter-Go/internal/db"
)

type rawImportShape struct {
	AccessToken           string            `json:"access_token"`
	AccessTokenCamel      string            `json:"accessToken"`
	RefreshToken          string            `json:"refresh_token"`
	RefreshTokenCamel     string            `json:"refreshToken"`
	IDToken               string            `json:"id_token"`
	IDTokenCamel          string            `json:"idToken"`
	Email                 string            `json:"email"`
	Name                  string            `json:"name"`
	DisplayName           string            `json:"displayName"`
	ExpiresAt             any               `json:"expires_at"`
	ExpiresAtCamel        any               `json:"expiresAt"`
	ExpiresIn             any               `json:"expires_in"`
	ExpiresInCamel        any               `json:"expiresIn"`
	ProviderSpecificData  map[string]string `json:"providerSpecificData"`
	ProviderSpecificSnake map[string]string `json:"provider_specific_data"`
}

func ImportCredentials(ctx context.Context, database *sql.DB, raw []byte) (string, error) {
	if database == nil {
		return "", fmt.Errorf("database not configured")
	}
	var shape rawImportShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}

	accessToken := firstNonEmpty(shape.AccessToken, shape.AccessTokenCamel)
	if accessToken == "" {
		return "", fmt.Errorf("missing access_token/accessToken")
	}
	refreshToken := firstNonEmpty(shape.RefreshToken, shape.RefreshTokenCamel)
	idToken := firstNonEmpty(shape.IDToken, shape.IDTokenCamel)
	email, accountID := parseJWTIdentity(idToken)
	if strings.TrimSpace(shape.Email) != "" {
		email = strings.TrimSpace(shape.Email)
	}

	psd := make(map[string]string, len(shape.ProviderSpecificData)+len(shape.ProviderSpecificSnake)+4)
	for key, value := range shape.ProviderSpecificSnake {
		psd[key] = value
	}
	for key, value := range shape.ProviderSpecificData {
		psd[key] = value
	}
	psd["authMethod"] = "device_code"
	if idToken != "" {
		psd["idToken"] = idToken
	}
	if email != "" {
		psd["email"] = email
	}
	if accountID != "" {
		psd["sub"] = accountID
	}

	if _, err := database.ExecContext(ctx, `
		INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at)
		VALUES ('grok-cli', 'Grok CLI (Grok Build)', 'grok-cli', 'https://cli-chat-proxy.grok.com', ?)
	`, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("ensure provider type: %w", err)
	}

	connName := firstNonEmpty(shape.DisplayName, shape.Name, email)
	if connName == "" {
		if accountID != "" {
			connName = "Grok CLI " + accountID
		} else {
			connName = "Grok CLI Imported"
		}
	}
	var psdJSON sql.NullString
	if len(psd) > 0 {
		b, _ := json.Marshal(psd)
		psdJSON = sql.NullString{String: string(b), Valid: true}
	}

	accountKey := firstNonEmpty(email, accountID)
	connID, _, err := dbpkg.UpsertOAuthConnection(ctx, database, "grok-cli", accountKey, connName, accessToken, refreshToken, parseExpiry(shape.ExpiresAt, shape.ExpiresAtCamel, shape.ExpiresIn, shape.ExpiresInCamel), psdJSON, time.Now().Unix())
	if err != nil {
		return "", fmt.Errorf("insert connection: %w", err)
	}
	return connID, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseExpiry(expiresAt, expiresAtCamel, expiresIn, expiresInCamel any) int64 {
	if expiry := parseTimestamp(expiresAt); expiry > 0 {
		return expiry
	}
	if expiry := parseTimestamp(expiresAtCamel); expiry > 0 {
		return expiry
	}
	if seconds := parseNumber(expiresIn); seconds > 0 {
		return time.Now().Add(time.Duration(seconds) * time.Second).Unix()
	}
	if seconds := parseNumber(expiresInCamel); seconds > 0 {
		return time.Now().Add(time.Duration(seconds) * time.Second).Unix()
	}
	return 0
}

func parseTimestamp(value any) int64 {
	switch value := value.(type) {
	case float64:
		return int64(value)
	case string:
		value = strings.TrimSpace(value)
		if timestamp, err := time.Parse(time.RFC3339, value); err == nil {
			return timestamp.Unix()
		}
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	default:
		return 0
	}
}

func parseNumber(value any) int64 {
	switch value := value.(type) {
	case float64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed
	default:
		return 0
	}
}
