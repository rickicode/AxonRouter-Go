package executor

import "context"

// OpenAIResponsesExecutor forwards both chat-completions and responses-style
// requests to the upstream OpenAI Responses API endpoint. It is intended for
// providers whose only native endpoint is /v1/responses (e.g. Qwen Cloud).
//
// Chat-completion requests are translated to Responses format by the
// translator layer before reaching the executor, so this executor simply
// passes the already-translated body to /v1/responses in both streaming and
// non-streaming modes.
type OpenAIResponsesExecutor struct {
	*OpenAIExecutor
}

// NewOpenAIResponsesExecutor creates a new Responses-only executor.
func NewOpenAIResponsesExecutor(base *BaseExecutor) *OpenAIResponsesExecutor {
	return &OpenAIResponsesExecutor{OpenAIExecutor: NewOpenAIExecutor(base)}
}

// Execute routes non-streaming chat requests to the upstream /v1/responses
// endpoint. The request body is already in Responses format by the time it
// reaches this method.
func (e *OpenAIResponsesExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	return e.OpenAIExecutor.Responses(ctx, req)
}

// ExecuteStream routes streaming chat requests to /v1/responses.
func (e *OpenAIResponsesExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return e.OpenAIExecutor.ResponsesStream(ctx, req)
}
