package v1

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/active"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
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

// TrackActive registers an in-flight request so the dashboard's live
// "ActiveOctopus" / in-flight panel can display it, and deregisters it
// once the handler returns. The chosen account is bound lazily in
// getConnection/prepareConnection once connection selection completes.
func (h *Handler) TrackActive() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBodySize))
		if err != nil {
			c.Next()
			return
		}
		// Restore body for downstream handlers (readBody reads it again).
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		model := executor.JSONGet(raw, "model")
		if model == "" {
			c.Next()
			return
		}
		provider, _ := executor.SplitModel(model)
		id := active.NewID()
		active.Register(&active.Request{
			ID:             id,
			StartedAt:      time.Now().UnixMilli(),
			ProviderTypeID: provider,
			ModelID:        model,
			Modality:       modalityFromPath(c.Request.URL.Path),
			Stream:         executor.IsStreamRequest(raw),
		})
		c.Request = c.Request.WithContext(active.WithID(c.Request.Context(), id))
		defer active.Deregister(id)
		c.Next()
	}
}

// modalityFromPath derives a human modality label from the request path.
func modalityFromPath(path string) string {
	switch {
	case strings.Contains(path, "chat"):
		return "chat"
	case strings.Contains(path, "messages"):
		return "messages"
	case strings.Contains(path, "responses"):
		return "responses"
	case strings.Contains(path, "embeddings"):
		return "embeddings"
	case strings.Contains(path, "images"):
		return "images"
	case strings.Contains(path, "video"):
		return "video"
	case strings.Contains(path, "speech"), strings.Contains(path, "tts"):
		return "tts"
	case strings.Contains(path, "transcriptions"), strings.Contains(path, "stt"):
		return "stt"
	default:
		return "chat"
	}
}

// bindActiveConn fills in the chosen account for the in-flight request
// tracked on the request context (if any).
func (h *Handler) bindActiveConn(ctx context.Context, conn *Connection) {
	if id, ok := active.IDFrom(ctx); ok {
		active.BindConn(id, conn.ID, conn.Name)
	}
}

// Connection holds runtime connection data for a provider.
type Connection struct {
	ID                   string
	Provider             string
	Name                 string
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
	db                  *sql.DB
	registry            *executor.Registry
	store               *connstate.Store
	elig                *connstate.EligibilityManager
	combo               *combo.Handler
	tracker             *usage.Tracker
	authMgr             *auth.Manager
	resolver            *proxypool.Resolver
	exhaustion          *quota.ExhaustionCache
	conns               sync.Map // provider -> cachedConns
	compressionStrategy compression.Strategy
	exactCache          cache.CacheStorage
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
	exhaustionCache *quota.ExhaustionCache,
	compressionStrategy compression.Strategy,
	exactCache cache.CacheStorage,
) *Handler {
	return &Handler{
		db:                  db,
		registry:            executor.GetRegistry(),
		store:               store,
		elig:                elig,
		combo:               comboHandler,
		tracker:             tracker,
		authMgr:             authManager,
		resolver:            resolver,
		exhaustion:          exhaustionCache,
		compressionStrategy: compressionStrategy,
		exactCache:          exactCache,
	}
}

// resolveExecutor finds the executor for a provider/model.
func (h *Handler) resolveExecutor(provider, model string) (executor.Executor, executor.ProviderFormat, error) {
	provider = provideralias.ResolveAlias(provider)
	exec, format, ok := h.registry.Get(provider)
	if !ok {
		return nil, "", fmt.Errorf("unknown provider: %s", provider)
	}
	return exec, format, nil
}


// getConnection returns an active connection for a provider using O(1) eligibility snapshot.
// Preflight: skips connections marked as exhausted in the quota exhaustion cache.
func (h *Handler) getConnection(ctx context.Context, provider string, modelID string) (*Connection, error) {
	// Resolve legacy long-form aliases to the canonical short prefix.
	provider = provideralias.ResolveAlias(provider)
	// Get all eligible connections for this provider (sorted by priority)
	connIDs := h.elig.GetByPrefix(provider)
	logging.Logger.Debug("getConnection", "provider", provider, "eligible", len(connIDs))
	if len(connIDs) == 0 {
		return nil, fmt.Errorf("no eligible connection for provider: %s", provider)
	}

	// Preflight: find first connection not marked exhausted
	for _, connID := range connIDs {
		cs := h.store.Get(connID)
		if cs == nil {
			continue
		}
		// Check connection-level cooldown
		if cs.IsInCooldown() {
			continue
		}
		// Check model-level cooldown
		if modelID != "" && cs.IsModelInCooldown(modelID) {
			continue
		}
		// Preflight quota exhaustion check (OmniRoute isAccountQuotaExhausted)
		if h.exhaustion.IsExhausted(connID) {
			continue
		}
		// Load credentials by ID
		conn, err := h.loadConnectionByID(ctx, connID)
		if err != nil {
			logging.Logger.Debug("load conn failed", "conn", connID[:8], "err", err)
			continue
		}
		logging.Logger.Info("getConnection selected", "provider", provider, "conn", conn.ID[:8], "name", conn.Name)
		h.bindActiveConn(ctx, conn)
		return conn, nil
	}
	// All eligible connections exhausted or failed to load
	return nil, fmt.Errorf("no available connection for provider: %s (all exhausted or failing)", provider)
}

// prepareConnection performs preflight checks and proactive token refresh for a
// specific connection. Used by combo routing so it gets the same cooldown/exhaustion
// guards and OAuth refresh as the regular single-model path.
func (h *Handler) prepareConnection(ctx context.Context, connID, provider, modelID string) (*Connection, error) {
	cs := h.store.Get(connID)
	if cs == nil {
		return nil, fmt.Errorf("connection state not found")
	}
	if cs.IsInCooldown() {
		return nil, fmt.Errorf("connection in cooldown")
	}
	if modelID != "" && cs.IsModelInCooldown(modelID) {
		return nil, fmt.Errorf("model in cooldown")
	}
	if h.exhaustion.IsExhausted(connID) {
		return nil, fmt.Errorf("connection exhausted")
	}

	conn, err := h.loadConnectionByID(ctx, connID)
	if err != nil {
		return nil, err
	}

	// Proactive token refresh (same as regular routing path)
	h.proactiveRefreshToken(ctx, conn, provider)
	logging.Logger.Info("combo step selected", "provider", provider, "conn", conn.ID[:8], "name", conn.Name)
	h.bindActiveConn(ctx, conn)
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
		SELECT c.id, c.name, c.provider_type_id as provider_prefix,
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
	`, connID).Scan(&conn.ID, &conn.Name, &conn.Provider, &conn.APIKey, &conn.AccessToken,
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
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Direct match
	if unrecoverableCodes[s] {
		return s
	}

	// Try parsing as JSON
	if s[0] == '{' || s[0] == '[' || s[0] == '"' {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			if code := extractOAuthErrorCodeFromParsed(parsed, depth+1); code != "" {
				return code
			}
		}
	}

	// Regex fallback: known code as value of "error" or "error_code" field
	lower := strings.ToLower(s)
	for _, code := range []string{"invalid_grant", "invalid_request", "refresh_token_reused", "refresh_token_invalidated", "invalid_token", "expired_token", "token_expired", "unauthorized_client", "access_denied", "unrecoverable_refresh_error"} {
		if strings.Contains(lower, code) {
			return code
		}
	}

	return ""
}

// extractOAuthErrorCodeFromParsed extracts error code from parsed JSON (object or string).
func extractOAuthErrorCodeFromParsed(raw any, depth int) string {
	if raw == nil || depth > 6 {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return extractOAuthErrorCode(v, depth+1)
	case map[string]any:
		for _, key := range []string{"error", "code", "error_code", "error_description", "message", "body", "details"} {
			if val, ok := v[key]; ok {
				if code := extractOAuthErrorCodeFromParsed(val, depth+1); code != "" {
					return code
				}
			}
		}
	}
	return ""
}

// isAuthError checks if an error indicates an authentication failure (401/403).
// Used for reactive retry: refresh token and retry once on auth errors.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "authentication") || strings.Contains(msg, "access denied")
}

// executeWithRetry runs the executor up to 3 times with linear backoff. On an
// auth error it refreshes the OAuth token (if configured) and retries once.
func (h *Handler) executeWithRetry(
	ctx context.Context,
	exec executor.Executor,
	req *executor.Request,
	conn *Connection,
	provider string,
	modelID string,
) (*executor.Response, *executor.StreamResult, error) {
	var resp *executor.Response
	var streamResult *executor.StreamResult
	var err error

	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		if req.Stream {
			streamer, ok := exec.(interface {
				ExecuteStream(context.Context, *executor.Request) (*executor.StreamResult, error)
			})
			if !ok {
				return nil, nil, errors.New("executor does not support streaming")
			}
			streamResult, err = streamer.ExecuteStream(ctx, req)
		} else {
			resp, err = exec.Execute(ctx, req)
		}

		if err == nil {
			return resp, streamResult, nil
		}
		if isUnrecoverableRefreshError(err) {
			break
		}
		if attempt < 2 && isAuthError(err) && h.proactiveRefreshToken(ctx, conn, provider) {
			req.AccessToken = conn.AccessToken
			continue
		}
		if !isAuthError(err) {
			break
		}
	}

	return resp, streamResult, err
}

// executeDirect runs the executor once with no retry — on any error, the caller
// should failover to the next connection. This matches the user's preference:
// "jika error ya langsung pindah ke akun lain".
func (h *Handler) executeDirect(ctx context.Context, exec executor.Executor, req *executor.Request) (*executor.Response, *executor.StreamResult, error) {
	if req.Stream {
		streamer, ok := exec.(interface {
			ExecuteStream(context.Context, *executor.Request) (*executor.StreamResult, error)
		})
		if !ok {
			return nil, nil, errors.New("executor does not support streaming")
		}
		streamResult, err := streamer.ExecuteStream(ctx, req)
		return nil, streamResult, err
	}
	resp, err := exec.Execute(ctx, req)
	return resp, nil, err
}

// handleFailoverError records an upstream failure, marks the connection
// exhausted/cooled-down when appropriate, refreshes eligibility, and logs it.
// Returns (shouldRetry, category): shouldRetry=false for non-retryable errors
// (model_not_found, auth_failed), category for the caller to build better error messages.
func (h *Handler) handleFailoverError(conn *Connection, provider, modelName string, err error, attempt int, latency int64) (bool, string) {
	det := connstate.DetectError(0, "", err, provider, modelName, nil)
	if det.Category == connstate.ErrorRateLimit {
		h.exhaustion.MarkExhausted(conn.ID, quota.DefaultExhaustionTTL)
	} else if det.Category == connstate.ErrorQuota {
		ttl := 24 * time.Hour // fallback for daily quotas
		if det.CooldownUntil != nil {
			ttl = time.Until(*det.CooldownUntil)
		}
		h.exhaustion.MarkExhausted(conn.ID, ttl)
	}
	h.combo.RecordFailure(conn.ID, det)
	h.persistCooldown(conn.ID, det)
	h.elig.Update(h.store)
	h.checkAutoDisable(conn.ID, provider)

	// Truncate error for log readability — full error goes to tracker DB
	errMsg := err.Error()
	if len(errMsg) > 120 {
		errMsg = errMsg[:120] + "…"
	}

	switch det.Category {
	case connstate.ErrorModelNotFound:
		logging.Logger.Warn("⊘ model not found — stop failover",
			"provider", provider, "model", modelName, "conn", conn.ID[:8], "name", conn.Name,
		)
	case connstate.ErrorAuth:
		logging.Logger.Warn("⊘ auth failed — stop failover",
			"provider", provider, "model", modelName, "conn", conn.ID[:8], "name", conn.Name,
		)
	case connstate.ErrorRateLimit:
		logging.Logger.Warn("⟳ rate limited — try next",
			"provider", provider, "model", modelName, "conn", conn.ID[:8], "name", conn.Name,
			"cooldown", det.CooldownUntil, "attempt", attempt+1,
		)
	case connstate.ErrorQuota:
		logging.Logger.Warn("⟳ quota exhausted — try next",
			"provider", provider, "model", modelName, "conn", conn.ID[:8], "name", conn.Name,
			"cooldown", det.CooldownUntil, "attempt", attempt+1,
		)
	default:
		logging.Logger.Error("⟳ upstream error — try next",
			"provider", provider, "model", modelName, "conn", conn.ID[:8], "name", conn.Name,
			"category", string(det.Category), "attempt", attempt+1, "error", errMsg,
		)
	}

	h.tracker.Log(&usage.LogEntry{
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        modelName,
		Modality:       "chat",
		LatencyMs:      latency,
		StatusCode:     usage.ExtractErrorStatus(err),
		ErrorMessage:   err.Error(),
	})

	// Non-retryable errors: model doesn't exist or auth failed —
	// retrying with another connection won't help.
	switch det.Category {
	case connstate.ErrorModelNotFound, connstate.ErrorAuth:
		return false, string(det.Category)
	}
	return true, string(det.Category)
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

// streamResponse writes a translated SSE stream to the client with heartbeat and
// client-disconnect detection. Each translated chunk already includes the SSE
// frame (data: ...\n\n), so the helper writes the bytes as-is and flushes.
func (h *Handler) streamResponse(
	c *gin.Context,
	result *executor.StreamResult,
	conn *Connection,
	provider, model string,
	clientFormat, providerFormat executor.ProviderFormat,
	originalReq, translatedReq []byte,
	errFormatter func(error) []byte,
	start time.Time,
) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "server_error"}})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	heartbeatInterval := 15 * time.Second
	if v := os.Getenv("SSE_HEARTBEAT_INTERVAL_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			heartbeatInterval = time.Duration(ms) * time.Millisecond
		}
	}
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	var lastChunk []byte
	lastChunkTime := time.Now()
	ctx := c.Request.Context()

	for {
		select {
		case chunk, ok := <-result.Chunks:
			if !ok {
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()

				latency := time.Since(start).Milliseconds()
				tokenCounts := ExtractTokensFromFinalChunk(lastChunk)
				h.tracker.Log(&usage.LogEntry{
					ConnectionID:    conn.ID,
					ProviderTypeID:  provider,
					ModelID:         model,
					Modality:        "chat",
					InputTokens:     tokenCounts.InputTokens,
					OutputTokens:    tokenCounts.OutputTokens,
					ReasoningTokens: tokenCounts.ReasoningTokens,
					CachedTokens:    tokenCounts.CachedTokens,
					LatencyMs:       latency,
					StatusCode:      http.StatusOK,
				})
				return
			}

			if chunk.Err != nil {
				c.Writer.Write([]byte("event: error\ndata: "))
				c.Writer.Write(errFormatter(chunk.Err))
				c.Writer.Write([]byte("\n\n"))
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
				h.tracker.Log(&usage.LogEntry{
					ConnectionID:   conn.ID,
					ProviderTypeID: provider,
					ModelID:        model,
					Modality:       "chat",
					LatencyMs:      time.Since(start).Milliseconds(),
					StatusCode:     usage.ExtractErrorStatus(chunk.Err),
					ErrorMessage:   chunk.Err.Error(),
				})
				return
			}

			lastChunkTime = time.Now()
			translatedChunks := registry.Response(ctx, string(providerFormat), string(clientFormat), model, originalReq, translatedReq, chunk.Payload, nil)
			for _, tc := range translatedChunks {
				c.Writer.Write(tc)
				flusher.Flush()
			}
			lastChunk = chunk.Payload

		case <-ticker.C:
			if time.Since(lastChunkTime) >= heartbeatInterval {
				executor.WriteSSEHeartbeat(c.Writer, flusher)
			}

		case <-ctx.Done():
			return
		}
	}
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
	banCount := cs.GetBanCount()

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
	if cs != nil && cs.GetBanCount() > 0 {
		cs.ResetBanCount()
		h.db.Exec(`UPDATE connections SET consecutive_ban_count = 0, updated_at = ? WHERE id = ?`,
			time.Now().Unix(), connID)
	}
}

// persistCooldown writes a quota/rate-limit cooldown to the DB so it survives restarts.
// Also updates last_error fields for debugging.
func (h *Handler) persistCooldown(connID string, det connstate.ErrorDetection) {
	if det.CooldownUntil == nil {
		return
	}
	status := string(det.Status)
	if det.Category == connstate.ErrorQuota {
		status = string(connstate.StatusQuotaExhausted)
	}
	h.db.Exec(`UPDATE connections SET status = ?, cooldown_until = ?, last_error = ?, last_error_code = ?, consecutive_error_count = consecutive_error_count + 1, updated_at = ? WHERE id = ?`,
		status, det.CooldownUntil.Unix(), det.Message, string(det.Category), time.Now().Unix(), connID)
}
