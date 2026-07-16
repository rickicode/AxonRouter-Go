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
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/active"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/tidwall/gjson"
)

const maxBodySize = 10 * 1024 * 1024 // 10MB

var (
	errBodyTooLarge = errors.New("request body too large")
	errReadBody     = errors.New("failed to read request body")
)

// readBody reads the request body with a size limit.
// Size-limit violations return errBodyTooLarge; other read failures return errReadBody.
func readBody(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			return nil, fmt.Errorf("%w (max %d bytes)", errBodyTooLarge, maxBodySize)
		}
		return nil, errReadBody
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
		// Enforce max body size here too, before any tracking reads.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if strings.Contains(err.Error(), "http: request body too large") {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": gin.H{"message": fmt.Sprintf("request body too large (max %d bytes)", maxBodySize), "type": "invalid_request_error"},
				})
				return
			}
			// For non-size read failures, continue without tracking rather than failing the request.
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
	writeQueue          *db.WriteQueue // centralized async writer — removes sync writes from request path
	registry            *executor.Registry
	store               *connstate.Store
	elig                *connstate.EligibilityManager
	combo               *combo.Handler
	tracker             *usage.Tracker
	authMgr             *auth.Manager
	resolver            *proxypool.Resolver
	exhaustion          *quota.ExhaustionCache
	conns               sync.Map // connID -> *Connection (write-through credential cache)
	compressionStrategy compression.Strategy
	exactCache          cache.CacheStorage
	providerCfg         *providercfg.Manager
}

// NewHandler creates a new v1 handler with all dependencies.
func NewHandler(
	db *sql.DB,
	writeQueue *db.WriteQueue,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	comboHandler *combo.Handler,
	tracker *usage.Tracker,
	authManager *auth.Manager,
	resolver *proxypool.Resolver,
	exhaustionCache *quota.ExhaustionCache,
	compressionStrategy compression.Strategy,
	exactCache cache.CacheStorage,
	providerCfg *providercfg.Manager,
) *Handler {
	return &Handler{
		db:                  db,
		writeQueue:          writeQueue,
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
		providerCfg:         providerCfg,
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

const pickMaxAttempts = 10

// getConnection returns an active connection for a provider using the precomputed
// eligibility snapshot. The hot path samples up to pickMaxAttempts candidates
// so routing cost stays bounded regardless of how many eligible connections a
// provider has. A full scan is only used as a fallback when every sampled
// connection fails model-level cooldown/exhaustion checks.
func (h *Handler) getConnection(ctx context.Context, provider string, modelID string) (*Connection, error) {
	provider = provideralias.ResolveAlias(provider)

	connIDs := h.elig.GetByPrefix(provider)
	logging.Logger.Debug("getConnection", "provider", provider, "eligible", len(connIDs))
	if len(connIDs) == 0 {
		return nil, fmt.Errorf("no eligible connection for provider: %s", provider)
	}

	start := h.pickStartIndex(provider, len(connIDs))
	bound := pickMaxAttempts
	if bound > len(connIDs) {
		bound = len(connIDs)
	}
	for i := 0; i < bound; i++ {
		idx := (start + i) % len(connIDs)
		if conn, ok := h.tryPickConnection(ctx, connIDs[idx], provider, modelID); ok {
			return conn, nil
		}
	}

	for i := bound; i < len(connIDs); i++ {
		idx := (start + i) % len(connIDs)
		if conn, ok := h.tryPickConnection(ctx, connIDs[idx], provider, modelID); ok {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("no available connection for provider: %s (all exhausted or failing)", provider)
}

// pickStartIndex returns the first index to inspect based on the configured
// routing mode. first_eligible always starts at 0; round_robin rotates an
// atomic counter; random chooses a uniform index.
func (h *Handler) pickStartIndex(provider string, total int) int {
	if total <= 1 {
		return 0
	}
	switch h.providerCfg.RoutingMode(provider) {
	case providercfg.RoundRobin:
		return h.providerCfg.NextRoundRobinIndex(provider, total)
	case providercfg.Random:
		return rand.IntN(total)
	default:
		return 0
	}
}

// tryPickConnection checks a single eligible connection for model-level
// cooldowns/quota exhaustion and loads its DB credentials when it passes.
func (h *Handler) tryPickConnection(ctx context.Context, connID, provider, modelID string) (*Connection, bool) {
	cs := h.store.Get(connID)
	if cs == nil {
		return nil, false
	}
	if cs.IsInCooldown() {
		return nil, false
	}
	if modelID != "" && cs.IsModelInCooldown(modelID) {
		return nil, false
	}
	if modelID != "" && h.exhaustion.IsExhaustedScope(connID, connstate.ModelScope(provider, modelID)) {
		return nil, false
	}
	if h.exhaustion.IsExhausted(connID) {
		return nil, false
	}
	conn, err := h.getCachedConn(ctx, connID)
	if err != nil {
		logging.Logger.Debug("load conn failed", "conn", connID[:8], "err", err)
		return nil, false
	}
	logging.Logger.Info("getConnection selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name, "mode", h.providerCfg.RoutingMode(provider))
	h.bindActiveConn(ctx, conn)
	return conn, true
}

// orderCandidates reorders eligible connection IDs according to the provider's
// configured routing mode.
//
//   - first_eligible: use the snapshot order (one account stays first until it
//     becomes ineligible).
//   - round_robin: rotate the starting index on every request.
//   - random: pick a random starting index for each request.
func (h *Handler) orderCandidates(provider string, candidates []string) []string {
	if len(candidates) <= 1 {
		return candidates
	}

	mode := h.providerCfg.RoutingMode(provider)
	var start int
	switch mode {
	case providercfg.RoundRobin:
		start = h.providerCfg.NextRoundRobinIndex(provider, len(candidates))
	case providercfg.Random:
		start = rand.IntN(len(candidates))
	default:
		return candidates
	}

	if start == 0 {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	out = append(out, candidates[start:]...)
	out = append(out, candidates[:start]...)
	return out
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
	if modelID != "" && h.exhaustion.IsExhaustedScope(connID, connstate.ModelScope(provider, modelID)) {
		return nil, fmt.Errorf("model exhausted")
	}
	if h.exhaustion.IsExhausted(connID) {
		return nil, fmt.Errorf("connection exhausted")
	}

	conn, err := h.getCachedConn(ctx, connID)
	if err != nil {
		return nil, err
	}

	// Proactive token refresh (same as regular routing path)
	h.proactiveRefreshToken(ctx, conn, provider)
	logging.Logger.Info("combo step selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name)
	h.bindActiveConn(ctx, conn)
	return conn, nil
}

// RefreshConnections clears the connection cache for a specific connection ID.
// Called by admin handlers after connection CRUD operations.
func (h *Handler) RefreshConnections(connID string) {
	h.conns.Delete(connID)
}

// connCacheTTL bounds credential staleness. Admin changes (API key rotation,
// connection update) are reflected within this window.
const connCacheTTL = 60 * time.Second

// cachedConn holds a connection with a cache timestamp for TTL expiry.
type cachedConn struct {
	conn     *Connection
	cachedAt time.Time
}

// getCachedConn returns a cached connection by ID, falling back to a DB load
// on miss or TTL expiry (60s). This eliminates the per-request DB SELECT on the
// hot path — the last remaining DB call in getConnection/prepareConnection.
func (h *Handler) getCachedConn(ctx context.Context, connID string) (*Connection, error) {
	if v, ok := h.conns.Load(connID); ok {
		cc := v.(cachedConn)
		if time.Since(cc.cachedAt) < connCacheTTL {
			copied := *cc.conn
			return &copied, nil
		}
	}
	// Cache miss or expired — load from DB (write-through).
	conn, err := h.loadConnectionByID(ctx, connID)
	if err != nil {
		return nil, err
	}
	h.conns.Store(connID, cachedConn{conn: conn, cachedAt: time.Now()})
	copied := *conn
	return &copied, nil
}

// loadConnectionByID loads a single connection by ID from the database.
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
	"cx":      5 * time.Minute,  // Codex: Auth0 rotating refresh tokens
	"ag":      15 * time.Minute, // Antigravity: Google non-rotating refresh tokens
	"kiro":    5 * time.Minute,  // Kiro: AWS SSO OIDC one-time-use refresh tokens
	"copilot": 5 * time.Minute,  // Copilot: GitHub device-code tokens refresh early due to Copilot token skew
}

const defaultRefreshLeadMs = 5 * time.Minute

// proactiveRefreshToken checks if a token should be refreshed proactively
// based on per-provider lead times. Matches OmniRoute checkAndRefreshToken.
func (h *Handler) proactiveRefreshToken(ctx context.Context, conn *Connection, provider string) bool {
	if h.authMgr == nil || conn.OAuthExpiresAt.IsZero() {
		return false
	}
	// GitHub device-code OAuth does not return a refresh token, but the
	// short-lived Copilot bearer token can still be refreshed from the access
	// token. For every other provider a refresh token is required.
	if conn.RefreshToken == "" && provider != "copilot" {
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
			connID := conn.ID
			h.writeQueue.EnqueueOrBlock(ctx, "refreshOAuth:authFailed", func(d *sql.DB) error {
				_, err := d.Exec(`UPDATE connections SET is_active = 0, status = 'auth_failed', updated_at = ? WHERE id = ?`,
					time.Now().Unix(), connID)
				return err
			})
			h.store.UpdateStatus(conn.ID, connstate.StatusAuthFailed)
			h.elig.ScheduleUpdate()
		}
		return fmt.Errorf("refresh token: %w", err)
	}

	// Update connection in memory
	conn.AccessToken = newCreds.AccessToken
	conn.OAuthExpiresAt = newCreds.ExpiresAt
	if len(newCreds.ProviderSpecific) > 0 {
		if psdBytes, err := json.Marshal(newCreds.ProviderSpecific); err == nil {
			conn.ProviderSpecificData = string(psdBytes)
		}
	}
	// Update the credential cache so subsequent requests see the new token immediately.
	h.conns.Store(conn.ID, cachedConn{conn: conn, cachedAt: time.Now()})
	// Persist to DB (async — does not block the request path).
	connID := conn.ID
	accessToken := conn.AccessToken
	refreshToken := conn.RefreshToken
	expiresAt := conn.OAuthExpiresAt.Unix()
	providerSpecificData := conn.ProviderSpecificData
	h.writeQueue.EnqueueOrBlock(ctx, "refreshOAuth:persist", func(d *sql.DB) error {
		_, err := d.Exec(`UPDATE connections SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, provider_specific_data = ?, updated_at = ? WHERE id = ?`,
			accessToken, refreshToken, expiresAt, providerSpecificData, time.Now().Unix(), connID)
		if err != nil {
			log.Printf("WARN: failed to persist OAuth token for connection %s: %v", connID, err)
		}
		return err
	})
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

// extractUpstreamError unwraps an *executor.UpstreamError from err.
func extractUpstreamError(err error) *executor.UpstreamError {
	if err == nil {
		return nil
	}
	var upErr *executor.UpstreamError
	if errors.As(err, &upErr) {
		return upErr
	}
	return nil
}

// extractErrorMessage extracts the .error.message string from an OpenAI-style
// error body. Returns "" for invalid or non-JSON bodies.
func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return envelope.Error.Message
}

// buildFailoverErrorResponse builds the final client-facing message, status code,
// and error type when every connection in the failover loop has been exhausted.
// It preserves the upstream provider message for quota/rate-limit errors so the
// client sees the real cause (e.g. Cloudflare's "daily free allocation" message)
// instead of the generic "all connections exhausted" fallback.
func buildFailoverErrorResponse(category string, lastErr error, modelName string) (string, int, string) {
	upstreamMessage := func(fallback string) string {
		if upErr := extractUpstreamError(lastErr); upErr != nil {
			if m := extractErrorMessage(upErr.Body); m != "" {
				return m
			}
			if upErr.Error() != "" {
				return upErr.Error()
			}
		}
		return fallback
	}

	switch connstate.ErrorCategory(category) {
	case connstate.ErrorModelNotFound:
		return "model not found: " + modelName, http.StatusNotFound, "invalid_request_error"
	case connstate.ErrorAuth:
		return "authentication failed for all connections", http.StatusUnauthorized, "authentication_error"
	case connstate.ErrorRateLimit:
		return upstreamMessage("rate limit exceeded for all connections"), http.StatusTooManyRequests, "rate_limit_error"
	case connstate.ErrorQuota:
		return upstreamMessage("quota exhausted for all connections"), http.StatusTooManyRequests, "insufficient_quota"
	default:
		return "all connections exhausted or failing", http.StatusServiceUnavailable, "server_error"
	}
}

// failoverBackoff sleeps briefly before the next failover attempt so the
// eligibility snapshot has time to rebuild (updateCoalesceWindow is 50ms) and
// transient upstream errors (e.g. CF capacity exceeded) have a moment to clear.
// It returns false if the request context is canceled.
// The delay is skipped for the final attempt since the loop will exit anyway.
func failoverBackoff(ctx context.Context, attempt int, maxAttempts int) bool {
	if attempt >= maxAttempts-1 {
		return true
	}
	// 50ms, 100ms, 200ms, 400ms, capped at 500ms.
	delay := time.Duration(50*(1<<attempt)) * time.Millisecond
	if delay > 500*time.Millisecond {
		delay = 500 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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

// shortID returns a safe prefix of id up to n bytes; it never panics on short IDs.
func shortID(id string, n int) string {
	if n <= 0 || len(id) <= n {
		return id
	}
	return id[:n]
}

// writeReadBodyError writes a 413 for oversize bodies and a generic 400 for other read failures.
func writeReadBodyError(c *gin.Context, err error) {
	if errors.Is(err, errBodyTooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": errReadBody.Error(), "type": "invalid_request_error"}})
}

// writeContextDone writes an explicit 499 for client cancellations and 504 for timeouts.
func writeContextDone(c *gin.Context) {
	err := c.Request.Context().Err()
	if err == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"message": "request timeout", "type": "timeout_error"}})
		return
	}
	c.Status(499)
	c.Writer.WriteHeaderNow()
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
			delay := time.Duration(attempt) * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-timer.C:
			}
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

// isClientCanceled reports whether err is a context cancellation that originated
// from the inbound request (client disconnect / client timeout), not from a
// server-side deadline. A plain "context canceled" from client.Do is almost
// always the client hanging up, so we must NOT failover or mark the
// connection degraded on it — it would just burn the retry budget and surface
// a misleading "all connections exhausted" 503.
func (h *Handler) isClientCanceled(c *gin.Context, err error) bool {
	if !errors.Is(err, context.Canceled) {
		return false
	}
	return c != nil && c.Request.Context().Err() != nil
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
func (h *Handler) handleFailoverError(ctx context.Context, c *gin.Context, conn *Connection, provider, modelName string, err error, attempt int, latency int64, stream bool) (bool, string) {
	det := connstate.DetectError(c.Request.Context(), 0, "", err, provider, modelName, nil)
	if connstate.HasPerModelQuota(provider) && det.ModelID != "" &&
		(det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
		scope := connstate.ModelScope(provider, det.ModelID)
		h.exhaustion.MarkExhausted(quota.ExhaustKey(conn.ID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
		// Per-model scope keeps the connection itself ready/eligible so other
		// models can still route through it.
	} else {
		if det.Category == connstate.ErrorRateLimit {
			h.exhaustion.MarkExhausted(conn.ID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
		} else if det.Category == connstate.ErrorQuota {
			ttl := 24 * time.Hour // fallback for daily quotas
			if det.CooldownUntil != nil {
				ttl = time.Until(*det.CooldownUntil)
			}
			h.exhaustion.MarkExhausted(conn.ID, ttl)
		}
	}
	h.combo.RecordFailure(conn.ID, det)
	h.persistCooldownScoped(conn.ID, det)
	// Update in-memory status so dashboard reflects rate_limited/quota_exhausted immediately.
	if det.Status != "" {
		h.store.UpdateStatus(conn.ID, det.Status)
	}
	h.elig.ScheduleUpdate()
	h.checkAutoDisable(conn.ID, provider)

	// Truncate error for log readability — full error goes to tracker DB
	errMsg := err.Error()
	if len(errMsg) > 120 {
		errMsg = errMsg[:120] + "…"
	}

	switch det.Category {
	case connstate.ErrorModelNotFound:
		logging.Logger.Warn(
			"⊘ model not found — stop failover",
			"provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "name", conn.Name,
		)
	case connstate.ErrorAuth:
		logging.Logger.Warn(
			"⊘ auth failed — stop failover",
			"provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "name", conn.Name,
		)
	case connstate.ErrorRateLimit:
		logging.Logger.Warn(
			"⟳ rate limited — try next",
			"provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "name", conn.Name,
			"cooldown", det.CooldownUntil, "attempt", attempt+1,
		)
	case connstate.ErrorQuota:
		logging.Logger.Warn(
			"⟳ quota exhausted — try next",
			"provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "name", conn.Name,
			"cooldown", det.CooldownUntil, "attempt", attempt+1,
		)
	default:
		logging.Logger.Error(
			"⟳ upstream error — try next",
			"provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "name", conn.Name,
			"category", string(det.Category), "attempt", attempt+1, "error", errMsg,
		)
	}

	h.tracker.Log(&usage.LogEntry{
		ApiKeyID: c.GetString("api_key_id"),
		ConnectionID: conn.ID,
		ProviderTypeID: provider,
		ModelID: modelName,
		ProxyPoolID: executor.ProxyPoolIDFromContext(ctx),
		Modality: "chat",
		Stream: stream,
		LatencyMs: latency,
		ErrorMessage: err.Error(),
	})

	// Non-retryable errors: model doesn't exist or auth failed —
	// retrying with another connection won't help.
	switch det.Category {
	case connstate.ErrorModelNotFound, connstate.ErrorAuth:
		return false, string(det.Category)
	}
	return true, string(det.Category)
}

// writeUpstreamClientError writes an OpenAI-compatible upstream error directly
// to the client and returns true if the error was a non-retryable client error.
// 429 is intentionally excluded so the failover loop can cooldown/mark exhausted
// and try the next connection.
func (h *Handler) writeUpstreamClientError(
	ctx context.Context,
	c *gin.Context,
	err error,
	conn *Connection,
	provider, modelName string,
	start time.Time,
	stream bool,
) bool {
	var upErr *executor.UpstreamError
	if !errors.As(err, &upErr) {
		return false
	}
	if upErr.StatusCode == http.StatusTooManyRequests {
		return false
	}
	c.Header("Content-Type", "application/json")
	c.Status(upErr.StatusCode)
	c.Writer.Write(upErr.Body)
	if h.tracker != nil && conn != nil {
		errBody := upErr.RawBody
		if len(errBody) == 0 {
			errBody = upErr.Body
		}
		h.tracker.Log(&usage.LogEntry{
			ApiKeyID: c.GetString("api_key_id"),
			ConnectionID: conn.ID,
			ProviderTypeID: provider,
			ModelID: modelName,
			ProxyPoolID: executor.ProxyPoolIDFromContext(ctx),
			Modality: "chat",
			Stream: stream,
			LatencyMs: time.Since(start).Milliseconds(),
			StatusCode: upErr.StatusCode,
			ErrorMessage: string(errBody),
		})
	}
	return true
}

// proxyCandidates resolves the ordered proxy configs to try for a connection.
// The first entry is the primary; the executor retries across the rest on
// transient proxy/network failures.
func (h *Handler) proxyCandidates(conn *Connection) []executor.ProxyConfig {
	if h.resolver == nil {
		return nil
	}
	cfgs := h.resolver.ResolveCandidates(conn.ProviderSpecificData, conn.Provider)
	out := make([]executor.ProxyConfig, 0, len(cfgs))
	for _, c := range cfgs {
		out = append(out, executor.ProxyConfig{
			Enabled:     c.Enabled,
			ProxyPoolID: c.ProxyPoolID,
			ProxyURL:    c.ProxyURL,
			NoProxy:     c.NoProxy,
			RelayURL:    c.RelayURL,
			RelayAuth:   c.RelayAuth,
			RelayType:   c.RelayType,
			StrictProxy: c.StrictProxy,
		})
	}
	return out
}

// proxyContext resolves proxy config for a connection and returns a context with
// the primary proxy and the retry candidates attached.
func (h *Handler) proxyContext(ctx context.Context, conn *Connection) context.Context {
	if h.resolver == nil {
		return ctx
	}
	cands := h.proxyCandidates(conn)
	if len(cands) == 0 {
		return ctx
	}
	ctx = executor.ContextWithProxy(ctx, cands[0])
	return executor.ContextWithProxyCandidates(ctx, cands)
}

// estimateOutputFromTranslatedChunk tries to extract actual output text from a
// translated SSE chunk so the streaming fallback estimate is based on content
// characters rather than raw upstream payload bytes (which include framing and
// JSON wrappers). It returns the rune count of the best matching content path.
func estimateOutputFromTranslatedChunk(tc []byte) int64 {
	line := bytes.TrimSpace(tc)
	if bytes.HasPrefix(line, []byte("data:")) {
		line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	}
	if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
		return 0
	}

	var n int64
	paths := []string{
		"choices.0.delta.content",
		"choices.0.delta.text",
		"delta.text",
		"output.0.content.0.text",
		"response.output.0.content.0.text",
	}
	for _, p := range paths {
		if v := gjson.GetBytes(line, p); v.Type == gjson.String {
			n += int64(utf8.RuneCountInString(v.String()))
		}
	}
	return n
}

// streamResponse writes a translated SSE stream to the client with heartbeat and
// client-disconnect detection. Each translated chunk already includes the SSE
// frame (data: ...\n\n), so the helper writes the bytes as-is and flushes.
func (h *Handler) streamResponse(
	ctx context.Context,
	c *gin.Context,
	result *executor.StreamResult,
	conn *Connection,
	provider, model string,
	clientFormat, providerFormat executor.ProviderFormat,
	originalReq, translatedReq []byte,
	errFormatter func(error) []byte,
	start time.Time,
	comboID string,
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

	var acc StreamTokenCounts
	var totalOutputBytes int64
	lastChunkTime := time.Now()
	var streamState any

	for {
		select {
		case chunk, ok := <-result.Chunks:
			if !ok {
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()

latency := time.Since(start).Milliseconds()
			tokensEstimated := false
			if acc.InputTokens+acc.OutputTokens == 0 {
				estInput := usage.EstimateTokensFromRequest(originalReq)
				var estOutput int64
				if totalOutputBytes > 0 {
					estOutput = totalOutputBytes / 4
				}
				if estInput > 0 || estOutput > 0 {
					acc.InputTokens = estInput
					acc.OutputTokens = estOutput
					tokensEstimated = true
				}
			}
			h.tracker.Log(&usage.LogEntry{
				ApiKeyID: c.GetString("api_key_id"),
				ConnectionID: conn.ID,
				ProviderTypeID: provider,
				ModelID: model,
				ComboID: comboID,
				ProxyPoolID: executor.ProxyPoolIDFromContext(ctx),
				Modality: "chat",
				Stream: true,
				InputTokens: acc.InputTokens,
				OutputTokens: acc.OutputTokens,
				ReasoningTokens: acc.ReasoningTokens,
				CachedTokens: acc.CachedTokens,
				CacheCreationTokens: acc.CacheCreationTokens,
				LatencyMs: latency,
				StatusCode: http.StatusOK,
				TokensEstimated: tokensEstimated,
			})
			h.incrementAPIKeyUsage(c.GetString("api_key_id"), acc.InputTokens+acc.OutputTokens)
			return
		}

		if chunk.Err != nil {
			// Keep errors inside OpenAI-compatible `data:` events so standard
			// clients can surface them; avoid non-standard `event: error`.
			// Only record upstream failures as exhaustion/cooldown; client
			// cancellations (e.g. disconnects) must not penalize connections.
			if h.isClientCanceled(c, chunk.Err) {
				// Client disconnected — log for debugging but don't penalize connection.
				logging.Logger.Info("stream ended: client disconnected", "provider", provider, "model", model)
				return
			}
			latency := time.Since(start).Milliseconds()
			h.handleFailoverError(ctx, c, conn, provider, model, chunk.Err, 0, latency, true)
			c.Writer.Write([]byte("data: "))
			c.Writer.Write(errFormatter(chunk.Err))
			c.Writer.Write([]byte("\n\n"))
			c.Writer.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			h.tracker.Log(&usage.LogEntry{
				ApiKeyID: c.GetString("api_key_id"),
				ConnectionID: conn.ID,
				ProviderTypeID: provider,
				ModelID: model,
				ComboID: comboID,
				ProxyPoolID: executor.ProxyPoolIDFromContext(ctx),
				Modality: "chat",
				Stream: true,
				LatencyMs: time.Since(start).Milliseconds(),
				ErrorMessage: chunk.Err.Error(),
			})
				return
			}

			lastChunkTime = time.Now()
		translatedChunks := registry.Response(ctx, string(clientFormat), string(providerFormat), model, originalReq, translatedReq, chunk.Payload, &streamState)
		for _, tc := range translatedChunks {
			c.Writer.Write(tc)
			flusher.Flush()
			totalOutputBytes += estimateOutputFromTranslatedChunk(tc)
		}
		if counts, found := ExtractTokensFromSSEChunk(chunk.Payload); found {
			MergeTokenCounts(&acc, &counts)
		}

	case <-ticker.C:
			if time.Since(lastChunkTime) >= heartbeatInterval {
				executor.WriteSSEHeartbeat(c.Writer, flusher)
			}

		case <-ctx.Done():
			// Log the context cancellation reason for debugging silent stream stops.
			// This helps distinguish between client disconnects, combo timeouts, and other cancellations.
			logging.Logger.Warn("stream context cancelled", "provider", provider, "model", model, "error", ctx.Err())
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

	// Persist ban count to DB (async — does not block the request path).
	banCountCopy := banCount
	h.writeQueue.Enqueue("checkAutoDisable", func(d *sql.DB) error {
		if _, err := d.Exec(`UPDATE connections SET consecutive_ban_count = ?, updated_at = ? WHERE id = ?`,
			banCountCopy, time.Now().Unix(), connID); err != nil {
			return err
		}
		if banCountCopy >= threshold {
			log.Printf("Auto-disabling connection %s after %d consecutive ban signals", connID, banCountCopy)
			if _, err := d.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', updated_at = ? WHERE id = ?`,
				time.Now().Unix(), connID); err != nil {
				return err
			}
		}
		return nil
	})

	// In-memory status update is synchronous (cheap, lock-free sync.Map).
	if banCount >= threshold {
		h.store.UpdateStatus(connID, connstate.StatusDisabled)
		h.elig.ScheduleUpdate()
	}
}

// resetBanCount resets the consecutive ban count on success (persists to DB).
func (h *Handler) resetBanCount(connID string) {
	cs := h.store.Get(connID)
	if cs != nil && cs.GetBanCount() > 0 {
		cs.ResetBanCount()
		h.writeQueue.Enqueue("resetBanCount", func(d *sql.DB) error {
			_, err := d.Exec(`UPDATE connections SET consecutive_ban_count = 0, updated_at = ? WHERE id = ?`,
				time.Now().Unix(), connID)
			return err
		})
	}
}

// persistCooldownScoped writes a quota/rate-limit cooldown to the DB so it survives restarts.
// Also updates last_error fields for debugging.
// For per-model scope the connection status and cooldown_until are left untouched
// so the connection stays eligible for other models.
func (h *Handler) persistCooldownScoped(connID string, det connstate.ErrorDetection) {
	if det.Scope == "model" {
		errMsg := det.Message
		errCode := string(det.Category)
		h.writeQueue.Enqueue("persistCooldownScoped", func(d *sql.DB) error {
			now := time.Now().Unix()
			_, err := d.Exec(`
				UPDATE connections
				SET last_error = ?,
				    last_error_code = ?,
				    failure_count = failure_count + 1,
				    last_failure_at = ?,
				    updated_at = ?
				WHERE id = ?
			`, errMsg, errCode, now, now, connID)
			return err
		})
		return
	}

	if det.CooldownUntil == nil {
		return
	}
	status := string(det.Status)
	if det.Category == connstate.ErrorQuota {
		status = string(connstate.StatusQuotaExhausted)
	}
	statusVal := status
	cooldownUntil := det.CooldownUntil.Unix()
	errMsg := det.Message
	errCode := string(det.Category)
	h.writeQueue.Enqueue("persistCooldownScoped", func(d *sql.DB) error {
		now := time.Now().Unix()
		_, err := d.Exec(`
			UPDATE connections
			SET status = ?,
			    cooldown_until = ?,
			    last_error = ?,
			    last_error_code = ?,
			    failure_count = failure_count + 1,
			    last_failure_at = ?,
			    updated_at = ?
			WHERE id = ?
		`, statusVal, cooldownUntil, errMsg, errCode, now, now, connID)
		return err
	})
}

// persistCooldown writes a quota/rate-limit cooldown to the DB so it survives restarts.
// Also updates last_error fields for debugging.
// Deprecated: use persistCooldownScoped in new code; kept for callers that intentionally
// want connection-wide behavior regardless of detection scope.
func (h *Handler) persistCooldown(connID string, det connstate.ErrorDetection) {
	if det.CooldownUntil == nil {
		return
	}
	status := string(det.Status)
	if det.Category == connstate.ErrorQuota {
		status = string(connstate.StatusQuotaExhausted)
	}
	statusVal := status
	cooldownUntil := det.CooldownUntil.Unix()
	errMsg := det.Message
	errCode := string(det.Category)
	h.writeQueue.Enqueue("persistCooldown", func(d *sql.DB) error {
		now := time.Now().Unix()
		_, err := d.Exec(`UPDATE connections SET status = ?, cooldown_until = ?, last_error = ?, last_error_code = ?, failure_count = failure_count + 1, last_failure_at = ?, updated_at = ? WHERE id = ?`,
			statusVal, cooldownUntil, errMsg, errCode, now, now, connID)
		return err
	})
}

// persistSuccess records a successful request so the dashboard reflects last_success_at.
func (h *Handler) persistSuccess(connID string) {
	now := time.Now().Unix()
	h.writeQueue.Enqueue("persistSuccess", func(d *sql.DB) error {
		_, err := d.Exec(`UPDATE connections SET last_success_at = ?, updated_at = ? WHERE id = ?`, now, now, connID)
		return err
	})
}

// codexPersistIfCodex refreshes the connection's cached quota from the live
// x-codex-5h-* / x-codex-7d-* response headers (plan item C.3). The /wham/usage
// endpoint only returns the session window, so the dual-window 5h/7d view must
// be sourced from real traffic. Safe no-op when no Codex quota headers present.
func (h *Handler) codexPersistIfCodex(conn *Connection, resp *executor.Response, sr *executor.StreamResult) {
	if conn == nil || conn.Provider != "cx" {
		return
	}
	var headers http.Header
	if resp != nil {
		headers = resp.Headers
	} else if sr != nil {
		headers = sr.Headers
	}
	if headers == nil || len(headers.Values("x-codex-5h-limit")) == 0 && len(headers.Values("x-codex-7d-limit")) == 0 {
		return
	}
	connID := conn.ID
	provider := conn.Provider
	connName := conn.Name
	h.writeQueue.Enqueue("codexHeaderQuota", func(d *sql.DB) error {
		var ptype, plan string
		_ = d.QueryRow(`SELECT provider_type_id, COALESCE(plan,'') FROM connections WHERE id = ?`, connID).Scan(&ptype, &plan)
		if ptype == "" {
			ptype = provider
		}
		quota.SaveCodexHeaderQuota(d, connID, ptype, connName, plan, headers)
		return nil
	})
}

// checkTokenBudget returns an error if the API key's lifetime token budget would be exceeded.
func (h *Handler) checkTokenBudget(c *gin.Context, body []byte) error {
	apiKeyID := c.GetString("api_key_id")
	if apiKeyID == "" {
		return nil
	}
	maxTokensVal, ok := c.Get("max_tokens")
	if !ok {
		return nil
	}
	maxTokens, ok := maxTokensVal.(int64)
	if !ok || maxTokens <= 0 {
		return nil
	}
	var total int64
	if err := h.db.QueryRow(`SELECT COALESCE(total_tokens, 0) FROM api_key_usage WHERE api_key_id = ?`, apiKeyID).Scan(&total); err != nil && err != sql.ErrNoRows {
		logging.Logger.Warn("checkTokenBudget: failed to read usage", "error", err.Error())
	}
	if total >= maxTokens {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "API key token budget exhausted", "code": "api_key_token_budget_exhausted"}})
		return errors.New("api key token budget exhausted")
	}
	requested := gjson.GetBytes(body, "max_tokens").Int()
	if requested > 0 && total+requested > maxTokens {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "requested tokens exceed API key budget", "code": "request_exceeds_api_key_token_budget"}})
		return errors.New("request exceeds api key token budget")
	}
	return nil
}

// incrementAPIKeyUsage adds consumed tokens to the API key's cumulative lifetime total.
func (h *Handler) incrementAPIKeyUsage(apiKeyID string, tokens int64) {
	if apiKeyID == "" || tokens <= 0 {
		return
	}
	now := time.Now().Unix()
	if _, err := h.db.Exec(`INSERT INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES (?, ?, ?) ON CONFLICT(api_key_id) DO UPDATE SET total_tokens = total_tokens + excluded.total_tokens, updated_at = excluded.updated_at`, apiKeyID, tokens, now); err != nil {
		logging.Logger.Warn("incrementAPIKeyUsage: failed to update usage", "error", err.Error())
	}
}
