package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// compactExecutor is implemented by providers that support the non-streaming
// /responses/compact endpoint.
type compactExecutor interface {
	ResponsesCompact(ctx context.Context, req *executor.Request) (*executor.Response, error)
}

// ResponsesCompact handles POST /v1/responses/compact (and the Codex alias).
// It rejects streaming requests and forwards a normalized, non-streaming request
// to the provider's /responses/compact endpoint.
func (h *Handler) ResponsesCompact(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	body = h.compressRequestBody(body)
	c.Set("service_tier", extractServiceTier(body))

	body, model, _ := h.parseThinkingSuffixFromBody(c, body)
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}
	if !h.isModelAllowed(c.Request.Context(), model) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model not allowed for this API key", "type": "invalid_request_error"}})
		return
	}

	if executor.IsStreamRequest(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "streaming not supported for /responses/compact", "type": "invalid_request_error"}})
		return
	}
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "cx"
		modelName = model
	}

	exec, _, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	compactExec, ok := exec.(compactExecutor)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "compact not supported for provider: " + provider, "type": "server_error"}})
		return
	}

	body = executor.JSONSet(body, "model", modelName)
	// Compact is a separate Responses surface, but it still accepts image input;
	// apply the bridge before invoking the provider-specific compact executor.
	body = h.applyVisionBridge(c, body, provider+"/"+modelName, executor.FormatOpenAIResponses)
	clientFormat := executor.FormatOpenAIResponses
	sessionID := h.sessionIDForAffinity(c, provider, modelName, body)

	maxAttempts := h.failoverAttempts()
	var lastConn *Connection
	var lastErr error
	var lastErrCategory string
	var lastFailedConnID string
attemptLoop:
	for attempt := range maxAttempts {
		if c.Request.Context().Err() != nil {
			writeContextDone(c)
			return
		}
		var conn *Connection
		var err error
		if lastFailedConnID != "" {
			conn, err = h.getConnection(c.Request.Context(), provider, modelName, sessionID, lastFailedConnID)
		} else {
			conn, err = h.getConnection(c.Request.Context(), provider, modelName, sessionID)
		}
		if err != nil {
			if attempt == 0 {
				if cat := h.classifyProviderUnavailableForModel(provider, modelName); cat != connstate.ErrorUnknown {
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
		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		psdMap := map[string]string{}
		if conn.ProviderSpecificData != "" {
			if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
				logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
			}
		}

		req := &executor.Request{
			Model:                modelName,
			Body:                 body,
			Stream:               false,
			APIKey:               conn.APIKey,
			AccessToken:          conn.AccessToken,
			BaseURL:              conn.BaseURL,
			Provider:             provider,
			ProviderSpecificData: psdMap,
		}
		proxyCtx := h.proxyContext(c.Request.Context(), conn)

		resp, err := compactExec.ResponsesCompact(proxyCtx, req)
		latency := time.Since(start).Milliseconds()
		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if provider == "cx" {
			h.codexPersistIfCodex(conn, resp, nil)
		}
		if err != nil {
			if h.isClientCanceled(c, err) {
				return
			}
			det := connstate.DetectError(proxyCtx, 0, "", err, provider, modelName, nil)
			if !isFailoverEligible(det.Category) {
				if h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, false) {
					return
				}
			}
			lastFailedConnID = conn.ID
			h.clearAffinitySession(provider, sessionID, modelName)
			retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, err, attempt, latency, false)
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

		// The upstream /responses/compact endpoint returns an OpenAI Responses
		// shape regardless of the provider's chat-completion format. Run it through
		// the registry so provider-specific response transforms can still apply.
		translatedResp := registry.ResponseNonStream(c.Request.Context(), string(executor.FormatOpenAIResponses), string(clientFormat), modelName, body, body, resp.Body, nil)
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
			Modality:            "responses",
			Stream:              false,
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
		h.writeJSONResponse(c, resp.StatusCode, translatedResp, responseCost{
			modelID:         modelName,
			exactCost:       resp.CostUsd,
			counts:          tokenCounts,
			tokensEstimated: tokensEstimated,
			flatRate:        h.isFlatRate(provider),
		})
		return
	}

	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)
	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}
