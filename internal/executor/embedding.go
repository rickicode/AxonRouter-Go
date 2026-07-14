package executor

import (
	"context"
	"fmt"
)

// EmbeddingsExecutor is implemented by executors that can route /v1/embeddings
// requests to the provider's embedding endpoint.
type EmbeddingsExecutor interface {
	Embeddings(ctx context.Context, req *Request) (*Response, error)
}

// embeddingsAdapter exposes an EmbeddingsExecutor through the standard Executor
// interface so executeWithRetry can drive it.
type embeddingsAdapter struct {
	exec EmbeddingsExecutor
}

// NewEmbeddingsAdapter wraps an EmbeddingsExecutor as a standard Executor.
func NewEmbeddingsAdapter(exec EmbeddingsExecutor) Executor {
	return &embeddingsAdapter{exec: exec}
}

func (a *embeddingsAdapter) Execute(ctx context.Context, req *Request) (*Response, error) {
	return a.exec.Embeddings(ctx, req)
}

func (a *embeddingsAdapter) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("embeddings do not support streaming")
}
