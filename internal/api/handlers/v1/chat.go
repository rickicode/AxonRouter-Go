package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Combo transient-error cooldown prevents a retry storm when a provider
// returns 502/503/504. Default 2 seconds, capped at 5 seconds.
const (
	defaultTransientCooldown = 2 * time.Second
	maxTransientCooldown     = 5 * time.Second
)

// transientErrorSleep waits out a transient cooldown while respecting context
// cancellation. It is overridable in tests to keep cooldown assertions fast.
var transientErrorSleep = func(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// upstreamHTTPStatus extracts the HTTP status code from an executor.UpstreamError,
// returning 0 when the error is not an upstream HTTP response.
func upstreamHTTPStatus(err error) int {
	var upErr *executor.UpstreamError
	if errors.As(err, &upErr) {
		return upErr.StatusCode
	}
	return 0
}

// isTransientUpstreamError reports whether err represents a 502/503/504 upstream
// response. These are the only transient statuses that trigger combo failover
// cooldown; non-transient errors still fail through immediately.
func isTransientUpstreamError(err error) bool {
	switch upstreamHTTPStatus(err) {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// transientCooldown returns the cooldown to apply before the next combo
// connection is tried. It clamps the value to maxTransientCooldown.
func transientCooldown() time.Duration {
	if maxTransientCooldown > 0 && defaultTransientCooldown > maxTransientCooldown {
		return maxTransientCooldown
	}
	return defaultTransientCooldown
}

// ChatCompletions handles POST /v1/chat/completions
func (h *Handler) ChatCompletions(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	h.trackDevice(c)

	// Apply compression (fail-open); skip if the request uses prompt-cache markers.
	body = h.compressRequestBody(body)

	body, model, _ := h.parseThinkingSuffixFromBody(c, body)
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}
	if !h.isModelAllowed(c.Request.Context(), model) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model not allowed for this API key", "type": "invalid_request_error"}})
		return
	}
	stream := executor.IsStreamRequest(body)
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		strategy := h.combo.EffectiveStrategy(comboResult.Combo.Name, comboResult.Combo.Strategy)
		comboResult.Steps = h.combo.ReorderStepsByCapabilities(comboResult.Steps, combo.DetectRequiredCapabilities(body))
		if strategy == "fusion" {
			h.handleFusionRequest(c, comboResult, strategy, body, model, start, stream)
			return
		}
		// Re-rotate using the effective strategy so overrides like round-robin/weighted are honored.
		comboResult.Steps = h.combo.RotateSteps(comboResult.Combo.ID, strategy, comboResult.Combo.StickyLimit, comboResult.Steps)
		h.handleComboRequest(c, comboResult, strategy, body, model, start, stream)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model must include provider prefix (e.g., openai/gpt-4o)", "type": "invalid_request_error"}})
		return
	}

	sessionID := h.sessionIDForAffinity(c, provider, modelName, body)

	// Cache check (exact match, non-stream, no tools, no cache_control)
	cacheKey := h.exactCacheKey(body, model, stream)
	if cacheKey != "" {
		if entry, ok := h.exactCache.Get(cacheKey); ok {
			h.serveCacheHit(c, body, entry)
			return
		}
	}

	// Replace model with unprefixed name
	body = executor.JSONSet(body, "model", modelName)

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	// Connection failover loop: try up to failoverMaxAttempts connections before giving up.
	// On each failure, mark the connection exhausted/cooldown and update eligibility
	// so the next getConnection call picks a different connection.
	clientFormat := executor.FormatOpenAI
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
	translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)
	translatedBody = h.applyThinkingOverrideFromContext(c.Request.Context(), translatedBody, string(providerFormat))
	// NOTE: failover limit now configurable via failover_max_attempts setting.
	maxAttempts := h.failoverAttempts()
	var lastConn *Connection
	var lastErr error
	var lastErrCategory string
attemptLoop:
	for attempt := range maxAttempts {
		if c.Request.Context().Err() != nil {
			writeContextDone(c)
			return
		}
		conn, err := h.getConnection(c.Request.Context(), provider, modelName, sessionID)
		if err != nil {
			if attempt == 0 {
				logging.Logger.Info("chat: get connection failed", "err", err.Error())
				// If every connection for this provider is in the same failure mode,
				// surface a precise error instead of a generic 503.
				if cat := h.store.ClassifyProviderUnavailable(provideralias.ResolveAlias(provider)); cat != connstate.ErrorUnknown {
					msg, statusCode, errType := buildFailoverErrorResponse(string(cat), nil, modelName)
					c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType}})
					return
				}
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
				return
			}
			break
		}
		lastConn = conn

		var psdMap map[string]string
		if conn.ProviderSpecificData != "" {
			if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
				logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
			}
		}

		// Resolve proxy config (+ retry candidates) early so we can log it
		var proxyCfg executor.ProxyConfig
		var proxyCands []executor.ProxyConfig
		if h.resolver != nil {
			proxyCands = h.proxyCandidates(conn)
			if len(proxyCands) > 0 {
				proxyCfg = proxyCands[0]
			}
		}

		logArgs := []any{"model", model, "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name, "attempt", attempt + 1, "proxy", proxyCfg.ProxyLabel()}
		if accountID := psdMap["accountId"]; accountID != "" {
			logArgs = append(logArgs, "account_id", accountID)
		}
		logging.Logger.Info("route", logArgs...)

		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		req := &executor.Request{
			Model:                modelName,
			Body:                 translatedBody,
			Stream:               stream,
			APIKey:               conn.APIKey,
			AccessToken:          conn.AccessToken,
			BaseURL:              conn.BaseURL,
			Provider:             provider,
			ProviderSpecificData: psdMap,
		}

		proxyCtx := executor.ContextWithProxy(c.Request.Context(), proxyCfg)
		if len(proxyCands) > 0 {
			proxyCtx = executor.ContextWithProxyCandidates(proxyCtx, proxyCands)
		}
		resp, streamResult, err := h.executeDirect(proxyCtx, exec, req)
		latency := time.Since(start).Milliseconds()
		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if streamResult != nil {
			connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
		}
		if provider == "cx" {
			h.codexPersistIfCodex(conn, resp, streamResult)
		}
		if err != nil {
			if h.isClientCanceled(c, err) {
				return
			}
			// NOTE: auth and balance failures now rotate to sibling connections
			// before being surfaced. Detect first so we skip the direct-writer.
			det := connstate.DetectError(proxyCtx, 0, "", err, provider, modelName, nil)
			if !isFailoverEligible(det.Category) {
				if h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, stream) {
					return
				}
			}
			retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, err, attempt, latency, stream)
			lastErr = err
			lastErrCategory = cat
			if !retry {
				break attemptLoop // non-retryable error, stop failover
			}
			if !failoverBackoff(c.Request.Context(), attempt, maxAttempts) {
				return
			}
			continue
		}

		h.resetBanCount(conn.ID)
		h.persistSuccess(conn.ID)
		h.combo.RecordSuccess(conn.ID)

		if req.Stream {
			// Use the client's request context (no timeout) for streaming.
			// Stream lifecycle is governed by StreamIdleTimeout/StreamReadinessTimeout/StallTimeout.
			//
			// Also use a holdback buffer and retry mid-stream failures across
			// connections, just like the combo path, so direct-mode streams
			// don't stop when one upstream connection dies.
			streamCtx, cancelStream := context.WithCancel(c.Request.Context())
			defer cancelStream()

			holdbackMs := 750
			holdbackBytes := 64 * 1024
			if req.StreamConfig != nil {
				if req.StreamConfig.HoldbackMs > 0 {
					holdbackMs = req.StreamConfig.HoldbackMs
				}
				if req.StreamConfig.HoldbackBytes > 0 {
					holdbackBytes = req.StreamConfig.HoldbackBytes
				}
			}
			holdbackChunks, holdbackErrCh := executor.WrapWithHoldback(streamCtx, streamResult.Chunks, holdbackMs, holdbackBytes)
			streamResult.Chunks = holdbackChunks

			select {
			case holdbackErr := <-holdbackErrCh:
				if holdbackErr != nil {
					cancelStream()
					logging.Logger.Warn("direct stream failed during holdback", "provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "error", holdbackErr.Error())
					det := connstate.DetectError(proxyCtx, 0, "", holdbackErr, provider, modelName, nil)
					if !isFailoverEligible(det.Category) {
						if h.writeUpstreamClientError(proxyCtx, c, holdbackErr, conn, provider, modelName, start, stream) {
							return
						}
					}
					retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, holdbackErr, attempt, time.Since(start).Milliseconds(), stream)
					lastErr = holdbackErr
					lastErrCategory = cat
					if !retry {
						break attemptLoop
					}
					if !failoverBackoff(c.Request.Context(), attempt, maxAttempts) {
						return
					}
					continue
				}
			case <-streamCtx.Done():
				return
			}

			if streamErr := h.handleStreamResponse(streamCtx, c, streamResult, conn, provider, modelName, start, translatedBody, body, "", true); streamErr != nil {
				if h.isClientCanceled(c, streamErr) {
					return
				}
				cancelStream()
				logging.Logger.Warn("direct mid-stream failure, failing over", "provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "error", streamErr.Error())
				det := connstate.DetectError(proxyCtx, 0, "", streamErr, provider, modelName, nil)
				if !isFailoverEligible(det.Category) {
					if h.writeUpstreamClientError(proxyCtx, c, streamErr, conn, provider, modelName, start, stream) {
						return
					}
				}
				retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, streamErr, attempt, time.Since(start).Milliseconds(), stream)
				lastErr = streamErr
				lastErrCategory = cat
				if !retry {
					break
				}
				if !failoverBackoff(c.Request.Context(), attempt, maxAttempts) {
					return
				}
				continue
			}
			return
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)
			tokenCounts := ExtractTokensFromBody(translatedResp)
			tokensEstimated := false
			if tokenCounts.InputTokens+tokenCounts.OutputTokens == 0 && resp.StatusCode < 400 {
				estInput := usage.EstimateTokensFromRequest(body)
				estOutput := usage.EstimateTokensFromResponse(translatedResp)
				if estInput > 0 || estOutput > 0 {
					tokenCounts.InputTokens = estInput
					tokenCounts.OutputTokens = estOutput
					tokensEstimated = true
				}
			}
			h.logRequest(c, &usage.LogEntry{
				ApiKeyID:            c.GetString("api_key_id"),
				ConnectionID:        conn.ID,
				ProviderTypeID:      provider,
				ModelID:             modelName,
				ProxyPoolID:         executor.ProxyPoolIDFromContext(proxyCtx),
				ApiType:             apiTypeFromPath(c.Request.URL.Path),
				Modality:            "chat",
				Stream:              stream,
				InputTokens:         tokenCounts.InputTokens,
				OutputTokens:        tokenCounts.OutputTokens,
				ReasoningTokens:     tokenCounts.ReasoningTokens,
				CachedTokens:        tokenCounts.CachedTokens,
				CacheCreationTokens: tokenCounts.CacheCreationTokens,
				CostUsd:             resp.CostUsd,
				LatencyMs:           latency,
				StatusCode:          resp.StatusCode,
				TokensEstimated:     tokensEstimated,
			})
			if resp.StatusCode < 300 {
				h.storeExactCache(cacheKey, translatedResp, resp.StatusCode)
			}
			h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, translatedResp, true)
			h.writeJSONResponse(c, resp.StatusCode, translatedResp)
		}
		return
	}

	// Build category-specific error message
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)

	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}

// comboStepResult is the structured outcome of attempting one combo step.
// It carries everything the orchestrator needs to either commit a successful
// response or decide whether to fall through to the next step.
type comboStepResult struct {
	step           db.ComboStep
	connID         string
	conn           *Connection
	exec           executor.Executor
	provider       string
	modelName      string
	resp           *executor.Response
	streamResult   *executor.StreamResult
	proxyCtx       context.Context
	translatedBody []byte
	streamCfg      *executor.StreamConfig
	latency        int64
	lastErr        error
	category       string
	retryable      bool
	handled        bool
	success        bool
}

// recordComboStepFailure applies the same cooldown/exhaustion side effects for a
// failed combo connection attempt that handleComboRequest currently applies. It
// mirrors that path exactly to avoid observable behavior changes.
func (h *Handler) recordComboStepFailure(comboCtx context.Context, connID, provider, modelName string, det connstate.ErrorDetection, err error) {
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
	if isTransientUpstreamError(err) {
		cd := transientCooldown()
		logging.Logger.Info("combo transient error cooldown before next connection",
			"provider", provider, "model", modelName,
			"conn", shortID(connID, 8), "status", upstreamHTTPStatus(err),
			"cooldown_ms", cd.Milliseconds())
		transientErrorSleep(comboCtx, cd)
	}
}

// executeComboStep executes one combo step end-to-end: resolve the executor,
// pick eligible connections, prepare each one, and call the provider. It
// classifies failures and applies cooldowns/exhaustion exactly like the inline
// logic in handleComboRequest so the orchestrator can decide what to do next.
func (h *Handler) executeComboStep(
	comboCtx context.Context,
	c *gin.Context,
	step db.ComboStep,
	body []byte,
	stream bool,
	clientFormat executor.ProviderFormat,
	start time.Time,
) comboStepResult {
	var result comboStepResult
	result.step = step

	provider, modelName := executor.SplitModel(step.ModelID)
	result.provider = provider
	result.modelName = modelName

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		result.lastErr = err
		result.category = "executor"
		result.retryable = true
		return result
	}
	result.exec = exec

	connIDs := h.combo.PickConnections(provider, modelName)
	for _, connID := range connIDs {
		if comboCtx.Err() != nil {
			break
		}
		result.connID = connID

		now := time.Now()
		conn, err := h.prepareConnection(comboCtx, connID, provider, modelName, now)
		if err != nil {
			continue
		}
		result.conn = conn

		translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
		translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)
		translatedBody = h.applyThinkingOverrideFromContext(c.Request.Context(), translatedBody, string(providerFormat))

		var streamCfg *executor.StreamConfig
		if stream {
			adaptiveMs := computeAdaptiveReadiness(body, modelName, 80000)
			streamCfg = &executor.StreamConfig{
				StreamReadinessTimeoutMs: adaptiveMs,
				AdaptiveReadiness:        true,
			}
		}

		execCtx := comboCtx
		if stream {
			execCtx = c.Request.Context()
		}
		proxyCtx, resp, streamResult, err := h.executeProviderCall(execCtx, exec, conn, provider, modelName, translatedBody, stream, streamCfg)
		if err == nil && !stream && resp != nil && (resp.StatusCode >= 500 || isUpstreamErrorBody(resp.Body)) {
			err = &executor.UpstreamError{
				StatusCode: resp.StatusCode,
				Body:       resp.Body,
				RawBody:    resp.Body,
			}
		}
		if err != nil {
			if h.isClientCanceled(c, err) {
				result.lastErr = err
				result.retryable = false
				return result
			}
			det := connstate.DetectError(comboCtx, 0, "", err, provider, modelName, nil)
			result.lastErr = err
			result.category = string(det.Category)

			if det.Category == connstate.ErrorModelNotFound {
				result.retryable = false
				if h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, stream) {
					result.handled = true
				}
				return result
			}

			result.retryable = true
			h.recordComboStepFailure(comboCtx, connID, provider, modelName, det, err)
			continue
		}

		result.success = true
		result.resp = resp
		result.streamResult = streamResult
		result.proxyCtx = proxyCtx
		result.translatedBody = translatedBody
		result.streamCfg = streamCfg
		result.latency = time.Since(start).Milliseconds()
		return result
	}

	result.retryable = true
	return result
}

// handleComboRequest handles a request that matched a combo.
//
// Semantics: iterate the combo's steps in strategy order. For each step, retry
// over all eligible connections of that step's model (mirroring the direct
// routing path's connection failover) before falling through to the next step.
// Non-retryable client errors (auth / model-not-found) are returned to the
// caller immediately instead of being fail-over'd to another model. If every
// step exhausts its connections, a detailed 503 is returned (last error + which
// connection/model was attempted) rather than a bare "all combo steps failed".
func (h *Handler) handleComboRequest(c *gin.Context, comboResult *combo.ComboResult, effectiveStrategy string, body []byte, model string, start time.Time, stream bool) {
	comboTimeout := 30 * time.Second
	if comboResult.Combo != nil && comboResult.Combo.TimeoutMs > 0 {
		comboTimeout = time.Duration(comboResult.Combo.TimeoutMs) * time.Millisecond
	}
	comboCtx, cancel := context.WithTimeout(c.Request.Context(), comboTimeout)
	defer cancel()

	clientFormat := executor.FormatOpenAI
	if strings.HasSuffix(c.Request.URL.Path, "/messages") {
		clientFormat = executor.FormatClaude
	} else if strings.HasSuffix(c.Request.URL.Path, "/responses") {
		clientFormat = executor.FormatOpenAIResponses
	}

	var lastConn *Connection
	var lastErr error
	var lastErrCategory string
	var lastModelName string

	for _, step := range comboResult.Steps {
		provider, modelName := executor.SplitModel(step.ModelID)
		lastModelName = modelName

		// Replace the model in the request body with this step's unprefixed model so
		// upstream providers receive the correct model ID (mirrors the direct path).
		body = executor.JSONSet(body, "model", modelName)

		exec, providerFormat, err := h.resolveExecutor(provider, modelName)
		if err != nil {
			// Cannot even build an executor for this model — record and try next step.
			lastErr = err
			lastErrCategory = "executor"
			continue
		}

		connIDs := h.combo.PickConnections(provider, modelName)
		if len(connIDs) == 0 {
			// No eligible connection for this step right now; fall through to next step.
			continue
		}

		for _, connID := range connIDs {
			if comboCtx.Err() != nil {
				// Overall combo deadline exceeded; stop trying further connections.
				break
			}
			now := time.Now()
			conn, err := h.prepareConnection(comboCtx, connID, provider, modelName, now)
			if err != nil {
				// Preflight rejected (cooldown/exhausted) — try next connection.
				continue
			}
			lastConn = conn

			translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
			translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)
			translatedBody = h.applyThinkingOverrideFromContext(c.Request.Context(), translatedBody, string(providerFormat))
			// Adaptive readiness: extend timeout for large/reasoning requests.
			var streamCfg *executor.StreamConfig
			if stream {
				adaptiveMs := computeAdaptiveReadiness(body, modelName, 80000)
				streamCfg = &executor.StreamConfig{
					StreamReadinessTimeoutMs: adaptiveMs,
					AdaptiveReadiness:        true,
				}
			}
			// Use the client's request context for execution to avoid combo timeout
			// cutting off long-lived streaming responses.
			execCtx := comboCtx
			if stream {
				execCtx = c.Request.Context()
			}
			proxyCtx, resp, streamResult, err := h.executeProviderCall(execCtx, exec, conn, provider, modelName, translatedBody, stream, streamCfg)
			latency := time.Since(start).Milliseconds()
			// Before declaring success, treat an upstream HTTP error (non-streaming)
			// as a retryable failure so the combo can failover instead of returning
			// an error status as if it were a real response.
			if err == nil && !stream && resp != nil && (resp.StatusCode >= 500 || isUpstreamErrorBody(resp.Body)) {
				err = &executor.UpstreamError{
					StatusCode: resp.StatusCode,
					Body:       resp.Body,
					RawBody:    resp.Body,
				}
			}
			if err != nil {
				if h.isClientCanceled(c, err) {
					return
				}
				det := connstate.DetectError(comboCtx, 0, "", err, provider, modelName, nil)

				// Non-retryable: only model-not-found is surfaced directly. Auth and
				// balance failures now fail over so a sibling connection gets a chance.
				if det.Category == connstate.ErrorModelNotFound {
					if h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, stream) {
						return
					}
					lastErr = err
					lastErrCategory = string(det.Category)
					break
				}

				// Retryable: apply model-scoped failover marking like the direct path.
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
				if isTransientUpstreamError(err) {
					cd := transientCooldown()
					logging.Logger.Info("combo transient error cooldown before next connection",
						"provider", provider, "model", modelName,
						"conn", shortID(connID, 8), "status", upstreamHTTPStatus(err),
						"cooldown_ms", cd.Milliseconds())
					transientErrorSleep(comboCtx, cd)
				}
				continue
			}

			// Success — write the response and stop.
			h.resetBanCount(connID)
			h.persistSuccess(connID)
			h.combo.RecordSuccess(connID)
			if provider == "cx" {
				h.codexPersistIfCodex(conn, resp, streamResult)
			}

			if stream {
				// Use the client's request context (no timeout) for streaming.
				// The comboCtx timeout is only for orchestration, not for live streams.
				// Stream lifecycle is governed by StreamIdleTimeout/StreamReadinessTimeout/StallTimeout.

				// Holdback buffer: wait 750ms/64KB before committing to this connection.
				// If the stream errors during holdback, still retry the next connection
				// transparently. Matches OmniRoute holdback behavior.
				holdbackMs := 750
				holdbackBytes := 64 * 1024
				if streamCfg != nil {
					if streamCfg.HoldbackMs > 0 {
						holdbackMs = streamCfg.HoldbackMs
					}
					if streamCfg.HoldbackBytes > 0 {
						holdbackBytes = streamCfg.HoldbackBytes
					}
				}
				// Holdback committed — stream is live, relay to client.
				// Use a dedicated sub-context so the holdback relay goroutine is cancelled
				// the moment we stop reading from it (upstream chunk error, normal end, or
				// client disconnect). Without this, after a mid-stream error the goroutine
				// leaks blocked on `out <- chunk` with no consumer.
				streamCtx, cancelStream := context.WithCancel(c.Request.Context())
				defer cancelStream() // safety net when handleComboRequest returns

				holdbackChunks, holdbackErrCh := executor.WrapWithHoldback(streamCtx, streamResult.Chunks, holdbackMs, holdbackBytes)
				streamResult.Chunks = holdbackChunks

				// Wait for holdback to commit or fail. Read directly from holdbackErrCh
				// (no forwarding goroutine) so a client disconnect doesn't leak a goroutine.
				select {
				case holdbackErr := <-holdbackErrCh:
					if holdbackErr != nil {
						// Stream failed during holdback window — kill the relay goroutine
						// and treat as retryable error so the next connection is tried.
						cancelStream()
						logging.Logger.Warn("combo stream failed during holdback",
							"provider", provider, "model", modelName,
							"conn", shortID(connID, 8), "error", holdbackErr.Error())
						det := connstate.DetectError(comboCtx, 0, "", holdbackErr, provider, modelName, nil)
						h.combo.RecordFailure(connID, det)
						lastErr = holdbackErr
						lastErrCategory = "holdback"
						continue // try next connection
					}
					// Holdback committed — stream is live, relay to client.
				case <-streamCtx.Done():
					cancelStream()
					return
				}

				if streamErr := h.handleClientStreamResponse(streamCtx, c, streamResult, conn, provider, modelName, start, translatedBody, body, comboResult.Combo.Name, true, clientFormat); streamErr != nil {
					// Mid-stream failure — failover to next connection/model instead of
					// stopping the stream. handleStreamResponse already skipped writing
					// error/DONE to the client when in combo mode.
					if h.isClientCanceled(c, streamErr) {
						cancelStream()
						return
					}
					cancelStream()
					det := connstate.DetectError(comboCtx, 0, "", streamErr, provider, modelName, nil)
					if det.Category == connstate.ErrorModelNotFound {
						lastErr = streamErr
						lastErrCategory = string(det.Category)
						break
					}
					// Always exhaust at model scope when we know the model, so other
					// models on the same connection remain routable.
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
					lastErr = streamErr
					lastErrCategory = "stream-" + string(det.Category)
					continue
				}
				return
			} else {
				translatedResp := registry.ResponseNonStream(comboCtx, string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)
				tokenCounts := ExtractTokensFromBody(translatedResp)
				tokensEstimated := false
				if tokenCounts.InputTokens+tokenCounts.OutputTokens == 0 && resp.StatusCode < 400 {
					estInput := usage.EstimateTokensFromRequest(body)
					estOutput := usage.EstimateTokensFromResponse(translatedResp)
					if estInput > 0 || estOutput > 0 {
						tokenCounts.InputTokens = estInput
						tokenCounts.OutputTokens = estOutput
						tokensEstimated = true
					}
				}
				h.logRequest(c, &usage.LogEntry{
					ApiKeyID:            c.GetString("api_key_id"),
					ConnectionID:        connID,
					ProviderTypeID:      provider,
					ModelID:             modelName,
					ComboID:             comboResult.Combo.Name,
					ProxyPoolID:         executor.ProxyPoolIDFromContext(proxyCtx),
					ApiType:             apiTypeFromPath(c.Request.URL.Path),
					Modality:            "chat",
					Stream:              stream,
					InputTokens:         tokenCounts.InputTokens,
					OutputTokens:        tokenCounts.OutputTokens,
					ReasoningTokens:     tokenCounts.ReasoningTokens,
					CachedTokens:        tokenCounts.CachedTokens,
					CacheCreationTokens: tokenCounts.CacheCreationTokens,
					LatencyMs:           latency,
					StatusCode:          resp.StatusCode,
					TokensEstimated:     tokensEstimated,
				})
				c.Header("Content-Type", "application/json")
				h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, translatedResp, true)
				c.Status(resp.StatusCode)
				c.Writer.Write(translatedResp)
			}
			return
		}
	}

	// Every step exhausted its connections (or had none eligible).
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, lastModelName)
	detail := gin.H{"model": model}
	if lastModelName != "" {
		detail["attempted_model"] = lastModelName
	}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "combo", model, "category", lastErrCategory)

	if stream {
		// Streaming clients expect an SSE error event and [DONE]. If the
		// stream already started, the HTTP status is already committed; keep
		// writing SSE frames. If it never started, send the same SSE error so
		// the client gets a consistent failure format.
		errBytes, _ := json.Marshal(gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(errBytes)
		c.Writer.Write([]byte("\n\ndata: [DONE]\n\n"))
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	if clientFormat == executor.FormatClaude {
		c.JSON(statusCode, claudeError(errType, msg))
		return
	}
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}

// handleFusionRequest runs a fusion combo: it executes all steps in parallel as
// a panel, waits for min_panel successes plus a grace period, then asks a judge
// model to synthesize the panel answers into one response for the client.
func (h *Handler) handleFusionRequest(c *gin.Context, comboResult *combo.ComboResult, effectiveStrategy string, body []byte, model string, start time.Time, stream bool) {
	// Fusion is currently only supported for the OpenAI chat completions API.
	// The panel/judge pipeline builds an OpenAI-compatible judge body and final
	// response; supporting Claude/Responses format requires additional translators.
	if !strings.HasSuffix(c.Request.URL.Path, "/chat/completions") {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "fusion strategy is only supported on /v1/chat/completions", "type": "invalid_request_error"}})
		return
	}

	cfg, err := combo.ParseFusionConfig(comboResult.Combo.FusionConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	if err := cfg.Validate(len(comboResult.Steps)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(cfg.PanelHardTimeoutMs)*time.Millisecond)
	defer cancel()

	resultsCh := make(chan fusionPanel, len(comboResult.Steps))
	var wg sync.WaitGroup
	for _, step := range comboResult.Steps {
		wg.Add(1)
		go func(step db.ComboStep) {
			defer wg.Done()
			connID, ok := h.combo.PickConnection(step)
			if !ok {
				resultsCh <- fusionPanel{err: errors.New("no eligible connection")}
				return
			}
			provider, modelName := executor.SplitModel(step.ModelID)
			now := time.Now()
			conn, err := h.prepareConnection(ctx, connID, provider, modelName, now)
			if err != nil {
				resultsCh <- fusionPanel{err: err}
				return
			}
			exec, providerFormat, err := h.resolveExecutor(provider, modelName)
			if err != nil {
				resultsCh <- fusionPanel{err: err}
				return
			}
			// Replace the model field with the panel model so OpenAI-compatible passthrough
			// providers receive the actual upstream model ID instead of "fusion".
			panelBody := setRequestModel(body, modelName)
			translatedBody := registry.Request(string(executor.FormatOpenAI), string(providerFormat), modelName, panelBody, false)
			translatedBody = sanitizeStreamOptions(translatedBody, false, executor.FormatOpenAI, providerFormat, c.Request.URL.Path)
			translatedBody = stripFusionTools(translatedBody)
			_, resp, _, err := h.executeProviderCall(ctx, exec, conn, provider, modelName, translatedBody, false, nil)
			if err != nil {
				resultsCh <- fusionPanel{err: err}
				return
			}
			if resp != nil && (resp.StatusCode >= 500 || isUpstreamErrorBody(resp.Body)) {
				resultsCh <- fusionPanel{err: fmt.Errorf("panel upstream error: status %d", resp.StatusCode)}
				return
			}
			content := extractAssistantContent(resp.Body)
			if content == "" {
				resultsCh <- fusionPanel{err: errors.New("empty panel response")}
				return
			}
			resultsCh <- fusionPanel{connID: connID, modelID: step.ModelID, content: content}
		}(step)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var successes []fusionPanel
	var failures int
	hardTimeout := time.NewTimer(time.Duration(cfg.PanelHardTimeoutMs) * time.Millisecond)
	defer hardTimeout.Stop()

	// Collect until min_panel successes, then hand off to the grace period.
	collectResults := func() bool {
		for {
			if len(successes) >= cfg.MinPanel {
				return false // not done — caller should collect grace
			}
			if len(successes)+failures >= len(comboResult.Steps) {
				return true // all panels reported; no grace needed
			}
			select {
			case r, ok := <-resultsCh:
				if !ok {
					return true
				}
				if r.err == nil {
					successes = append(successes, r)
				} else {
					failures++
				}
			case <-hardTimeout.C:
				return true
			}
		}
	}

	done := collectResults()
	if len(successes) >= cfg.MinPanel && !done {
		grace := time.NewTimer(time.Duration(cfg.StragglerGraceMs) * time.Millisecond)
	collectGrace:
		for {
			select {
			case r, ok := <-resultsCh:
				if !ok {
					break collectGrace
				}
				if r.err == nil {
					successes = append(successes, r)
				}
				if len(successes) >= len(comboResult.Steps) {
					break collectGrace
				}
			case <-grace.C:
				break collectGrace
			case <-hardTimeout.C:
				break collectGrace
			}
		}
		grace.Stop()
	}

	if len(successes) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "all fusion panel models failed", "type": "fusion_panel_failed"}})
		return
	}

	if len(successes) == 1 {
		h.writeFusionResponse(c, successes[0].content, model, stream)
		return
	}

	// Run judge synthesis.
	judgeModel := cfg.JudgeModel
	if judgeModel == "" {
		judgeModel = comboResult.Steps[0].ModelID
	}
	judgeBody := buildFusionJudgeBody(body, successes, cfg.AnonymizeSources)
	judgeConnID, ok := h.combo.PickConnection(db.ComboStep{ModelID: judgeModel})
	if !ok {
		// Fallback to the first successful panel response if no judge connection.
		logging.Logger.Warn("fusion judge has no eligible connection; returning first panel response", "judge_model", judgeModel)
		h.writeFusionResponse(c, successes[0].content, model, stream)
		return
	}
	judgeProvider, judgeModelName := executor.SplitModel(judgeModel)
	now := time.Now()
	judgeConn, err := h.prepareConnection(c.Request.Context(), judgeConnID, judgeProvider, judgeModelName, now)
	if err != nil {
		logging.Logger.Warn("fusion judge connection rejected; returning first panel response", "error", err.Error())
		h.writeFusionResponse(c, successes[0].content, model, stream)
		return
	}
	judgeExec, judgeFormat, err := h.resolveExecutor(judgeProvider, judgeModelName)
	if err != nil {
		logging.Logger.Warn("fusion judge executor unavailable; returning first panel response", "error", err.Error())
		h.writeFusionResponse(c, successes[0].content, model, stream)
		return
	}

	// Same model-field replacement for the judge so passthrough providers get the
	// actual judge model ID instead of the combo name.
	judgeReqBody := setRequestModel(judgeBody, judgeModelName)
	translatedJudge := registry.Request(string(executor.FormatOpenAI), string(judgeFormat), judgeModelName, judgeReqBody, stream)
	translatedJudge = sanitizeStreamOptions(translatedJudge, stream, executor.FormatOpenAI, judgeFormat, c.Request.URL.Path)

	execCtx := ctx
	if stream {
		execCtx = c.Request.Context()
	}
	_, resp, streamResult, err := h.executeProviderCall(execCtx, judgeExec, judgeConn, judgeProvider, judgeModelName, translatedJudge, stream, nil)
	if err != nil {
		logging.Logger.Warn("fusion judge execution failed; returning first panel response", "error", err.Error())
		h.writeFusionResponse(c, successes[0].content, model, stream)
		return
	}
	if !stream && (resp.StatusCode >= 500 || isUpstreamErrorBody(resp.Body)) {
		logging.Logger.Warn("fusion judge returned upstream error; returning first panel response", "status", resp.StatusCode)
		h.writeFusionResponse(c, successes[0].content, model, stream)
		return
	}

	if stream {
		streamCtx, cancelStream := context.WithCancel(c.Request.Context())
		defer cancelStream()
		if err := h.handleStreamResponse(streamCtx, c, streamResult, judgeConn, judgeProvider, judgeModelName, start, translatedJudge, judgeReqBody, comboResult.Combo.Name, true); err != nil {
			logging.Logger.Warn("fusion judge stream failed; falling back to first panel response", "error", err.Error(), "judge_model", judgeModel, "combo", comboResult.Combo.Name)
			h.writeFusionResponse(c, successes[0].content, model, stream)
			return
		}
		return
	}

	// Non-streaming: translate judge response back to client format.
	translatedResp := registry.ResponseNonStream(ctx, string(judgeFormat), string(executor.FormatOpenAI), judgeModelName, judgeReqBody, translatedJudge, resp.Body, nil)
	for k, v := range resp.Headers {
		c.Writer.Header()[k] = v
	}
	c.Data(resp.StatusCode, c.Writer.Header().Get("Content-Type"), translatedResp)
}

// fusionPanel holds the result of one panel execution for fusion.
type fusionPanel struct {
	connID  string
	modelID string
	content string
	err     error
}

// stripFusionTools removes tool-related fields from a request body for fusion panels
// and flattens any tool/function turns into plain prose so panel models keep the
// context without being able to emit tool_calls.
func stripFusionTools(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	delete(m, "tools")
	delete(m, "tool_choice")
	if msgs, ok := m["messages"].([]any); ok {
		m["messages"] = flattenToolHistory(msgs)
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

const (
	fusionToolCallPrefix   = "[Called tools: "
	fusionToolResultPrefix = "[Tool result: "
)

// flattenToolHistory converts tool/function turns in a message list into plain
// assistant prose so panel models keep the context but can't emit tool_calls.
// It mirrors 9router's flattenToolHistory behavior for OpenAI-compatible
// message shapes.
func flattenToolHistory(messages []any) []any {
	out := make([]any, 0, len(messages))
	for _, raw := range messages {
		if raw == nil {
			continue
		}
		msg, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}

		role, _ := msg["role"].(string)

		// Tool/function result -> assistant text.
		if role == "tool" || role == "function" {
			content := extractToolHistoryContent(msg["content"])
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": fusionToolResultPrefix + content + "]",
			})
			continue
		}

		// Assistant with tool_calls -> strip tool_calls and inline names.
		if role == "assistant" {
			if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				names := make([]string, 0, len(toolCalls))
				for _, tc := range toolCalls {
					name := extractToolCallName(tc)
					if name == "" {
						name = "tool"
					}
					names = append(names, name)
				}
				content := extractToolHistoryContent(msg["content"])
				if content != "" {
					content += "\n"
				}
				content += fusionToolCallPrefix + strings.Join(names, ", ") + "]"
				clean := map[string]any{
					"role":    "assistant",
					"content": content,
				}
				for k, v := range msg {
					if k != "role" && k != "content" && k != "tool_calls" {
						clean[k] = v
					}
				}
				out = append(out, clean)
				continue
			}

			// Array content with tool_use/tool_result blocks (Anthropic-style).
			if arr, ok := msg["content"].([]any); ok {
				textParts := []string{}
				toolNames := []string{}
				toolResults := []string{}
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}
					typ, _ := block["type"].(string)
					switch typ {
					case "text":
						if t, ok := block["text"].(string); ok && t != "" {
							textParts = append(textParts, t)
						}
					case "tool_use":
						name := "tool"
						if n, ok := block["name"].(string); ok && n != "" {
							name = n
						}
						toolNames = append(toolNames, name)
					case "tool_result":
						toolResults = append(toolResults, extractToolHistoryContent(block["content"]))
					}
				}
				if len(toolNames) > 0 || len(toolResults) > 0 {
					newContent := strings.Join(textParts, "\n")
					if len(toolNames) > 0 {
						if newContent != "" {
							newContent += "\n"
						}
						newContent += fusionToolCallPrefix + strings.Join(toolNames, ", ") + "]"
					}
					if len(toolResults) > 0 {
						if newContent != "" {
							newContent += "\n"
						}
						newContent += fusionToolResultPrefix + strings.Join(toolResults, "\n") + "]"
					}
					clean := map[string]any{
						"role":    "assistant",
						"content": newContent,
					}
					for k, v := range msg {
						if k != "role" && k != "content" {
							clean[k] = v
						}
					}
					out = append(out, clean)
					continue
				}
			}
		}

		out = append(out, msg)
	}
	return out
}

func extractToolHistoryContent(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	var parts []string
	collect := func(item map[string]any) {
		if typ, _ := item["type"].(string); typ == "text" {
			if t, ok := item["text"].(string); ok {
				parts = append(parts, t)
			}
		}
	}
	if arr, ok := v.([]any); ok {
		for _, item := range arr {
			if itemMap, ok := item.(map[string]any); ok {
				collect(itemMap)
			}
		}
		return strings.Join(parts, "\n")
	}
	if arr, ok := v.([]map[string]any); ok {
		for _, item := range arr {
			collect(item)
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprint(v)
}

func extractToolCallName(tc any) string {
	m, ok := tc.(map[string]any)
	if !ok {
		return ""
	}
	if fn, ok := m["function"].(map[string]any); ok {
		if n, ok := fn["name"].(string); ok {
			return n
		}
	}
	if n, ok := m["name"].(string); ok {
		return n
	}
	return ""
}

// setRequestModel replaces the model field in a request body.
func setRequestModel(body []byte, model string) []byte {
	return executor.JSONSet(body, "model", model)
}

// extractAssistantContent extracts the assistant message content from a non-streaming
// chat completion response body. It supports the standard OpenAI-compatible shape.
func extractAssistantContent(respBody []byte) string {
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ""
	}
	if len(out.Choices) == 0 {
		return ""
	}
	return out.Choices[0].Message.Content
}

// buildFusionJudgeBody builds a new request body for the judge model.
func buildFusionJudgeBody(originalReq []byte, panels []fusionPanel, anonymize bool) []byte {
	userQuestion := extractUserQuestion(originalReq)
	prompt := "You are a synthesis assistant. Multiple expert panel models answered the user's question. Review the answers below and produce a single, concise, accurate answer that best addresses the user's original question.\n\n"
	prompt += "User question: " + userQuestion + "\n\n"
	for i, p := range panels {
		label := fmt.Sprintf("Source %d", i+1)
		if !anonymize {
			label = fmt.Sprintf("Source %s", p.modelID)
		}
		prompt += fmt.Sprintf("%s (%s):\n%s\n\n", label, p.modelID, p.content)
	}
	prompt += "Synthesize the best answer."

	return executor.JSONSet(originalReq, "messages", []map[string]any{
		{"role": "system", "content": prompt},
		{"role": "user", "content": userQuestion},
	})
}

// extractUserQuestion returns the content of the last user message from a request body.
func extractUserQuestion(reqBody []byte) string {
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(reqBody, &payload); err != nil {
		return ""
	}
	for i := len(payload.Messages) - 1; i >= 0; i-- {
		if payload.Messages[i].Role == "user" && payload.Messages[i].Content != "" {
			return payload.Messages[i].Content
		}
	}
	return ""
}

// writeFusionResponse returns a single panel answer to the client as a normal
// chat completion response.
func (h *Handler) writeFusionResponse(c *gin.Context, content, model string, stream bool) {
	if stream {
		// Produce a minimal SSE stream containing the content.
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.WriteHeader(http.StatusOK)
		id := "fusion-" + strconv.Itoa(int(time.Now().UnixNano()))
		chunk, _ := json.Marshal(gin.H{
			"id":      id,
			"object":  "chat.completion.chunk",
			"model":   model,
			"choices": []gin.H{{"index": 0, "delta": gin.H{"content": content}}},
		})
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(chunk)
		c.Writer.Write([]byte("\n\ndata: [DONE]\n\n"))
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":     "fusion-" + strconv.Itoa(int(time.Now().UnixNano())),
		"object": "chat.completion",
		"model":  model,
		"choices": []gin.H{{
			"index": 0,
			"message": gin.H{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": "stop",
		}},
	})
}

// isUpstreamErrorBody reports whether a non-streaming response body is clearly
// an error envelope (e.g. OpenAI's {"error":{...}}). Used by the combo path to
// promote a 200-status error body to a retryable upstream error.
func isUpstreamErrorBody(body []byte) bool {
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	return envelope.Error != nil && (envelope.Error.Message != "" || envelope.Error.Type != "")
}

// handleClientStreamResponse dispatches to the correct stream handler based on
// the client-facing API format. It is used by both the direct and combo paths.
func (h *Handler) handleClientStreamResponse(ctx context.Context, c *gin.Context, result *executor.StreamResult, conn *Connection, provider, model string, start time.Time, translatedReq, originalReq []byte, comboID string, silent bool, clientFormat executor.ProviderFormat) error {
	switch clientFormat {
	case executor.FormatClaude:
		return h.handleClaudeStreamResponse(ctx, c, result, conn, provider, model, start, translatedReq, originalReq, comboID, silent)
	default:
		return h.handleStreamResponse(ctx, c, result, conn, provider, model, start, translatedReq, originalReq, comboID, silent)
	}
}

// handleStreamResponse handles streaming chat completions.
func (h *Handler) handleStreamResponse(ctx context.Context, c *gin.Context, result *executor.StreamResult, conn *Connection, provider, model string, start time.Time, translatedReq, originalReq []byte, comboID string, silent bool) error {
	_, providerFormat, _ := h.registry.Get(provider)
	errFormatter := func(err error) []byte {
		var upErr *executor.UpstreamError
		if errors.As(err, &upErr) {
			return upErr.Body
		}
		logging.Logger.Error("upstream streaming error", "provider", provider, "model", model, "error", err)
		b, _ := json.Marshal(gin.H{"error": gin.H{"message": "upstream streaming error"}})
		return b
	}
	return h.streamResponse(ctx, c, result, conn, provider, model, executor.FormatOpenAI, providerFormat, originalReq, translatedReq, errFormatter, start, comboID, silent)
}
