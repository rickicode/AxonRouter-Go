package v1

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// unifiedPaths maps /v1/unified modes to the client-facing surface path used
// for logging and the in-flight request tracker. The actual HTTP request stays
// on /v1/unified; only the label derived from the path changes.
var unifiedPaths = map[string]string{
	"text":  "/v1/chat/completions",
	"image": "/v1/images/generations",
	"audio": "/v1/audio/speech",
	"video": "/v1/video/generations",
}

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

	// Tag the request with the real client-facing surface for logging and the
	// active-request panel without mutating the public URL.
	if surfacePath, ok := unifiedPaths[mode]; ok {
		c.Request.URL.Path = surfacePath
	}

	switch mode {
	case "text":
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		h.ChatCompletions(c)

	case "image":
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		h.Images(c)

	case "audio":
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		h.TTS(c)

	case "video":
		c.Request.Body = io.NopCloser(executor.JSONToReader(body))
		h.Video(c)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "unsupported mode: " + mode + ". Supported: text, image, audio, video",
			"type":    "invalid_request_error",
		}})
	}
}
