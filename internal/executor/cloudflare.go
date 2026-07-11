package executor

import (
	"context"
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

// Execute sanitizes the request for Cloudflare constraints and delegates to the
// underlying OpenAI executor.
func (e *CloudflareExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	cp := cloneRequest(req)
	cp.Body = sanitizeCFRequest(cp.Body)
	return e.OpenAIExecutor.Execute(ctx, cp)
}

// ExecuteStream sanitizes the request for Cloudflare constraints and delegates
// to the underlying OpenAI executor.
func (e *CloudflareExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	cp := cloneRequest(req)
	cp.Body = sanitizeCFRequest(cp.Body)
	return e.OpenAIExecutor.ExecuteStream(ctx, cp)
}
