package v1

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

const maxBodySize = 10 * 1024 * 1024 // 10MB

// readBody reads the request body with a size limit.
func readBody(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			return nil, fmt.Errorf("request body too large (max %d bytes)", maxBodySize)
		}
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// Connection holds runtime connection data for a provider.
type Connection struct {
	ID                   string
	Provider             string
	Priority             int
	APIKey               string
	AccessToken          string
	RefreshToken         string
	OAuthExpiresAt       time.Time
	BaseURL              string
	Status               string
	ProviderSpecificData string
	LastUsed             time.Time
}

// Handler is the base handler for all /v1/* endpoints.
type Handler struct {
	db       *sql.DB
	registry *executor.Registry
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	combo    *combo.Handler
	tracker  *usage.Tracker
	authMgr  *auth.Manager
	resolver *proxypool.Resolver
	conns    sync.Map // provider -> cachedConns
}

// cachedConns holds cached connections with expiry.
type cachedConns struct {
	conns     []*Connection
	expiresAt time.Time
}

// NewHandler creates a new v1 handler with all dependencies.
func NewHandler(
	db *sql.DB,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	comboHandler *combo.Handler,
	tracker *usage.Tracker,
	authManager *auth.Manager,
	resolver *proxypool.Resolver,
) *Handler {
	return &Handler{
		db:       db,
		registry: executor.GetRegistry(),
		store:    store,
		elig:     elig,
		combo:    comboHandler,
		tracker:  tracker,
		authMgr:  authManager,
		resolver: resolver,
	}
}

// resolveExecutor finds the executor for a provider/model.
func (h *Handler) resolveExecutor(provider, model string) (executor.Executor, executor.ProviderFormat, error) {
	exec, format, ok := h.registry.Get(provider)
	if !ok {
		return nil, "", fmt.Errorf("unknown provider: %s", provider)
	}
	return exec, format, nil
}

// getConnection returns an active connection for a provider using O(1) eligibility snapshot.
// Replaces the old O(n) linear scan with TTL cache.
func (h *Handler) getConnection(ctx context.Context, provider string, modelID string) (*Connection, error) {
	// O(1) eligibility pick
	cs := h.elig.PickConnection(provider, modelID)
	if cs == nil {
		return nil, fmt.Errorf("no eligible connection for provider: %s", provider)
	}

	// Load credentials by ID (single row query, not full scan)
	conn, err := h.loadConnectionByID(ctx, cs.ID)
	if err != nil {
		return nil, fmt.Errorf("load connection %s: %w", cs.ID, err)
	}
	return conn, nil
}

// RefreshConnections clears the connection cache for a provider.
// ponytail: kept for backward compat but no longer used by getConnection (eligibility snapshot is always fresh).
func (h *Handler) RefreshConnections(provider string) {
	h.conns.Delete(provider)
}

// invalidateCache clears the connection cache for a provider.
// ponytail: kept for backward compat but no longer needed — eligibility snapshot is always fresh.
func (h *Handler) invalidateCache(provider string) {
	h.conns.Delete(provider)
}

// loadConnections loads connections from the database.
func (h *Handler) loadConnections(ctx context.Context, provider string) ([]*Connection, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT c.id, pt.id as provider_prefix,
			COALESCE(c.priority, 0) as priority,
			COALESCE(c.api_key, '') as api_key,
			COALESCE(c.oauth_token, '') as oauth_token,
			COALESCE(c.oauth_refresh_token, '') as oauth_refresh_token,
			COALESCE(c.oauth_expires_at, 0) as oauth_expires_at,
			COALESCE(pt.base_url, '') as base_url,
			COALESCE(c.status, 'ready') as status
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE pt.id = ? AND c.is_active = 1
		ORDER BY c.priority DESC, c.id
	`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []*Connection
	for rows.Next() {
		var conn Connection
		var expiresAt int64
		if err := rows.Scan(&conn.ID, &conn.Provider, &conn.Priority, &conn.APIKey, &conn.AccessToken,
			&conn.RefreshToken, &expiresAt, &conn.BaseURL, &conn.Status); err != nil {
			continue
		}
		if expiresAt > 0 {
			conn.OAuthExpiresAt = time.Unix(expiresAt, 0)
		}
		if conn.Status == "" {
			conn.Status = "ready"
		}
		conns = append(conns, &conn)
	}

	return conns, nil
}

// loadConnectionByID loads a single connection by ID.
func (h *Handler) loadConnectionByID(ctx context.Context, connID string) (*Connection, error) {
	var conn Connection
	var expiresAt int64
	var psd sql.NullString
	err := h.db.QueryRowContext(ctx, `
		SELECT c.id, c.provider_type_id as provider_prefix,
			COALESCE(c.api_key, '') as api_key,
			COALESCE(c.oauth_token, '') as oauth_token,
			COALESCE(c.oauth_refresh_token, '') as oauth_refresh_token,
			COALESCE(c.oauth_expires_at, 0) as oauth_expires_at,
			COALESCE(pt.base_url, '') as base_url,
			COALESCE(c.status, 'ready') as status,
			c.provider_specific_data
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.id = ?
	`, connID).Scan(&conn.ID, &conn.Provider, &conn.APIKey, &conn.AccessToken,
		&conn.RefreshToken, &expiresAt, &conn.BaseURL, &conn.Status, &psd)
	if err != nil {
		return nil, err
	}
	if expiresAt > 0 {
		conn.OAuthExpiresAt = time.Unix(expiresAt, 0)
	}
	if psd.Valid {
		conn.ProviderSpecificData = psd.String
	}
	return &conn, nil
}

// refreshLeadMs defines per-provider proactive refresh lead times (ms).
// Matches OmniRoute REFRESH_LEAD_MS at open-sse/services/tokenRefresh.ts:32-49.
var refreshLeadMs = map[string]time.Duration{
	"cx":   5 * time.Minute,  // Codex: Auth0 rotating refresh tokens
	"ag":   15 * time.Minute, // Antigravity: Google non-rotating refresh tokens
	"kiro": 5 * time.Minute,  // Kiro: AWS SSO OIDC one-time-use refresh tokens
}

const defaultRefreshLeadMs = 5 * time.Minute

// proactiveRefreshToken checks if a token should be refreshed proactively
// based on per-provider lead times. Matches OmniRoute checkAndRefreshToken.
func (h *Handler) proactiveRefreshToken(ctx context.Context, conn *Connection, provider string) bool {
	if h.authMgr == nil || conn.RefreshToken == "" || conn.OAuthExpiresAt.IsZero() {
		return false
	}
	lead := defaultRefreshLeadMs
	if v, ok := refreshLeadMs[provider]; ok {
		lead = v
	}
	if time.Until(conn.OAuthExpiresAt) > lead {
		return false
	}
	if err := h.refreshOAuthToken(ctx, conn, provider); err != nil {
		log.Printf("Proactive refresh failed for %s/%s: %v", provider, conn.ID, err)
		return false
	}
	return true
}

// refreshOAuthToken refreshes an expired OAuth token.
func (h *Handler) refreshOAuthToken(ctx context.Context, conn *Connection, provider string) error {
	if h.authMgr == nil {
		return fmt.Errorf("auth manager not configured")
	}

	providerType := auth.ProviderType(provider)
	if _, ok := h.authMgr.GetService(providerType); !ok {
		return fmt.Errorf("oauth not supported for provider: %s", provider)
	}

	creds := &auth.Credentials{
		AccessToken:  conn.AccessToken,
		RefreshToken: conn.RefreshToken,
		ExpiresAt:    conn.OAuthExpiresAt,
	}

	newCreds, err := h.authMgr.RefreshToken(ctx, providerType, creds)
	if err != nil {
		// Check for unrecoverable errors (matches OmniRoute isUnrecoverableRefreshError)
		if isUnrecoverableRefreshError(err) {
			log.Printf("Unrecoverable refresh error for %s/%s: %v — blocking connection", provider, conn.ID, err)
			h.db.ExecContext(ctx, `UPDATE connections SET is_active = 0, status = 'auth_failed', updated_at = ? WHERE id = ?`,
				time.Now().Unix(), conn.ID)
			h.store.UpdateStatus(conn.ID, connstate.StatusAuthFailed)
			h.elig.Update(h.store)
		}
		return fmt.Errorf("refresh token: %w", err)
	}

	// Update connection in memory
	conn.AccessToken = newCreds.AccessToken
	conn.OAuthExpiresAt = newCreds.ExpiresAt
	if newCreds.RefreshToken != "" {
		conn.RefreshToken = newCreds.RefreshToken
	}

	// Persist to DB (use conn.RefreshToken which preserves existing token when newCreds omits it)
	_, err = h.db.ExecContext(ctx, `UPDATE connections SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, updated_at = ? WHERE id = ?`,
		conn.AccessToken, conn.RefreshToken, conn.OAuthExpiresAt.Unix(), time.Now().Unix(), conn.ID)
	if err != nil {
		log.Printf("WARN: failed to persist OAuth token for connection %s: %v", conn.ID, err)
	}

	return nil
}

// isUnrecoverableRefreshError checks if a refresh error indicates the token is
// permanently invalid and should not be retried. Matches OmniRoute isUnrecoverableRefreshError
// at open-sse/services/tokenRefresh.ts:9-19.
func isUnrecoverableRefreshError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	unrecoverable := []string{
		"invalid_grant",
		"invalid_request",
		"invalid_token",
		"token_expired",
		"refresh_token_reused",
		"refresh_token_invalidated",
		"unrecoverable_refresh_error",
	}
	for _, kw := range unrecoverable {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// proxyContext resolves proxy config for a connection and returns a context with it attached.
func (h *Handler) proxyContext(ctx context.Context, conn *Connection) context.Context {
	if h.resolver == nil {
		return ctx
	}
	cfg := h.resolver.Resolve(conn.ProviderSpecificData, conn.Provider)
	return executor.ContextWithProxy(ctx, executor.ProxyConfig{
		Enabled:     cfg.Enabled,
		ProxyURL:    cfg.ProxyURL,
		NoProxy:     cfg.NoProxy,
		RelayURL:    cfg.RelayURL,
		RelayAuth:   cfg.RelayAuth,
		RelayType:   cfg.RelayType,
		StrictProxy: cfg.StrictProxy,
	})
}

// checkAutoDisable checks if a connection should be auto-disabled due to repeated ban signals.
// Matches OmniRoute autoDisableBannedAccounts: permanently disable after threshold consecutive bans.
// Persists ban count to DB so it survives restarts.
func (h *Handler) checkAutoDisable(connID, provider string) {
	cs := h.store.Get(connID)
	if cs == nil {
		return
	}
	threshold := 3
	banCount := cs.BanCount

	// Persist ban count to DB
	h.db.Exec(`UPDATE connections SET consecutive_ban_count = ?, updated_at = ? WHERE id = ?`,
		banCount, time.Now().Unix(), connID)

	if banCount >= threshold {
		log.Printf("Auto-disabling connection %s after %d consecutive ban signals", connID, banCount)
		h.db.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', updated_at = ? WHERE id = ?`,
			time.Now().Unix(), connID)
		h.store.UpdateStatus(connID, connstate.StatusDisabled)
		h.elig.Update(h.store)
	}
}

// resetBanCount resets the consecutive ban count on success (persists to DB).
func (h *Handler) resetBanCount(connID string) {
	cs := h.store.Get(connID)
	if cs != nil && cs.BanCount > 0 {
		cs.BanCount = 0
		h.db.Exec(`UPDATE connections SET consecutive_ban_count = 0, updated_at = ? WHERE id = ?`,
			time.Now().Unix(), connID)
	}
}
