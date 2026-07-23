package v1

import (
	"context"
	"encoding/json"
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

// Responses handles POST /v1/responses (OpenAI Responses format)
func (h *Handler) Responses(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

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

	// Exact cache check (non-stream, no tools, no cache_control)
	cacheKey := h.exactCacheKey(body, model, stream)
	if cacheKey != "" {
		if entry, ok := h.exactCache.Get(cacheKey); ok {
			h.serveCacheHit(c, body, entry)
			return
		}
	}

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		strategy := h.combo.EffectiveStrategy(comboResult.Combo.Name, comboResult.Combo.Strategy)
		comboResult.Steps = h.combo.ReorderStepsByCapabilities(comboResult.Steps, combo.DetectRequiredCapabilities(body))
		if strategy == "fusion" {
			h.handleFusionRequest(c, comboResult, strategy, body, model, start, stream)
			return
		}
		comboResult.Steps = h.combo.RotateSteps(comboResult.Combo.ID, strategy, comboResult.Combo.StickyLimit, comboResult.Steps)
		h.handleComboRequest(c, comboResult, strategy, body, model, start, stream)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "cx"
		modelName = model
	}

	sessionID := h.sessionIDForAffinity(c, provider, modelName, body)

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	body = executor.JSONSet(body, "model", modelName)
	clientFormat := executor.FormatOpenAIResponses
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
	translatedBody = h.applyThinkingOverrideFromContext(c.Request.Context(), translatedBody, string(providerFormat))
	translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)

	// NOTE: configurable via failover_max_attempts setting.
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
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
				return
			}
			break
		}
		lastConn = conn
		h.proactiveRefreshToken(c.Request.Context(), conn, provider)
		psdMap := map[string]string{}
		if conn.ProviderSpecificData != "" {
			if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
				logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
			}
		}

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
		// Adaptive readiness: extend timeout for large/reasoning requests.
		if stream {
			adaptiveMs := computeAdaptiveReadiness(body, modelName, 80000)
			req.StreamConfig = &executor.StreamConfig{
				StreamReadinessTimeoutMs: adaptiveMs,
				AdaptiveReadiness:        true,
			}
		}
		proxyCtx := h.proxyContext(c.Request.Context(), conn)
		var resp *executor.Response
		var streamResult *executor.StreamResult
		if stream {
			streamResult, err = exec.ExecuteStream(proxyCtx, req)
		} else {
			resp, err = exec.Execute(proxyCtx, req)
		}
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
			// NOTE: auth and balance failures rotate to sibling connections before surfacing.
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
				break attemptLoop
			}
			if !failoverBackoff(c.Request.Context(), attempt, maxAttempts) {
				return
			}
			continue
		}
		h.resetBanCount(conn.ID)
		h.persistSuccess(conn.ID)
		h.combo.RecordSuccess(conn.ID)

		if stream {
			_, providerFmt, _ := h.registry.Get(provider)
			errFormatter := func(err error) []byte {
				logging.Logger.Error("upstream streaming error", "provider", provider, "model", modelName, "error", err)
				b, _ := json.Marshal(gin.H{"error": gin.H{"message": "upstream streaming error", "type": "server_error"}})
				return b
			}

			// Holdback buffer: wait 750ms/64KB before committing to this connection.
			// If the stream errors during holdback, retry the next connection
			// transparently. Matches OmniRoute holdback behavior.
			holdbackMs := 750
			holdbackBytes := 64 * 1024
			streamCtx, cancelStream := context.WithCancel(c.Request.Context())
			defer cancelStream()
			holdbackChunks, holdbackErrCh := executor.WrapWithHoldback(streamCtx, streamResult.Chunks, holdbackMs, holdbackBytes)
			streamResult.Chunks = holdbackChunks

			select {
			case holdbackErr := <-holdbackErrCh:
				if holdbackErr != nil {
					cancelStream()
					logging.Logger.Warn("responses stream failed during holdback", "provider", provider, "model", modelName, "conn", shortID(conn.ID, 8), "error", holdbackErr.Error())
					det := connstate.DetectError(proxyCtx, 0, "", holdbackErr, provider, modelName, nil)
					if !isFailoverEligible(det.Category) {
						if h.writeUpstreamClientError(proxyCtx, c, holdbackErr, conn, provider, modelName, start, stream) {
							return
						}
					}
					if det.ModelID != "" && (det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
						scope := connstate.ModelScope(provider, det.ModelID)
						h.exhaustion.MarkExhausted(quota.ExhaustKey(conn.ID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
					} else if det.Category == connstate.ErrorRateLimit {
						h.exhaustion.MarkExhausted(conn.ID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
					} else if det.Category == connstate.ErrorQuota {
						ttl := 24 * time.Hour
						if det.CooldownUntil != nil {
							ttl = time.Until(*det.CooldownUntil)
						}
						h.exhaustion.MarkExhausted(conn.ID, ttl)
					}
					h.combo.RecordFailure(conn.ID, det)
					h.persistCooldownScoped(conn.ID, det)
					if det.Status != connstate.StatusReady {
						h.elig.ScheduleUpdateProvider(provider)
					}
					lastErr = holdbackErr
					lastErrCategory = string(det.Category)
					if !failoverBackoff(c.Request.Context(), attempt, maxAttempts) {
						return
					}
					continue
				}
			case <-streamCtx.Done():
				return
			}

			// Holdback committed — relay to client. Silent mode lets us retry on
			// mid-stream failure without writing a terminal [DONE] prematurely.
			if streamErr := h.streamResponse(streamCtx, c, streamResult, conn, provider, modelName, executor.FormatOpenAIResponses, providerFmt, body, translatedBody, errFormatter, start, "", true); streamErr != nil {
				if h.isClientCanceled(c, streamErr) {
					return
				}
				cancelStream()
				det := connstate.DetectError(proxyCtx, 0, "", streamErr, provider, modelName, nil)
				if !isFailoverEligible(det.Category) {
					if h.writeUpstreamClientError(proxyCtx, c, streamErr, conn, provider, modelName, start, stream) {
						return
					}
				}
				if det.ModelID != "" && (det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
					scope := connstate.ModelScope(provider, det.ModelID)
					h.exhaustion.MarkExhausted(quota.ExhaustKey(conn.ID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
				} else if det.Category == connstate.ErrorRateLimit {
					h.exhaustion.MarkExhausted(conn.ID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
				} else if det.Category == connstate.ErrorQuota {
					ttl := 24 * time.Hour
					if det.CooldownUntil != nil {
						ttl = time.Until(*det.CooldownUntil)
					}
					h.exhaustion.MarkExhausted(conn.ID, ttl)
				}
				h.combo.RecordFailure(conn.ID, det)
				h.persistCooldownScoped(conn.ID, det)
				if det.Status != connstate.StatusReady {
					h.elig.ScheduleUpdateProvider(provider)
				}
				lastErr = streamErr
				lastErrCategory = "stream-" + string(det.Category)
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
			h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, translatedResp, true)
			if resp.StatusCode < 300 {
				h.storeExactCache(cacheKey, translatedResp, resp.StatusCode)
			}
			h.writeJSONResponse(c, resp.StatusCode, translatedResp)
		}
		return
	}
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)

	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)

	if stream {
		// Streaming clients expect an SSE error event and [DONE]. If the stream
		// already started, the status is already committed; keep writing SSE.
		errBytes, _ := json.Marshal(gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(errBytes)
		c.Writer.Write([]byte("\n\ndata: [DONE]\n\n"))
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}
