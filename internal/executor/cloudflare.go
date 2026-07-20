package executor

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

// CloudflareExecutor wraps OpenAIExecutor with Cloudflare Workers AI-specific
// request sanitization and timeout defaults.
type CloudflareExecutor struct {
	*OpenAIExecutor
}

// NewCloudflareExecutor creates a dedicated Cloudflare executor.
func NewCloudflareExecutor(base *OpenAIExecutor) *CloudflareExecutor {
	return &CloudflareExecutor{OpenAIExecutor: base}
}

// cloneRequest returns a shallow copy of req with mutable fields snapped.
func cloneRequest(req *Request) *Request {
	cp := *req
	return &cp
}

// Execute sanitizes the request using the provider's compatibility config and
// delegates to the underlying OpenAI executor.
func (e *CloudflareExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	cp := cloneRequest(req)
	provider := req.Provider
	if provider == "" {
		provider = "cf"
	}
	c := providercfg.CompatibilityFor(provider)
	cp.Provider = provider
	cp.Body = sanitizeRequestWithCompatibility(cp.Body, c)
	cp.Body = cfInjectReasoningControl(cp.Body)
	resp, err := e.OpenAIExecutor.Execute(ctx, cp)
	translateIfCloudflare(err)
	return resp, err
}

// ExecuteStream sanitizes the request using the provider's compatibility config
// and delegates to the underlying OpenAI executor.
func (e *CloudflareExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	cp := cloneRequest(req)
	provider := req.Provider
	if provider == "" {
		provider = "cf"
	}
	c := providercfg.CompatibilityFor(provider)
	cp.Provider = provider
	cp.Body = sanitizeRequestWithCompatibility(cp.Body, c)
	cp.Body = cfInjectReasoningControl(cp.Body)
	result, err := e.OpenAIExecutor.ExecuteStream(ctx, cp)
	translateIfCloudflare(err)
	return result, err
}

func translateIfCloudflare(err error) {
	if err == nil {
		return
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		return
	}
	upErr.TranslateErrorBody("cf")
}
