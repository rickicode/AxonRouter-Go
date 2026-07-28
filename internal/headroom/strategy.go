package headroom

import (
	"context"
)

// CompressionStrategy wraps a headroom client so it can participate in the
// compression.Strategy pipeline as an engine. Used by request_optimization to
// apply headroom before or after other compression stages.
type CompressionStrategy struct {
	client   *Client
	fallback func([]byte) ([]byte, error)
}

// NewCompressionStrategy creates a strategy bound to a headroom client.
func NewCompressionStrategy(client *Client) *CompressionStrategy {
	return &CompressionStrategy{client: client}
}

// Apply runs headroom compression on tool_result blocks inside the request body.
// It is fail-open and returns the original body on any issue.
func (s *CompressionStrategy) Apply(body []byte) []byte {
	if s.client == nil {
		return body
	}
	cfg := s.client.Config()
	if !cfg.Enabled || s.client.Status() != "running" {
		return body
	}
	return ApplyToRequestBody(context.Background(), s.client, body)
}
