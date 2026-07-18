package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// QuotaItem represents a single quota window.
type QuotaItem struct {
	Name         string  `json:"name"`
	Used         float64 `json:"used"`
	Total        float64 `json:"total"`
	RemainingPct float64 `json:"remaining_pct"`
	ResetAt      string  `json:"reset_at,omitempty"`
	Unlimited    bool    `json:"unlimited"`
	ModelKey     string  `json:"model_key,omitempty"`
	Family       string  `json:"family,omitempty"`
	Scope        string  `json:"scope,omitempty"` // "codex", "spark", or "" (default/unknown)
}

// ConnectionQuota holds quota data for a single connection.
type ConnectionQuota struct {
	ConnectionID   string      `json:"connection_id"`
	ConnectionName string      `json:"connection_name"`
	ProviderID     string      `json:"provider_id"`
	ProviderName   string      `json:"provider_name"`
	Plan           string      `json:"plan,omitempty"`
	Quotas         []QuotaItem `json:"quotas"`
	Message        string      `json:"message,omitempty"`
	Error          string      `json:"error,omitempty"`
	FetchedAt      int64       `json:"fetched_at"`
}

// ProviderQuota groups connections under a provider.
type ProviderQuota struct {
	ProviderID   string            `json:"provider_id"`
	ProviderName string            `json:"provider_name"`
	DisplayName  string            `json:"display_name"`
	Color        string            `json:"color"`
	IconFile     string            `json:"icon_file"`
	Connections  []ConnectionQuota `json:"connections"`
}

// connRow is the DB row we need for quota fetching.
type connRow struct {
	ID                   string
	ProviderTypeID       string
	Name                 string
	OAuthToken           sql.NullString
	OAuthRefreshToken    sql.NullString
	OAuthExpiresAt       int64
	ProviderSpecificData sql.NullString
}

// providerMeta holds display info for a provider type.
type providerMeta struct {
	DisplayName string
	Color       string
	IconFile    string
}

// knownProviders maps provider_type_id to display metadata (colors, icons).
// Display names are loaded from the DB provider_types table at runtime.
var knownProviders = map[string]providerMeta{
	"cx": {DisplayName: "Codex", Color: "#10a37f", IconFile: "codex.svg"},
	"ag": {DisplayName: "Antigravity", Color: "#4285f4", IconFile: "antigravity.svg"},
	"kiro": {DisplayName: "Kiro", Color: "#ff9900", IconFile: "kiro.svg"},
	"copilot": {DisplayName: "GitHub Copilot", Color: "#24292E", IconFile: "copilot.png"},
	"grok-cli": {DisplayName: "Grok CLI (Grok Build)", Color: "#000000", IconFile: "grok-cli.png"},
}

// ProviderMeta returns display metadata for a provider type, if known.
func ProviderMeta(providerID string) (providerMeta, bool) {
	m, ok := knownProviders[providerID]
	return m, ok
}

// authMgr is the package-level auth manager used to refresh OAuth tokens.
// Injected via SetAuthManager so public quota fetch signatures stay stable.
var authMgr *auth.Manager

// SetAuthManager injects the auth manager used for OAuth token refresh.
func SetAuthManager(m *auth.Manager) {
	authMgr = m
}

// FetchAllQuota fetches quota for all OAuth connections across all providers.
func FetchAllQuota(db *sql.DB) ([]ProviderQuota, error) {
	// Load provider display names from DB
	providerNames := loadProviderNames(db)

	rows, err := db.Query(`
		SELECT id, provider_type_id, name, oauth_token,
		       oauth_refresh_token, COALESCE(oauth_expires_at, 0), provider_specific_data
		FROM connections
		WHERE auth_type = 'oauth' AND is_active = 1
		ORDER BY provider_type_id, name
	`)
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer rows.Close()

	var conns []connRow
	for rows.Next() {
		var c connRow
		if err := rows.Scan(&c.ID, &c.ProviderTypeID, &c.Name, &c.OAuthToken, &c.OAuthRefreshToken, &c.OAuthExpiresAt, &c.ProviderSpecificData); err != nil {
			log.Printf("quota: scan connection: %v", err)
			continue
		}
		conns = append(conns, c)
	}

	if len(conns) == 0 {
		return []ProviderQuota{}, nil
	}

	// Group by provider
	grouped := make(map[string][]connRow)
	for _, c := range conns {
		grouped[c.ProviderTypeID] = append(grouped[c.ProviderTypeID], c)
	}

	var mu sync.Mutex
	var results []ProviderQuota

	for providerID, providerConns := range grouped {
		meta, ok := knownProviders[providerID]
		if !ok {
			meta = providerMeta{DisplayName: providerID, Color: "#888888", IconFile: ""}
		}
		// Use DB display name if available
		if dbName, ok := providerNames[providerID]; ok && dbName != "" {
			meta.DisplayName = dbName
		}

		pq := ProviderQuota{
			ProviderID:   providerID,
			ProviderName: providerID,
			DisplayName:  meta.DisplayName,
			Color:        meta.Color,
			IconFile:     meta.IconFile,
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, 5) // max 5 parallel fetches

		for _, conn := range providerConns {
			wg.Add(1)
			sem <- struct{}{}
			go func(c connRow) {
				defer wg.Done()
				defer func() { <-sem }()

				cq := fetchConnectionQuota(c, providerID, db)

				mu.Lock()
				pq.Connections = append(pq.Connections, cq)
				mu.Unlock()
			}(conn)
		}

		wg.Wait()
		mu.Lock()
		results = append(results, pq)
		mu.Unlock()
	}

	return results, nil
}

// loadProviderNames loads display_name from provider_types table.
func loadProviderNames(db *sql.DB) map[string]string {
	names := make(map[string]string)
	rows, err := db.Query(`SELECT id, display_name FROM provider_types`)
	if err != nil {
		return names
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err == nil {
			names[id] = name
		}
	}
	return names
}

// FetchConnectionQuota fetches quota for a single connection.
func FetchConnectionQuota(db *sql.DB, connectionID string) (*ConnectionQuota, error) {
	var c connRow
	err := db.QueryRow(`
		SELECT id, provider_type_id, name, oauth_token,
		       oauth_refresh_token, COALESCE(oauth_expires_at, 0), provider_specific_data
		FROM connections
		WHERE id = ? AND auth_type = 'oauth' AND is_active = 1
	`, connectionID).Scan(&c.ID, &c.ProviderTypeID, &c.Name, &c.OAuthToken, &c.OAuthRefreshToken, &c.OAuthExpiresAt, &c.ProviderSpecificData)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	cq := fetchConnectionQuota(c, c.ProviderTypeID, db)
	return &cq, nil
}

// parseProviderSpecificData parses the JSON provider_specific_data column.
func parseProviderSpecificData(raw sql.NullString) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw.String), &m); err != nil {
		return nil
	}
	return m
}

// mapStringToAny converts a string-string map to the map[string]any used by fetchers.
func mapStringToAny(in map[string]string) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// refreshOAuthToken refreshes an expired OAuth token using the provider's refresh token.
// Returns new access token, new refresh token (may be same as input if not rotated), and expiry unix timestamp.
func refreshOAuthToken(providerID, refreshToken string) (string, string, int64, error) {
	// Use provider-specific client credentials (same as auth package)
	var clientID, clientSecret, tokenURL string
	switch providerID {
	case "cx":
		clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
		tokenURL = "https://auth.openai.com/oauth/token"
	case "ag":
		clientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
		clientSecret = "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"
		tokenURL = "https://oauth2.googleapis.com/token"
	default:
		return "", "", 0, fmt.Errorf("token refresh not supported for provider: %s", providerID)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", 0, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("refresh failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", "", 0, fmt.Errorf("parse response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", "", 0, fmt.Errorf("empty access_token in refresh response")
	}

	// Preserve old refresh_token when provider omits it (Auth0 rotation gives a new one; Google often omits it)
	newRefreshToken := tokenResp.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	return tokenResp.AccessToken, newRefreshToken, time.Now().Unix() + tokenResp.ExpiresIn, nil
}

// unrecoverableOAuthCodes are OAuth error codes that mean the refresh token is permanently dead.
var unrecoverableOAuthCodes = map[string]bool{
	"invalid_grant":             true,
	"invalid_request":           true,
	"refresh_token_reused":      true,
	"refresh_token_invalidated": true,
	"invalid_token":             true,
	"token_expired":             true,
	"expired_token":             true,
	"unauthorized_client":       true,
	"access_denied":             true,
	"unrecoverable_refresh_error": true,
}

// isUnrecoverableRefreshError checks if a refresh error indicates the token is permanently invalid.
func isUnrecoverableRefreshError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for code := range unrecoverableOAuthCodes {
		if strings.Contains(s, code) {
			return true
		}
	}
	return false
}

// fetchConnectionQuota dispatches to the right provider fetcher.
// Refreshes expired OAuth tokens before fetching quota.
func fetchConnectionQuota(c connRow, providerID string, db *sql.DB) ConnectionQuota {
	if cached, ok := getCachedQuota(c.ID); ok {
		cached.FetchedAt = time.Now().UnixMilli()
		return cached
	}
	cq := ConnectionQuota{
		ConnectionID:   c.ID,
		ConnectionName: c.Name,
		ProviderID:     providerID,
		ProviderName:   providerID,
		FetchedAt:      time.Now().UnixMilli(),
	}

	if !c.OAuthToken.Valid || c.OAuthToken.String == "" {
		cq.Error = "No OAuth token available"
		return cq
	}

	token := c.OAuthToken.String
	psd := parseProviderSpecificData(c.ProviderSpecificData)

	// Proactive refresh with per-provider lead times (matches handler.go refreshLeadMs).
	refreshLead := int64(300) // default 5 minutes
	switch providerID {
	case "ag":
		refreshLead = 900 // 15 minutes for Antigravity (Google non-rotating)
	case "kiro":
		refreshLead = 300 // 5 minutes for Kiro
	}

	if c.OAuthExpiresAt > 0 && c.OAuthRefreshToken.Valid && c.OAuthRefreshToken.String != "" &&
		time.Now().Unix() > c.OAuthExpiresAt-refreshLead {
		var (
			newToken            string
			newRefreshToken     string
			newExpiry           int64
			newProviderSpecific map[string]string
			refreshed           bool
		)

		// Prefer auth manager for all OAuth providers (includes Codex singleflight + rotation groups).
		if authMgr != nil {
			providerType := auth.ProviderType(providerID)
			if _, ok := authMgr.GetService(providerType); ok {
				creds := &auth.Credentials{
					AccessToken: token,
					RefreshToken: c.OAuthRefreshToken.String,
					ExpiresAt:    time.Unix(c.OAuthExpiresAt, 0),
				}
				newCreds, err := authMgr.RefreshToken(context.Background(), providerType, creds)
				if err == nil {
					newToken = newCreds.AccessToken
					newRefreshToken = newCreds.RefreshToken
					if !newCreds.ExpiresAt.IsZero() {
						newExpiry = newCreds.ExpiresAt.Unix()
					}
					newProviderSpecific = newCreds.ProviderSpecific
					refreshed = true
			} else {
				log.Printf("quota: auth manager token refresh failed for %s (%s): %v", c.ID, c.Name, err)
				cq.Error = fmt.Sprintf("token refresh failed: %v", err)
				if isUnrecoverableRefreshError(err) && db != nil {
					now := time.Now().Unix()
					if _, derr := db.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', updated_at = ? WHERE id = ?`, now, c.ID); derr == nil {
						log.Printf("quota: connection %s disabled due to unrecoverable refresh error", c.ID)
					}
				}
				return cq
			}
		}
	}

	// Raw fallback for Antigravity and Kiro when auth manager is unavailable/failed.
		if !refreshed && (providerID == "ag" || providerID == "kiro") {
			var err error
			newToken, newRefreshToken, newExpiry, err = refreshOAuthToken(providerID, c.OAuthRefreshToken.String)
		if err != nil {
			log.Printf("quota: raw token refresh failed for %s (%s): %v", c.ID, c.Name, err)
			cq.Error = fmt.Sprintf("token refresh failed: %v", err)
			if isUnrecoverableRefreshError(err) && db != nil {
				now := time.Now().Unix()
				if _, derr := db.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', updated_at = ? WHERE id = ?`, now, c.ID); derr == nil {
					log.Printf("quota: connection %s disabled due to unrecoverable refresh error", c.ID)
				}
			}
			return cq
		}
		refreshed = true
	}

		if refreshed {
			token = newToken
			var psdJSON []byte
			if len(newProviderSpecific) > 0 {
				psdJSON, _ = json.Marshal(newProviderSpecific)
				psd = mapStringToAny(newProviderSpecific)
				c.ProviderSpecificData = sql.NullString{Valid: true, String: string(psdJSON)}
			}
			if db != nil {
				now := time.Now().Unix()
				if psdJSON != nil {
					db.Exec(`UPDATE connections SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, provider_specific_data = ?, updated_at = ? WHERE id = ?`,
						newToken, newRefreshToken, newExpiry, psdJSON, now, c.ID)
				} else {
					db.Exec(`UPDATE connections SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, updated_at = ? WHERE id = ?`,
						newToken, newRefreshToken, newExpiry, now, c.ID)
				}
			}
		}
	}

	type fetchResult struct {
		quotas  []QuotaItem
		plan    string
		headers http.Header
		msg     string
		err     error
	}

	ch := make(chan fetchResult, 1)
	go func() {
		var r fetchResult
	switch providerID {
	case "cx":
		r.quotas, r.plan, r.headers, r.err = fetchCodexQuota(token, psd)
	case "ag":
		r.quotas, r.plan, r.err = fetchAntigravityQuota(token, psd)
	case "kiro":
		r.quotas, r.plan, r.err = fetchKiroQuota(token, psd)
	case "grok-cli":
		r.quotas, r.plan, r.err = fetchGrokCliQuota(token, psd)
	case "copilot":
			// The /user endpoint requires the GitHub OAuth access token, not the
			// short-lived Copilot token. See OmniRoute open-sse/services/usage.ts:643.
			r.quotas, r.plan, r.err = fetchCopilotQuota(token)
			// Refresh the short-lived Copilot token in the background so the
			// executor and dashboard expiry timer stay current.
			if _, _, syncErr := refreshCopilotTokenIfNeeded(db, c.ID, token, psd); syncErr != nil {
				log.Printf("quota: failed to sync Copilot token expiry for %s: %v", c.ID, syncErr)
			}
		default:
			if _, known := knownProviders[providerID]; known {
				// A provider in knownProviders must have a fetcher; fail loudly so it
				// shows up in the dashboard instead of silently showing "No quota data".
				r.err = fmt.Errorf("no quota fetcher implemented for provider: %s", providerID)
			} else {
				r.msg = "Quota fetching not supported for this provider"
			}
		}
		ch <- r
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			cq.Error = r.err.Error()
		} else {
			cq.Quotas = r.quotas
			cq.Plan = r.plan
			cq.Message = r.msg

			// Codex /wham/usage may carry x-codex-5h-* / x-codex-7d-* headers. If so,
			// merge them into the cached quota so the dashboard can render 5h/7d bars
			// even before a live Codex chat request happens.
			if providerID == "cx" && r.headers != nil && (r.headers.Get("x-codex-5h-limit") != "" || r.headers.Get("x-codex-7d-limit") != "") {
				SaveCodexHeaderQuota(db, c.ID, providerID, c.Name, r.plan, r.headers)
			}
		}
	case <-time.After(15 * time.Second):
		cq.Error = "Quota fetch timed out (15s)"
	}

	setCachedQuota(c.ID, cq)
	return cq
}
