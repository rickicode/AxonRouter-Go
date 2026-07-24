package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// STT handles POST /v1/audio/transcriptions
func (h *Handler) STT(c *gin.Context) {
	start := time.Now()

	// STT uses multipart form data
	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Content-Type required", "type": "invalid_request_error"}})
		return
	}

	// Parse multipart form
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "failed to parse form: " + err.Error(), "type": "invalid_request_error"}})
		return
	}

	model := c.PostForm("model")
	if model == "" {
		model = "whisper-1"
	}
	if !h.isModelAllowed(c.Request.Context(), model) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model not allowed for this API key", "type": "invalid_request_error"}})
		return
	}

	provider, _ := executor.SplitModel(model)
	if provider == "" {
		provider = "openai"
	}

	// Get the uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "file is required", "type": "invalid_request_error"}})
		return
	}
	defer file.Close()

	// Read file data
	audioData, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "failed to read file", "type": "invalid_request_error"}})
		return
	}
	if h.checkTokenBudget(c, nil) != nil {
		return
	}

	// Build multipart body
	filename := header.Filename
	if filename == "" {
		filename = "audio.wav"
	}

	language := c.PostForm("language")

	multipartBody, multipartContentType, err := executor.BuildMultipartBody(audioData, filename, model, language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to build request", "type": "server_error"}})
		return
	}

	// Get STT executor (test hook overrides the real one).
	var sttExec executor.Executor
	if h.sttExecutorFactory != nil {
		sttExec = h.sttExecutorFactory()
	} else {
		sttExec = executor.NewSTTExecutor(executor.NewBaseExecutor())
	}

	conn, err := h.getConnection(c.Request.Context(), provider, model, "")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
		return
	}

	// Proactive token refresh
	h.proactiveRefreshToken(c.Request.Context(), conn, provider)
	// Parse provider-specific data
	var psdMap map[string]string
	if conn.ProviderSpecificData != "" {
		if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
			logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
		}
	}

	req := &executor.Request{
		Model:                model,
		Body:                 multipartBody,
		APIKey:               conn.APIKey,
		AccessToken:          conn.AccessToken,
		BaseURL:              conn.BaseURL,
		Provider:             provider,
		ProviderSpecificData: psdMap,
		Headers: map[string]string{
			"Content-Type": multipartContentType,
		},
	}

	proxyCtx := h.proxyContext(c.Request.Context(), conn)

	// Execute with reactive 401/403 retry (3 attempts, linear backoff)
	var resp *executor.Response
	var streamResult *executor.StreamResult
	resp, streamResult, err = h.executeWithRetry(proxyCtx, sttExec, req, conn, provider, model)
	_ = streamResult
	if err != nil {
		if !h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, model, start, false) {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "internal server error", "type": "server_error"}})
		}
		return
	}

	h.logRequest(c, &usage.LogEntry{
		ApiKeyID:       c.GetString("api_key_id"),
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        model,
		ProxyPoolID:    executor.ProxyPoolIDFromContext(proxyCtx),
		ApiType:        apiTypeFromPath(c.Request.URL.Path),
		Modality:       "audio",
		Stream:         false,
		LatencyMs:      time.Since(start).Milliseconds(),
		StatusCode:     resp.StatusCode})

	h.accumulateAPIKeyUsage(c.GetString("api_key_id"), nil, resp.Body, false)
	if h.isFlatRate(provider) {
		c.Header(costHeader, "0")
	}
	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}

// parseMultipartBoundary extracts boundary from Content-Type header.
func parseMultipartBoundary(contentType string) (string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", fmt.Errorf("parse media type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return "", fmt.Errorf("no boundary in content type")
	}
	return boundary, nil
}

// forwardMultipartBody reads the original multipart body and returns it with content type.
func forwardMultipartBody(c *gin.Context) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Copy form fields
	for key, values := range c.Request.PostForm {
		for _, v := range values {
			writer.WriteField(key, v)
		}
	}

	// Copy files
	for key, files := range c.Request.MultipartForm.File {
		for _, fh := range files {
			file, err := fh.Open()
			if err != nil {
				continue
			}
			part, err := writer.CreateFormFile(key, fh.Filename)
			if err != nil {
				file.Close()
				continue
			}
			io.Copy(part, file)
			file.Close()
		}
	}

	writer.Close()
	return buf.Bytes(), writer.FormDataContentType(), nil
}
