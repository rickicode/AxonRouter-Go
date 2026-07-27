package executor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// cfNativeRunEndpoint builds the Cloudflare Workers AI native /ai/run/{model}
// URL for a given (possibly prefixed) model name. The model name is normalized
// to the upstream "@cf/author/model" form.
func cfNativeRunEndpoint(modelName string, psd map[string]string) (string, error) {
	accountID := psd["accountId"]
	if accountID == "" {
		accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	}
	if accountID == "" {
		return "", fmt.Errorf("cloudflare Workers AI requires an Account ID. Add it in provider settings or set CLOUDFLARE_ACCOUNT_ID env var")
	}
	canonical := strings.TrimPrefix(modelName, "@")
	canonical = strings.TrimPrefix(canonical, "cf/")
	if !strings.Contains(canonical, "/") {
		return "", fmt.Errorf("invalid Cloudflare model id: %s", modelName)
	}
	base := os.Getenv("CLOUDFLARE_BASE_URL")
	if base == "" {
		base = "https://api.cloudflare.com"
	}
	return fmt.Sprintf("%s/client/v4/accounts/%s/ai/run/@cf/%s", strings.TrimRight(base, "/"), accountID, canonical), nil
}

// cfNativeRun performs a request against the Cloudflare Workers AI native
// /ai/run/{model} endpoint. It centralizes auth, error translation, and
// response reading so the task-specific branches stay small.
func (e *CloudflareExecutor) cfNativeRun(ctx context.Context, req *Request, modelName string, payload []byte) (*Response, error) {
	url, err := cfNativeRunEndpoint(modelName, req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	resp, err := e.DoRequest(ctx, "POST", url, headers, payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       resp.Body,
			RawBody:    resp.Body,
			Headers:    resp.Headers,
		}
		upErr.TranslateErrorBody("cf")
		return nil, upErr
	}
	return resp, nil
}

// cfUnwrapResult extracts the "result" field from a Cloudflare native response.
// If the field is missing or the body is not JSON, it returns the original body.
func cfUnwrapResult(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	r := gjson.GetBytes(body, "result")
	if !r.Exists() {
		return body
	}
	out, err := sjson.SetBytes([]byte(`{}`), "result", r.Value())
	if err != nil {
		return body
	}
	return out
}
