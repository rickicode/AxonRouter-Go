package combo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
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

// ErrNoEligibleConnection is returned when no eligible connection exists for a requested model.
var ErrNoEligibleConnection = fmt.Errorf("no eligible connection")

// IsValidStrategy reports whether a combo strategy name is supported.
func IsValidStrategy(s string) bool { return ValidStrategies[s] }

// Handler manages combo resolution and routing.
type Handler struct {
	mu         sync.RWMutex
	db         *sql.DB
	writeQueue *db.WriteQueue
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

// NewHandler creates a new combo handler. The optional writeQueue serializes
// combo mutations through a single DB writer to avoid SQLite lock contention.
func NewHandler(
	database *sql.DB,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	writeQueue ...*db.WriteQueue,
) *Handler {
	h := &Handler{
		db:          database,
		writeQueue:  nil,
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
	if len(writeQueue) > 0 {
		h.writeQueue = writeQueue[0]
	}
	if err := h.loadFromDB(); err != nil {
		log.Printf("WARN: combo handler failed to load combos from DB: %v", err)
	}
	return h
}

// loadFromDB loads all combos into memory. If the database read fails, the
// previously populated maps are left untouched so callers never operate on
// partially-initialised data.
func (h *Handler) loadFromDB() error {
	combos, byName, smartCombos, steps, err := h.snapshotFromDB()
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.combos = combos
	h.byName = byName
	h.smartCombos = smartCombos
	h.steps = steps
	return nil
}

// snapshotFromDB reads combos and steps from the database WITHOUT holding h.mu.
// On failure it returns an error; the accompanying maps may be nil or partial
// and should only be used when err is nil.
func (h *Handler) snapshotFromDB() (map[string]*db.Combo, map[string]*db.Combo, map[string]*db.Combo, map[string][]db.ComboStep, error) {
	combos := make(map[string]*db.Combo)
	byName := make(map[string]*db.Combo)
	smartCombos := make(map[string]*db.Combo)
	steps := make(map[string][]db.ComboStep)

	rows, err := h.db.Query(`
	SELECT id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal,
	fusion_config, is_active, created_at, updated_at
	FROM combos WHERE is_active = 1
	`)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("query combos: %w", err)
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
		return nil, nil, nil, nil, fmt.Errorf("iterate combos: %w", err)
	}

	steps = h.loadAllSteps(combos)

	return combos, byName, smartCombos, steps, nil
}

// loadAllSteps fetches all combo steps in a single query and buckets them by comboID.
func (h *Handler) loadAllSteps(combos map[string]*db.Combo) map[string][]db.ComboStep {
	steps := make(map[string][]db.ComboStep, len(combos))
	if len(combos) == 0 {
		return steps
	}

	ids := make([]string, 0, len(combos))
	for id := range combos {
		ids = append(ids, id)
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := h.db.Query(`
		SELECT id, combo_id, connection_id, model_id, priority, weight, created_at
		FROM combo_steps WHERE combo_id IN (`+placeholders+`) ORDER BY priority ASC
	`, args...)
	if err != nil {
		log.Printf("WARN: failed to bulk-load combo steps: %v", err)
		return steps
	}
	defer rows.Close()

	for rows.Next() {
		s := db.ComboStep{}
		if err := rows.Scan(&s.ID, &s.ComboID, &s.ConnectionID, &s.ModelID,
			&s.Priority, &s.Weight, &s.CreatedAt); err != nil {
			log.Printf("WARN: failed to scan combo_step row: %v", err)
			continue
		}
		steps[s.ComboID] = append(steps[s.ComboID], s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("WARN: combo_step bulk iteration error: %v", err)
	}
	return steps
}

// loadStepsForCombo loads steps for a combo from DB without touching h.mu.
func (h *Handler) loadStepsForCombo(comboID string) []db.ComboStep {
	rows, err := h.db.Query(`
		SELECT id, combo_id, connection_id, model_id, priority, weight, created_at
		FROM combo_steps WHERE combo_id = ? ORDER BY priority ASC
	`, comboID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var steps []db.ComboStep
	for rows.Next() {
		s := db.ComboStep{}
		if err := rows.Scan(&s.ID, &s.ComboID, &s.ConnectionID, &s.ModelID,
			&s.Priority, &s.Weight, &s.CreatedAt); err != nil {
			log.Printf("WARN: failed to scan combo_step row for combo %s: %v", comboID, err)
			continue
		}
		steps = append(steps, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("WARN: combo_step rows iteration error for combo %s: %v", comboID, err)
	}
	return steps
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
		// Copy the combo value before releasing the lock so callers cannot observe
		// concurrent mutations to the shared cache object after Resolve returns.
		comboCopy := *c
		h.mu.RUnlock()
		if len(steps) == 0 {
			return nil, false
		}
		return &ComboResult{Combo: &comboCopy, Steps: steps}, true
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
	// Copy the combo value before releasing the lock so callers cannot observe
	// concurrent mutations to the shared cache object after Resolve returns.
	comboCopy := *combo
	h.mu.RUnlock()

	if len(steps) == 0 {
		return nil, false
	}
	return &ComboResult{Combo: &comboCopy, Steps: steps}, true
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

// stepInsert is an internal plan for inserting combo steps.
type stepInsert struct {
	stepID       string
	connectionID string
	modelID      string
	priority     int
	weight       int
}

// doWrite runs fn through the centralized write queue when configured, otherwise
// falls back to a direct DB call. This keeps tests simple while letting production
// serialize writes.
func (h *Handler) doWrite(label string, fn func(*sql.DB) error) error {
	if h.writeQueue != nil {
		return h.writeQueue.Do(context.Background(), label, fn)
	}
	return fn(h.db)
}

// insertComboDB writes a new combo and its steps in a single transaction.
func insertComboDB(d *sql.DB, combo *db.Combo, steps []stepInsert) error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("begin combo transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
	INSERT INTO combos (id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal, fusion_config, is_active, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, combo.ID, combo.Name, combo.Strategy, combo.StickyLimit,
		combo.TimeoutMs, boolToInt(combo.IsSmart), combo.SmartGoal, combo.FusionConfig,
		combo.CreatedAt, combo.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create combo: %w", err)
	}

	for _, s := range steps {
		_, err := tx.Exec(`
		INSERT INTO combo_steps (id, combo_id, connection_id, model_id, priority, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`, s.stepID, combo.ID, s.connectionID, s.modelID, s.priority, s.weight, combo.CreatedAt)
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

	// Pick connections outside the mutex; PickConnection only touches eligibility
	// and connection state, both of which are safe for concurrent use.
	var inserts []stepInsert
	for _, s := range steps {
		connID := s.ConnectionID
		if connID == "" {
			if picked, ok := h.PickConnection(db.ComboStep{ModelID: s.ModelID}); ok {
				connID = picked
			}
		}
		if connID == "" {
			return nil, fmt.Errorf("%w for model %s", ErrNoEligibleConnection, s.ModelID)
		}
		inserts = append(inserts, stepInsert{
			stepID:       uuid.New().String(),
			connectionID: connID,
			modelID:      s.ModelID,
			priority:     s.Priority,
			weight:       s.Weight,
		})
	}

	combo := &db.Combo{
		ID:           comboID,
		Name:         name,
		Strategy:     strategy,
		StickyLimit:    stickyLimit,
		TimeoutMs:      timeoutMs,
		IsSmart:        isSmart,
		SmartGoal:      sg,
		FusionConfig:   fusionConfig,
		IsActive:       true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := h.doWrite("combo.create", func(d *sql.DB) error {
		return insertComboDB(d, combo, inserts)
	}); err != nil {
		return nil, err
	}

	// Build the in-memory step list from what we just persisted.
	var savedSteps []db.ComboStep
	for _, s := range inserts {
		savedSteps = append(savedSteps, db.ComboStep{
			ID:           s.stepID,
			ComboID:      comboID,
			ConnectionID: s.connectionID,
			ModelID:      s.modelID,
			Priority:     s.priority,
			Weight:       s.weight,
			CreatedAt:    now,
		})
	}

	// Update cache under a brief lock; no DB work is performed here.
	h.mu.Lock()
	defer h.mu.Unlock()
	h.combos[comboID] = combo
	h.byName[name] = combo
	if isSmart {
		h.smartCombos[comboID] = combo
	}
	h.steps[comboID] = savedSteps
	return combo, nil
}

// deleteComboDB removes a combo and all related state in a single transaction.
func deleteComboDB(d *sql.DB, comboID string) error {
	tx, err := d.Begin()
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
	if err := h.doWrite("combo.delete", func(d *sql.DB) error {
		return deleteComboDB(d, comboID)
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

// UpdateComboInput holds mutable combo fields for UpdateCombo.
type UpdateComboInput struct {
	Name         string
	Strategy     string
	TimeoutMs    *int
	StickyLimit  *int
	IsSmart      *bool
	SmartGoal    *string
	FusionConfig string
	IsActive     *bool
}

// allowedComboUpdateColumns is the whitelist of columns that UpdateCombo may
// mutate. The clause templates contain only hardcoded identifiers and "?"
// placeholders so user input is never interpolated into SQL.
var allowedComboUpdateColumns = map[string]string{
	"name":          "name = ?",
	"strategy":      "strategy = ?",
	"timeout_ms":    "timeout_ms = ?",
	"sticky_limit":  "sticky_limit = ?",
	"is_smart":      "is_smart = ?",
	"smart_goal":    "smart_goal = ?",
	"fusion_config": "fusion_config = ?",
	"is_active":     "is_active = ?",
}

// comboUpdateClause returns a safe SET clause for col. It panics for unknown
// columns; callers must only pass names from allowedComboUpdateColumns.
func comboUpdateClause(col string) string {
	if clause, ok := allowedComboUpdateColumns[col]; ok {
		return clause
	}
	panic("invalid combo update column: " + col)
}

// UpdateCombo updates a combo's mutable fields and refreshes the in-memory cache.
func (h *Handler) UpdateCombo(comboID string, input UpdateComboInput) error {
	if comboID == "" {
		return fmt.Errorf("combo id required")
	}

	// Build the SET list from the allowlisted column templates. Values are kept
	// separate in args and bound via placeholders, so user input never reaches
	// the SQL string.
	sets := make([]string, 0, 9)
	args := make([]interface{}, 0, 10)
	add := func(col string, value interface{}) {
		sets = append(sets, comboUpdateClause(col))
		args = append(args, value)
	}

	if input.Name != "" {
		add("name", input.Name)
	}
	if input.Strategy != "" {
		add("strategy", input.Strategy)
	}
	if input.TimeoutMs != nil {
		add("timeout_ms", *input.TimeoutMs)
	}
	if input.StickyLimit != nil {
		add("sticky_limit", *input.StickyLimit)
	}
	if input.IsSmart != nil {
		add("is_smart", boolToInt(*input.IsSmart))
	}
	if input.SmartGoal != nil {
		normalized := normalizeSmartGoal(*input.SmartGoal)
		sg := sql.NullString{}
		if normalized != "" {
			sg = sql.NullString{String: normalized, Valid: true}
		}
		add("smart_goal", sg)
	}
	if input.FusionConfig != "" {
		add("fusion_config", input.FusionConfig)
	}
	if input.IsActive != nil {
		add("is_active", boolToInt(*input.IsActive))
	}
	if len(sets) == 0 {
		return fmt.Errorf("nothing to update")
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, db.UnixNow(), comboID)

	var rowsAffected int64
	if err := h.doWrite("combo.update", func(d *sql.DB) error {
		query := "UPDATE combos SET " + strings.Join(sets, ", ") + " WHERE id = ?"
		result, err := d.Exec(query, args...)
		if err != nil {
			return err
		}
		rowsAffected, _ = result.RowsAffected()
		return nil
	}); err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Refresh the specific combo in memory without a full DB reload.
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.combos[comboID]
	if !ok {
		return nil
	}
	oldName := c.Name
	if input.Name != "" {
		c.Name = input.Name
	}
	if input.Strategy != "" {
		c.Strategy = input.Strategy
	}
	if input.TimeoutMs != nil {
		c.TimeoutMs = *input.TimeoutMs
	}
	if input.StickyLimit != nil {
		c.StickyLimit = *input.StickyLimit
	}
	if input.IsSmart != nil {
		c.IsSmart = *input.IsSmart
	}
	if input.SmartGoal != nil {
		goal := normalizeSmartGoal(*input.SmartGoal)
		if goal == "" {
			c.SmartGoal = sql.NullString{}
		} else {
			c.SmartGoal = sql.NullString{String: goal, Valid: true}
		}
	}
	if input.FusionConfig != "" {
		c.FusionConfig = input.FusionConfig
	}
	if input.IsActive != nil {
		c.IsActive = *input.IsActive
	}
	if c.Name != oldName {
		delete(h.byName, oldName)
		h.byName[c.Name] = c
	}
	if c.IsSmart {
		h.smartCombos[comboID] = c
	} else {
		delete(h.smartCombos, comboID)
	}
	return nil
}

// AddComboStepInput holds data for adding a step to an existing combo.
type AddComboStepInput struct {
	ConnectionID string
	ModelID      string
	Priority     int
	Weight       int
}

// AddComboStep inserts a step and loads the combo's steps back into memory.
func (h *Handler) AddComboStep(comboID string, input AddComboStepInput) (string, error) {
	connectionID := input.ConnectionID
	if connectionID == "" {
		if picked, ok := h.PickConnection(db.ComboStep{ModelID: input.ModelID}); ok {
			connectionID = picked
		}
	}
	if connectionID == "" {
		return "", fmt.Errorf("%w for model %s", ErrNoEligibleConnection, input.ModelID)
	}

	stepID := uuid.New().String()
	now := db.UnixNow()
	if err := h.doWrite("combo.addStep", func(d *sql.DB) error {
		_, err := d.Exec(`
		INSERT INTO combo_steps (id, combo_id, connection_id, model_id, priority, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`, stepID, comboID, connectionID, input.ModelID, input.Priority, input.Weight, now)
		return err
	}); err != nil {
		return "", err
	}

	// Reload steps for this combo under a brief lock.
	loaded := h.loadStepsForCombo(comboID)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps[comboID] = loaded
	return stepID, nil
}

// RemoveComboStep deletes a step by ID and refreshes its combo steps in memory.
func (h *Handler) RemoveComboStep(stepID string) error {
	var comboID string
	if err := h.doWrite("combo.removeStep", func(d *sql.DB) error {
		row := d.QueryRow(`SELECT combo_id FROM combo_steps WHERE id = ?`, stepID)
		if err := row.Scan(&comboID); err != nil {
			return err
		}
		_, err := d.Exec(`DELETE FROM combo_steps WHERE id = ?`, stepID)
		return err
	}); err != nil {
		return err
	}

	loaded := h.loadStepsForCombo(comboID)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps[comboID] = loaded
	return nil
}

// RefreshFromDB reloads combos from the database without blocking Resolve().
// On failure the in-memory cache is left unchanged.
func (h *Handler) RefreshFromDB() error {
	combos, byName, smartCombos, steps, err := h.snapshotFromDB()
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.combos = combos
	h.byName = byName
	h.smartCombos = smartCombos
	h.steps = steps
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
