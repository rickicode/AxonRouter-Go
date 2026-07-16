package v1

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// Unified handles POST /v1/unified — dispatches to the right endpoint by mode.
func (h *Handler) Unified(c *gin.Context) {
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	mode := executor.JSONGet(body, "mode")
	if mode == "" {
		mode = "text"
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}

	provider, _ := executor.SplitModel(model)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model must include provider prefix", "type": "invalid_request_error"}})
		return
	}

	switch mode {
	case "text":
		// Dispatch to chat completions; rewrite path so logs/tagging reflect the
		// actual client-facing surface instead of the generic /v1/unified path.
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		c.Request.URL.Path = "/v1/chat/completions"
		h.ChatCompletions(c)

	case "image":
		// Dispatch to image generation
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		c.Request.URL.Path = "/v1/images/generations"
		h.Images(c)

	case "audio":
		// Dispatch to TTS
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		c.Request.URL.Path = "/v1/audio/speech"
		h.TTS(c)

	case "video":
		// Dispatch to video generation
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		c.Request.URL.Path = "/v1/video/generations"
		h.Video(c)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "unsupported mode: " + mode + ". Supported: text, image, audio, video",
			"type":    "invalid_request_error",
		}})
	}
}
