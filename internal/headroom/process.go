package headroom

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Process manages the headroom service lifecycle in-process.
// External process spawning is intentionally out of scope for this stage; the
// manager starts a goroutine-bound HTTP server and monitors it.
type Process struct {
	cfg     Config
	service *Service
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	lastErr error
}

// NewProcess creates a lifecycle manager that hosts the headroom HTTP service
// in-process.
func NewProcess(cfg Config, detector Detector, compressor Compressor, metrics *Metrics) *Process {
	return &Process{
		cfg:     cfg,
		service: NewService(cfg, detector, compressor, metrics),
	}
}

// Start launches the in-process headroom server if enabled and not already
// running. Returns nil if already running.
func (p *Process) Start() error {
	if !p.cfg.Enabled {
		return errors.New("headroom is disabled")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.running = true
	p.lastErr = nil

	go p.run(ctx)
	if err := p.waitForReady(time.Duration(p.cfg.TimeoutMs) * time.Millisecond); err != nil {
		p.running = false
		p.lastErr = err
		return fmt.Errorf("headroom process did not become ready: %w", err)
	}
	return nil
}

// Stop shuts down the in-process headroom server.
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	err := p.service.Close()
	p.running = false
	p.cancel = nil
	return err
}

// Restart stops and starts the in-process headroom server.
func (p *Process) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	return p.Start()
}

// Running reports whether the server is currently running.
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// LastError returns the last error encountered by the server goroutine.
func (p *Process) LastError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

func (p *Process) run(ctx context.Context) {
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.service.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		return
	case err := <-errCh:
		p.mu.Lock()
		p.running = false
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.lastErr = err
		}
		p.mu.Unlock()
	}
}

func (p *Process) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := p.cfg.Endpoint
	if addr == "" {
		addr = DefaultEndpoint
	}
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("timeout waiting for headroom listener")
}
