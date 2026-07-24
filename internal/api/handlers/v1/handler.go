package v1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"sort"
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

		// Skip body-reading for non-JSON request kinds. Multipart bodies (STT)
		// cannot be read here and restored reliably for downstream handlers, so
		// we still track them using the form fields from the original request.
		contentType := c.GetHeader("Content-Type")
		isMultipart := strings.Contains(contentType, "multipart/")

		model := ""
		stream := false
		if isMultipart {
			if err := c.Request.ParseMultipartForm(32 << 20); err == nil {
				model = c.PostForm("model")
			}
		} else {
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
			model = executor.JSONGet(raw, "model")
			stream = executor.IsStreamRequest(raw)
		}

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
			Stream:         stream,
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

// apiTypeFromPath derives a client-facing API type label from the request path.
// This tells the dashboard which surface the caller used (openai, claude, etc.).
func apiTypeFromPath(path string) string {
	return unifiedSurface(path)
}

// logRequest enqueues a usage log entry enriched with the client IP and
// User-Agent from the gin context. It is a single place where all /v1
// request logging captures request metadata.
func (h *Handler) logRequest(c *gin.Context, entry *usage.LogEntry) {
	if h.tracker == nil || entry == nil || c == nil {
		return
	}
	entry.ClientIP = c.ClientIP()
	entry.UserAgent = c.Request.UserAgent()
	h.tracker.Log(entry)
}

// trackDevice records the calling client device for the resolved API key.
// It is a no-op when no device tracker is configured or the request is
// unauthenticated, so handlers can call it unconditionally after auth.
func (h *Handler) trackDevice(c *gin.Context) {
	if h.deviceTracker == nil || c == nil {
		return
	}
	apiKeyID := c.GetString("api_key_id")
	if apiKeyID == "" {
		return
	}
	h.deviceTracker.Track(apiKeyID, c.Request.Header, c.Request.UserAgent())
}

// unifiedSurface maps a proxy path to the client-facing API surface name.
// It is shared by logging and the active-request tracker. The special-case
// ordering matters: more-specific paths must match before generic substrings.
func unifiedSurface(path string) string {
	switch {
	case strings.Contains(path, "/chat/completions"):
		return "openai"
	case strings.Contains(path, "/messages"):
		return "claude"
	case strings.Contains(path, "/responses"):
		return "responses"
	case strings.Contains(path, "/embeddings"):
		return "embeddings"
	case strings.Contains(path, "/images/generations"):
		return "images"
	case strings.Contains(path, "/video/generations"):
		return "video"
	case strings.Contains(path, "/audio/speech"):
		return "tts"
	case strings.Contains(path, "/audio/transcriptions"):
		return "stt"
	case strings.Contains(path, "/count_tokens"):
		return "count_tokens"
	default:
		return modalityFromPath(path)
	}
}

// bindActiveConn fills in the chosen account for the in-flight request
// tracked on the request context (if any).
func (h *Handler) bindActiveConn(ctx context.Context, conn *Connection) {
	if id, ok := active.IDFrom(ctx); ok {
		active.BindConn(id, conn.ID, conn.Name, conn.Provider)
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
	deviceTracker       *usage.DeviceTracker
	authMgr             *auth.Manager
	resolver            *proxypool.Resolver
	exhaustion          *quota.ExhaustionCache
	conns               sync.Map // connID -> *Connection (write-through credential cache)
	compressionStrategy compression.Strategy
	exactCache          cache.CacheStorage
	providerCfg         *providercfg.Manager
	sessions            *connstate.SessionCache

	// failoverMaxAttempts caps how many connections the failover loop tries
	// before giving up. Loaded once from the failover_max_attempts setting (default 5).
	failoverMaxAttempts int

	// usageAccumulator is a test-only hook for asserting accumulateAPIKeyUsage
	// arguments. It is nil in production and does not alter request behavior.
	usageAccumulator func(apiKeyID string, reqBody []byte, respBody []byte, estimateOutput bool)

	// test-only executor factories so TTS/STT/Video endpoint tests can bypass
	// real network calls. Nil in production; handlers fall back to the real
	// executor constructors when these are unset.
	ttsExecutorFactory   func() executor.Executor
	sttExecutorFactory   func() executor.Executor
	videoExecutorFactory func() executor.Executor
}

// NewHandler creates a new v1 handler with all dependencies.
func NewHandler(
	db *sql.DB,
	writeQueue *db.WriteQueue,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	comboHandler *combo.Handler,
	tracker *usage.Tracker,
	deviceTracker *usage.DeviceTracker,
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
		deviceTracker:       deviceTracker,
		authMgr:             authManager,
		resolver:            resolver,
		exhaustion:          exhaustionCache,
		compressionStrategy: compressionStrategy,
		exactCache:          exactCache,
		providerCfg:         providerCfg,
		sessions:            connstate.NewSessionCache(),
		failoverMaxAttempts: loadFailoverMaxAttempts(db),
	}
}

// loadFailoverMaxAttempts reads the failover_max_attempts setting (default 5).
// A zero/negative value is clamped to the minimum of 1 so the loop always runs at least once.
func loadFailoverMaxAttempts(database *sql.DB) int {
	const def = 5
	var value string
	if err := database.QueryRow(`SELECT value FROM settings WHERE key = ?`, "failover_max_attempts").Scan(&value); err != nil || value == "" {
		return def
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// failoverAttempts returns the configured failover max attempts, falling back to
// the hard-coded default when the field is unset so manually constructed
// handlers or stale tests still work.
func (h *Handler) failoverAttempts() int {
	if h.failoverMaxAttempts <= 0 {
		return 5
	}
	return h.failoverMaxAttempts
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
func (h *Handler) getConnection(ctx context.Context, provider string, modelID string, sessionID string) (conn *Connection, err error) {
	provider = provideralias.ResolveAlias(provider)
	mode := h.providerCfg.RoutingMode(provider)
	now := time.Now()

	// Remember the selected connection for affinity routing, and try to
	// reuse a previously cached connection for this session/model.
	defer func() {
		if err == nil && conn != nil && mode == providercfg.Affinity && sessionID != "" {
			h.sessions.Put(connstate.SessionKey(provider, sessionID, modelID), conn.ID)
		}
	}()
	if mode == providercfg.Affinity && sessionID != "" {
		if c, ok := h.tryAffinityConnection(ctx, provider, sessionID, modelID, now, mode); ok {
			conn = c
			return
		}
	}

	conns := h.elig.GetByPrefixState(provider)
	logging.Logger.Debug("getConnection", "provider", provider, "eligible", len(conns))
	if len(conns) > 0 {
		start := h.pickStartIndex(provider, modelID, len(conns), mode)
		bound := pickMaxAttempts
		if bound > len(conns) {
			bound = len(conns)
		}
		for i := 0; i < bound; i++ {
			idx := (start + i) % len(conns)
			if conn, ok := h.tryPickConnection(ctx, conns[idx], provider, modelID, now, mode); ok {
				return conn, nil
			}
		}

		for i := bound; i < len(conns); i++ {
			idx := (start + i) % len(conns)
			if conn, ok := h.tryPickConnection(ctx, conns[idx], provider, modelID, now, mode); ok {
				return conn, nil
			}
		}
	}

	// Safety net: if the eligibility snapshot is empty or every eligible
	// candidate was filtered out (e.g. all accounts are in a transient cooldown),
	// scan all known connections for this provider and try them, ignoring
	// cooldown windows but still honoring exhaustion and terminal statuses.
	return h.getConnectionFallback(ctx, provider, modelID, now, mode)
}

// getConnectionFallback is the last-resort router used when the eligibility
// snapshot has no usable connection. It tries two passes:
//  1. Respect cooldowns but consider any non-terminal connection.
//  2. If everything is in cooldown, bypass cooldown once as emergency fallback
//     so a healthy account that was briefly cooled down can still receive traffic.
func (h *Handler) getConnectionFallback(ctx context.Context, provider, modelID string, now time.Time, mode providercfg.RoutingMode) (*Connection, error) {
	var candidates []*connstate.ConnectionState
	h.store.Range(func(connID string, cs *connstate.ConnectionState) bool {
		if cs.Prefix != provider {
			return true
		}
		if cs.GetStatus() == connstate.StatusDisabled {
			return true
		}
		candidates = append(candidates, cs)
		return true
	})

	candidates = h.orderCandidates(provider, modelID, candidates, mode)

	// Pass 1: respect cooldowns, just expand the pool beyond the eligibility snapshot.
	for _, cs := range candidates {
		if conn, ok := h.tryPickConnection(ctx, cs, provider, modelID, now, mode); ok {
			logging.Logger.Info("getConnection fallback (cooldown-aware) selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name)
			return conn, nil
		}
	}

	// Pass 2: every account is in cooldown. Bypass it as emergency fallback.
	for _, cs := range candidates {
		if conn, ok := h.tryPickConnectionFallback(ctx, cs.ID, provider, modelID, now, mode); ok {
			logging.Logger.Info("getConnection emergency fallback selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name)
			return conn, nil
		}
	}

	return nil, fmt.Errorf("no available connection for provider: %s (all exhausted or failing)", provider)
}

// tryPickConnectionFallback is like tryPickConnection but ignores connection-level
// cooldowns. It still respects model-level cooldowns and exhaustion so we do not
// hammer accounts that are genuinely out of quota.
func (h *Handler) tryPickConnectionFallback(ctx context.Context, connID, provider, modelID string, now time.Time, mode providercfg.RoutingMode) (*Connection, bool) {
	cs := h.store.Get(connID)
	if cs == nil {
		return nil, false
	}
	if cs.GetStatus().IsRoutingTerminal() {
		return nil, false
	}
	if status := cs.GetStatus(); status.IsHealable() && !cs.IsInCooldownAt(now) {
		cs.SetStatus(connstate.StatusReady, "")
	}
	if modelID != "" && cs.IsModelInCooldownAt(modelID, now) {
		return nil, false
	}
	if modelID != "" && h.exhaustion.IsExhaustedScopeAt(connID, connstate.ModelScope(provider, modelID), now) {
		return nil, false
	}
	if h.exhaustion.IsExhaustedAt(connID, now) {
		return nil, false
	}
	conn, err := h.getCachedConn(ctx, connID, now)
	if err != nil {
		logging.Logger.Debug("load conn failed", "conn", connID[:8], "err", err)
		return nil, false
	}
	cs.RecordUsed()
	logging.Logger.Debug("getConnection fallback selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name, "mode", mode)
	h.bindActiveConn(ctx, conn)
	return conn, true
}

// tryAffinityConnection attempts to reuse a cached connection for the
// provider/session/model combination. The cached connection is validated
// with the same cooldown/exhaustion checks as normal selection.
func (h *Handler) tryAffinityConnection(ctx context.Context, provider, sessionID, modelID string, now time.Time, mode providercfg.RoutingMode) (*Connection, bool) {
	if h.sessions == nil {
		return nil, false
	}
	cachedID, ok := h.sessions.Get(connstate.SessionKey(provider, sessionID, modelID))
	if !ok {
		return nil, false
	}
	cs := h.store.Get(cachedID)
	if cs == nil {
		return nil, false
	}
	if c, ok := h.tryPickConnection(ctx, cs, provider, modelID, now, mode); ok {
		logging.Logger.Debug("getConnection affinity hit", "provider", provider, "model", modelID, "conn", shortID(c.ID, 8))
		return c, true
	}
	return nil, false
}

// sessionIDForAffinity extracts a session identifier when the provider is
// configured for affinity routing. It is a no-op for other routing modes so
// the default hot path avoids unnecessary JSON/header parsing.
func (h *Handler) sessionIDForAffinity(c *gin.Context, provider, modelID string, body []byte) string {
	if h.sessions == nil || h.providerCfg.RoutingMode(provider) != providercfg.Affinity {
		return ""
	}
	return h.extractSessionID(c, body)
}

var sessionUUIDRegex = regexp.MustCompile(`_session_([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// extractSessionID returns a stable session identifier for affinity routing.
// The priority order is:
//  1. UUID embedded in metadata.user_id after "_session_".
//  2. X-Session-ID header.
//  3. Session-Id / Session_id header.
//  4. X-Client-Request-Id header.
//  5. conversation_id field in the JSON body.
//  6. Stable SHA-256 hash of the first system+user+assistant message contents.
func (h *Handler) extractSessionID(c *gin.Context, body []byte) string {
	if len(body) > 0 {
		if userID := gjson.GetBytes(body, "metadata.user_id").String(); userID != "" {
			if m := sessionUUIDRegex.FindStringSubmatch(userID); len(m) > 1 {
				return m[1]
			}
		}
	}
	if c != nil && c.Request != nil {
		if v := firstHeaderValue(c.Request.Header, "x-session-id"); v != "" {
			return v
		}
		if v := firstHeaderValue(c.Request.Header, "session-id"); v != "" {
			return v
		}
		if v := firstHeaderValue(c.Request.Header, "session_id"); v != "" {
			return v
		}
		if v := firstHeaderValue(c.Request.Header, "x-client-request-id"); v != "" {
			return v
		}
	}
	if len(body) > 0 {
		if conv := gjson.GetBytes(body, "conversation_id").String(); conv != "" {
			return conv
		}
		if messages := gjson.GetBytes(body, "messages"); messages.IsArray() {
			var parts []string
			var seenSystem, seenUser, seenAssistant bool
			for _, msg := range messages.Array() {
				role := msg.Get("role").String()
				content := msg.Get("content").String()
				switch role {
				case "system":
					if !seenSystem {
						parts = append(parts, content)
						seenSystem = true
					}
				case "user":
					if !seenUser {
						parts = append(parts, content)
						seenUser = true
					}
				case "assistant":
					if !seenAssistant {
						parts = append(parts, content)
						seenAssistant = true
					}
				}
				if seenSystem && seenUser && seenAssistant {
					break
				}
			}
			if len(parts) > 0 {
				sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
				return hex.EncodeToString(sum[:])
			}
		}
	}
	return ""
}

func firstHeaderValue(h http.Header, name string) string {
	if h == nil {
		return ""
	}
	lower := strings.ToLower(name)
	for k, v := range h {
		if strings.ToLower(k) == lower && len(v) > 0 {
			if s := strings.TrimSpace(v[0]); s != "" {
				return s
			}
		}
	}
	return ""
}

// pickStartIndex returns the first index to inspect based on the configured
// routing mode. first_eligible always starts at 0; round_robin rotates an
// atomic counter keyed by provider and modelID; random chooses a uniform index.
func (h *Handler) pickStartIndex(provider, modelID string, total int, mode providercfg.RoutingMode) int {
	if total <= 1 {
		return 0
	}
	switch mode {
	case providercfg.RoundRobin:
		return h.providerCfg.NextRoundRobinIndex(provider, modelID, total)
	case providercfg.Random:
		return rand.IntN(total)
	default:
		return 0
	}
}

// tryPickConnection checks a single eligible connection for model-level
// cooldowns/quota exhaustion and loads its DB credentials when it passes.
// The caller supplies the pre-resolved *ConnectionState so the routing hot path
// avoids a store.Get lookup per candidate.
//
// Terminal statuses are checked explicitly as a safety net: the eligibility
// snapshot is rebuilt asynchronously, so an account that was just marked
// auth_failed/disabled/etc. could still appear in the snapshot for a few
// milliseconds and be selected again. Cooldown/expired statuses are *not*
// terminal: a connection whose cooldown has already expired must be usable
// even if the snapshot still lists it as cooldown/quota_exhausted.
func (h *Handler) tryPickConnection(ctx context.Context, cs *connstate.ConnectionState, provider, modelID string, now time.Time, mode providercfg.RoutingMode) (*Connection, bool) {
	if cs.GetStatus().IsRoutingTerminal() {
		return nil, false
	}
	if status := cs.GetStatus(); status.IsHealable() && !cs.IsInCooldownAt(now) {
		cs.SetStatus(connstate.StatusReady, "")
	}
	if cs.IsInCooldownAt(now) {
		return nil, false
	}
	if modelID != "" && cs.IsModelInCooldownAt(modelID, now) {
		return nil, false
	}
	connID := cs.ID
	if modelID != "" && h.exhaustion.IsExhaustedScopeAt(connID, connstate.ModelScope(provider, modelID), now) {
		return nil, false
	}
	if h.exhaustion.IsExhaustedAt(connID, now) {
		return nil, false
	}
	conn, err := h.getCachedConn(ctx, connID, now)
	if err != nil {
		logging.Logger.Debug("load conn failed", "conn", connID[:8], "err", err)
		return nil, false
	}
	cs.RecordUsed()
	logging.Logger.Debug("getConnection selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name, "mode", mode)
	h.bindActiveConn(ctx, conn)
	return conn, true
}

// orderCandidates reorders eligible connections according to the provider's
// configured routing mode.
//
//   - first_eligible: accounts are sorted by remaining quota (highest first).
//   - round_robin: rotate the starting index keyed by provider and modelID,
//     but the rotation is applied on top of the quota-sorted list so healthier
//     accounts are preferred.
//   - random: pick a random starting index on the quota-sorted list.
func (h *Handler) orderCandidates(provider, modelID string, candidates []*connstate.ConnectionState, mode providercfg.RoutingMode) []*connstate.ConnectionState {
	if len(candidates) <= 1 {
		return candidates
	}

	// Quota-aware, then recency-aware: put accounts with the most remaining quota
	// first, and break ties by preferring least-recently-used connections. This
	// spreads simultaneous requests across siblings instead of concentrating them
	// on the same freshly-selected connection.
	sort.SliceStable(candidates, func(i, j int) bool {
		ri, rj := candidates[i].GetRemainingPct(), candidates[j].GetRemainingPct()
		if ri != rj {
			return ri > rj
		}
		return candidates[i].LastUsedAt().Before(candidates[j].LastUsedAt())
	})

	var start int
	switch mode {
	case providercfg.RoundRobin:
		start = h.providerCfg.NextRoundRobinIndex(provider, modelID, len(candidates))
	case providercfg.Random:
		start = rand.IntN(len(candidates))
	default:
		return candidates
	}

	if start == 0 {
		return candidates
	}
	out := make([]*connstate.ConnectionState, 0, len(candidates))
	out = append(out, candidates[start:]...)
	out = append(out, candidates[:start]...)
	return out
}

// prepareConnection performs preflight checks and proactive token refresh for a
// specific connection. Used by combo routing so it gets the same cooldown/exhaustion
// guards and OAuth refresh as the regular single-model path.
func (h *Handler) prepareConnection(ctx context.Context, connID, provider, modelID string, now time.Time) (*Connection, error) {
	cs := h.store.Get(connID)
	if cs == nil {
		return nil, fmt.Errorf("connection state not found")
	}
	if cs.GetStatus().IsRoutingTerminal() {
		return nil, fmt.Errorf("connection terminal status")
	}
	if status := cs.GetStatus(); status.IsHealable() && !cs.IsInCooldownAt(now) {
		cs.SetStatus(connstate.StatusReady, "")
	}
	if cs.IsInCooldownAt(now) {
		return nil, fmt.Errorf("connection in cooldown")
	}
	if modelID != "" && cs.IsModelInCooldownAt(modelID, now) {
		return nil, fmt.Errorf("model in cooldown")
	}
	if modelID != "" && h.exhaustion.IsExhaustedScopeAt(connID, connstate.ModelScope(provider, modelID), now) {
		return nil, fmt.Errorf("model exhausted")
	}
	if h.exhaustion.IsExhaustedAt(connID, now) {
		return nil, fmt.Errorf("connection exhausted")
	}

	conn, err := h.getCachedConn(ctx, connID, now)
	if err != nil {
		return nil, err
	}
	cs.RecordUsed()

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
func (h *Handler) getCachedConn(ctx context.Context, connID string, now time.Time) (*Connection, error) {
	if v, ok := h.conns.Load(connID); ok {
		cc := v.(cachedConn)
		if now.Sub(cc.cachedAt) < connCacheTTL {
			copied := *cc.conn
			return &copied, nil
		}
	}
	// Cache miss or expired — load from DB (write-through).
	conn, err := h.loadConnectionByID(ctx, connID)
	if err != nil {
		return nil, err
	}
	h.conns.Store(connID, cachedConn{conn: conn, cachedAt: now})
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
	"cx":       5 * time.Minute,  // Codex: Auth0 rotating refresh tokens
	"ag":       15 * time.Minute, // Antigravity: Google non-rotating refresh tokens
	"kiro":     5 * time.Minute,  // Kiro: AWS SSO OIDC one-time-use refresh tokens
	"copilot":  5 * time.Minute,  // Copilot: GitHub device-code tokens refresh early due to Copilot token skew
	"grok-cli": 5 * time.Minute,  // Grok CLI: xAI OIDC device-code tokens refresh before expiry
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

	providerSpecific := map[string]string{}
	if conn.ProviderSpecificData != "" {
		var raw map[string]any
		if e := json.Unmarshal([]byte(conn.ProviderSpecificData), &raw); e == nil {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					providerSpecific[k] = s
				}
			}
		}
	}

	creds := &auth.Credentials{
		AccessToken:      conn.AccessToken,
		RefreshToken:     conn.RefreshToken,
		ExpiresAt:        conn.OAuthExpiresAt,
		ProviderSpecific: providerSpecific,
	}

	newCreds, err := h.authMgr.RefreshTokenForConnection(ctx, conn.ID, providerType, creds)
	if err != nil {
		// Check for unrecoverable errors (matches OmniRoute isUnrecoverableRefreshError)
		if isUnrecoverableRefreshError(err) {
			log.Printf("Unrecoverable refresh error for %s/%s: %v — blocking connection", provider, conn.ID, err)
			connID := conn.ID
			h.writeQueue.EnqueueOrBlock(ctx, "refreshOAuth:authFailed", func(d *sql.DB) error {
				_, err := d.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', disabled_reason = 'auth_failed', updated_at = ? WHERE id = ?`,
					time.Now().Unix(), connID)
				return err
			})
			h.store.UpdateStatus(conn.ID, connstate.StatusDisabled, "auth_failed")
			h.elig.ScheduleUpdateProvider(provider)
		}
		return fmt.Errorf("refresh token: %w", err)
	}

	// Update connection in memory
	conn.AccessToken = newCreds.AccessToken
	conn.RefreshToken = newCreds.RefreshToken
	conn.OAuthExpiresAt = newCreds.ExpiresAt
	if len(newCreds.ProviderSpecific) > 0 {
		if psdBytes, err := json.Marshal(newCreds.ProviderSpecific); err == nil {
			conn.ProviderSpecificData = string(psdBytes)
		}
	}
	// Update the credential cache so subsequent requests see the new token immediately.
	h.conns.Store(conn.ID, cachedConn{conn: conn, cachedAt: time.Now()})
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
		return upstreamMessage("authentication failed"), http.StatusUnauthorized, "authentication_error"
	case connstate.ErrorRateLimit:
		return upstreamMessage("rate limit exceeded for all connections"), http.StatusTooManyRequests, "rate_limit_error"
	case connstate.ErrorQuota:
		return upstreamMessage("quota exhausted for all connections"), http.StatusTooManyRequests, "insufficient_quota"
	case connstate.ErrorBalanceEmpty:
		return upstreamMessage("balance empty for all connections"), http.StatusTooManyRequests, "insufficient_quota"
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

// allowedModelsFromContext extracts the API-key allowlist set stored on the
// request context by the auth middleware. A nil or empty set means unlimited.
func allowedModelsFromContext(ctx context.Context) map[string]struct{} {
	if v := ctx.Value("allowed_models"); v != nil {
		if set, ok := v.(map[string]struct{}); ok {
			return set
		}
	}
	return nil
}

// isModelAllowed reports whether modelID is permitted by the API-key allowlist
// stored on the request context. An absent or empty allowlist means unlimited
// access (legacy keys or the internal admin surface).
func (h *Handler) isModelAllowed(ctx context.Context, modelID string) bool {
	return modelIDAllowed(modelID, allowedModelsFromContext(ctx))
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
		if attempt < 2 && auth.IsAuthError(err) && h.proactiveRefreshToken(ctx, conn, provider) {
			req.AccessToken = conn.AccessToken
			continue
		}
		if !auth.IsAuthError(err) {
			break
		}
	}

	return resp, streamResult, err
}

// handleMediaCombo routes a non-streaming request (image or TTS) through a
// combo's steps. The caller supplies an executor factory per step. The first
// successful step response is written to the client; if every step fails, a
// combo-style 503 error is returned.
func (h *Handler) handleMediaCombo(
	c *gin.Context,
	comboResult *combo.ComboResult,
	body []byte,
	model string,
	start time.Time,
	modality string,
	defaultContentType string,
	accumulateBody bool,
	execForStep func(provider, modelName string) (executor.Executor, error),
) {
	strategy := h.combo.EffectiveStrategy(comboResult.Combo.Name, comboResult.Combo.Strategy)
	steps := h.combo.RotateSteps(comboResult.Combo.ID, strategy, comboResult.Combo.StickyLimit, comboResult.Steps)

	comboTimeout := 30 * time.Second
	if comboResult.Combo != nil && comboResult.Combo.TimeoutMs > 0 {
		comboTimeout = time.Duration(comboResult.Combo.TimeoutMs) * time.Millisecond
	}
	comboCtx, cancel := context.WithTimeout(c.Request.Context(), comboTimeout)
	defer cancel()

	var lastErr error
	var lastErrCategory string
	var lastModelName string

	for _, step := range steps {
		provider, modelName := executor.SplitModel(step.ModelID)
		if provider == "" {
			continue
		}
		lastModelName = modelName

		stepExec, err := execForStep(provider, modelName)
		if err != nil {
			lastErr = err
			lastErrCategory = "executor"
			continue
		}

		connIDs := h.combo.PickConnections(provider, modelName)
		if len(connIDs) == 0 {
			continue
		}

		stepBody := executor.JSONSet(body, "model", modelName)
		for _, connID := range connIDs {
			if comboCtx.Err() != nil {
				break
			}
			now := time.Now()
			conn, err := h.prepareConnection(comboCtx, connID, provider, modelName, now)
			if err != nil {
				continue
			}

			var psdMap map[string]string
			if conn.ProviderSpecificData != "" {
				if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
					logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
				}
			}

			req := &executor.Request{
				Model:                modelName,
				Body:                 stepBody,
				APIKey:               conn.APIKey,
				AccessToken:          conn.AccessToken,
				BaseURL:              conn.BaseURL,
				Provider:             provider,
				ProviderSpecificData: psdMap,
			}
			proxyCtx := h.proxyContext(comboCtx, conn)
			resp, _, err := h.executeWithRetry(proxyCtx, stepExec, req, conn, provider, modelName)
			latency := time.Since(start).Milliseconds()
			if err != nil {
				det := connstate.DetectError(proxyCtx, 0, "", err, provider, modelName, nil)
				if det.ModelID != "" && (det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
					scope := connstate.ModelScope(provider, det.ModelID)
					h.exhaustion.MarkExhausted(quota.ExhaustKey(connID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
				} else if det.Category == connstate.ErrorRateLimit {
					h.exhaustion.MarkExhausted(connID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
				} else if det.Category == connstate.ErrorQuota {
					ttl := 24 * time.Hour
					if det.CooldownUntil != nil {
						ttl = time.Until(*det.CooldownUntil)
					}
					h.exhaustion.MarkExhausted(connID, ttl)
				}
				h.combo.RecordFailure(connID, det)
				h.persistCooldownScoped(connID, det)
				if det.Status != connstate.StatusReady {
					h.elig.ScheduleUpdateProvider(provider)
				}
				lastErr = err
				lastErrCategory = string(det.Category)
				continue
			}

			h.resetBanCount(connID)
			h.persistSuccess(connID)
			h.combo.RecordSuccess(connID)

			var respBody []byte
			if accumulateBody {
				respBody = resp.Body
			}
			h.logRequest(c, &usage.LogEntry{
				ApiKeyID:       c.GetString("api_key_id"),
				ConnectionID:   connID,
				ProviderTypeID: provider,
				ModelID:        modelName,
				ComboID:        comboResult.Combo.Name,
				ProxyPoolID:    executor.ProxyPoolIDFromContext(proxyCtx),
				ApiType:        apiTypeFromPath(c.Request.URL.Path),
				Modality:       modality,
				Stream:         false,
				LatencyMs:      latency,
				StatusCode:     resp.StatusCode,
			})
			h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, respBody, false)
			contentType := defaultContentType
			if ct := resp.Headers.Get("Content-Type"); ct != "" {
				contentType = ct
			}
			c.Header("Content-Type", contentType)
			c.Status(resp.StatusCode)
			c.Writer.Write(resp.Body)
			return
		}
	}

	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, lastModelName)
	detail := gin.H{"model": model}
	if lastModelName != "" {
		detail["attempted_model"] = lastModelName
	}
	logging.Logger.Error(msg, "combo", model, "category", lastErrCategory)
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
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

// executeProviderCall builds an executor.Request from a connection and translated
// body, attaches proxy context, and runs executeDirect. It returns the proxy
// context (for downstream logging/PoolID lookups), the response, stream result,
// and any error from the executor.
func (h *Handler) executeProviderCall(
	ctx context.Context,
	exec executor.Executor,
	conn *Connection,
	provider, modelName string,
	translatedBody []byte,
	stream bool,
	streamCfg *executor.StreamConfig,
) (context.Context, *executor.Response, *executor.StreamResult, error) {
	var psdMap map[string]string
	if conn.ProviderSpecificData != "" {
		if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
			logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
		}
	}
	_, providerFormat, _ := h.registry.Get(provider)
	translatedBody = h.applyThinkingOverrideFromContext(ctx, translatedBody, string(providerFormat))
	req := &executor.Request{
		Model:                modelName,
		Body:                 translatedBody,
		Stream:               stream,
		APIKey:               conn.APIKey,
		AccessToken:          conn.AccessToken,
		BaseURL:              conn.BaseURL,
		Provider:             provider,
		ProviderSpecificData: psdMap,
		StreamConfig:         streamCfg,
	}
	req.ConnectionID = conn.ID
	req.PersistProviderSpecificData = h.persistProviderSpecificData(ctx, conn)
	proxyCtx := h.proxyContext(ctx, conn)
	resp, streamResult, err := h.executeDirect(proxyCtx, exec, req)
	return proxyCtx, resp, streamResult, err
}

// persistProviderSpecificData returns a callback that writes an updated PSD map
// back to the connection cache and database. It is used by executors that
// mutate per-connection state (e.g., Grok CLI session/turn headers).
func (h *Handler) persistProviderSpecificData(ctx context.Context, conn *Connection) func(map[string]string) error {
	return func(psd map[string]string) error {
		psdBytes, err := json.Marshal(psd)
		if err != nil {
			return fmt.Errorf("marshal provider_specific_data: %w", err)
		}
		conn.ProviderSpecificData = string(psdBytes)
		h.conns.Store(conn.ID, cachedConn{conn: conn, cachedAt: time.Now()})
		connID := conn.ID
		if h.writeQueue == nil {
			_, err := h.db.ExecContext(ctx, `UPDATE connections SET provider_specific_data = ?, updated_at = ? WHERE id = ?`, psdBytes, time.Now().Unix(), connID)
			return err
		}
		return h.writeQueue.Do(ctx, "grokcli:persistPSD", func(d *sql.DB) error {
			_, err := d.ExecContext(ctx, `UPDATE connections SET provider_specific_data = ?, updated_at = ? WHERE id = ?`, psdBytes, time.Now().Unix(), connID)
			return err
		})
	}
}

// handleFailoverError records an upstream failure, marks the connection
// exhausted/cooled-down when appropriate, refreshes eligibility, and logs it.
// Returns (shouldRetry, category): shouldRetry=false for non-retryable errors
// (model_not_found, auth_failed), category for the caller to build better error messages.
func (h *Handler) handleFailoverError(ctx context.Context, c *gin.Context, conn *Connection, provider, modelName string, err error, attempt int, latency int64, stream bool) (bool, string) {
	det := connstate.DetectError(c.Request.Context(), 0, "", err, provider, modelName, nil)
	// Always mark rate-limit/quota exhaustion at model scope when we know the
	// model. This keeps the connection eligible for other models unless the
	// provider itself is globally exhausted. Connection-wide exhaustion is a
	// fallback when the model is unknown.
	if det.ModelID != "" && (det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
		scope := connstate.ModelScope(provider, det.ModelID)
		h.exhaustion.MarkExhausted(quota.ExhaustKey(conn.ID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
	} else if det.Category == connstate.ErrorRateLimit {
		h.exhaustion.MarkExhausted(conn.ID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
	} else if det.Category == connstate.ErrorQuota {
		ttl := 24 * time.Hour // fallback for daily quotas
		if det.CooldownUntil != nil {
			ttl = time.Until(*det.CooldownUntil)
		}
		h.exhaustion.MarkExhausted(conn.ID, ttl)
	}
	h.combo.RecordFailure(conn.ID, det)
	h.persistCooldownScoped(conn.ID, det)
	// Update in-memory status so dashboard reflects rate_limited/quota_exhausted immediately.
	if det.Status != "" {
		if det.Status == connstate.StatusDisabled && det.DisabledReason != "" {
			h.store.UpdateStatus(conn.ID, det.Status, det.DisabledReason)
		} else {
			h.store.UpdateStatus(conn.ID, det.Status)
		}
	}
	// For providers with API-backed quota (CodeBuddy), refresh quota inline so
	// the next routing decision uses the latest state instead of stale cache.
	if provider == "codebuddy" && (det.Category == connstate.ErrorQuota || det.Category == connstate.ErrorRateLimit) {
		h.refreshQuotaAsync(conn.ID)
	}
	h.elig.ScheduleUpdateProvider(provider)
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

	h.logRequest(c, &usage.LogEntry{
		ApiKeyID:       c.GetString("api_key_id"),
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        modelName,
		ProxyPoolID:    executor.ProxyPoolIDFromContext(ctx),
		ApiType:        apiTypeFromPath(c.Request.URL.Path),
		Modality:       "chat",
		Stream:         stream,
		LatencyMs:      latency,
		ErrorMessage:   err.Error(),
	})

	// Non-retryable errors: only model-not-found is a hard stop. Auth failures
	// now fail over too (a sibling connection may hold a valid key), so they are
	// retryable here.
	switch det.Category {
	case connstate.ErrorModelNotFound:
		return false, string(det.Category)
	}
	return true, string(det.Category)
}

// refreshQuotaAsync fetches and caches the latest quota for a connection in
// the background. Used after a quota/rate-limit error so routing decisions
// use fresh data instead of the scheduled cache.
func (h *Handler) refreshQuotaAsync(connID string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logging.Logger.Warn("refreshQuotaAsync panic recovered", "conn", shortID(connID, 8), "panic", r)
			}
		}()

		cq, err := quota.FetchConnectionQuota(h.db, connID)
		if err != nil {
			logging.Logger.Debug("inline quota refresh failed", "conn", shortID(connID, 8), "err", err.Error())
			return
		}

		var changed bool
		quota.SaveQuotaCache(h.db, []quota.ProviderQuota{{
			ProviderID:   cq.ProviderID,
			ProviderName: cq.ProviderName,
			Connections:  []quota.ConnectionQuota{*cq},
		}})
		quota.UpdateConnectionQuotaStatus(h.db, h.store, h.exhaustion, connID, cq.Quotas, cq.Error, &changed)
		if changed {
			h.scheduleEligibilityUpdate(connID)
		}
	}()
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
	if upErr.StatusCode == http.StatusTooManyRequests || upErr.StatusCode == http.StatusPaymentRequired {
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
		h.logRequest(c, &usage.LogEntry{
			ApiKeyID:       c.GetString("api_key_id"),
			ConnectionID:   conn.ID,
			ProviderTypeID: provider,
			ModelID:        modelName,
			ProxyPoolID:    executor.ProxyPoolIDFromContext(ctx),
			ApiType:        apiTypeFromPath(c.Request.URL.Path),
			Modality:       "chat",
			Stream:         stream,
			LatencyMs:      time.Since(start).Milliseconds(),
			StatusCode:     upErr.StatusCode,
			ErrorMessage:   string(errBody),
		})
	}
	return true
}

// isFailoverEligible returns true for errors where trying another connection
// (different credentials/quota/state) might succeed. The chat/responses/messages
// failover loops use this to skip writeUpstreamClientError for auth/balance/etc
// and route through handleFailoverError instead.
func isFailoverEligible(cat connstate.ErrorCategory) bool {
	switch cat {
	case connstate.ErrorAuth, connstate.ErrorRateLimit, connstate.ErrorQuota, connstate.ErrorBalanceEmpty, connstate.ErrorTimeout, connstate.ErrorNetwork, connstate.ErrorServer:
		return true
	}
	return false
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

// computeAdaptiveReadiness extends the base readiness timeout based on request
// characteristics. Matches OmniRoute streamReadinessPolicy.ts.
//
// Large prompts, many messages, tool-heavy requests, and reasoning models need
// more time before the first SSE chunk arrives. Without adaptation, these
// requests false-positive as "readiness timeout" and kill viable streams.
//
// ponytail: simple arithmetic on body size and JSON fields, no heavy parsing.
func computeAdaptiveReadiness(body []byte, model string, baseMs int) int {
	if baseMs <= 0 {
		baseMs = 80000
	}
	extra := 0
	bodyLen := len(body)

	// Estimate message count by counting "role" occurrences (cheap heuristic).
	msgCount := bytes.Count(body, []byte(`"role"`))
	if msgCount > 150 {
		extra += 20000
	}
	if msgCount > 400 {
		extra += 25000 // total +45s for very large histories
	}

	// Tool count: count "function" or "tool" occurrences in tools array area.
	toolCount := bytes.Count(body, []byte(`"function"`))
	if toolCount > 15 {
		extra += 15000
	}

	// Payload size.
	if bodyLen > 250_000 {
		extra += 20000
	}
	if bodyLen > 750_000 {
		extra += 25000 // total +45s for very large payloads
	}

	// Reasoning models need more time for first chunk.
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "o1") ||
		strings.Contains(modelLower, "o3") ||
		strings.Contains(modelLower, "o4-mini") ||
		strings.Contains(modelLower, "gpt-5") ||
		strings.Contains(modelLower, "deepseek-r1") ||
		strings.Contains(modelLower, "thinking") {
		extra += 30000
	}

	total := baseMs + extra
	const cap = 180000 // 180s max, matches OmniRoute STREAM_READINESS_MAX_TIMEOUT_MS
	if total > cap {
		total = cap
	}
	return total
}

// streamResponse writes a translated SSE stream to the client with heartbeat and
// client-disconnect detection. Each translated chunk already includes the SSE
// frame (data: ...\n\n), so the helper writes the bytes as-is and flushes.
//
// It returns nil on a normal stream end or a client disconnect. A non-nil error
// indicates a mid-stream upstream failure. In combo mode (comboID != "") that
// error is returned to the caller instead of being written to the response,
// so the combo can failover to another connection/model without stopping the
// stream.
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
	silent bool,
) error {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "server_error"}})
		return nil
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
	var sawContent bool
	var sawFinish bool
	streamStart := time.Now()
	isCombo := comboID != ""

	for {
		select {
		case chunk, ok := <-result.Chunks:
			if !ok {
				// A clean upstream close that produced zero content is treated as a
				// failure. In combo/direct silent mode the caller can still fail over;
				// otherwise the caller will write an in-band error before closing.
				if !sawContent {
					logging.Logger.Warn("stream closed without delivering any content",
						"provider", provider, "model", model, "combo", comboID, "conn", shortID(conn.ID, 8))
					return errors.New("upstream stream closed without content")
				}

				// Some strict clients (e.g. PI Coding Agent) require a terminal
				// finish_reason chunk before [DONE]. If the upstream stream ended
				// without emitting one, synthesize it. Only do this for the OpenAI
				// chat-completions SSE format; Claude/Responses streams have their
				// own terminal shapes.
				if !sawFinish && clientFormat == executor.FormatOpenAI {
					finishChunk, _ := json.Marshal(map[string]any{
						"id":      "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10),
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   model,
						"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
					})
					c.Writer.Write([]byte("data: "))
					c.Writer.Write(finishChunk)
					c.Writer.Write([]byte("\n\n"))
					flusher.Flush()
				}

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
				h.logRequest(c, &usage.LogEntry{
					ApiKeyID:            c.GetString("api_key_id"),
					ConnectionID:        conn.ID,
					ProviderTypeID:      provider,
					ModelID:             model,
					ComboID:             comboID,
					ProxyPoolID:         executor.ProxyPoolIDFromContext(ctx),
					ApiType:             apiTypeFromPath(c.Request.URL.Path),
					Modality:            "chat",
					Stream:              true,
					InputTokens:         acc.InputTokens,
					OutputTokens:        acc.OutputTokens,
					ReasoningTokens:     acc.ReasoningTokens,
					CachedTokens:        acc.CachedTokens,
					CacheCreationTokens: acc.CacheCreationTokens,
					CostUsd:             acc.CostUsd,
					LatencyMs:           latency,
					StatusCode:          http.StatusOK,
					TokensEstimated:     tokensEstimated,
				})
				h.incrementAPIKeyUsage(c.GetString("api_key_id"), acc.InputTokens+acc.OutputTokens)
				return nil
			}

			if chunk.Err != nil {
				// Keep errors inside OpenAI-compatible `data:` events so standard
				// clients can surface them; avoid non-standard `event: error`.
				// Only record upstream failures as exhaustion/cooldown; client
				// cancellations (e.g. disconnects) must not penalize connections.
				if h.isClientCanceled(c, chunk.Err) {
					// Client disconnected — log for debugging but don't penalize connection.
					logging.Logger.Info("stream ended: client disconnected", "provider", provider, "model", model)
					return nil
				}
				latency := time.Since(start).Milliseconds()
				h.handleFailoverError(ctx, c, conn, provider, model, chunk.Err, 0, latency, true)
				// If this is a combo stream or the caller asked for silent failure,
				// don't write error/DONE yet. Return the error so the caller can
				// failover to the next connection/model and keep the stream alive.
				if isCombo || silent {
					errMsg := chunk.Err.Error()
					logging.Logger.Warn("mid-stream failure in combo, failing over",
						"provider", provider, "model", model, "combo", comboID,
						"error", errMsg)
					h.logRequest(c, &usage.LogEntry{
						ApiKeyID:       c.GetString("api_key_id"),
						ConnectionID:   conn.ID,
						ProviderTypeID: provider,
						ModelID:        model,
						ComboID:        comboID,
						ProxyPoolID:    executor.ProxyPoolIDFromContext(ctx),
						ApiType:        apiTypeFromPath(c.Request.URL.Path),
						Modality:       "chat",
						Stream:         true,
						LatencyMs:      latency,
						StatusCode:     http.StatusBadGateway,
						ErrorMessage:   errMsg,
					})
					return chunk.Err
				}
				c.Writer.Write([]byte("data: "))
				c.Writer.Write(errFormatter(chunk.Err))
				c.Writer.Write([]byte("\n\n"))
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
				h.logRequest(c, &usage.LogEntry{
					ApiKeyID:       c.GetString("api_key_id"),
					ConnectionID:   conn.ID,
					ProviderTypeID: provider,
					ModelID:        model,
					ComboID:        comboID,
					ProxyPoolID:    executor.ProxyPoolIDFromContext(ctx),
					ApiType:        apiTypeFromPath(c.Request.URL.Path),
					Modality:       "chat",
					Stream:         true,
					LatencyMs:      latency,
					ErrorMessage:   chunk.Err.Error(),
				})
				return chunk.Err
			}

			lastChunkTime = time.Now()
			translatedChunks := registry.Response(ctx, string(clientFormat), string(providerFormat), model, originalReq, translatedReq, chunk.Payload, &streamState)
			for _, tc := range translatedChunks {
				c.Writer.Write(tc)
				flusher.Flush()
				totalOutputBytes += estimateOutputFromTranslatedChunk(tc)
				if !sawFinish {
					sawFinish = chunkHasNonNullFinishReason(tc)
				}
			}

			// Stream quality peek: log time-to-first-content and validate
			// the stream is not a zombie (200 OK but empty content).
			// ponytail: fail-open on ambiguous, log-only, no rejection.
			if !sawContent && len(chunk.Payload) > 0 {
				// Skip SSE comments/keepalive pings (lines starting with `:`).
				if len(chunk.Payload) > 0 && chunk.Payload[0] != ':' {
					sawContent = true
					ttfc := time.Since(streamStart).Milliseconds()
					payload := chunk.Payload
					hasContent := bytes.Contains(payload, []byte(`"choices"`)) ||
						bytes.Contains(payload, []byte(`"delta"`)) ||
						bytes.Contains(payload, []byte(`content_block`)) ||
						bytes.Contains(payload, []byte(`"text"`)) ||
						bytes.Contains(payload, []byte(`"candidates"`))
					if !hasContent {
						logging.Logger.Warn("stream quality: first chunk has no recognized content markers",
							"provider", provider, "model", model,
							"ttfc_ms", ttfc, "payload_len", len(payload),
							"combo", comboID)
					} else {
						logging.Logger.Debug("stream quality: first content",
							"provider", provider, "model", model,
							"ttfc_ms", ttfc, "payload_len", len(payload))
					}
				}
			}

			if counts, found := ExtractTokensFromSSEChunk(chunk.Payload); found {
				MergeTokenCounts(&acc, &counts)
			}
			if costUsd, found := ExtractCostInUsdTicksFromSSEChunk(chunk.Payload); found {
				acc.CostUsd = costUsd
			}

		case <-ticker.C:
			if time.Since(lastChunkTime) >= heartbeatInterval {
				executor.WriteSSEHeartbeat(c.Writer, flusher)
			}

		case <-ctx.Done():
			// Log the context cancellation reason for debugging silent stream stops.
			// This helps distinguish between client disconnects, combo timeouts, and other cancellations.
			logging.Logger.Warn("stream context cancelled", "provider", provider, "model", model, "error", ctx.Err())

			if !silent {
				// Send in-band error event + [DONE] so the client knows the stream
				// ended. Without this, clients hang waiting for data that will never come.
				// Matches OmniRoute buildStreamErrorChunks / 9router streamHandler behavior.
				if !h.isClientCanceled(c, ctx.Err()) {
					errMsg := ctx.Err().Error()
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						errMsg = "stream deadline exceeded"
					}
					errBytes, _ := json.Marshal(gin.H{"error": gin.H{"message": errMsg, "type": "server_error"}})
					c.Writer.Write([]byte("data: "))
					c.Writer.Write(errBytes)
					c.Writer.Write([]byte("\n\n"))
				}
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
			}
			return ctx.Err()
		}
	}
}

// chunkHasNonNullFinishReason reports whether a translated SSE frame contains a
// non-null finish_reason value. This is a fast path check; it intentionally
// ignores null-valued finish_reason markers that appear on intermediate chunks.
func chunkHasNonNullFinishReason(frame []byte) bool {
	data := bytes.TrimSpace(frame)
	if bytes.HasPrefix(data, []byte("data:")) {
		data = bytes.TrimSpace(data[5:])
	}
	if len(data) == 0 || string(data) == "[DONE]" {
		return false
	}
	idx := bytes.Index(data, []byte(`"finish_reason"`))
	if idx == -1 {
		return false
	}
	i := idx + len(`"finish_reason"`)
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i < len(data) && data[i] == ':' {
		i++
	}
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	return i < len(data) && data[i] == '"'
}

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
		h.scheduleEligibilityUpdate(connID)
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

	// Balance-empty is a terminal state (needs manual top-up), and auth failures
	// are also terminal (invalid credentials). Persist these immediately even when
	// there is no cooldown horizon so a restart does not resurrect a bad account.
	status := string(det.Status)
	if det.Category == connstate.ErrorQuota {
		status = string(connstate.StatusQuotaExhausted)
	}
	statusVal := status
	// Only persist terminal failures or cooldown-bearing errors. Transient errors
	// without a cooldown (e.g. degraded/5xx) are left for the scheduler to heal.
	if det.CooldownUntil == nil && !connstate.Status(statusVal).IsRoutingTerminal() {
		return
	}
	var cooldownUntil *int64
	if det.CooldownUntil != nil {
		u := det.CooldownUntil.Unix()
		cooldownUntil = &u
	}
	// Terminal statuses should also be marked inactive so they stop being routed to
	// and become eligible for lifecycle garbage collection.
	isActive := 1
	disabledReason := ""
	if connstate.Status(statusVal).IsRoutingTerminal() {
		isActive = 0
		switch det.Category {
		case connstate.ErrorAuth:
			disabledReason = "auth_failed"
		case connstate.ErrorBalanceEmpty:
			disabledReason = "balance_empty"
		}
	}
	errMsg := det.Message
	errCode := string(det.Category)
	h.writeQueue.Enqueue("persistCooldownScoped", func(d *sql.DB) error {
		now := time.Now().Unix()
		_, err := d.Exec(`
UPDATE connections
SET is_active = ?,
    status = ?,
    disabled_reason = ?,
    cooldown_until = ?,
    last_error = ?,
    last_error_code = ?,
    failure_count = failure_count + 1,
    last_failure_at = ?,
    updated_at = ?
WHERE id = ?
`, isActive, statusVal, disabledReason, cooldownUntil, errMsg, errCode, now, now, connID)
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

// persistSuccess records a successful request so the dashboard reflects
// last_success_at. It also heals a stale cooldown/quota status back to ready
// so a connection that was briefly exhausted can be reused immediately after
// a successful request or test-connection.
func (h *Handler) persistSuccess(connID string) {
	now := time.Now().Unix()

	// In-memory recovery: reset status so the next eligibility snapshot
	// includes this connection right away. We intentionally do NOT clear the
	// exhaustion cache here; model-scoped rate limits may still be active for
	// other models, and expired entries are ignored by the pick checks anyway.
	if cs := h.store.Get(connID); cs != nil {
		if cs.GetStatus().IsHealable() {
			cs.SetStatus(connstate.StatusReady, "")
		}
	}
	h.scheduleEligibilityUpdate(connID)

	// Only clear a stale DB cooldown row when the cooldown has actually
	// expired. If the cooldown is still active, leave the DB row alone so the
	// scheduler can recover it when the time comes.
	h.writeQueue.Enqueue("persistSuccess", func(d *sql.DB) error {
		_, err := d.Exec(`
			UPDATE connections
			SET status = CASE
					WHEN status IN ('cooldown','rate_limited','quota_exhausted','degraded')
						 AND (cooldown_until IS NULL OR cooldown_until <= ?)
					THEN 'ready'
					ELSE status
				END,
				cooldown_until = CASE
					WHEN status IN ('cooldown','rate_limited','quota_exhausted','degraded')
					     AND (cooldown_until IS NULL OR cooldown_until <= ?)
					THEN NULL
					ELSE cooldown_until
				END,
				last_success_at = ?,
				updated_at = ?
			WHERE id = ?
		`, now, now, now, now, connID)
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
//
// Budget enforcement is best-effort: concurrent requests may race between the
// pre-flight check here and the post-response increment in incrementAPIKeyUsage.
// In practice the small window means a key can slightly exceed its limit under
// concurrency, but the guard still prevents unbounded overuse. When the budget
// is exhausted the handler chain is aborted so no downstream handler runs.
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
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "API key token budget exhausted", "code": "api_key_token_budget_exhausted"}})
		return errors.New("api key token budget exhausted")
	}
	requested := requestedTokenBudget(body)
	if requested > 0 && total+requested > maxTokens {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "requested tokens exceed API key budget", "code": "request_exceeds_api_key_token_budget"}})
		return errors.New("request exceeds api key token budget")
	}
	return nil
}

// requestedTokenBudget returns the largest requested token budget from the
// request body without mutating it. It reads max_tokens, max_completion_tokens,
// and max_output_tokens so the pre-flight budget check covers all common
// OpenAI-compatible limit fields.
func requestedTokenBudget(body []byte) int64 {
	mt := gjson.GetBytes(body, "max_tokens").Int()
	mct := gjson.GetBytes(body, "max_completion_tokens").Int()
	mot := gjson.GetBytes(body, "max_output_tokens").Int()
	requested := mt
	if mct > requested {
		requested = mct
	}
	if mot > requested {
		requested = mot
	}
	return requested
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

// accumulateAPIKeyUsage centralizes token extraction and budget updates for all
// /v1/* endpoints. If the response body includes explicit usage, those counts are
// used; otherwise it falls back to usage.EstimateTokensFromRequest. The
// estimateOutput flag controls whether output tokens are estimated from the
// response body — enabled only for chat/text endpoints so binary image/audio
// bytes are never counted as tokens.
func (h *Handler) accumulateAPIKeyUsage(apiKeyID string, reqBody, respBody []byte, estimateOutput bool) {
	if h.usageAccumulator != nil {
		h.usageAccumulator(apiKeyID, reqBody, respBody, estimateOutput)
		return
	}
	var total int64
	if counts := ExtractTokensFromBody(respBody); counts.InputTokens > 0 || counts.OutputTokens > 0 {
		total = counts.InputTokens + counts.OutputTokens
	} else {
		total = usage.EstimateTokensFromRequest(reqBody)
		if estimateOutput {
			total += usage.EstimateTokensFromResponse(respBody)
		}
	}
	h.incrementAPIKeyUsage(apiKeyID, total)
}

// scheduleEligibilityUpdate triggers a per-provider eligibility rebuild for the
// provider that owns connID. If the connection is not found in the store, it
// falls back to a full rebuild.
func (h *Handler) scheduleEligibilityUpdate(connID string) {
	cs := h.store.Get(connID)
	if cs == nil || h.elig == nil {
		if h.elig != nil {
			h.elig.ScheduleUpdate()
		}
		return
	}
	h.elig.ScheduleUpdateProvider(cs.Prefix)
}
