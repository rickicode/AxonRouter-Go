package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

// cfImageRunURL returns the native Cloudflare Workers AI /ai/run/{model} base
// URL and resolved accountID. It resolves {accountId} from psd, then env var,
// and honors a custom base_url when present (matching how CF chat/embeddings
// connections are configured in the dashboard).
func cfImageRunURL(baseURL string, psd map[string]string) (string, string, error) {
	accountID := ""
	if psd != nil {
		accountID = psd["accountId"]
	}
	if accountID == "" {
		accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	}
	if accountID == "" {
		return "", "", fmt.Errorf(
			"cloudflare Workers AI requires an Account ID. " +
				"Add it in provider settings or set CLOUDFLARE_ACCOUNT_ID env var. " +
				"Find it at: https://dash.cloudflare.com (right sidebar)")
	}

	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err == nil && strings.Contains(u.Path, "{accountId}") {
			resolved := strings.ReplaceAll(baseURL, "{accountId}", accountID)
			resolved = strings.TrimRight(resolved, "/")
			// Strip known suffixes and append the native run root.
			for _, suffix := range []string{"/chat/completions", "/embeddings", "/models", "/images/generations"} {
				if strings.HasSuffix(resolved, suffix) {
					resolved = strings.TrimSuffix(resolved, suffix)
					break
				}
			}
			return resolved + "/ai/run", accountID, nil
		}
	}
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run", accountID), accountID, nil
}

// cfNormalizeImageModelName turns gateway-style model IDs into CF native model
// IDs used by the /ai/run/{model} endpoint (e.g. "@cf/black-forest-labs/flux-1-schnell").
func cfNormalizeImageModelName(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.Index(model, "/"); idx >= 0 {
		prefix := model[:idx]
		rest := model[idx+1:]
		switch {
		case prefix == "@cf" || prefix == "cf":
			return "@cf/" + rest
		case prefix == "@":
			// "@vendor/model" is missing the CF namespace; add it.
			return "@cf/" + rest
		default:
			return "@cf/" + model
		}
	}
	if model == "" {
		return ""
	}
	return "@cf/" + model
}

// cfImageTaskPayload translates an OpenAI images/generations payload into the
// Cloudflare Workers AI text-to-image task payload.
func cfImageTaskPayload(body []byte) ([]byte, error) {
	prompt := JSONGet(body, "prompt")
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	out := map[string]any{"prompt": prompt}
	if n := getGJSONInt(body, "n"); n > 1 {
		out["num_steps"] = n
	}
	if steps := getGJSONInt(body, "num_steps"); steps > 0 {
		out["num_steps"] = steps
	}
	if guidance := getGJSONFloat(body, "guidance"); guidance > 0 {
		out["guidance"] = guidance
	}
	return json.Marshal(out)
}

func getGJSONInt(body []byte, path string) int64 {
	v := JSONGet(body, path)
	if v == "" {
		return 0
	}
	var n int64
	fmt.Sscanf(v, "%d", &n)
	return n
}

func getGJSONFloat(body []byte, path string) float64 {
	v := JSONGet(body, path)
	if v == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(v, "%f", &f)
	return f
}

// CFImageResult is the partial shape of the CF Workers AI image run response.
type CFImageResult struct {
	// Image string is delivered as a base64 payload for some models.
	Image string `json:"image"`
	// Name is the synthetic filename returned by CF run endpoint.
	Name string `json:"name"`
}

// CFImageResponse is the outer envelope returned by CF /ai/run/{model}.
type CFImageResponse struct {
	Result  CFImageResult `json:"result"`
	Success bool          `json:"success"`
	Errors  []CFError     `json:"errors"`
}

// CFError mirrors the error objects inside CF response envelope.
type CFError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// cfBuildOpenAIResponse unwraps the CF envelope and returns OpenAI-compatible
// JSON. If response_format is "b64_json" it embeds the base64 payload,
// otherwise it returns a data URL (image/png) containing the base64 image.
func cfBuildOpenAIResponse(upstream []byte, requestedFormat string) ([]byte, error) {
	var envelope CFImageResponse
	if err := json.Unmarshal(upstream, &envelope); err != nil {
		return nil, fmt.Errorf("invalid cloudflare image response: %w", err)
	}
	for _, e := range envelope.Errors {
		return nil, fmt.Errorf("cloudflare image error %d: %s", e.Code, e.Message)
	}
	if envelope.Result.Image == "" {
		return nil, fmt.Errorf("cloudflare image response did not contain image data")
	}
	contentType := "image/png"
	dataURL := "data:" + contentType + ";base64," + envelope.Result.Image
	resp := map[string]any{
		"created": time.Now().Unix(),
		"data":    []map[string]any{},
	}
	var dataItem map[string]any
	if requestedFormat == "b64_json" {
		dataItem = map[string]any{"b64_json": envelope.Result.Image}
	} else {
		dataItem = map[string]any{"url": dataURL}
	}
	resp["data"] = []map[string]any{dataItem}
	return json.Marshal(resp)
}

// CloudflareImageGenerator executes image generation through the native
// Cloudflare Workers AI /ai/run/{model} endpoint and normalizes the response
// into OpenAI-compatible images/generations shape.
type CloudflareImageGenerator struct {
	*BaseExecutor
}

// NewCloudflareImageGenerator creates a CF-specific image generator.
func NewCloudflareImageGenerator(base *BaseExecutor) *CloudflareImageGenerator {
	return &CloudflareImageGenerator{BaseExecutor: base}
}

// Images satisfies the ImageGenerator interface.
func (e *CloudflareImageGenerator) Images(ctx context.Context, req *Request) (*Response, error) {
	baseURL, _, err := cfImageRunURL(req.BaseURL, req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	c := providercfg.CompatibilityFor("cf")
	body := sanitizeRequestWithCompatibility(req.Body, c)
	model := cfNormalizeImageModelName(JSONGet(body, "model"))
	if model == "" {
		return nil, fmt.Errorf("cloudflare image generation requires a model")
	}
	payload, err := cfImageTaskPayload(body)
	if err != nil {
		return nil, err
	}
	responseFormat := JSONGet(body, "response_format")
	url := fmt.Sprintf("%s/%s", strings.TrimRight(baseURL, "/"), model)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

	resp, err := e.DoRequest(ctx, "POST", url, headers, payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
	}

	normalized, err := cfBuildOpenAIResponse(resp.Body, responseFormat)
	if err != nil {
		body, _ := json.Marshal(map[string]any{
			"error": map[string]any{
				"message": err.Error(),
				"type":    "server_error",
			},
		})
		return nil, &UpstreamError{StatusCode: http.StatusBadGateway, Body: body, RawBody: body}
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
		Body:       normalized,
	}, nil
}
