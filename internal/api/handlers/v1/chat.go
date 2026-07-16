package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// ChatCompletions handles POST /v1/chat/completions
func (h *Handler) ChatCompletions(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	// Apply compression (fail-open); skip if the request uses prompt-cache markers.
	body = h.compressRequestBody(body)

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}
	stream := executor.IsStreamRequest(body)
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		h.handleComboRequest(c, comboResult, body, model, start, stream)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model must include provider prefix (e.g., openai/gpt-4o)", "type": "invalid_request_error"}})
		return
	}

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

	// Connection failover loop: try up to 3 connections before giving up.
	// On each failure, mark the connection exhausted/cooldown and update eligibility
	// so the next getConnection call picks a different connection.
	clientFormat := executor.FormatOpenAI
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
	translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)
	maxAttempts := 5
	var lastConn *Connection
	var lastErr error
	var lastErrCategory string
	for attempt := range maxAttempts {
		if c.Request.Context().Err() != nil {
			writeContextDone(c)
			return
		}
		conn, err := h.getConnection(c.Request.Context(), provider, modelName)
		if err != nil {
			if attempt == 0 {
				logging.Logger.Info("chat: get connection failed", "err", err.Error())
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
			if h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, stream) {
				return
			}
			retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, err, attempt, latency, stream)
			lastErr = err
			lastErrCategory = cat
			if !retry {
				break // non-retryable error, stop failover
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
			// The comboCtx timeout is only for orchestration, not for live streams.
			// Stream lifecycle is governed by StreamIdleTimeout/StreamReadinessTimeout.
			h.handleStreamResponse(c.Request.Context(), c, streamResult, conn, provider, modelName, start, translatedBody, body, "")
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
			h.tracker.Log(&usage.LogEntry{
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

// handleComboRequest handles a request that matched a combo.
//
// Semantics: iterate the combo's steps in strategy order. For each step, retry
// over all eligible connections of that step's model (mirroring the direct
// routing path's connection failover) before falling through to the next step.
// Non-retryable client errors (auth / model-not-found) are returned to the
// caller immediately instead of being fail-over'd to another model. If every
// step exhausts its connections, a detailed 503 is returned (last error + which
// connection/model was attempted) rather than a bare "all combo steps failed".
func (h *Handler) handleComboRequest(c *gin.Context, comboResult *combo.ComboResult, body []byte, model string, start time.Time, stream bool) {
	comboTimeout := 30 * time.Second
	if comboResult.Combo != nil && comboResult.Combo.TimeoutMs > 0 {
		comboTimeout = time.Duration(comboResult.Combo.TimeoutMs) * time.Millisecond
	}
	comboCtx, cancel := context.WithTimeout(c.Request.Context(), comboTimeout)
	defer cancel()

	clientFormat := executor.FormatOpenAI

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
			conn, err := h.prepareConnection(comboCtx, connID, provider, modelName)
			if err != nil {
				// Preflight rejected (cooldown/exhausted) — try next connection.
				continue
			}
			lastConn = conn

			var psdMap map[string]string
			if conn.ProviderSpecificData != "" {
				if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
					logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
				}
			}
			translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
			translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)
			req := &executor.Request{

		Model:         modelName,
		Body:          translatedBody,
		Stream:        stream,
		APIKey:        conn.APIKey,
		AccessToken:   conn.AccessToken,
		BaseURL:       conn.BaseURL,
		Provider:      provider,
		ProviderSpecificData: psdMap,
	}
	// Use client's request context for execution to avoid 30s combo timeout
	// cutting off long-lived streaming responses. comboCtx is only for loop control.
	execCtx := comboCtx
	if stream {
		execCtx = c.Request.Context()
	}
	proxyCtx := h.proxyContext(execCtx, conn)
	resp, streamResult, err := h.executeDirect(proxyCtx, exec, req)
			latency := time.Since(start).Milliseconds()
			if err != nil {
				if h.isClientCanceled(c, err) {
					return
				}
				det := connstate.DetectError(comboCtx, 0, "", err, provider, modelName, nil)

				// Non-retryable client errors must be surfaced, not fail-over'd.
				if det.Category == connstate.ErrorModelNotFound || det.Category == connstate.ErrorAuth {
					if h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, stream) {
						return
					}
					lastErr = err
					lastErrCategory = string(det.Category)
					break
				}

				// Retryable: apply the same failover marking as the direct path.
				if connstate.HasPerModelQuota(provider) && det.ModelID != "" &&
					(det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
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
					h.elig.ScheduleUpdate()
				}
				lastErr = err
				lastErrCategory = string(det.Category)
				continue
			}

			// Success — write the response and stop.
			h.resetBanCount(connID)
			h.persistSuccess(connID)
			h.combo.RecordSuccess(connID)
			if provider == "cx" {
				h.codexPersistIfCodex(conn, resp, streamResult)
			}

		if req.Stream {
			// Use the client's request context (no timeout) for streaming.
			// The comboCtx timeout is only for orchestration, not for live streams.
			// Stream lifecycle is governed by StreamIdleTimeout/StreamReadinessTimeout.
			h.handleStreamResponse(c.Request.Context(), c, streamResult, conn, provider, modelName, start, translatedBody, body, comboResult.Combo.Name)
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
			h.tracker.Log(&usage.LogEntry{
				ApiKeyID: c.GetString("api_key_id"),
				ConnectionID: connID,
				ProviderTypeID: provider,
				ModelID: modelName,
				ComboID: comboResult.Combo.Name,
				ProxyPoolID: executor.ProxyPoolIDFromContext(proxyCtx),
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
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}

// handleStreamResponse handles streaming chat completions.
func (h *Handler) handleStreamResponse(ctx context.Context, c *gin.Context, result *executor.StreamResult, conn *Connection, provider, model string, start time.Time, translatedReq, originalReq []byte, comboID string) {
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
	h.streamResponse(ctx, c, result, conn, provider, model, executor.FormatOpenAI, providerFormat, originalReq, translatedReq, errFormatter, start, comboID)
}
