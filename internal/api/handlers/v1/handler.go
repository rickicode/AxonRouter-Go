package v1

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
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
			COALESCE(c.api_key, '') as api_key,
			COALESCE(c.oauth_token, '') as oauth_token,
			COALESCE(c.oauth_refresh_token, '') as oauth_refresh_token,
			COALESCE(c.oauth_expires_at, 0) as oauth_expires_at,
			COALESCE(pt.base_url, '') as base_url,
			COALESCE(c.status, 'ready') as status
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE pt.id = ? AND c.is_active = 1
		ORDER BY c.id
	`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []*Connection
	for rows.Next() {
		var conn Connection
		var expiresAt int64
		if err := rows.Scan(&conn.ID, &conn.Provider, &conn.APIKey, &conn.AccessToken,
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

// refreshOAuthToken refreshes an expired OAuth token.
func (h *Handler) refreshOAuthToken(ctx context.Context, conn *Connection, provider string) error {
	if h.authMgr == nil {
		return fmt.Errorf("auth manager not configured")
	}

	// Map provider prefix to auth ProviderType (constants match DB IDs)
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
		return fmt.Errorf("refresh token: %w", err)
	}

	// Update connection in memory
	conn.AccessToken = newCreds.AccessToken
	conn.OAuthExpiresAt = newCreds.ExpiresAt

	// Update DB synchronously with error logging (Q2 fix)
	_, err = h.db.ExecContext(ctx, `UPDATE connections SET oauth_token = ?, oauth_expires_at = ?, updated_at = ? WHERE id = ?`,
		newCreds.AccessToken, newCreds.ExpiresAt.Unix(), time.Now().Unix(), conn.ID)
	if err != nil {
		// Log but don't fail — in-memory token is valid for this request
		log.Printf("WARN: failed to persist OAuth token for connection %s: %v", conn.ID, err)
	}

	return nil
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
