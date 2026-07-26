package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// CodexAlphaSearch handles the standalone Codex alpha search endpoints used by
// current Codex CLI clients. The payload is already in Codex search format and
// is forwarded directly to the upstream Codex endpoint without protocol
// translation.
func (h *Handler) CodexAlphaSearch(c *gin.Context) {
	start := time.Now()

	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		model = "o4-mini"
	}
	if !h.isModelAllowed(c.Request.Context(), model) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model not allowed for this API key", "type": "invalid_request_error"}})
		return
	}
	if h.checkTokenBudget(c, body) != nil {
		return
	}
	if h.checkAPIKeyBudget(c) != nil {
		return
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "cx"
		modelName = model
	}
	if provider != "cx" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "codex alpha search requires a cx (Codex) provider model", "type": "invalid_request_error"}})
		return
	}

	sessionID := h.sessionIDForAffinity(c, provider, modelName, body)
	conn, err := h.getConnection(c.Request.Context(), provider, modelName, sessionID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available codex connection", "type": "server_error"}})
		return
	}

	h.proactiveRefreshToken(c.Request.Context(), conn, provider)

	psdMap := map[string]string{}
	if conn.ProviderSpecificData != "" {
		_ = json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
	}

	upstreamURL := "https://chatgpt.com/backend-api/codex/alpha/search"
	if conn.BaseURL != "" {
		upstreamURL = strings.TrimSuffix(conn.BaseURL, "/") + "/backend-api/codex/alpha/search"
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("User-Agent", c.GetHeader("User-Agent"))
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", executor.DefaultCodexUserAgent)
	}
	headers.Set("Authorization", "Bearer "+conn.AccessToken)

	accountID := executor.CodexAccountIDFromConnection(conn.ProviderSpecificData, conn.AccessToken)
	if accountID == "" {
		accountID = psdMap["accountId"]
	}
	if accountID != "" {
		headers.Set("Chatgpt-Account-Id", accountID)
	}

	for _, name := range []string{"Version", "Session_id", "X-Client-Request-Id"} {
		if v := strings.TrimSpace(c.GetHeader(name)); v != "" {
			headers.Set(name, v)
		}
	}
	if sessionID := strings.TrimSpace(executor.JSONGet(body, "id")); sessionID != "" {
		headers.Set("X-Session-ID", sessionID)
	}
	if headers.Get("Session_id") == "" && strings.Contains(headers.Get("User-Agent"), "Mac OS") {
		headers.Set("Session_id", uuid.NewString())
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}
	req.Header = headers

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Logger.Warn("codex alpha search upstream failed", "error", err.Error(), "conn", shortID(conn.ID, 8))
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}
	defer resp.Body.Close()

	upstreamBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "failed to read codex search response", "type": "server_error"}})
		return
	}

	h.logRequest(c, &usage.LogEntry{
		ApiKeyID:       c.GetString("api_key_id"),
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        modelName,
		ApiType:        apiTypeFromPath(c.Request.URL.Path),
		Modality:       "codex_search",
		Stream:         false,
		LatencyMs:      time.Since(start).Milliseconds(),
		StatusCode:     resp.StatusCode,
	})

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Status(resp.StatusCode)
	c.Writer.Write(upstreamBody)
}
