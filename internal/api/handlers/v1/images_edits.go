package v1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ImagesEdits handles POST /v1/images/edits.
// It accepts either a JSON body or a multipart/form-data upload, normalises the
// payload to the same JSON shape used by /v1/images/generations, and routes it
// through the existing image-generation execution path.
func (h *Handler) ImagesEdits(c *gin.Context) {
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))

	var body []byte
	if strings.HasPrefix(contentType, "multipart/form-data") || contentType == "" {
		parsed, err := buildImagesEditsJSONFromMultipart(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			return
		}
		body = parsed
	} else {
		var err error
		body, err = readBody(c)
		if err != nil {
			writeReadBodyError(c, err)
			return
		}
	}

	if gjson.GetBytes(body, "prompt").String() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "prompt is required", "type": "invalid_request_error"}})
		return
	}

	// Inject the upstream endpoint so the executor hits /v1/images/edits rather
	// than the default /v1/images/generations.
	ctx := executor.ContextWithImagesPath(c.Request.Context(), "/v1/images/edits")
	c.Request = c.Request.WithContext(ctx)

	// Replace the request body so the shared Images handler can process it.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Images(c)
}

// buildImagesEditsJSONFromMultipart parses a multipart/form-data request and
// builds a JSON payload equivalent to the OpenAI images edits API.
func buildImagesEditsJSONFromMultipart(c *gin.Context) ([]byte, error) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		return nil, fmt.Errorf("invalid multipart form: %w", err)
	}

	model := strings.TrimSpace(c.PostForm("model"))
	if model == "" {
		model = "dall-e-2"
	}
	prompt := strings.TrimSpace(c.PostForm("prompt"))
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	out := []byte(`{"model":"","prompt":""}`)
	out, _ = sjson.SetBytes(out, "model", model)
	out, _ = sjson.SetBytes(out, "prompt", prompt)

	if image, err := firstMultipartFileToDataURL(c.Request.MultipartForm, "image"); err == nil && image != "" {
		out, _ = sjson.SetBytes(out, "image", image)
	}
	if mask, err := firstMultipartFileToDataURL(c.Request.MultipartForm, "mask"); err == nil && mask != "" {
		out, _ = sjson.SetBytes(out, "mask", mask)
	}
	if v := strings.TrimSpace(c.PostForm("size")); v != "" {
		out, _ = sjson.SetBytes(out, "size", v)
	}
	if v := strings.TrimSpace(c.PostForm("quality")); v != "" {
		out, _ = sjson.SetBytes(out, "quality", v)
	}
	if v := strings.TrimSpace(c.PostForm("response_format")); v != "" {
		out, _ = sjson.SetBytes(out, "response_format", v)
	}
	if v := strings.TrimSpace(c.PostForm("n")); v != "" {
		if n := gjson.Parse(v).Int(); n > 0 {
			out, _ = sjson.SetBytes(out, "n", n)
		}
	}

	return out, nil
}

func firstMultipartFileToDataURL(form *multipart.Form, field string) (string, error) {
	if form == nil {
		return "", nil
	}
	files := form.File[field]
	if len(files) == 0 {
		files = form.File[field+"[]"]
	}
	if len(files) == 0 {
		return "", nil
	}
	fh := files[0]
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	mime := fh.Header.Get("Content-Type")
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

// imagesUpstreamPath extracts the override upstream path stored on the context
// by ImagesEdits. The default images executor uses /v1/images/generations; this
// lets edits requests target /v1/images/edits when the executor supports it.
func imagesUpstreamPath(c *gin.Context) string {
	if v, ok := c.Get("images_upstream_path"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// imagesEditsRequestBody returns the normalised JSON body for an images edit
// request, whether the original request was JSON or multipart.
func imagesEditsRequestBody(c *gin.Context) []byte {
	if v, ok := c.Get("images_edits_body"); ok {
		if b, ok := v.([]byte); ok {
			return b
		}
	}
	body, _ := readBody(c)
	return body
}

// marshalAny converts a value to compact JSON, returning an empty JSON object on
// failure so the caller never has to handle a nil slice.
func marshalAny(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
