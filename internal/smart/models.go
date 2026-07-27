package smart

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// VirtualModelID values are the only smart-routable virtual models.
type VirtualModelID string

const (
	ModelAuto        VirtualModelID = "smart/auto"
	ModelAutoFast    VirtualModelID = "smart/auto-fast"
	ModelAutoQuality VirtualModelID = "smart/auto-quality"
)

var knownVirtualModels = map[VirtualModelID]bool{
	ModelAuto:        true,
	ModelAutoFast:    true,
	ModelAutoQuality: true,
}

// IsVirtualModel reports whether modelID is a known smart virtual model.
func IsVirtualModel(modelID string) bool {
	_, ok := knownVirtualModels[VirtualModelID(modelID)]
	return ok
}

// VirtualModel describes a candidate list and enable toggle for a virtual model.
type VirtualModel struct {
	ID        VirtualModelID `json:"id"`
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	Candidates []string      `json:"candidates"`
	Strategy  Strategy       `json:"strategy"`
	CreatedAt int64          `json:"created_at"`
	UpdatedAt int64          `json:"updated_at"`
}

// Strategy is the routing goal for a virtual model.
type Strategy string

const (
	StrategyBalanced Strategy = "balanced"
	StrategyFast     Strategy = "fast"
	StrategyQuality  Strategy = "quality"
)

var virtualModelDefaults = map[VirtualModelID]*VirtualModel{
	ModelAuto: {
		ID:         ModelAuto,
		Name:       "Smart Auto",
		Enabled:    true,
		Strategy:   StrategyBalanced,
		Candidates: []string{},
	},
	ModelAutoFast: {
		ID:         ModelAutoFast,
		Name:       "Smart Auto Fast",
		Enabled:    true,
		Strategy:   StrategyFast,
		Candidates: []string{},
	},
	ModelAutoQuality: {
		ID:         ModelAutoQuality,
		Name:       "Smart Auto Quality",
		Enabled:    true,
		Strategy:   StrategyQuality,
		Candidates: []string{},
	},
}

// Registry persists and caches virtual model configuration.
type Registry struct {
	mu       sync.RWMutex
	db       *sql.DB
	models   map[VirtualModelID]*VirtualModel
	loadedAt time.Time
	ttl      time.Duration
}

// NewRegistry creates a virtual model registry backed by db and cached in memory.
func NewRegistry(database *sql.DB) *Registry {
	r := &Registry{
		db:     database,
		models: make(map[VirtualModelID]*VirtualModel),
		ttl:    30 * time.Second,
	}
	if err := r.load(); err != nil && database != nil {
		// best effort; will retry on first access
	}
	return r
}

const registryCacheSettingKey = "smart_virtual_models"

// load reads virtual models from settings and populates the in-memory cache.
func (r *Registry) load() error {
	models := make(map[VirtualModelID]*VirtualModel)
	for id, def := range virtualModelDefaults {
		copy := *def
		copy.Candidates = make([]string, len(def.Candidates))
		copy.CreatedAt = db.UnixNow()
		copy.UpdatedAt = db.UnixNow()
		models[id] = &copy
	}
	if r.db == nil {
		r.mu.Lock()
		r.models = models
		r.loadedAt = time.Now()
		r.mu.Unlock()
		return nil
	}
	var raw string
	row := r.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, registryCacheSettingKey)
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			// seed defaults
			_ = r.saveLocked(models)
			r.mu.Lock()
			r.models = models
			r.loadedAt = time.Now()
			r.mu.Unlock()
			return nil
		}
		return err
	}
	var stored []*VirtualModel
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		// keep defaults if stored data is corrupt
		r.mu.Lock()
		r.models = models
		r.loadedAt = time.Now()
		r.mu.Unlock()
		return nil
	}
	for _, m := range stored {
		if m == nil {
			continue
		}
		if def, ok := models[m.ID]; ok {
			def.Enabled = m.Enabled
			if len(m.Candidates) > 0 {
				def.Candidates = m.Candidates
			}
			if m.Name != "" {
				def.Name = m.Name
			}
			if m.Strategy != "" {
				def.Strategy = m.Strategy
			}
			def.CreatedAt = m.CreatedAt
			def.UpdatedAt = m.UpdatedAt
		}
	}
	r.mu.Lock()
	r.models = models
	r.loadedAt = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *Registry) saveLocked(models map[VirtualModelID]*VirtualModel) error {
	if r.db == nil {
		return nil
	}
	list := make([]*VirtualModel, 0, len(models))
	for _, m := range models {
		list = append(list, m)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, registryCacheSettingKey, string(raw), db.UnixMilliNow())
	return err
}

// ensureLoaded refreshes the cache if stale.
func (r *Registry) ensureLoaded() {
	r.mu.RLock()
	stale := r.ttl > 0 && time.Since(r.loadedAt) > r.ttl
	r.mu.RUnlock()
	if stale {
		_ = r.load()
	}
}

// Get returns a virtual model configuration by id.
func (r *Registry) Get(id VirtualModelID) (*VirtualModel, bool) {
	r.ensureLoaded()
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[id]
	return m, ok
}

// List returns all virtual models.
func (r *Registry) List() []*VirtualModel {
	r.ensureLoaded()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*VirtualModel, 0, len(r.models))
	for _, m := range r.models {
		out = append(out, m)
	}
	return out
}

// Upsert creates or updates a virtual model. Only known IDs are accepted.
func (r *Registry) Upsert(m *VirtualModel) (*VirtualModel, error) {
	if m == nil {
		return nil, fmt.Errorf("virtual model is nil")
	}
	if _, ok := knownVirtualModels[m.ID]; !ok {
		return nil, fmt.Errorf("unknown virtual model %q", m.ID)
	}
	if m.Strategy == "" {
		m.Strategy = virtualModelDefaults[m.ID].Strategy
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := r.models[m.ID]
	if existing == nil {
		existing = &VirtualModel{ID: m.ID}
	}
	now := db.UnixNow()
	*existing = *m
	if existing.CreatedAt == 0 {
		existing.CreatedAt = now
	}
	existing.UpdatedAt = now
	if _, err := uuid.Parse(string(existing.ID)); err == nil {
		existing.UpdatedAt = db.UnixMilliNow()
	}
	r.models[m.ID] = existing
	if err := r.saveLocked(r.models); err != nil {
		return nil, err
	}
	r.loadedAt = time.Now()
	return existing, nil
}

// SetEnabled toggles a virtual model on/off.
func (r *Registry) SetEnabled(id VirtualModelID, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.models[id]
	if !ok {
		return fmt.Errorf("unknown virtual model %q", id)
	}
	m.Enabled = enabled
	m.UpdatedAt = db.UnixNow()
	return r.saveLocked(r.models)
}

// Refresh reloads configuration from the DB.
func (r *Registry) Refresh() {
	r.mu.Lock()
	r.loadedAt = time.Time{}
	r.mu.Unlock()
	_ = r.load()
}
