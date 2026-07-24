package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/provider"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

// ProviderHandler handles provider CRUD operations.
type ProviderHandler struct {
	db          *sql.DB
	registry    *executor.Registry
	store       *connstate.Store
	elig        *connstate.EligibilityManager
	providerCfg *providercfg.Manager
	writeQueue  *db.WriteQueue
	authMgr     *auth.Manager
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(database *sql.DB, registry *executor.Registry, store *connstate.Store, elig *connstate.EligibilityManager, providerCfg *providercfg.Manager, writeQueue *db.WriteQueue, authMgr *auth.Manager) *ProviderHandler {
	return &ProviderHandler{db: database, registry: registry, store: store, elig: elig, providerCfg: providerCfg, writeQueue: writeQueue, authMgr: authMgr}
}

// List returns all providers with connection counts.
func (h *ProviderHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
	SELECT pt.id, pt.display_name, pt.format, pt.base_url, pt.is_custom, pt.custom_headers, pt.category, pt.service_kinds, pt.created_at,
	COUNT(c.id) as connection_count
	FROM provider_types pt
	LEFT JOIN connections c ON c.provider_type_id = pt.id
	GROUP BY pt.id
	ORDER BY pt.display_name
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Collect all providers first, then close rows before nested queries
	var providers []db.ProviderWithCounts
	for rows.Next() {
		p := db.ProviderWithCounts{}
		var serviceKindsRaw string
		rows.Scan(&p.ID, &p.DisplayName, &p.Format, &p.BaseURL,
			&p.IsCustom, &p.CustomHeaders, &p.Category, &serviceKindsRaw, &p.CreatedAt, &p.ConnectionCount)
		p.ServiceKinds = parseServiceKinds(serviceKindsRaw)
		providers = append(providers, p)
	}
	rows.Close()

	// Fill status counts after outer rows are closed (avoids SQLite deadlock)
	for i := range providers {
		providers[i].StatusCounts = h.getStatusCounts(providers[i].ID)
		providers[i].DisabledReasons = h.getDisabledReasonCounts(providers[i].ID)
		if info, ok := provider.Registry[providers[i].ID]; ok {
			providers[i].Aliases = info.Aliases
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": providers})
}

// Get returns a single provider with its connection status breakdown.
func (h *ProviderHandler) Get(c *gin.Context) {
	id := c.Param("id")
	p := db.ProviderWithCounts{}
	var serviceKindsRaw string
	err := h.db.QueryRow(`
	SELECT id, display_name, format, base_url, is_custom, custom_headers, category, service_kinds, created_at
	FROM provider_types WHERE id = ?
	`, id).Scan(&p.ID, &p.DisplayName, &p.Format, &p.BaseURL,
		&p.IsCustom, &p.CustomHeaders, &p.Category, &serviceKindsRaw, &p.CreatedAt)
	p.ServiceKinds = parseServiceKinds(serviceKindsRaw)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = ?`, id).Scan(&p.ConnectionCount)
	p.StatusCounts = h.getStatusCounts(id)
	if info, ok := provider.Registry[id]; ok {
		p.Aliases = info.Aliases
	}
	c.JSON(http.StatusOK, p)
}

// Create adds a custom provider.
func (h *ProviderHandler) Create(c *gin.Context) {
	var req struct {
		Name          string            `json:"name" binding:"required"`
		DisplayName   string            `json:"display_name"`
		Format        string            `json:"format" binding:"required"`
		BaseURL       string            `json:"base_url" binding:"required"`
		CustomHeaders map[string]string `json:"custom_headers"`
		Category      string            `json:"category"`
		ServiceKinds  []string          `json:"service_kinds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check uniqueness
	var exists int
	h.db.QueryRow(`SELECT COUNT(*) FROM provider_types WHERE id = ?`, req.Name).Scan(&exists)
	if exists > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "provider name already exists"})
		return
	}

	headersJSON := sql.NullString{}
	if req.CustomHeaders != nil {
		b, err := json.Marshal(req.CustomHeaders)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid custom_headers: " + err.Error()})
			return
		}
		headersJSON = sql.NullString{String: string(b), Valid: true}
	}

	now := time.Now().Unix()
	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Name
	}
	category := req.Category
	if category == "" {
		category = "compatible"
	}
	serviceKinds := req.ServiceKinds
	if len(serviceKinds) == 0 {
		serviceKinds = []string{"llm"}
	}
	serviceKindsJSON, _ := json.Marshal(serviceKinds)

	_, err := h.db.Exec(`
		INSERT INTO provider_types (id, display_name, format, base_url, is_custom, custom_headers, category, service_kinds, created_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)
	`, req.Name, displayName, req.Format, req.BaseURL, headersJSON, category, string(serviceKindsJSON), now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	executor.RegisterCustomProviders(h.db)

	c.JSON(http.StatusCreated, gin.H{
		"id":            req.Name,
		"display_name":  displayName,
		"format":        req.Format,
		"base_url":      req.BaseURL,
		"is_custom":     true,
		"category":      category,
		"service_kinds": serviceKinds,
	})
}

// Update modifies a provider.
func (h *ProviderHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		DisplayName   string            `json:"display_name"`
		BaseURL       string            `json:"base_url"`
		CustomHeaders map[string]string `json:"custom_headers"`
		Category      string            `json:"category"`
		ServiceKinds  []string          `json:"service_kinds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []string{}
	args := []interface{}{}
	if req.DisplayName != "" {
		updates = append(updates, "display_name = ?")
		args = append(args, req.DisplayName)
	}
	if req.BaseURL != "" {
		updates = append(updates, "base_url = ?")
		args = append(args, req.BaseURL)
	}
	if req.Category != "" {
		updates = append(updates, "category = ?")
		args = append(args, req.Category)
	}
	if len(req.ServiceKinds) > 0 {
		serviceKindsJSON, _ := json.Marshal(req.ServiceKinds)
		updates = append(updates, "service_kinds = ?")
		args = append(args, string(serviceKindsJSON))
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}

	args = append(args, id)
	query := "UPDATE provider_types SET " + joinStrings(updates, ", ") + " WHERE id = ?"
	result, err := h.db.Exec(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a custom provider (only if no connections).
func (h *ProviderHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	// Check if custom
	var isCustom bool
	h.db.QueryRow(`SELECT is_custom FROM provider_types WHERE id = ?`, id).Scan(&isCustom)
	if !isCustom {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete built-in provider"})
		return
	}

	// Check connections
	var connCount int
	h.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = ?`, id).Scan(&connCount)
	if connCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "provider has connections, delete them first"})
		return
	}

	h.db.Exec(`DELETE FROM provider_types WHERE id = ?`, id)
	executor.GetRegistry().Unregister(id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ProviderHandler) getStatusCounts(providerID string) map[string]int {
	counts := make(map[string]int)
	rows, err := h.db.Query(`
		SELECT status, COUNT(*) FROM connections
		WHERE provider_type_id = ?
		GROUP BY status
	`, providerID)
	if err != nil {
		return counts
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		counts[status] = count
	}
	return counts
}

// getDisabledReasonCounts returns per-reason totals for disabled connections of
// a provider so the provider cards can show *why* accounts are disabled.
func (h *ProviderHandler) getDisabledReasonCounts(providerID string) map[string]int {
	counts := make(map[string]int)
	rows, err := h.db.Query(`
		SELECT COALESCE(disabled_reason, 'unknown'), COUNT(*) FROM connections
		WHERE provider_type_id = ? AND status = 'disabled'
		GROUP BY disabled_reason
	`, providerID)
	if err != nil {
		return counts
	}
	defer rows.Close()
	for rows.Next() {
		var reason string
		var count int
		rows.Scan(&reason, &count)
		counts[reason] = count
	}
	return counts
}

// parseServiceKinds parses the JSON service_kinds column and falls back to ["llm"]
// when the value is empty, malformed, or an empty array.
func parseServiceKinds(raw string) []string {
	if raw == "" {
		return []string{"llm"}
	}
	var kinds []string
	if err := json.Unmarshal([]byte(raw), &kinds); err != nil || len(kinds) == 0 {
		return []string{"llm"}
	}
	return kinds
}

// testAllBatchSize limits how many connections are tested in parallel at once.
// A weighted semaphore uses this as the maximum number of in-flight goroutines.
const testAllBatchSize = 10

// testConnTimeout is the per-connection hard ceiling for the upstream test
// stream and any auxiliary network calls made during the test.
const testConnTimeout = 30 * time.Second

// testAllRefreshLead defines per-provider proactive refresh lead times.
// Matches OmniRoute REFRESH_LEAD_MS at open-sse/services/tokenRefresh.ts:32-49.
var testAllRefreshLead = map[string]time.Duration{
	"cx":       5 * time.Minute,  // Codex: Auth0 rotating refresh tokens
	"ag":       15 * time.Minute, // Antigravity: Google non-rotating refresh tokens
	"kiro":     5 * time.Minute,  // Kiro: AWS SSO OIDC one-time-use refresh tokens
	"copilot":  5 * time.Minute,  // Copilot: GitHub device-code tokens refresh early
	"grok-cli": 5 * time.Minute,  // Grok CLI: xAI OIDC device-code tokens refresh before expiry
}

const testAllDefaultRefreshLead = 5 * time.Minute

// TestAll tests all connections for a provider using streaming.
func (h *ProviderHandler) TestAll(c *gin.Context) {
	providerID := c.Param("id")

	// Get provider format
	var format string
	err := h.db.QueryRow(`SELECT format FROM provider_types WHERE id = ?`, providerID).Scan(&format)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	rows, err := h.db.Query(`
		SELECT c.id, COALESCE(c.api_key,''), COALESCE(c.oauth_token,''), COALESCE(c.auth_type,''), COALESCE(c.oauth_refresh_token,''), COALESCE(c.oauth_expires_at,0), COALESCE(pt.base_url,''), COALESCE(c.provider_specific_data, '')
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.provider_type_id = ? AND c.is_active = 1
	`, providerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type testInput struct {
		connID       string
		apiKey       string
		access       string
		refreshToken string
		expiresAt    int64
		authType     string
		baseURL      string
		psdMap       map[string]string
	}
	type testResult struct {
		ConnectionID string `json:"connection_id"`
		Status       string `json:"status"`
		Error        string `json:"error,omitempty"`
		LatencyMs    int64  `json:"latency_ms"`
	}

	exec, _, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no executor for provider: " + providerID})
		return
	}

	bodyBytes := buildTestBody(format, defaultTestModel(providerID), providerID)
	ctx := c.Request.Context()

	var inputs []testInput
	for rows.Next() {
		var connID, apiKey, accessToken, authType, refreshToken, baseURL, psdRaw string
		var expiresAt int64
		if err := rows.Scan(&connID, &apiKey, &accessToken, &authType, &refreshToken, &expiresAt, &baseURL, &psdRaw); err != nil {
			continue
		}
		var psdMap map[string]string
		if psdRaw != "" {
			json.Unmarshal([]byte(psdRaw), &psdMap)
		}
		inputs = append(inputs, testInput{connID: connID, apiKey: apiKey, access: accessToken, authType: authType, refreshToken: refreshToken, expiresAt: expiresAt, baseURL: baseURL, psdMap: psdMap})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	results := make([]testResult, len(inputs))

	// Cap peak concurrency with a semaphore; all goroutines share one WaitGroup.
	sem := make(chan struct{}, testAllBatchSize)
	var wg sync.WaitGroup
	requestCtx := ctx
	for i, in := range inputs {
		i, in := i, in
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// Per-connection timeout so a single stalled stream never blocks the whole batch.
			ctx, cancel := context.WithTimeout(requestCtx, testConnTimeout)
			defer cancel()

			start := time.Now()

			// Refresh expired/near-expiry OAuth tokens before testing.
			accessToken := in.access
			expiresAt := in.expiresAt
			psd := in.psdMap
			if h.authMgr != nil && in.authType == "oauth" && shouldRefreshTestToken(providerID, in.refreshToken, expiresAt) {
				creds := &auth.Credentials{
					AccessToken:  in.access,
					RefreshToken: in.refreshToken,
					ExpiresAt:    time.Unix(expiresAt, 0),
				}
				newCreds, err := h.authMgr.RefreshTokenForConnection(ctx, in.connID, auth.ProviderType(providerID), creds)
				if err != nil {
					latency := time.Since(start).Milliseconds()
					if isUnrecoverableRefreshError(err) {
						log.Printf("Unrecoverable refresh error for %s/%s: %v — blocking connection", providerID, in.connID, err)
						connID := in.connID
						h.execWrite(requestCtx, "testall:auth_failed:"+connID, func(d *sql.DB) error {
							_, err := d.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', disabled_reason = 'auth_failed', updated_at = ? WHERE id = ?`, time.Now().Unix(), connID)
							return err
						})
						if h.store != nil {
							h.store.UpdateStatus(connID, connstate.StatusDisabled, "auth_failed")
						}
					}
					results[i] = testResult{ConnectionID: in.connID, Status: "failed", Error: err.Error(), LatencyMs: latency}
					return
				}
				accessToken = newCreds.AccessToken
				expiresAt = newCreds.ExpiresAt.Unix()
				refreshToken := newCreds.RefreshToken
				if refreshToken == "" {
					refreshToken = in.refreshToken
				}
				if len(newCreds.ProviderSpecific) > 0 {
					psd = newCreds.ProviderSpecific
				}
			}

			streamResult, err := exec.ExecuteStream(ctx, &executor.Request{
				APIKey:               in.apiKey,
				AccessToken:          accessToken,
				BaseURL:              in.baseURL,
				Body:                 bodyBytes,
				Provider:             providerID,
				ProviderSpecificData: psd,
			})
			if err != nil {
				latency := time.Since(start).Milliseconds()
				if h.store != nil {
					det := connstate.DetectError(ctx, 0, "", err, providerID, "", nil)
					h.store.RecordFailure(in.connID, det)
				}
				results[i] = testResult{ConnectionID: in.connID, Status: "failed", Error: err.Error(), LatencyMs: latency}
				return
			}

			// Drain stream
			var firstErr error
			for chunk := range streamResult.Chunks {
				if chunk.Err != nil {
					firstErr = chunk.Err
					break
				}
			}
			latency := time.Since(start).Milliseconds()

			if firstErr != nil {
				if h.store != nil {
					det := connstate.DetectError(ctx, 0, "", firstErr, providerID, "", nil)
					h.store.RecordFailure(in.connID, det)
				}
				results[i] = testResult{ConnectionID: in.connID, Status: "failed", Error: firstErr.Error(), LatencyMs: latency}
			} else {
				if h.store != nil {
					h.store.RecordSuccess(in.connID)
				}
				results[i] = testResult{ConnectionID: in.connID, Status: "ok", LatencyMs: latency}
			}
		}()
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{"provider_id": providerID, "results": results})
}

// execWrite runs a DB write through the single-writer queue when available,
// falling back to a direct exec so callers can run with or without a queue.
func (h *ProviderHandler) execWrite(ctx context.Context, label string, fn func(*sql.DB) error) error {
	if h.writeQueue != nil {
		return h.writeQueue.Do(ctx, label, fn)
	}
	return fn(h.db)
}

// shouldRefreshTestToken reports whether an OAuth token should be refreshed
// before testing based on per-provider lead times.
func shouldRefreshTestToken(providerID, refreshToken string, expiresAt int64) bool {
	if expiresAt == 0 {
		return false
	}
	// GitHub device-code OAuth does not return a refresh token, but the
	// short-lived Copilot bearer token can still be refreshed from the access
	// token. For every other provider a refresh token is required.
	if refreshToken == "" && providerID != "copilot" {
		return false
	}
	lead := testAllDefaultRefreshLead
	if v, ok := testAllRefreshLead[providerID]; ok {
		lead = v
	}
	return time.Until(time.Unix(expiresAt, 0)) <= lead
}

// isUnrecoverableRefreshError checks if a refresh error indicates the token is
// permanently invalid and should not be retried. Matches OmniRoute isUnrecoverableRefreshError
// at open-sse/services/tokenRefresh.ts:9-19.
func isUnrecoverableRefreshError(err error) bool {
	if err == nil {
		return false
	}
	code := extractOAuthErrorCode(err.Error(), 0)
	return code != ""
}

// unrecoverableCodes are OAuth error codes that mean the refresh token is permanently dead.
// Matches OmniRoute UNRECOVERABLE_OAUTH_ERROR_CODES at tokenRefresh.ts:204-212.
var unrecoverableCodes = map[string]bool{
	"invalid_grant":               true,
	"invalid_request":             true,
	"refresh_token_reused":        true,
	"refresh_token_invalidated":   true,
	"invalid_token":               true,
	"token_expired":               true,
	"expired_token":               true,
	"unauthorized_client":         true,
	"access_denied":               true,
	"unrecoverable_refresh_error": true,
}

// extractOAuthErrorCode extracts a canonical OAuth error code from an error body
// of ANY shape. Handles JSON objects, double-encoded JSON strings, and regex fallback.
// Matches OmniRoute extractOAuthErrorCode at tokenRefresh.ts:229-262.
func extractOAuthErrorCode(raw string, depth int) string {
	if raw == "" || depth > 6 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		if code, ok := obj["error"].(string); ok {
			code = strings.ToLower(code)
			if unrecoverableCodes[code] {
				return code
			}
		}
		if detail, ok := obj["error_description"].(string); ok {
			if code := extractOAuthErrorCode(detail, depth+1); code != "" {
				return code
			}
		}
		return ""
	}
	// Fallback: search for canonical OAuth error codes anywhere in the string.
	for code := range unrecoverableCodes {
		if strings.Contains(strings.ToLower(raw), code) {
			return code
		}
	}
	return ""
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}

// AddConnection adds a connection to a provider.
func (h *ProviderHandler) AddConnection(c *gin.Context) {
	providerID := c.Param("id")
	var req struct {
		Name                 string            `json:"name" binding:"required"`
		APIKey               string            `json:"api_key"`
		AuthType             string            `json:"auth_type"`
		Priority             int               `json:"priority"`
		ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify provider exists
	var exists bool
	h.db.QueryRow(`SELECT COUNT(*) > 0 FROM provider_types WHERE id = ?`, providerID).Scan(&exists)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Validate CF connections require an Account ID
	if providerID == "cf" {
		accountID := req.ProviderSpecificData["accountId"]
		if accountID == "" {
			accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
		if accountID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cloudflare Workers AI requires an Account ID. Add it in provider_specific_data.accountId or set CLOUDFLARE_ACCOUNT_ID env var."})
			return
		}
	}

	// OpenCode Free (oc): additional connections must use a proxy pool.
	// The default direct connection is auto-seeded by migration and cannot be added via API.
	if providerID == "oc" {
		proxyPoolID := req.ProviderSpecificData["proxyPoolId"]
		if proxyPoolID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "OpenCode Free connections require a proxy pool. Select one from the proxy pools list."})
			return
		}
		// Verify the proxy pool exists and is active
		var poolExists bool
		h.db.QueryRow("SELECT COUNT(*) > 0 FROM proxy_pools WHERE id = ? AND is_active = 1", proxyPoolID).Scan(&poolExists)
		if !poolExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Selected proxy pool not found or inactive"})
			return
		}
		// oc is no-auth; force auth_type to none
		req.AuthType = "none"
		req.APIKey = ""
		// Each additional oc connection is treated as a distinct logical account.
		// Assign a stable account id and label so it can be tracked/exhausted independently.
		if req.ProviderSpecificData["accountId"] == "" {
			req.ProviderSpecificData["accountId"] = "oc-" + uuid.New().String()[:8]
		}
		if req.ProviderSpecificData["accountLabel"] == "" {
			req.ProviderSpecificData["accountLabel"] = req.Name
		}
	}

	// MiMoCode: additional connections must use a proxy pool.
	// The default direct connection is auto-seeded by migration and cannot be added via API.
	if providerID == "mimocode" {
		proxyPoolID := req.ProviderSpecificData["proxyPoolId"]
		if proxyPoolID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MiMoCode connections require a proxy pool. Select one from the proxy pools list."})
			return
		}
		// Verify the proxy pool exists and is active
		var poolExists bool
		h.db.QueryRow("SELECT COUNT(*) > 0 FROM proxy_pools WHERE id = ? AND is_active = 1", proxyPoolID).Scan(&poolExists)
		if !poolExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Selected proxy pool not found or inactive"})
			return
		}
		// mimocode is no-auth; force auth_type to none
		req.AuthType = "none"
		req.APIKey = ""
		// Each additional mimocode connection must behave as a brand-new logical account.
		// Always generate a fresh account id, label, and fingerprint so no two connections
		// share the same device identity, which is required to avoid MiMoCode anti-abuse flags.
		req.ProviderSpecificData["accountId"] = "mimocode-" + uuid.New().String()[:8]
		req.ProviderSpecificData["accountLabel"] = req.Name
		req.ProviderSpecificData["fingerprint"] = generateFingerprint()
	}

	connID := uuid.New().String()
	now := time.Now().Unix()
	if req.AuthType == "" {
		req.AuthType = "api_key"
	}

	// Enforce upstream key validation before persisting API-key-backed connections.
	// Providers can opt out via provider_types.skip_key_validation.
	if req.AuthType != "oauth" && req.AuthType != "none" && req.APIKey != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		valid, errMsg := h.validateKeyForRequest(ctx, providerID, req.APIKey, req.ProviderSpecificData)
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
	}

	apiKey := sql.NullString{}
	if req.APIKey != "" {
		apiKey = sql.NullString{String: req.APIKey, Valid: true}
	}
	initialStatus := "ready"
	disabledReason := sql.NullString{}
	active := 1
	if req.AuthType == "oauth" {
		// Not eligible until OAuth completes. Mark as disabled with a manual
		// reason so it is excluded from routing and can be enabled by the
		// OAuth callback flow without ambiguity.
		initialStatus = "disabled"
		disabledReason = sql.NullString{String: "manual", Valid: true}
		active = 0
	}

	var psdJSON sql.NullString
	if len(req.ProviderSpecificData) > 0 {
		b, err := json.Marshal(req.ProviderSpecificData)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider_specific_data"})
			return
		}
		psdJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := h.db.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, priority, provider_specific_data, status, disabled_reason, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, connID, providerID, req.Name, req.AuthType, apiKey, req.Priority, psdJSON, initialStatus, disabledReason, active, now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Seed in-memory store so eligibility routing picks up the new connection immediately
	if h.store != nil {
		h.store.SeedConnection(connID, providerID, initialStatus, req.Priority)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"id": connID, "name": req.Name, "status": initialStatus})
}

// BulkAddConnections adds multiple connections at once, processing them in
// batches. Each batch is written as a single transaction. When a WriteQueue is
// wired it runs through the queue (the app's only SQLite writer), so a large
// import never contends with the live gateway for the write lock. Per-row
// failures are reported instead of being silently dropped.
func (h *ProviderHandler) BulkAddConnections(c *gin.Context) {
	providerID := c.Param("id")
	type bulkInput struct {
		Name                 string            `json:"name"`
		APIKey               string            `json:"api_key"`
		Priority             int               `json:"priority"`
		ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
	}
	var req struct {
		Connections        []bulkInput `json:"connections"`
		ValidateSampleSize int         `json:"validate_sample_size"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// OpenCode Free (oc): bulk add not supported — each connection requires a proxy pool.
	if providerID == "oc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OpenCode Free connections require a proxy pool and cannot be bulk-added. Use the single add connection form."})
		return
	}

	// Validate CF connections require Account ID (fail fast before any inserts).
	if providerID == "cf" {
		for i, conn := range req.Connections {
			accountID := conn.ProviderSpecificData["accountId"]
			if accountID == "" {
				accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
			}
			if accountID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "connection #" + fmt.Sprintf("%d", i+1) + ": Cloudflare Workers AI requires an Account ID"})
				return
			}
		}
	}

	const maxBulkConnections = 5000
	if len(req.Connections) > maxBulkConnections {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "too many connections: maximum " + strconv.Itoa(maxBulkConnections) + " per bulk import"})
		return
	}

	const batchSize = 200
	now := time.Now().Unix()

	type classifiedConn struct {
		bulkInput
		state  string // accepted, rejected, duplicate
		errMsg string
	}

	// Classify duplicate API keys within this import request. Empty keys are not
	// considered duplicates because they typically represent no-auth providers.
	seenKeys := make(map[string]int)
	classified := make([]classifiedConn, 0, len(req.Connections))
	for i, conn := range req.Connections {
		if conn.APIKey != "" {
			if prev, dup := seenKeys[conn.APIKey]; dup {
				classified = append(classified, classifiedConn{
					bulkInput: conn,
					state:     "duplicate",
					errMsg:    fmt.Sprintf("connection %q: duplicate api_key (first at index %d)", conn.Name, prev+1),
				})
				continue
			}
			seenKeys[conn.APIKey] = i
		}
		classified = append(classified, classifiedConn{bulkInput: conn, state: "accepted"})
	}

	// Sample validation: test up to N accepted, non-empty keys against the provider.
	// Rows that fail validation are still persisted with status='disabled',
	// disabled_reason='auth_failed', and is_active=0 so the lifecycle GC can
	// clean them up later.
	if req.ValidateSampleSize > 0 {
		sample := make([]*classifiedConn, 0, len(classified))
		for i := range classified {
			if classified[i].state == "accepted" && classified[i].APIKey != "" {
				sample = append(sample, &classified[i])
			}
		}
		if len(sample) > req.ValidateSampleSize {
			sample = sample[:req.ValidateSampleSize]
		}
		ctx := c.Request.Context()
		for _, item := range sample {
			valid, errMsg := h.validateKeyForRequest(ctx, providerID, item.APIKey, item.ProviderSpecificData)
			if !valid {
				item.state = "rejected"
				item.errMsg = fmt.Sprintf("connection %q: validation failed: %s", item.Name, errMsg)
			}
		}
	}

	type seedInfo struct {
		id       string
		priority int
	}
	var errors []string
	var seeded []seedInfo
	var totalAccepted, totalRejected, totalDuplicates, dbErrors int

	// batchResult is returned from the batch closure; the handler reads it
	// only AFTER WriteQueue.Do returns (the queue's done-channel establishes a
	// happens-before edge, so this is race-free — do NOT mutate handler
	// locals from inside the closure).
	type batchResult struct {
		accepted   int
		rejected   int
		duplicates int
		seeded     []seedInfo
		fails      []string
		err        error
	}

	runBatch := func(d *sql.DB, batch []classifiedConn) batchResult {
		res := batchResult{}
		tx, err := d.Begin()
		if err != nil {
			res.err = err
			return res
		}
		for _, item := range batch {
			connID := uuid.New().String()
			apiKey := sql.NullString{String: item.APIKey, Valid: item.APIKey != ""}
			var psdJSON sql.NullString
			if len(item.ProviderSpecificData) > 0 {
				if b, err := json.Marshal(item.ProviderSpecificData); err == nil {
					psdJSON = sql.NullString{String: string(b), Valid: true}
				}
			}
			status := "ready"
			disabledReason := sql.NullString{}
			active := 1
			switch item.state {
			case "rejected":
				status = "disabled"
				disabledReason = sql.NullString{String: "auth_failed", Valid: true}
				active = 0
			case "duplicate":
				status = "disabled"
				disabledReason = sql.NullString{String: "manual", Valid: true}
				active = 0
			}
			if _, err := tx.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, priority, provider_specific_data, status, disabled_reason, is_active, created_at, updated_at) VALUES (?, ?, ?, 'api_key', ?, ?, ?, ?, ?, ?, ?, ?)`,
				connID, providerID, item.Name, apiKey, item.Priority, psdJSON, status, disabledReason, active, now, now); err != nil {
				res.fails = append(res.fails, fmt.Sprintf("connection %q: %s", item.Name, err.Error()))
				continue
			}
			switch item.state {
			case "accepted":
				res.accepted++
				res.seeded = append(res.seeded, seedInfo{id: connID, priority: item.Priority})
			case "rejected":
				res.rejected++
			case "duplicate":
				res.duplicates++
			}
		}
		if err := tx.Commit(); err != nil {
			res.err = err
			return res
		}
		return res
	}

	for start := 0; start < len(classified); start += batchSize {
		end := start + batchSize
		if end > len(classified) {
			end = len(classified)
		}
		batch := classified[start:end]
		var br batchResult
		var batchErr error
		if h.writeQueue != nil {
			batchErr = h.writeQueue.Do(c.Request.Context(), "bulkAddConnections:batch", func(d *sql.DB) error {
				br = runBatch(d, batch)
				return br.err
			})
		} else {
			br = runBatch(h.db, batch)
		}
		if batchErr != nil {
			// Whole-batch failure: nothing in this batch was persisted.
			for _, item := range batch {
				dbErrors++
				errors = append(errors, fmt.Sprintf("connection %q: %s", item.Name, batchErr.Error()))
			}
			continue
		}
		totalAccepted += br.accepted
		totalRejected += br.rejected
		totalDuplicates += br.duplicates
		dbErrors += len(br.fails)
		errors = append(errors, br.fails...)
		seeded = append(seeded, br.seeded...)
	}

	// Append duplicate/validation error messages so callers can see why each row
	// was not accepted.
	for _, item := range classified {
		if item.errMsg != "" {
			errors = append(errors, item.errMsg)
		}
	}

	// Seed in-memory store ONLY for committed accepted rows, then recompute eligibility once.
	if h.store != nil {
		for _, s := range seeded {
			h.store.SeedConnection(s.id, providerID, "ready", s.priority)
		}
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"total":      len(req.Connections),
		"accepted":   totalAccepted,
		"rejected":   totalRejected,
		"duplicates": totalDuplicates,
		"created":    totalAccepted,
		"failed":     totalRejected + totalDuplicates + dbErrors,
		"errors":     errors,
	})
}

// BulkAssignProxy assigns (or unbinds) a proxy pool for multiple connections of
// a provider that requires proxy pools. It mutates provider_specific_data in a
// single transaction.
func (h *ProviderHandler) BulkAssignProxy(c *gin.Context) {
	providerID := c.Param("id")
	var req struct {
		ConnectionIDs []string `json:"connection_ids" binding:"required"`
		ProxyPoolID   *string  `json:"proxy_pool_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only oc and mimocode use proxy pools today.
	if providerID != "oc" && providerID != "mimocode" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bulk proxy assignment is only supported for providers that require proxy pools"})
		return
	}

	var exists bool
	h.db.QueryRow(`SELECT COUNT(*) > 0 FROM provider_types WHERE id = ?`, providerID).Scan(&exists)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	if len(req.ConnectionIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"updated": 0})
		return
	}

	// Validate proxy pool when binding.
	if req.ProxyPoolID != nil && *req.ProxyPoolID != "" {
		var poolExists bool
		h.db.QueryRow(`SELECT COUNT(*) > 0 FROM proxy_pools WHERE id = ? AND is_active = 1`, *req.ProxyPoolID).Scan(&poolExists)
		if !poolExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "proxy pool not found or inactive"})
			return
		}
	}

	const maxBulk = 5000
	if len(req.ConnectionIDs) > maxBulk {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "too many connections: maximum " + strconv.Itoa(maxBulk)})
		return
	}

	placeholders := make([]string, len(req.ConnectionIDs))
	args := make([]interface{}, 0, len(req.ConnectionIDs)+1)
	for i, id := range req.ConnectionIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")

	now := time.Now().Unix()
	run := func(d *sql.DB) (int, error) {
		tx, err := d.Begin()
		if err != nil {
			return 0, err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()

		// Lock the relevant rows and load current provider_specific_data.
		rows, err := tx.Query(`
			SELECT id, COALESCE(provider_specific_data, '') FROM connections
			WHERE provider_type_id = ? AND is_active = 1 AND id IN (`+inClause+`)
		`, append([]interface{}{providerID}, args...)...)
		if err != nil {
			return 0, err
		}
		defer rows.Close()

		type item struct {
			id  string
			psd map[string]string
		}
		var items []item
		for rows.Next() {
			var id, raw string
			if err := rows.Scan(&id, &raw); err != nil {
				return 0, err
			}
			psd := map[string]string{}
			if raw != "" {
				if e := json.Unmarshal([]byte(raw), &psd); e != nil {
					return 0, e
				}
			}
			items = append(items, item{id: id, psd: psd})
		}
		if err := rows.Err(); err != nil {
			return 0, err
		}
		if len(items) == 0 {
			_ = tx.Commit()
			return 0, nil
		}

		// Update each row.
		for _, it := range items {
			if req.ProxyPoolID != nil && *req.ProxyPoolID != "" {
				it.psd["proxyPoolId"] = *req.ProxyPoolID
			} else {
				delete(it.psd, "proxyPoolId")
			}
			b, err := json.Marshal(it.psd)
			if err != nil {
				return 0, err
			}
			var psdJSON interface{} = b
			if len(it.psd) == 0 {
				psdJSON = nil
			}
			if _, err := tx.Exec(`UPDATE connections SET provider_specific_data = ?, updated_at = ? WHERE id = ?`, psdJSON, now, it.id); err != nil {
				return 0, err
			}
		}

		return len(items), tx.Commit()
	}

	var updated int
	var err error
	if h.writeQueue != nil {
		err = h.writeQueue.Do(c.Request.Context(), "bulk-assign-proxy", func(d *sql.DB) error {
			updated, err = run(d)
			return err
		})
	} else {
		updated, err = run(h.db)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

// ValidateKey checks if an API key is valid for a provider by attempting a lightweight model list request.
func (h *ProviderHandler) ValidateKey(c *gin.Context) {
	var req struct {
		Provider             string            `json:"provider" binding:"required"`
		APIKey               string            `json:"api_key" binding:"required"`
		ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	valid, errMsg := h.validateKeyForRequest(c.Request.Context(), req.Provider, req.APIKey, req.ProviderSpecificData)
	if !valid && errMsg == "provider not found" {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": valid})
}

// validateKeyForRequest performs a lightweight upstream request to verify the
// API key. It returns (true, "") when the key is accepted by the upstream, or
// (false, <upstream message>) otherwise. Providers may opt out via
// provider_types.skip_key_validation.
func (h *ProviderHandler) validateKeyForRequest(ctx context.Context, providerID, apiKey string, psd map[string]string) (bool, string) {
	var provider struct {
		ID                string
		Format            string
		BaseURL           string
		SkipKeyValidation int
	}
	if err := h.db.QueryRow(`SELECT id, format, base_url, skip_key_validation FROM provider_types WHERE id = ?`, providerID).
		Scan(&provider.ID, &provider.Format, &provider.BaseURL, &provider.SkipKeyValidation); err != nil {
		if err == sql.ErrNoRows {
			return false, "provider not found"
		}
		return false, err.Error()
	}

	if provider.SkipKeyValidation == 1 {
		return true, ""
	}

	exec, _, ok := h.registry.Get(provider.ID)
	if !ok {
		return false, "no executor for provider"
	}

	testModel := defaultTestModel(providerID)
	if testModel == "" {
		testModel = "gpt-4o-mini"
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := exec.ExecuteStream(ctx, &executor.Request{
		APIKey:               apiKey,
		BaseURL:              provider.BaseURL,
		Body:                 buildTestBody(provider.Format, testModel, providerID),
		Provider:             providerID,
		Model:                testModel,
		ProviderSpecificData: psd,
	})
	if err != nil {
		return false, err.Error()
	}

	for chunk := range resp.Chunks {
		if chunk.Err != nil {
			return false, chunk.Err.Error()
		}
		if chunk.Payload != nil {
			return true, ""
		}
	}

	return false, "invalid API key"
}

// GetSettings returns the persistent JSON-file settings for a provider.
func (h *ProviderHandler) GetSettings(c *gin.Context) {
	id := c.Param("id")
	s, err := h.providerCfg.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider_id":  id,
		"routing_mode": s.RoutingMode,
		"flat_rate":    s.FlatRate,
	})
}

// UpdateSettings persists per-provider settings to a JSON file.
func (h *ProviderHandler) UpdateSettings(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		RoutingMode providercfg.RoutingMode `json:"routing_mode"`
		FlatRate    bool                    `json:"flat_rate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch req.RoutingMode {
	case providercfg.FirstEligible, providercfg.RoundRobin, providercfg.Random, providercfg.Affinity:
		// ok
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "routing_mode must be one of: first_eligible, round_robin, random, affinity",
		})
		return
	}

	if err := h.providerCfg.Save(id, providercfg.ProviderSettings{RoutingMode: req.RoutingMode, FlatRate: req.FlatRate}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"provider_id":  id,
		"routing_mode": req.RoutingMode,
		"flat_rate":    req.FlatRate,
	})
}
