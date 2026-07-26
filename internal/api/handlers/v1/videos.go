package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const defaultXAIVideosModel = "xai/grok-imagine-video"

// VideosCreate handles POST /v1/videos, an alias for the XAI native video
// creation surface. It delegates to the existing video-generation path after
// applying the XAI default model.
func (h *Handler) VideosCreate(c *gin.Context) {
	h.withDefaultVideoModel(c, "/v1/videos")
}

// VideosGenerations handles POST /v1/videos/generations.
func (h *Handler) VideosGenerations(c *gin.Context) {
	h.withDefaultVideoModel(c, "/v1/videos/generations")
}

// VideosEdits handles POST /v1/videos/edits.
func (h *Handler) VideosEdits(c *gin.Context) {
	h.withDefaultVideoModel(c, "/v1/videos/edits")
}

// VideosExtensions handles POST /v1/videos/extensions.
func (h *Handler) VideosExtensions(c *gin.Context) {
	h.withDefaultVideoModel(c, "/v1/videos/extensions")
}

// withDefaultVideoModel injects the default XAI video model when the client
// omits one and then forwards to the shared video handler.
func (h *Handler) withDefaultVideoModel(c *gin.Context, path string) {
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}
	if gjson.GetBytes(body, "model").String() == "" {
		body, _ = sjson.SetBytes(body, "model", defaultXAIVideosModel)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.URL.Path = path
	h.Video(c)
}

// VideosRetrieve handles GET /v1/videos/:request_id. It proxies the request to
// the upstream XAI video retrieval endpoint using the configured XAI connection.
func (h *Handler) VideosRetrieve(c *gin.Context) {
	requestID := c.Param("request_id")
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "request_id is required", "type": "invalid_request_error"}})
		return
	}
	h.proxyVideoRequest(c, "xai", defaultXAIVideosModel, http.MethodGet, "/v1/videos/"+requestID)
}

// OpenAIVideosCreate handles POST /openai/v1/videos. It maps the OpenAI-style
// video creation request to the XAI native format and forwards it.
func (h *Handler) OpenAIVideosCreate(c *gin.Context) {
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		model = defaultXAIVideosModel
		body, _ = sjson.SetBytes(body, "model", model)
	}
	provider, _ := executor.SplitModel(model)
	if provider == "" || provider == "sora" {
		provider = "xai"
		body, _ = sjson.SetBytes(body, "model", defaultXAIVideosModel)
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.URL.Path = "/v1/videos"
	h.Video(c)
}

// OpenAIVideosRetrieve handles GET /openai/v1/videos/:video_id.
func (h *Handler) OpenAIVideosRetrieve(c *gin.Context) {
	videoID := c.Param("video_id")
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "video_id is required", "type": "invalid_request_error"}})
		return
	}
	h.proxyVideoRequest(c, "xai", defaultXAIVideosModel, http.MethodGet, "/openai/v1/videos/"+videoID)
}

// OpenAIVideosContent handles GET /openai/v1/videos/:video_id/content.
func (h *Handler) OpenAIVideosContent(c *gin.Context) {
	videoID := c.Param("video_id")
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "video_id is required", "type": "invalid_request_error"}})
		return
	}
	h.proxyVideoRequest(c, "xai", defaultXAIVideosModel, http.MethodGet, "/openai/v1/videos/"+videoID+"/content")
}

// proxyVideoRequest forwards a GET to the upstream video endpoint for the
// configured provider connection.
func (h *Handler) proxyVideoRequest(c *gin.Context, provider, modelName, method, upstreamPath string) {
	start := time.Now()

	if !h.isModelAllowed(c.Request.Context(), provider+"/"+modelName) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model not allowed for this API key", "type": "invalid_request_error"}})
		return
	}

	sessionID := h.sessionIDForAffinity(c, provider, modelName, nil)
	conn, err := h.getConnection(c.Request.Context(), provider, modelName, sessionID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available " + provider + " connection", "type": "server_error"}})
		return
	}

	h.proactiveRefreshToken(c.Request.Context(), conn, provider)

	psdMap := map[string]string{}
	if conn.ProviderSpecificData != "" {
		_ = json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
	}

	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = defaultProviderBaseURL(provider)
	}
	upstreamURL := strings.TrimSuffix(baseURL, "/") + upstreamPath

	var reqBody io.Reader
	if c.Request.Body != nil {
		body, _ := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), method, upstreamURL, reqBody)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}

	if conn.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+conn.AccessToken)
	} else if conn.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+conn.APIKey)
	}
	req.Header.Set("Accept", "application/json")
	for k, vv := range c.Request.Header {
		for _, v := range vv {
			if isForwardedVideoHeader(k) {
				req.Header.Add(k, v)
			}
		}
	}

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Logger.Warn("video proxy upstream failed", "error", err.Error(), "conn", shortID(conn.ID, 8))
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "failed to read video response", "type": "server_error"}})
		return
	}

	h.logRequest(c, &usage.LogEntry{
		ApiKeyID:       c.GetString("api_key_id"),
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        modelName,
		ApiType:        apiTypeFromPath(c.Request.URL.Path),
		Modality:       "video",
		Stream:         false,
		LatencyMs:      time.Since(start).Milliseconds(),
		StatusCode:     resp.StatusCode,
	})

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Status(resp.StatusCode)
	c.Writer.Write(respBody)
}

func isForwardedVideoHeader(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case "authorization", "content-length", "host", "connection", "accept-encoding":
		return false
	default:
		return true
	}
}

func defaultProviderBaseURL(provider string) string {
	switch provider {
	case "xai":
		return "https://api.x.ai"
	case "openai":
		return "https://api.openai.com"
	default:
		return "https://api.openai.com"
	}
}

// VideoEdits is provided so the route table can use the same name as the
// CLIProxyAPI surface; it behaves identically to VideosEdits.
func (h *Handler) VideoEdits(c *gin.Context) { h.VideosEdits(c) }

// VideoExtensions is provided for naming symmetry with the CLIProxyAPI surface.
func (h *Handler) VideoExtensions(c *gin.Context) { h.VideosExtensions(c) }
