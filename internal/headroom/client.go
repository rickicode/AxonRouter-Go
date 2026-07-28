package headroom

import (
	"context"
	"errors"
	"time"
)

// Client is the headroom client used by other packages.
type Client struct {
	cfg        Config
	service    Service
	local      *InternalService
	compressor Compressor
}

// NewClient creates a headroom client from config. It sets up an in-process
// service when enabled and falls back to a local compressor for direct calls.
func NewClient(cfg Config) (*Client, error) {
	comp, err := NewDefaultCompressor()
	if err != nil {
		return nil, err
	}
	c := &Client{
		cfg:        cfg,
		compressor: comp,
	}
	if cfg.Enabled {
		svc, err := NewInternalService(cfg)
		if err != nil {
			return nil, err
		}
		c.service = svc
		c.local = svc
	}
	return c, nil
}

// NewClientWithService creates a client backed by an existing service.
func NewClientWithService(cfg Config, svc Service) *Client {
	return &Client{cfg: cfg, service: svc}
}

// Enabled reports whether headroom is enabled.
func (c *Client) Enabled() bool {
	return c != nil && c.cfg.Enabled && c.service != nil && c.service.Enabled()
}

// Compress attempts compression through the configured service, falling back to
// the in-process compressor if the service is unavailable.
func (c *Client) Compress(ctx context.Context, in Input) (Output, error) {
	if c == nil {
		return Output{}, errors.New("headroom: nil client")
	}
	if len(in.Payload) > c.cfg.MaxPayloadBytes {
		return Output{}, errors.New("headroom: payload exceeds maximum size")
	}
	start := time.Now()
	_ = start

	if c.service != nil && c.service.Enabled() {
		out, err := c.service.Compress(ctx, in)
		if err == nil {
			return out, nil
		}
		// Fall through to local compressor on failure.
	}

	// In-process fallback using the default detector and compressor.
	kind, _ := Detect(in)
	compressed, err := c.compressor.Compress(kind, in.Payload)
	if err != nil {
		return Output{}, err
	}
	return Output{
		Payload:        compressed,
		OriginalSize:   len(in.Payload),
		CompressedSize: len(compressed),
		Kind:           kind,
		Method:         TransformMethod(kind),
	}, nil
}

// LocalCompress performs classification and compression entirely in-process,
// ignoring any configured service.
func (c *Client) LocalCompress(ctx context.Context, in Input) (Output, error) {
	if c == nil {
		return Output{}, errors.New("headroom: nil client")
	}
	if len(in.Payload) > c.cfg.MaxPayloadBytes {
		return Output{}, errors.New("headroom: payload exceeds maximum size")
	}
	kind, _ := Detect(in)
	compressed, err := c.compressor.Compress(kind, in.Payload)
	if err != nil {
		return Output{}, err
	}
	return Output{
		Payload:        compressed,
		OriginalSize:   len(in.Payload),
		CompressedSize: len(compressed),
		Kind:           kind,
		Method:         TransformMethod(kind),
	}, nil
}

// StartService starts the local internal HTTP service if enabled.
func (c *Client) StartService(ctx context.Context) error {
	if c == nil || c.local == nil {
		return errors.New("headroom: no local service configured")
	}
	return c.local.Start(ctx)
}

// StopService stops the local internal HTTP service.
func (c *Client) StopService(ctx context.Context) error {
	if c == nil || c.local == nil {
		return nil
	}
	return c.local.Stop(ctx)
}

// ServiceAddr returns the listening address of the local service.
func (c *Client) ServiceAddr() string {
	if c == nil || c.local == nil {
		return ""
	}
	return c.local.Addr()
}
