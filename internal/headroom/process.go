package headroom

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Manager owns the lifecycle of the Headroom service and is safe for use by
// multiple goroutines.
type Manager struct {
	mu       sync.RWMutex
	cfg      Config
	client   *Client
	server   *Server
	closed   bool
	stopCh   chan struct{}
	monitor  *time.Ticker
}

// NewManager creates a headroom manager. If cfg.Endpoint is empty it starts
// the in-process server automatically when Enabled.
func NewManager(cfg Config) (*Manager, error) {
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		cfg:     cfg,
		client:  client,
		stopCh:  make(chan struct{}),
		monitor: time.NewTicker(10 * time.Second),
	}
	if cfg.Enabled {
		if err := m.start(); err != nil {
			log.Printf("headroom manager start failed: %v", err)
		}
	}
	go m.loop()
	return m, nil
}

// SetConfig updates the manager config at runtime (e.g. from dashboard).
func (m *Manager) SetConfig(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	if err := m.client.SetConfig(cfg); err != nil {
		return err
	}
	if cfg.Enabled {
		return m.startLocked()
	}
	m.stopLocked()
	return nil
}

// Client returns the underlying headroom client.
func (m *Manager) Client() *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// Status returns running/stopped/error.
func (m *Manager) Status() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.cfg.Enabled {
		return "stopped"
	}
	if m.client.Status() == "running" {
		return "running"
	}
	return "error"
}

// Endpoint returns the effective endpoint.
func (m *Manager) Endpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	endpoint := m.client.Endpoint()
	if endpoint != "" {
		return endpoint
	}
	if m.server != nil {
		return m.server.Endpoint()
	}
	return ""
}

// Close shuts down the manager and server.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.stopLocked()
	m.mu.Unlock()
	close(m.stopCh)
	m.monitor.Stop()
	return nil
}

func (m *Manager) start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startLocked()
}

func (m *Manager) startLocked() error {
	if m.server != nil && m.server.Running() {
		// Already running; ensure client uses its endpoint if none configured.
		m.client.BindInternal(m.server.Endpoint())
		return nil
	}

	// External endpoint configured: don't start the in-process server.
	if m.cfg.Endpoint != "" {
		return nil
	}

	m.server = NewServer()
	endpoint, err := m.server.Start()
	if err != nil {
		return fmt.Errorf("headroom start server: %w", err)
	}
	m.client.BindInternal(endpoint)
	log.Printf("headroom internal server started at %s", endpoint)
	return nil
}

func (m *Manager) stopLocked() {
	if m.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = m.server.Stop(ctx)
		cancel()
		m.server = nil
	}
}

func (m *Manager) loop() {
	for {
		select {
		case <-m.stopCh:
			return
		case <-m.monitor.C:
			m.mu.RLock()
			enabled := m.cfg.Enabled
			server := m.server
			m.mu.RUnlock()
			if !enabled {
				continue
			}
			if server == nil || !server.Running() {
				m.mu.Lock()
				if m.closed {
					m.mu.Unlock()
					return
				}
				if m.cfg.Enabled && m.cfg.Endpoint == "" {
					log.Printf("headroom server not running, restarting...")
					if err := m.startLocked(); err != nil {
						log.Printf("headroom restart failed: %v", err)
					}
				}
				m.mu.Unlock()
			}
		}
	}
}

// HealthCheck performs a quick probe against the endpoint.
func HealthCheck(endpoint string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("headroom health status %d", resp.StatusCode)
	}
	return nil
}
