package providercfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"
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
	// Affinity routes repeat calls from the same session to the same
	// connection when it is still eligible.
	Affinity RoutingMode = "affinity"
)

// DefaultRoutingMode is applied when no explicit setting has been saved.
const DefaultRoutingMode = RoundRobin

// Defaults for the streaming holdback buffer.
const (
	DefaultHoldbackMs    = 750
	DefaultHoldbackBytes = 64 * 1024
)

// ValidRoutingModes lists the routing modes that may be persisted for a
// provider. It is used by API validation to reject unknown values.
func ValidRoutingModes() []RoutingMode {
	return []RoutingMode{FirstEligible, RoundRobin, Random, Affinity}
}

// ProviderSettings holds per-provider runtime configuration stored outside the
// database to avoid SQLite write contention on the hot routing path.
type ProviderSettings struct {
	RoutingMode   RoutingMode    `json:"routing_mode"`
	Compatibility *Compatibility `json:"compatibility,omitempty"`
	// HoldbackMs overrides the default streaming holdback window in milliseconds.
	HoldbackMs *int `json:"holdback_ms,omitempty"`
	// HoldbackBytes overrides the default streaming holdback byte limit.
	HoldbackBytes *int `json:"holdback_bytes,omitempty"`
	// FlatRate marks subscription/cookie-web providers that charge a fixed fee
	// rather than per-token. When true, dashboard cost display and the response
	// cost header report $0, while the stored cost_usd continues to be estimated
	// for internal budget/quota tracking.
	FlatRate bool `json:"flat_rate,omitempty"`
}

// readFileHook is swapped during tests to count disk reads. In production it
// always points to os.ReadFile.
var readFileHook = os.ReadFile

// Manager loads and persists per-provider settings from JSON files in the data
// directory. It keeps settings in memory and is safe for concurrent use.
type Manager struct {
	dir        string
	mu         sync.RWMutex
	settings   map[string]ProviderSettings
	rrCounters map[string]*atomic.Uint64
	group      singleflight.Group
}

// NewManager creates a Manager that stores settings under dataDir.
func NewManager(dataDir string) *Manager {
	dir := filepath.Join(dataDir, "provider-settings")
	_ = os.MkdirAll(dir, 0o755)

	m := &Manager{
		dir:        dir,
		settings:   make(map[string]ProviderSettings),
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
// when no file exists yet. Concurrent first-time reads for the same provider are
// collapsed into a single disk load via singleflight.
func (m *Manager) Get(providerID string) (ProviderSettings, error) {
	m.mu.RLock()
	gs, ok := m.settings[providerID]
	m.mu.RUnlock()
	if ok {
		return gs, nil
	}

	v, err, _ := m.group.Do(providerID, func() (interface{}, error) {
		m.mu.RLock()
		gs, ok := m.settings[providerID]
		m.mu.RUnlock()
		if ok {
			return gs, nil
		}

		data, err := readFileHook(m.settingsPath(providerID))
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
	})
	if err != nil {
		return ProviderSettings{RoutingMode: DefaultRoutingMode}, err
	}
	return v.(ProviderSettings), nil
}

// RoutingMode returns the effective routing mode for a provider.
func (m *Manager) RoutingMode(providerID string) RoutingMode {
	s, err := m.Get(providerID)
	if err != nil {
		return DefaultRoutingMode
	}
	return s.RoutingMode
}

// Holdback returns the streaming holdback window (ms) and byte limit for a
// provider. Environment variables AXON_RESPONSES_HOLDBACK_MS and
// AXON_RESPONSES_HOLDBACK_BYTES take precedence, then per-provider settings
// holdback_ms / holdback_bytes, then defaults.
func (m *Manager) Holdback(providerID string) (ms, bytes int) {
	ms, bytes = DefaultHoldbackMs, DefaultHoldbackBytes

	s, err := m.Get(providerID)
	if err == nil {
		if s.HoldbackMs != nil && *s.HoldbackMs >= 0 {
			ms = *s.HoldbackMs
		}
		if s.HoldbackBytes != nil && *s.HoldbackBytes >= 0 {
			bytes = *s.HoldbackBytes
		}
	}

	if v := os.Getenv("AXON_RESPONSES_HOLDBACK_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ms = n
		}
	}
	if v := os.Getenv("AXON_RESPONSES_HOLDBACK_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			bytes = n
		}
	}

	return ms, bytes
}

// FlatRate returns whether the provider is billed as a flat monthly/service
// subscription rather than per-token. When true, display surfaces report $0.
func (m *Manager) FlatRate(providerID string) bool {
	s, err := m.Get(providerID)
	if err != nil {
		return false
	}
	return s.FlatRate
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
