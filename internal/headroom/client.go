package headroom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Client calls a Headroom HTTP endpoint. Failures are recorded and the caller
// should fall back to the original payload.
type Client struct {
	mu       sync.RWMutex
	cfg      Config
	endpoint string
	http     *http.Client
	
	metrics Metrics
}

// NewClient creates a client from config. It resolves and verifies the endpoint,
// starting the internal server if no explicit endpoint is configured.
func NewClient(cfg Config) (*Client, error) {
	c := &Client{
		cfg: cfg,
	}
	if err := c.applyConfig(cfg); err != nil {
		return nil, err
	}
	return c, nil
}

// SetConfig updates the client configuration at runtime.
func (c *Client) SetConfig(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.applyConfigLocked(cfg)
}

func (c *Client) applyConfig(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.applyConfigLocked(cfg)
}

func (c *Client) applyConfigLocked(cfg Config) error {
	c.cfg = cfg
	timeout := cfg.TimeoutMs
	if timeout <= 0 {
		timeout = DefaultTimeoutMs
	}
	c.http = &http.Client{
		Timeout: time.Duration(timeout) * time.Millisecond,
	}

	if !cfg.Enabled {
		c.endpoint = ""
		return nil
	}

	c.endpoint = cfg.Endpoint
	return nil
}

// Enable starts the internal server when no endpoint is configured. Used by
// process manager to auto-start a local Headroom service. The returned string
// is the effective endpoint.
func (c *Client) BindInternal(serverEndpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Endpoint == "" || c.endpoint == "" {
		c.endpoint = serverEndpoint
	}
}

// Config returns the current client configuration.
func (c *Client) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// Endpoint returns the resolved endpoint.
func (c *Client) Endpoint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.endpoint
}

// Status reports whether the client is configured and can reach its endpoint.
func (c *Client) Status() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.cfg.Enabled {
		return "stopped"
	}
	if c.endpoint == "" {
		return "error"
	}
	return "running"
}

// Compress sends the original text to the Headroom endpoint and returns the
// compressed result. On any failure it returns the original text.
func (c *Client) Compress(ctx context.Context, kind PayloadKind, original string) (string, error) {
	c.mu.RLock()
	endpoint := c.endpoint
	enabled := c.cfg.Enabled
	maxBytes := c.cfg.MaxPayloadBytes
	c.mu.RUnlock()

	if !enabled || endpoint == "" {
		return original, nil
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxPayloadBytes
	}
	if len(original) > maxBytes {
		atomic.AddInt64(&c.metrics.Total, 1)
		// Oversized payloads are not compressed.
		return original, nil
	}

	reqBody := PayloadHeader{
		Kind:       kind,
		SourceSize: len(original),
		Original:   original,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		atomic.AddInt64(&c.metrics.Errors, 1)
		return original, fmt.Errorf("headroom marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/compress", bytes.NewReader(data))
	if err != nil {
		atomic.AddInt64(&c.metrics.Errors, 1)
		return original, fmt.Errorf("headroom request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		atomic.AddInt64(&c.metrics.Errors, 1)
		return original, fmt.Errorf("headroom call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		atomic.AddInt64(&c.metrics.Errors, 1)
		return original, fmt.Errorf("headroom read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		atomic.AddInt64(&c.metrics.Errors, 1)
		return original, fmt.Errorf("headroom status %d: %s", resp.StatusCode, string(respBody))
	}

	var result CompressedResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		atomic.AddInt64(&c.metrics.Errors, 1)
		return original, fmt.Errorf("headroom decode: %w", err)
	}

	atomic.AddInt64(&c.metrics.Total, 1)
	atomic.AddInt64(&c.metrics.BytesSaved, int64(result.SavedBytes))
	return result.Compressed, nil
}

// Metrics returns a copy of the client metrics.
func (c *Client) Metrics() Metrics {
	return Metrics{
		Total:      atomic.LoadInt64(&c.metrics.Total),
		BytesSaved: atomic.LoadInt64(&c.metrics.BytesSaved),
		Errors:     atomic.LoadInt64(&c.metrics.Errors),
	}
}
