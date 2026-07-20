package providercfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// RoutingMode determines how requests are distributed across a provider's
// eligible connections.
type RoutingMode string

const (
	// FirstEligible keeps the original behavior: pick the first eligible
	// connection from the shuffled snapshot.
	FirstEligible RoutingMode = "first_eligible"
	// RoundRobin rotates across eligible connections per request.
	RoundRobin RoutingMode = "round_robin"
	// Random picks one eligible connection uniformly at random per request.
	Random RoutingMode = "random"
)

// DefaultRoutingMode is applied when no explicit setting has been saved.
const DefaultRoutingMode = RoundRobin

// ProviderSettings holds per-provider runtime configuration stored outside the
// database to avoid SQLite write contention on the hot routing path.
type ProviderSettings struct {
	RoutingMode    RoutingMode    `json:"routing_mode"`
	Compatibility  *Compatibility `json:"compatibility,omitempty"`
}

// Manager loads and persists per-provider settings from JSON files in the data
// directory. It keeps settings in memory and is safe for concurrent use.
type Manager struct {
	dir        string
	mu         sync.RWMutex
	settings   map[string]ProviderSettings
	rrCounters map[string]*atomic.Uint64
}

// NewManager creates a Manager that stores settings under dataDir.
func NewManager(dataDir string) *Manager {
	dir := filepath.Join(dataDir, "provider-settings")
	_ = os.MkdirAll(dir, 0o755)

	m := &Manager{
		dir: dir,
		settings: make(map[string]ProviderSettings),
		rrCounters: make(map[string]*atomic.Uint64),
	}
	m.loadAll()
	setCompatibilityManager(m)
	return m
}

func (m *Manager) settingsPath(providerID string) string {
	return filepath.Join(m.dir, providerID+".json")
}

// loadAll reads any existing provider setting files on startup.
func (m *Manager) loadAll() {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		providerID := e.Name()[:len(e.Name())-len(".json")]
		_, _ = m.Get(providerID)
	}
}

// Get returns the stored settings for a provider, falling back to the default
// when no file exists yet.
func (m *Manager) Get(providerID string) (ProviderSettings, error) {
	m.mu.RLock()
	gs, ok := m.settings[providerID]
	m.mu.RUnlock()
	if ok {
		return gs, nil
	}

	data, err := os.ReadFile(m.settingsPath(providerID))
	if err != nil {
		if os.IsNotExist(err) {
			return ProviderSettings{RoutingMode: DefaultRoutingMode}, nil
		}
		return ProviderSettings{RoutingMode: DefaultRoutingMode}, fmt.Errorf("read settings: %w", err)
	}

	var s ProviderSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return ProviderSettings{RoutingMode: DefaultRoutingMode}, fmt.Errorf("parse settings: %w", err)
	}
	if s.RoutingMode == "" {
		s.RoutingMode = DefaultRoutingMode
	}

	m.mu.Lock()
	m.settings[providerID] = s
	m.mu.Unlock()
	return s, nil
}

// RoutingMode returns the effective routing mode for a provider.
func (m *Manager) RoutingMode(providerID string) RoutingMode {
	s, err := m.Get(providerID)
	if err != nil {
		return DefaultRoutingMode
	}
	return s.RoutingMode
}

// Save persists settings for a provider and updates the in-memory cache.
func (m *Manager) Save(providerID string, s ProviderSettings) error {
	if s.RoutingMode == "" {
		s.RoutingMode = DefaultRoutingMode
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	path := m.settingsPath(providerID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	m.mu.Lock()
	m.settings[providerID] = s
	m.mu.Unlock()
	return nil
}

// NextRoundRobinIndex returns the next index to use for round-robin selection
// across the given number of candidates. The cursor is keyed by
// providerID + "\x00" + modelID so high-traffic models do not steal rotation
// from sibling models. Callers must ensure total > 0.
func (m *Manager) NextRoundRobinIndex(providerID, modelID string, total int) int {
	key := providerID + "\x00" + modelID
	m.mu.RLock()
	counter := m.rrCounters[key]
	m.mu.RUnlock()
	if counter == nil {
		m.mu.Lock()
		counter = m.rrCounters[key]
		if counter == nil {
			counter = &atomic.Uint64{}
			m.rrCounters[key] = counter
		}
		m.mu.Unlock()
	}
	next := counter.Add(1)
	return int((next - 1) % uint64(total))
}
