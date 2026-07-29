package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/tidwall/gjson"
)

// GeminiModels handles GET /v1beta/models and returns the Gemini-shaped catalog
// for the gemini provider.
func (h *Handler) GeminiModels(c *gin.Context) {
	allowed, _ := c.Get("allowed_models")
	allowedSet, _ := allowed.(map[string]struct{})

	defaultMethods := []string{"generateContent", "countTokens", "streamGenerateContent"}
	var models []map[string]any
	for _, m := range h.ListActiveModels() {
		id, _ := m["id"].(string)
		if id == "" || !strings.HasPrefix(id, "gemini/") {
			continue
		}
		if !modelIDAllowed(id, allowedSet) {
			continue
		}
		bare := strings.TrimPrefix(id, "gemini/")
		name := "models/" + bare
		displayName := bare
		if n := modelDisplayName("gemini", bare); n != "" {
			displayName = n
		}
		models = append(models, map[string]any{
			"name":                       name,
			"displayName":                displayName,
			"description":                displayName,
			"supportedGenerationMethods": defaultMethods,
			"version":                    bare,
		})
	}

	c.JSON(http.StatusOK, gin.H{"object": "list", "models": models})
}

// GeminiGetHandler handles GET /v1beta/models/*action for retrieving a single model.
func (h *Handler) GeminiGetHandler(c *gin.Context) {
	action := strings.TrimPrefix(c.Param("action"), "/")
	if action == "" {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", "model name is required"))
		return
	}

	allowed, _ := c.Get("allowed_models")
	allowedSet, _ := allowed.(map[string]struct{})

	bare := strings.TrimPrefix(action, "models/")
	modelID := "gemini/" + bare
	if !modelIDAllowed(modelID, allowedSet) {
		c.JSON(http.StatusForbidden, geminiError("invalid_request_error", "model not allowed for this API key"))
		return
	}

	defaultMethods := []string{"generateContent", "countTokens", "streamGenerateContent"}
	displayName := bare
	if n := modelDisplayName("gemini", bare); n != "" {
		displayName = n
	}

	c.JSON(http.StatusOK, map[string]any{
		"name":                       "models/" + bare,
		"displayName":                displayName,
		"description":                displayName,
		"supportedGenerationMethods": defaultMethods,
		"version":                    bare,
	})
}

// GeminiHandler handles POST /v1beta/models/*action for generateContent,
// streamGenerateContent and countTokens.
func (h *Handler) GeminiHandler(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, geminiError("invalid_request_error", err.Error()))
		} else {
			c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", errReadBody.Error()))
		}
		return
	}

	action := strings.TrimPrefix(c.Param("action"), "/")
	if action == "" {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", "action is required"))
		return
	}

	modelName, op, ok := splitGeminiAction(action)
	if !ok {
		modelName = strings.TrimPrefix(action, "models/")
		op = "generateContent"
	}
	if modelName == "" {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", "model name is required"))
		return
	}

	if !h.isModelAllowed(c.Request.Context(), "gemini/"+modelName) {
		c.JSON(http.StatusForbidden, geminiError("invalid_request_error", "model not allowed for this API key"))
		return
	}

	h.runGeminiAction(c, modelName, op, body, start)
}

// Interactions handles POST /v1beta/interactions and converts the request to/from
// the native Gemini surface for model targets. Agent targets are rejected because
// AxonRouter does not currently ship an agent execution runtime.
func (h *Handler) Interactions(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, geminiError("invalid_request_error", err.Error()))
		} else {
			c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", errReadBody.Error()))
		}
		return
	}

	target := gjson.ParseBytes(body).Get("target")
	if target.Get("agent").Exists() && target.Get("agent").String() != "" {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", "agent interactions are not supported yet"))
		return
	}

	modelName := target.Get("model").String()
	modelName = strings.TrimPrefix(modelName, "models/")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", "target.model is required"))
		return
	}

	if !h.isModelAllowed(c.Request.Context(), "gemini/"+modelName) {
		c.JSON(http.StatusForbidden, geminiError("invalid_request_error", "model not allowed for this API key"))
		return
	}

	if gjson.GetBytes(body, "stream").Bool() {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", "streaming interactions are not supported yet"))
		return
	}

	geminiReq := convertInteractionsToGemini(modelName, body)
	h.runGeminiInteraction(c, modelName, geminiReq, start)
}

// runGeminiAction routes a single Gemini action to the appropriate executor method.
func (h *Handler) runGeminiAction(c *gin.Context, modelName, action string, body []byte, start time.Time) {
	provider := "gemini"
	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", err.Error()))
		return
	}

	stream := action == "streamGenerateContent"
	maxAttempts := h.failoverAttempts()
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
			conn, err = h.getConnection(c.Request.Context(), provider, modelName, "", lastFailedConnID)
		} else {
			conn, err = h.getConnection(c.Request.Context(), provider, modelName, "")
		}
		if err != nil {
			if attempt == 0 {
				c.JSON(http.StatusServiceUnavailable, geminiError("server_error", "no available connection"))
				return
			}
			break
		}
		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		var resp *executor.Response
		var streamResult *executor.StreamResult
		var execErr error

		proxyCtx := h.proxyContext(c.Request.Context(), conn)
		latency := time.Since(start).Milliseconds()

		switch action {
		case "countTokens":
			counter, ok := exec.(executor.TokenCounter)
			if !ok {
				c.JSON(http.StatusInternalServerError, geminiError("server_error", "executor does not support countTokens"))
				return
			}
			resp, execErr = h.runGeminiCountTokens(proxyCtx, counter, conn, provider, modelName, body)
		case "generateContent", "streamGenerateContent":
			_, resp, streamResult, execErr = h.executeProviderCall(proxyCtx, exec, conn, provider, modelName, body, stream, nil)
		default:
			c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", fmt.Sprintf("unsupported action: %s", action)))
			return
		}

		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if streamResult != nil {
			connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
		}
		if execErr != nil {
			if h.isClientCanceled(c, execErr) {
				return
			}
			var upErr *executor.UpstreamError
			if errors.As(execErr, &upErr) && upErr.StatusCode >= 400 && upErr.StatusCode < 500 {
				lastErr = execErr
				lastErrCategory = "client_error"
				break attemptLoop
			}
			lastFailedConnID = conn.ID
			retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, execErr, attempt, latency, stream)
			lastErr = execErr
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
			errFormatter := func(err error) []byte {
				var ue *executor.UpstreamError
				if errors.As(err, &ue) && len(ue.Body) > 0 {
					return ue.Body
				}
				b, _ := json.Marshal(gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
				return b
			}
			h.streamResponse(proxyCtx, c, streamResult, conn, provider, modelName, providerFormat, providerFormat, body, body, errFormatter, start, "", false)
			return
		}

		tokenCounts := ExtractTokensFromBody(resp.Body)
		tokensEstimated := false
		if tokenCounts.InputTokens+tokenCounts.OutputTokens == 0 && resp.StatusCode < 400 {
			estInput := usage.EstimateTokensFromRequest(body)
			estOutput := usage.EstimateTokensFromResponse(resp.Body)
			if estInput > 0 || estOutput > 0 {
				tokenCounts.InputTokens = estInput
				tokenCounts.OutputTokens = estOutput
				tokensEstimated = true
			}
		}

		latency2 := time.Since(start).Milliseconds()
		h.logRequest(c, &usage.LogEntry{
			ApiKeyID:            c.GetString("api_key_id"),
			ConnectionID:        conn.ID,
			ProviderTypeID:      provider,
			ModelID:             modelName,
			ProxyPoolID:         executor.ProxyPoolIDFromContext(proxyCtx),
			ApiType:             apiTypeFromPath(c.Request.URL.Path),
			Modality:            modalityFromPath(c.Request.URL.Path),
			Stream:              stream,
			InputTokens:         tokenCounts.InputTokens,
			OutputTokens:        tokenCounts.OutputTokens,
			ReasoningTokens:     tokenCounts.ReasoningTokens,
			CachedTokens:        tokenCounts.CachedTokens,
			CacheCreationTokens: tokenCounts.CacheCreationTokens,
			CostUsd:             usage.EstimateCost(modelName, "chat", 0, tokenCounts.InputTokens, tokenCounts.OutputTokens, tokenCounts.ReasoningTokens, tokenCounts.CachedTokens, tokenCounts.CacheCreationTokens),
			LatencyMs:           latency2,
			StatusCode:          resp.StatusCode,
			TokensEstimated:     tokensEstimated,
		})
		if h.usageAccumulator != nil {
			h.usageAccumulator(c.GetString("api_key_id"), body, resp.Body, true)
		}
		h.writeJSONResponse(c, resp.StatusCode, resp.Body, responseCost{
			modelID:         modelName,
			exactCost:       resp.CostUsd,
			counts:          tokenCounts,
			tokensEstimated: tokensEstimated,
			flatRate:        h.isFlatRate(provider),
		})
		return
	}

	if lastErrCategory == "client_error" {
		if upErr := extractUpstreamError(lastErr); upErr != nil {
			c.Header("Content-Type", "application/json")
			c.Status(upErr.StatusCode)
			c.Writer.Write(upErr.Body)
			return
		}
	}
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, geminiError(errType, msg))
}

// runGeminiInteraction executes a converted interactions request and translates the response back.
func (h *Handler) runGeminiInteraction(c *gin.Context, modelName string, geminiReq []byte, start time.Time) {
	provider := "gemini"
	exec, _, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, geminiError("invalid_request_error", err.Error()))
		return
	}

	maxAttempts := h.failoverAttempts()
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
			conn, err = h.getConnection(c.Request.Context(), provider, modelName, "", lastFailedConnID)
		} else {
			conn, err = h.getConnection(c.Request.Context(), provider, modelName, "")
		}
		if err != nil {
			if attempt == 0 {
				c.JSON(http.StatusServiceUnavailable, geminiError("server_error", "no available connection"))
				return
			}
			break
		}
		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		proxyCtx := h.proxyContext(c.Request.Context(), conn)
		_, resp, _, execErr := h.executeProviderCall(proxyCtx, exec, conn, provider, modelName, geminiReq, false, nil)
		latency := time.Since(start).Milliseconds()
		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if execErr != nil {
			if h.isClientCanceled(c, execErr) {
				return
			}
			var upErr *executor.UpstreamError
			if errors.As(execErr, &upErr) && upErr.StatusCode >= 400 && upErr.StatusCode < 500 {
				lastErr = execErr
				lastErrCategory = "client_error"
				break attemptLoop
			}
			lastFailedConnID = conn.ID
			retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, execErr, attempt, latency, false)
			lastErr = execErr
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

		interactionResp := convertGeminiResponseToInteractions(modelName, resp.Body)
		c.JSON(http.StatusOK, interactionResp)
		return
	}

	if lastErrCategory == "client_error" {
		if upErr := extractUpstreamError(lastErr); upErr != nil {
			c.Header("Content-Type", "application/json")
			c.Status(upErr.StatusCode)
			c.Writer.Write(upErr.Body)
			return
		}
	}
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, geminiError(errType, msg))
}

// geminiError returns a Gemini-compatible JSON error payload.
func geminiError(errType, message string) gin.H {
	return gin.H{"error": gin.H{"message": message, "type": errType}}
}

// splitGeminiAction splits a path like "gemini-2.0-flash:generateContent" into
// model name and operation. Returns false if there is no operation suffix.
func splitGeminiAction(action string) (modelName, op string, ok bool) {
	if idx := strings.LastIndex(action, ":"); idx > 0 && idx < len(action)-1 {
		return action[:idx], action[idx+1:], true
	}
	return action, "", false
}

// modelDisplayName returns the catalog display name for a model if available.
func modelDisplayName(providerKey, modelID string) string {
	names := models.GetModelDisplayNames(providerKey)
	if n, ok := names[modelID]; ok {
		return n
	}
	return ""
}

// runGeminiCountTokens builds an executor request and calls CountTokens.
func (h *Handler) runGeminiCountTokens(ctx context.Context, counter executor.TokenCounter, conn *Connection, provider, modelName string, body []byte) (*executor.Response, error) {
	var psdMap map[string]string
	if conn.ProviderSpecificData != "" {
		_ = json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
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
	return counter.CountTokens(ctx, req)
}
