package combo

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// ComboResult holds the resolved combo steps to try.
type ComboResult struct {
	Combo *db.Combo
	Steps []db.ComboStep
}

// Handler manages combo resolution and routing.
type Handler struct {
	mu       sync.RWMutex
	db       *sql.DB
	rotation *RotationManager
	smart    *SmartCombo
	fallback *FallbackManager
	store    *connstate.Store
	elig     *connstate.EligibilityManager

	// In-memory combo cache
	combos map[string]*db.Combo
	steps  map[string][]db.ComboStep // comboID → steps
}

// NewHandler creates a new combo handler.
func NewHandler(
	database *sql.DB,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
) *Handler {
	h := &Handler{
		db:       database,
		rotation: NewRotationManager(database),
		smart:    NewSmartCombo(database),
		fallback: NewFallbackManager(),
		store:    store,
		elig:     elig,
		combos:   make(map[string]*db.Combo),
		steps:    make(map[string][]db.ComboStep),
	}
	h.loadFromDB()
	return h
}

// loadFromDB loads all combos into memory.
func (h *Handler) loadFromDB() {
	rows, err := h.db.Query(`
		SELECT id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal,
		       is_active, created_at, updated_at
		FROM combos WHERE is_active = 1
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		c := &db.Combo{}
		rows.Scan(&c.ID, &c.Name, &c.Strategy, &c.StickyLimit,
			&c.TimeoutMs, &c.IsSmart, &c.SmartGoal,
			&c.IsActive, &c.CreatedAt, &c.UpdatedAt)
		h.combos[c.ID] = c
	}

	// Load steps
	for comboID := range h.combos {
		h.loadSteps(comboID)
	}
}

// loadSteps loads steps for a combo from DB.
func (h *Handler) loadSteps(comboID string) {
	rows, err := h.db.Query(`
		SELECT id, combo_id, connection_id, model_id, priority, weight, created_at
		FROM combo_steps WHERE combo_id = ? ORDER BY priority ASC
	`, comboID)
	if err != nil {
		return
	}
	defer rows.Close()

	var steps []db.ComboStep
	for rows.Next() {
		s := db.ComboStep{}
		rows.Scan(&s.ID, &s.ComboID, &s.ConnectionID, &s.ModelID,
			&s.Priority, &s.Weight, &s.CreatedAt)
		steps = append(steps, s)
	}
	h.steps[comboID] = steps
}

// Resolve resolves a model string to combo steps.
// Returns (combo, steps, true) if it's a combo, or (nil, nil, false) if it's a single model.
func (h *Handler) Resolve(modelStr string) (*ComboResult, bool) {
	// Check smart combos first
	if goal, ok := isSmartCombo(modelStr); ok {
		return h.resolveSmart(goal)
	}

	// Check regular combos
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.combos {
		if c.Name == modelStr {
			steps := h.steps[c.ID]
			if len(steps) == 0 {
				continue
			}
			// Apply rotation
			rotated := h.rotation.GetRotatedSteps(c.ID, c.Strategy, c.StickyLimit, steps)
			return &ComboResult{Combo: c, Steps: rotated}, true
		}
	}
	return nil, false
}

// resolveSmart resolves a smart combo goal to actual combo steps.
func (h *Handler) resolveSmart(goal SmartGoal) (*ComboResult, bool) {
	telemetry := h.smart.GetTelemetry(60)
	combo, err := h.smart.Resolve(goal, telemetry)
	if err != nil || combo == nil {
		return nil, false
	}

	h.mu.RLock()
	steps := h.steps[combo.ID]
	h.mu.RUnlock()

	if len(steps) == 0 {
		return nil, false
	}
	rotated := h.rotation.GetRotatedSteps(combo.ID, combo.Strategy, combo.StickyLimit, steps)
	return &ComboResult{Combo: combo, Steps: rotated}, true
}

// PickConnection picks the next eligible connection for a combo step.
// Returns (connectionID, true) if found, or ("", false) if unavailable.
func (h *Handler) PickConnection(step db.ComboStep) (string, bool) {
	prefix, _ := splitModel(step.ModelID)
	if prefix == "" {
		return "", false
	}

	cs := h.elig.PickConnection(prefix, step.ModelID)
	if cs == nil {
		return "", false
	}

	if h.fallback.CanUseConnection(cs) {
		return cs.ID, true
	}
	return "", false
}

// RecordSuccess records a successful request for a connection.
func (h *Handler) RecordSuccess(connID string) {
	h.fallback.RecordSuccess(connID)
	h.store.RecordSuccess(connID)
}

// RecordFailure records a failed request for a connection.
func (h *Handler) RecordFailure(connID string, statusCode int, body string) {
	h.fallback.RecordFailure(connID)
	det := connstate.DetectError(statusCode, body, nil, "", "", nil)
	if det.Category != connstate.ErrorNone {
		h.store.RecordFailure(connID, det)
	}
}

// CreateCombo creates a new combo.
func (h *Handler) CreateCombo(name, strategy string, timeoutMs int, steps []CreateStepInput) (*db.Combo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	comboID := uuid.New().String()
	now := db.UnixNow()

	_, err := h.db.Exec(`
		INSERT INTO combos (id, name, strategy, sticky_limit, timeout_ms, is_smart, is_active, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, 0, 1, ?, ?)
	`, comboID, name, strategy, timeoutMs, now, now)
	if err != nil {
		return nil, fmt.Errorf("create combo: %w", err)
	}

	for _, s := range steps {
		h.db.Exec(`
			INSERT INTO combo_steps (id, combo_id, connection_id, model_id, priority, weight, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, uuid.New().String(), comboID, s.ConnectionID, s.ModelID, s.Priority, s.Weight, now)
	}

	combo := &db.Combo{
		ID:        comboID,
		Name:      name,
		Strategy:  strategy,
		TimeoutMs: timeoutMs,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	h.combos[comboID] = combo
	h.loadSteps(comboID)
	return combo, nil
}

// DeleteCombo removes a combo.
func (h *Handler) DeleteCombo(comboID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.db.Exec(`DELETE FROM combo_steps WHERE combo_id = ?`, comboID)
	h.db.Exec(`DELETE FROM rotation_state WHERE combo_id = ?`, comboID)
	_, err := h.db.Exec(`DELETE FROM combos WHERE id = ?`, comboID)
	if err != nil {
		return err
	}
	delete(h.combos, comboID)
	delete(h.steps, comboID)
	return nil
}

// ListCombos returns all active combos with their steps.
func (h *Handler) ListCombos() []ComboWithSteps {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var result []ComboWithSteps
	for _, c := range h.combos {
		result = append(result, ComboWithSteps{
			Combo: *c,
			Steps: h.steps[c.ID],
		})
	}
	return result
}

// GetCombo returns a combo by ID.
func (h *Handler) GetCombo(comboID string) (*ComboWithSteps, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.combos[comboID]
	if !ok {
		return nil, false
	}
	return &ComboWithSteps{Combo: *c, Steps: h.steps[comboID]}, true
}

// CreateStepInput holds data for creating a combo step.
type CreateStepInput struct {
	ConnectionID string
	ModelID      string
	Priority     int
	Weight       int
}

// ComboWithSteps pairs a combo with its steps.
type ComboWithSteps struct {
	Combo db.Combo       `json:"combo"`
	Steps []db.ComboStep `json:"steps"`
}

// isSmartCombo checks if a model string is a smart combo goal.
func isSmartCombo(s string) (SmartGoal, bool) {
	switch s {
	case "auto", "smart/auto":
		return GoalAuto, true
	case "economy", "smart/economy":
		return GoalEconomy, true
	case "balanced", "smart/balanced":
		return GoalBalanced, true
	case "premium", "smart/premium":
		return GoalPremium, true
	}
	return "", false
}

// splitModel splits "provider/model" into (provider, model).
func splitModel(modelStr string) (string, string) {
	for i, c := range modelStr {
		if c == '/' {
			return modelStr[:i], modelStr[i+1:]
		}
	}
	return "", modelStr
}

// CleanupBreakers removes circuit breakers for inactive connections.
func (h *Handler) CleanupBreakers() {
	active := make(map[string]bool)
	h.store.RangeByConnID(func(connID string, cs *connstate.ConnectionState) bool {
		active[connID] = true
		return true
	})
	h.fallback.Cleanup(active)
}

// RefreshFromDB reloads combos from the database.
func (h *Handler) RefreshFromDB() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.combos = make(map[string]*db.Combo)
	h.steps = make(map[string][]db.ComboStep)
	h.loadFromDB()
}
