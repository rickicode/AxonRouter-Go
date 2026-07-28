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

const (
	readyProbeTimeout  = 100 * time.Millisecond
	readyProbeInterval = 50 * time.Millisecond
)

// Process manages the headroom service lifecycle in-process.
// External process spawning is intentionally out of scope for this stage; the
// manager starts a goroutine-bound HTTP server and monitors it.
type Process struct {
	cfg        Config
	detector   Detector
	compressor Compressor
	metrics    *Metrics
	mu         sync.Mutex
	cancel     context.CancelFunc
	running    bool
	lastErr    error
}

// NewProcess creates a lifecycle manager that hosts the headroom HTTP service
// in-process.
func NewProcess(cfg Config, detector Detector, compressor Compressor, metrics *Metrics) *Process {
	return &Process{
		cfg:        cfg,
		detector:   detector,
		compressor: compressor,
		metrics:    metrics,
	}
}

// Start launches the in-process headroom server if enabled and not already
// running. Returns nil if already running.
func (p *Process) Start() error {
	if !p.cfg.Enabled {
		return errors.New("headroom is disabled")
	}

	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}

	// Build a fresh service for every start so a previous Stop/Close does not
	// prevent reuse.
	svc := NewService(p.cfg, p.detector, p.compressor, p.metrics)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.running = true
	p.lastErr = nil
	p.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.ListenAndServe()
	}()

	go p.monitor(ctx, svc, errCh)

	if err := p.waitForReady(svc, time.Duration(p.cfg.TimeoutMs)*time.Millisecond); err != nil {
		cancel()
		_ = svc.Close()
		p.mu.Lock()
		p.running = false
		p.lastErr = err
		p.cancel = nil
		p.mu.Unlock()
		return fmt.Errorf("headroom process did not become ready: %w", err)
	}
	return nil
}

func (p *Process) monitor(ctx context.Context, svc *Service, errCh <-chan error) {
	select {
	case <-ctx.Done():
		_ = svc.Close()
	case err := <-errCh:
		p.mu.Lock()
		p.running = false
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.lastErr = err
		}
		p.cancel = nil
		p.mu.Unlock()
	}
}

// Stop shuts down the in-process headroom server.
func (p *Process) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.running = false
	p.cancel = nil
	p.mu.Unlock()
	return nil
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

func (p *Process) waitForReady(svc *Service, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = readyProbeInterval
	}
	deadline := time.Now().Add(timeout)
	addr := svc.Addr()
	if addr == "" {
		addr = p.cfg.Endpoint
	}
	if addr == "" {
		addr = DefaultEndpoint
	}
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, readyProbeTimeout)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(readyProbeInterval)
	}
	return errors.New("timeout waiting for headroom listener")
}
