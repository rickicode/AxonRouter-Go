package headroom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to a headroom HTTP endpoint and falls back to in-process
// compression when the endpoint is unreachable or disabled.
type Client struct {
	cfg        Config
	detector   Detector
	compressor Compressor
	httpClient *http.Client
	metrics    *Metrics
}

// NewClient creates a headroom client with the supplied config.
func NewClient(cfg Config, detector Detector, compressor Compressor, metrics *Metrics) *Client {
	if detector == nil {
		detector = NewDefaultDetector()
	}
	if compressor == nil {
		compressor = NewDefaultCompressor()
	}
	if metrics == nil {
		metrics = NewMetrics()
	}
	return &Client{
		cfg:        cfg,
		detector:   detector,
		compressor: compressor,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond},
		metrics:    metrics,
	}
}

// Compress sends the payload to the headroom endpoint when enabled, otherwise
// compresses in-process.
func (c *Client) Compress(data []byte) (*Output, error) {
	return c.CompressWithKind(data, "")
}

// CompressWithKind uses an explicit kind hint when provided.
func (c *Client) CompressWithKind(data []byte, hint Kind) (*Output, error) {
	if len(data) == 0 {
		return nil, ErrEmptyPayload
	}

	if len(data) > c.cfg.MaxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds %d byte limit", c.cfg.MaxPayloadBytes)
	}

	if !c.cfg.Enabled {
		return c.compressLocal(data, hint)
	}

	out, err := c.callRemote(data, hint)
	if err == nil {
		c.metrics.RecordSuccess(out.OriginalBytes, out.CompressedBytes)
		return out, nil
	}

	c.metrics.RecordError()
	// Fall back to in-process compression.
	return c.compressLocal(data, hint)
}

func (c *Client) callRemote(data []byte, hint Kind) (*Output, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.TimeoutMs)*time.Millisecond)
	defer cancel()

	in := Input{Data: data, KindHint: hint}
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}

	endpoint := c.cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultServiceEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+endpoint+"/compress", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("headroom remote returned %d: %s", resp.StatusCode, string(respBody))
	}

	var out Output
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) compressLocal(data []byte, hint Kind) (*Output, error) {
	kind := hint
	if kind == "" {
		kind = c.detector.Detect(data)
	}
	if kind == "" {
		kind = KindUnknown
	}

	compressed, err := c.compressor.Compress(data, kind)
	var final []byte
	if err != nil {
		c.metrics.RecordError()
		final = data
		kind = KindUnknown
	} else {
		final = compressed
	}

	out := &Output{
		Data:             final,
		Kind:             kind,
		OriginalBytes:    len(data),
		CompressedBytes:  len(final),
		OriginalTokens:   TokenEstimate(data),
		CompressedTokens: TokenEstimate(final),
	}
	if err == nil {
		c.metrics.RecordSuccess(out.OriginalBytes, out.CompressedBytes)
	}
	return out, nil
}
