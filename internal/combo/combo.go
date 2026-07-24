package combo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// ComboResult holds the resolved combo steps to try.
type ComboResult struct {
	Combo *db.Combo
	Steps []db.ComboStep
}

const strategySettingsTTL = 5 * time.Second

// ValidStrategies is the set of strategies supported by combos.
var ValidStrategies = map[string]bool{
	"priority":    true,
	"round-robin": true,
	"weighted":    true,
	"random":      true,
	"least-used":  true,
	"fusion":      true,
}

// IsValidStrategy reports whether a combo strategy name is supported.
func IsValidStrategy(s string) bool { return ValidStrategies[s] }

// Handler manages combo resolution and routing.
type Handler struct {
	mu         sync.RWMutex
	db         *sql.DB
	writeQueue *db.WriteQueue // nil → direct DB writes (queue optional for tests)
	rotation   *RotationManager
	smart      *SmartCombo
	fallback   *FallbackManager
	store      *connstate.Store
	elig       *connstate.EligibilityManager

	// In-memory combo cache
	combos      map[string]*db.Combo
	byName      map[string]*db.Combo      // combo name → combo (O(1) resolve by name)
	steps       map[string][]db.ComboStep // comboID → steps
	smartCombos map[string]*db.Combo      // comboID → smart combo

	// In-memory strategy overrides (combo_strategies) and default (combo_strategy).
	strategyMu        sync.RWMutex
	strategyLoadedAt  time.Time
	strategyDefault   string
	strategyOverrides map[string]string
}

// NewHandler creates a new combo handler.
// writeQueue may be nil; when nil, combo mutations fall back to direct DB
// transactions so tests and other callers can run without a queue.
func NewHandler(
	database *sql.DB,
	writeQueue *db.WriteQueue,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
) *Handler {
	h := &Handler{
		db:          database,
		writeQueue:  writeQueue,
		rotation:    NewRotationManager(database),
		smart:       NewSmartCombo(database),
		fallback:    NewFallbackManager(),
		store:       store,
		elig:        elig,
		combos:      make(map[string]*db.Combo),
		byName:      make(map[string]*db.Combo),
		steps:       make(map[string][]db.ComboStep),
		smartCombos: make(map[string]*db.Combo),
	}
	h.loadFromDB()
	return h
}

// loadFromDB loads all combos into the handler's in-memory maps.
func (h *Handler) loadFromDB() {
	h.loadInto(h.combos, h.byName, h.smartCombos, h.steps)
}

// loadInto reads combos/steps from the DB into the supplied maps. Callers must
// synchronize access to the maps; this function performs only read-only DB work.
func (h *Handler) loadInto(combos map[string]*db.Combo, byName map[string]*db.Combo, smartCombos map[string]*db.Combo, steps map[string][]db.ComboStep) {
	rows, err := h.db.Query(`
	SELECT id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal,
	fusion_config, is_active, created_at, updated_at
	FROM combos WHERE is_active = 1
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		c := &db.Combo{}
		var fusionConfig sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Strategy, &c.StickyLimit,
			&c.TimeoutMs, &c.IsSmart, &c.SmartGoal, &fusionConfig,
			&c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
			log.Printf("WARN: failed to scan combo row: %v", err)
			continue
		}
		c.FusionConfig = fusionConfig.String
		combos[c.ID] = c
		byName[c.Name] = c
		if c.IsSmart {
			smartCombos[c.ID] = c
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("WARN: combo rows iteration error: %v", err)
	}

	// Load steps
	for comboID := range combos {
		h.loadStepsInto(comboID, steps)
	}
}

// loadStepsInto reads steps for a combo from the DB into the supplied map.
func (h *Handler) loadStepsInto(comboID string, steps map[string][]db.ComboStep) {
	rows, err := h.db.Query(`
		SELECT id, combo_id, connection_id, model_id, priority, weight, created_at
		FROM combo_steps WHERE combo_id = ? ORDER BY priority ASC
	`, comboID)
	if err != nil {
		return
	}
	defer rows.Close()

	var comboSteps []db.ComboStep
	for rows.Next() {
		s := db.ComboStep{}
		if err := rows.Scan(&s.ID, &s.ComboID, &s.ConnectionID, &s.ModelID,
			&s.Priority, &s.Weight, &s.CreatedAt); err != nil {
			log.Printf("WARN: failed to scan combo_step row for combo %s: %v", comboID, err)
			continue
		}
		comboSteps = append(comboSteps, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("WARN: combo_step rows iteration error for combo %s: %v", comboID, err)
	}
	steps[comboID] = comboSteps
}

// Resolve resolves a model string to combo steps.
// Returns (combo, steps, true) if it's a combo, or (nil, nil, false) if it's a single model.
func (h *Handler) Resolve(modelStr string) (*ComboResult, bool) {
	// Check regular combos first so names like "balanced" / "economy" / "premium"
	// resolve to the combo the user created, not to a smart goal keyword.
	h.mu.RLock()
	c, ok := h.byName[modelStr]
	if ok {
		steps := h.steps[c.ID]
		h.mu.RUnlock()
		if len(steps) == 0 {
			return nil, false
		}
		return &ComboResult{Combo: c, Steps: steps}, true
	}
	h.mu.RUnlock()

	// No regular combo matched; check smart combo goals.
	if goal, ok := isSmartCombo(modelStr); ok {
		return h.resolveSmart(goal)
	}
	return nil, false
}

// resolveSmart resolves a smart combo goal to actual combo steps.
func (h *Handler) resolveSmart(goal SmartGoal) (*ComboResult, bool) {
	h.mu.RLock()
	combos := make([]*db.Combo, 0, len(h.smartCombos))
	for _, c := range h.smartCombos {
		combos = append(combos, c)
	}
	h.mu.RUnlock()

	sort.Slice(combos, func(i, j int) bool { return combos[i].Name < combos[j].Name })

	combo, err := h.smart.Resolve(goal, combos)
	if err != nil || combo == nil {
		return nil, false
	}

	h.mu.RLock()
	steps := h.steps[combo.ID]
	h.mu.RUnlock()

	if len(steps) == 0 {
		return nil, false
	}
	return &ComboResult{Combo: combo, Steps: steps}, true
}

// loadStrategySettings reads combo_strategy / combo_strategies from DB once
// per TTL. Invalid values are ignored. It is safe to call repeatedly.
func (h *Handler) loadStrategySettings() {
	h.strategyMu.Lock()
	defer h.strategyMu.Unlock()

	if !h.strategyLoadedAt.IsZero() && time.Since(h.strategyLoadedAt) < strategySettingsTTL {
		return
	}

	defaultStr := "priority"
	var defaultVal string
	row := h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, "combo_strategy")
	if err := row.Scan(&defaultVal); err == nil && defaultVal != "" && IsValidStrategy(defaultVal) {
		defaultStr = defaultVal
	}
	h.strategyDefault = defaultStr

	overrides := make(map[string]string)
	var raw string
	row = h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, "combo_strategies")
	if err := row.Scan(&raw); err == nil && raw != "" {
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			for name, strat := range m {
				if IsValidStrategy(strat) {
					overrides[name] = strat
				}
			}
		}
	}
	h.strategyOverrides = overrides
	h.strategyLoadedAt = time.Now()
}

// RefreshStrategySettings forces strategy settings to be reloaded on the next
// EffectiveStrategy call. Call this after settings are changed.
func (h *Handler) RefreshStrategySettings() {
	h.strategyMu.Lock()
	h.strategyLoadedAt = time.Time{}
	h.strategyMu.Unlock()
}

// DefaultStrategy returns the global default combo strategy from settings.
func (h *Handler) DefaultStrategy() string {
	if h.db == nil {
		return "priority"
	}
	h.loadStrategySettings()
	h.strategyMu.RLock()
	defer h.strategyMu.RUnlock()
	if h.strategyDefault != "" {
		return h.strategyDefault
	}
	return "priority"
}

// EffectiveStrategy returns the strategy that should be used for a combo.
// A per-combo override in settings (`combo_strategies` JSON map) takes
// precedence over the combo's own strategy, which in turn falls back to the
// global `combo_strategy` setting when empty. Invalid override values are ignored.
func (h *Handler) EffectiveStrategy(comboName string, comboStrategy string) string {
	if h.db == nil {
		if IsValidStrategy(comboStrategy) {
			return comboStrategy
		}
		return "priority"
	}
	h.loadStrategySettings()
	h.strategyMu.RLock()
	defer h.strategyMu.RUnlock()

	base := comboStrategy
	if !IsValidStrategy(base) {
		base = "priority"
		if IsValidStrategy(h.strategyDefault) {
			base = h.strategyDefault
		}
	}
	if s, ok := h.strategyOverrides[comboName]; ok {
		if IsValidStrategy(s) {
			base = s
		}
	}
	return base
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

// PickConnections returns all eligible, breaker-ok connection IDs for a step's
// prefix/model, in eligibility-snapshot order (already shuffled for load
// balancing). The combo request path uses this to retry multiple connections
// within a single step before falling through to the next step, instead of
// picking just one connection and jumping straight to a different model.
func (h *Handler) PickConnections(prefix, modelID string) []string {
	conns := h.elig.GetByPrefix(prefix)
	out := make([]string, 0, len(conns))
	for _, id := range conns {
		cs := h.store.Get(id)
		if cs == nil {
			continue
		}
		if cs.IsInCooldown() {
			continue
		}
		if modelID != "" && cs.IsModelInCooldown(modelID) {
			continue
		}
		if !h.fallback.CanUseConnection(cs) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// RecordSuccess records a successful request for a connection.
func (h *Handler) RecordSuccess(connID string) {
	h.fallback.RecordSuccess(connID)
	h.store.RecordSuccess(connID)
}

// RecordFailure records a failed request for a connection.
func (h *Handler) RecordFailure(connID string, det connstate.ErrorDetection) {
	h.fallback.RecordFailure(connID)
	if det.Category != connstate.ErrorNone {
		h.store.RecordFailure(connID, det)
	}
}

// execWrite runs a DB write through the centralized WriteQueue when one is
// configured, otherwise executes it directly on the DB. This lets callers run
// with or without a queue.
func (h *Handler) execWrite(label string, fn func(*sql.DB) error) error {
	if h.writeQueue != nil {
		return h.writeQueue.Do(context.Background(), label, fn)
	}
	return fn(h.db)
}

// insertComboTx inserts a combo and its steps inside a single DB transaction.
func insertComboTx(database *sql.DB, combo *db.Combo, steps []db.ComboStep) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin combo transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
	INSERT INTO combos (id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal, fusion_config, is_active, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, combo.ID, combo.Name, combo.Strategy, combo.StickyLimit, combo.TimeoutMs,
		boolToInt(combo.IsSmart), combo.SmartGoal, combo.FusionConfig, combo.CreatedAt, combo.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create combo: %w", err)
	}

	for _, s := range steps {
		_, err := tx.Exec(`
		INSERT INTO combo_steps (id, combo_id, connection_id, model_id, priority, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`, s.ID, s.ComboID, s.ConnectionID, s.ModelID, s.Priority, s.Weight, s.CreatedAt)
		if err != nil {
			return fmt.Errorf("create combo step: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit combo transaction: %w", err)
	}
	return nil
}

// CreateCombo creates a new combo.
func (h *Handler) CreateCombo(name, strategy string, timeoutMs, stickyLimit int, isSmart bool, smartGoal string, fusionConfig string, steps []CreateStepInput) (*db.Combo, error) {
	comboID := uuid.New().String()
	now := db.UnixNow()

	sg := sql.NullString{}
	if normalized := normalizeSmartGoal(smartGoal); normalized != "" {
		sg = sql.NullString{String: normalized, Valid: true}
	}

	// Resolve connection IDs before touching the DB. This does not need h.mu.
	resolvedSteps := make([]db.ComboStep, 0, len(steps))
	for _, s := range steps {
		connID := s.ConnectionID
		if connID == "" {
			if picked, ok := h.PickConnection(db.ComboStep{ModelID: s.ModelID}); ok {
				connID = picked
			}
		}
		if connID == "" {
			return nil, fmt.Errorf("no eligible connection for model %s", s.ModelID)
		}
		resolvedSteps = append(resolvedSteps, db.ComboStep{
			ID:           uuid.New().String(),
			ComboID:      comboID,
			ConnectionID: connID,
			ModelID:      s.ModelID,
			Priority:     s.Priority,
			Weight:       s.Weight,
			CreatedAt:    now,
		})
	}

	combo := &db.Combo{
		ID:           comboID,
		Name:         name,
		Strategy:     strategy,
		StickyLimit:  stickyLimit,
		TimeoutMs:    timeoutMs,
		IsSmart:      isSmart,
		SmartGoal:    sg,
		FusionConfig: fusionConfig,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Run the insert through the WriteQueue if available, otherwise directly.
	// h.mu is intentionally not held during DB work so Resolve() stays unblocked.
	if err := h.execWrite("create-combo", func(database *sql.DB) error {
		return insertComboTx(database, combo, resolvedSteps)
	}); err != nil {
		return nil, err
	}

	h.mu.Lock()
	h.combos[comboID] = combo
	h.byName[combo.Name] = combo
	if isSmart {
		h.smartCombos[comboID] = combo
	}
	h.steps[comboID] = resolvedSteps
	h.mu.Unlock()

	return combo, nil
}

// deleteComboTx deletes a combo and its related rows inside a single DB transaction.
func deleteComboTx(database *sql.DB, comboID string) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin delete combo transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM combo_steps WHERE combo_id = ?`, comboID); err != nil {
		return fmt.Errorf("delete combo steps: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM rotation_state WHERE combo_id = ?`, comboID); err != nil {
		return fmt.Errorf("delete rotation state: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM combos WHERE id = ?`, comboID); err != nil {
		return fmt.Errorf("delete combo: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete combo transaction: %w", err)
	}
	return nil
}

// DeleteCombo removes a combo.
func (h *Handler) DeleteCombo(comboID string) error {
	if err := h.execWrite("delete-combo", func(database *sql.DB) error {
		return deleteComboTx(database, comboID)
	}); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if c, ok := h.combos[comboID]; ok {
		delete(h.byName, c.Name)
	}
	delete(h.combos, comboID)
	delete(h.steps, comboID)
	delete(h.smartCombos, comboID)
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
	s = normalizeSmartGoal(s)
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
// If the model identifier cannot be parsed, it returns ("", modelStr) so the
// original model string is preserved.
func splitModel(modelStr string) (string, string) {
	provider, model, ok := SplitProviderModel(modelStr)
	if ok {
		return provider, model
	}
	return "", modelStr
}

// RotateSteps applies the rotation strategy to the given steps. Callers use this
// after resolving an effective strategy override so the steps are ordered
// according to the strategy that will actually run.
func (h *Handler) RotateSteps(comboID, strategy string, stickyLimit int, steps []db.ComboStep) []db.ComboStep {
	return h.rotation.GetRotatedSteps(comboID, strategy, stickyLimit, steps)
}

// ResetRotationCounter clears the rotation counter for a combo in memory and DB.
func (h *Handler) ResetRotationCounter(comboID string) {
	h.rotation.ResetCounter(comboID)
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
	// Load into temporary maps first so the DB read does not hold h.mu.
	combos := make(map[string]*db.Combo)
	byName := make(map[string]*db.Combo)
	smartCombos := make(map[string]*db.Combo)
	steps := make(map[string][]db.ComboStep)
	h.loadInto(combos, byName, smartCombos, steps)

	h.mu.Lock()
	defer h.mu.Unlock()
	h.combos = combos
	h.byName = byName
	h.smartCombos = smartCombos
	h.steps = steps
}
