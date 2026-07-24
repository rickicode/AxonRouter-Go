package combo

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func newComboTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "combo-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func seedConnectionForCombo(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('combo-test','Combo Test','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?,'combo-test','c1','none','ready',1,?,?)`, id, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
}

func TestCreateCombo_AutoPicksConnection(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{
		ID:     "conn-1",
		Prefix: "openai",
		Status: connstate.StatusReady,
	}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("auto-conn", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	var connectionID string
	row := database.QueryRow(`SELECT connection_id FROM combo_steps WHERE combo_id = ?`, combo.ID)
	if err := row.Scan(&connectionID); err != nil {
		t.Fatalf("scan step: %v", err)
	}
	if connectionID != "conn-1" {
		t.Fatalf("connection_id = %q, want conn-1", connectionID)
	}
}

func TestCreateCombo_PersistsSmartAndStickyFields(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("premium-smart", "priority", 15000, 3, true, "premium", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}
	if combo.StickyLimit != 3 {
		t.Fatalf("StickyLimit = %d, want 3", combo.StickyLimit)
	}
	if !combo.IsSmart {
		t.Fatalf("IsSmart = false, want true")
	}
	if !combo.SmartGoal.Valid || combo.SmartGoal.String != "premium" {
		t.Fatalf("SmartGoal = %v, want premium", combo.SmartGoal)
	}

	var stickyLimit int
	var isSmart int
	var smartGoal sql.NullString
	row := database.QueryRow(`SELECT sticky_limit, is_smart, smart_goal FROM combos WHERE id = ?`, combo.ID)
	if err := row.Scan(&stickyLimit, &isSmart, &smartGoal); err != nil {
		t.Fatalf("scan db row: %v", err)
	}
	if stickyLimit != 3 {
		t.Fatalf("db sticky_limit = %d, want 3", stickyLimit)
	}
	if isSmart != 1 {
		t.Fatalf("db is_smart = %d, want 1", isSmart)
	}
	if !smartGoal.Valid || smartGoal.String != "premium" {
		t.Fatalf("db smart_goal = %v, want premium", smartGoal)
	}
}

// seedExtraConnection inserts another connection under the shared test provider type.
func seedExtraConnection(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?,'combo-test',?,'none','ready',1,?,?)`, id, id, now, now); err != nil {
		t.Fatalf("seed extra connection: %v", err)
	}
}

func TestRoundRobin_StickyLimitZeroDoesNotPanic(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{
		ID:     "conn-1",
		Prefix: "openai",
		Status: connstate.StatusReady,
	}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("rr-zero", "round-robin", 30000, 0, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	result, ok := h.Resolve(combo.Name)
	if !ok {
		t.Fatalf("Resolve returned false")
	}
	if len(result.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(result.Steps))
	}
	if result.Combo.Strategy != "round-robin" {
		t.Fatalf("strategy = %q, want round-robin", result.Combo.Strategy)
	}
}

func TestResolve_RegularComboShadowsSmartKeyword(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")
	seedExtraConnection(t, database, "conn-2")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{
		ID:     "conn-1",
		Prefix: "openai",
		Status: connstate.StatusReady,
	}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	regular, err := h.CreateCombo("balanced", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("create regular combo: %v", err)
	}
	_, err = h.CreateCombo("smart-balanced", "priority", 30000, 1, true, "balanced", "", []CreateStepInput{
		{ConnectionID: "conn-2", ModelID: "openai/gpt-4o-mini", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("create smart combo: %v", err)
	}

	result, ok := h.Resolve("balanced")
	if !ok {
		t.Fatalf("Resolve(balanced) returned false")
	}
	if result.Combo.ID != regular.ID {
		t.Fatalf("Resolve(balanced) picked combo %q, want regular combo %q", result.Combo.ID, regular.ID)
	}
}

func TestResolveSmart_DeterministicWhenMultipleCombosShareGoal(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")
	seedExtraConnection(t, database, "conn-2")

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	_, err := h.CreateCombo("z-economy", "priority", 30000, 1, true, "economy", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("create z-economy: %v", err)
	}
	_, err = h.CreateCombo("a-economy", "priority", 30000, 1, true, "economy", "", []CreateStepInput{
		{ConnectionID: "conn-2", ModelID: "openai/gpt-4o-mini", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("create a-economy: %v", err)
	}

	seen := map[string]int{}
	for i := 0; i < 20; i++ {
		result, ok := h.Resolve("economy")
		if !ok {
			t.Fatalf("Resolve(economy) returned false on iteration %d", i)
		}
		seen[result.Combo.Name]++
	}
	if len(seen) != 1 {
		t.Fatalf("expected deterministic smart selection, got %v", seen)
	}
	for name := range seen {
		if name != "a-economy" {
			t.Fatalf("expected a-economy (sorted first), got %q", name)
		}
	}
}

func TestRandomStrategy_ReturnsPermutation(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")
	seedConnectionForCombo(t, database, "conn-2")

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("random-combo", "random", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
		{ConnectionID: "conn-2", ModelID: "openai/gpt-4o-mini", Priority: 2, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	result, ok := h.Resolve(combo.Name)
	if !ok {
		t.Fatalf("Resolve failed for random combo")
	}
	if result.Combo.Strategy != "random" {
		t.Fatalf("strategy = %q, want random", result.Combo.Strategy)
	}

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		result, _ = h.Resolve(combo.Name)
		result.Steps = h.RotateSteps(result.Combo.ID, result.Combo.Strategy, result.Combo.StickyLimit, result.Steps)
		seen[result.Steps[0].ModelID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("random strategy never rotated to second model, got %v", seen)
	}
}

func TestLeastUsedStrategy_OrdersByRecentUsage(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")
	seedConnectionForCombo(t, database, "conn-2")

	// Seed some request logs so openai/gpt-4o has 3 successes and openai/gpt-4o-mini has 1.
	now := time.Now().UnixMilli()
	_, err := database.Exec(`
		INSERT INTO request_logs (id, timestamp, connection_id, provider_type_id, model_id, modality, stream, status_code, created_at)
		VALUES
			('r1', ?, 'conn-1', 'combo-test', 'gpt-4o', 'chat', 0, 200, ?),
			('r2', ?, 'conn-1', 'combo-test', 'gpt-4o', 'chat', 0, 200, ?),
			('r3', ?, 'conn-1', 'combo-test', 'gpt-4o', 'chat', 0, 200, ?),
			('r4', ?, 'conn-2', 'combo-test', 'gpt-4o-mini', 'chat', 0, 200, ?)
	`, now, now, now, now, now, now, now, now)
	if err != nil {
		t.Fatalf("seed request logs: %v", err)
	}

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("least-used-combo", "least-used", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "combo-test/gpt-4o", Priority: 1, Weight: 100},
		{ConnectionID: "conn-2", ModelID: "combo-test/gpt-4o-mini", Priority: 2, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	result, ok := h.Resolve(combo.Name)
	if !ok {
		t.Fatalf("Resolve failed for least-used combo")
	}
	if result.Combo.Strategy != "least-used" {
		t.Fatalf("strategy = %q, want least-used", result.Combo.Strategy)
	}
	result.Steps = h.RotateSteps(result.Combo.ID, result.Combo.Strategy, result.Combo.StickyLimit, result.Steps)
	if result.Steps[0].ModelID != "combo-test/gpt-4o-mini" {
		t.Fatalf("least-used should prefer lower-usage model, got %v", result.Steps)
	}
}

func TestRoundRobin_AdvancesOneStepPerRequest(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")
	seedConnectionForCombo(t, database, "conn-2")
	seedConnectionForCombo(t, database, "conn-3")

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("rr-three", "round-robin", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
		{ConnectionID: "conn-2", ModelID: "openai/gpt-4o-mini", Priority: 2, Weight: 100},
		{ConnectionID: "conn-3", ModelID: "openai/gpt-4.1", Priority: 3, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	wantOrder := []string{"openai/gpt-4o", "openai/gpt-4o-mini", "openai/gpt-4.1"}
	counts := map[string]int{}
	for i := 0; i < 30; i++ {
		result, ok := h.Resolve(combo.Name)
		if !ok {
			t.Fatalf("Resolve failed on iteration %d", i)
		}
		result.Steps = h.RotateSteps(result.Combo.ID, result.Combo.Strategy, result.Combo.StickyLimit, result.Steps)
		first := result.Steps[0].ModelID
		counts[first]++
		if first != wantOrder[i%len(wantOrder)] {
			t.Fatalf("first step on iteration %d = %q, want %q", i, first, wantOrder[i%len(wantOrder)])
		}
	}
	for _, modelID := range wantOrder {
		if counts[modelID] != 10 {
			t.Fatalf("first-step count for %s = %d, want 10 (all counts: %v)", modelID, counts[modelID], counts)
		}
	}
}

func TestCreateCombo_RollbackOnStepInsertFailure(t *testing.T) {
	database := newComboTestDB(t)
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	_, err := h.CreateCombo("rollback-combo", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err == nil {
		t.Fatalf("expected error for step without eligible connection")
	}

	var count int
	row := database.QueryRow(`SELECT COUNT(*) FROM combos WHERE name = ?`, "rollback-combo")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan combo count: %v", err)
	}
	if count != 0 {
		t.Fatalf("combo row count = %d, want 0 after rollback", count)
	}
}

func TestDeleteCombo_RemovesFromMemoryAndDB(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{
		ID:     "conn-1",
		Prefix: "openai",
		Status: connstate.StatusReady,
	}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("delete-me", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	if err := h.DeleteCombo(combo.ID); err != nil {
		t.Fatalf("DeleteCombo failed: %v", err)
	}

	if _, ok := h.Resolve("delete-me"); ok {
		t.Fatalf("Resolve returned true after delete")
	}

	var count int
	row := database.QueryRow(`SELECT COUNT(*) FROM combos WHERE id = ?`, combo.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan combo count: %v", err)
	}
	if count != 0 {
		t.Fatalf("combos row count = %d, want 0", count)
	}

	row = database.QueryRow(`SELECT COUNT(*) FROM combo_steps WHERE combo_id = ?`, combo.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan step count: %v", err)
	}
	if count != 0 {
		t.Fatalf("combo_steps row count = %d, want 0", count)
	}
}

func TestWeightedShuffle_ReturnsPermutation(t *testing.T) {
	steps := []db.ComboStep{
		{ID: "a", Weight: 10},
		{ID: "b", Weight: 20},
		{ID: "c", Weight: 30},
	}
	out := weightedShuffle(steps)
	if len(out) != len(steps) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(steps))
	}
	seen := map[string]bool{}
	for _, s := range out {
		if seen[s.ID] {
			t.Fatalf("step %q appears twice", s.ID)
		}
		seen[s.ID] = true
	}
	for _, s := range steps {
		if !seen[s.ID] {
			t.Fatalf("step %q missing from output", s.ID)
		}
	}
}

func TestEffectiveStrategy_OverridesAndDefaults(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	// No settings: returns the combo's own strategy when valid.
	if got := h.EffectiveStrategy("mycombo", "priority"); got != "priority" {
		t.Fatalf("EffectiveStrategy without override = %q, want priority", got)
	}

	// Invalid combo strategy falls back to priority.
	if got := h.EffectiveStrategy("mycombo", "nope"); got != "priority" {
		t.Fatalf("EffectiveStrategy invalid fallback = %q, want priority", got)
	}

	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "combo_strategy", "round-robin", now); err != nil {
		t.Fatalf("insert combo_strategy: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "combo_strategies", `{"mycombo":"random"}`, now); err != nil {
		t.Fatalf("insert combo_strategies: %v", err)
	}

	h.RefreshStrategySettings()

	// Per-combo override wins over the combo's own strategy.
	if got := h.EffectiveStrategy("mycombo", "priority"); got != "random" {
		t.Fatalf("EffectiveStrategy with override = %q, want random", got)
	}

	// Per-combo override still wins even when the combo's own strategy is invalid.
	if got := h.EffectiveStrategy("mycombo", "nope"); got != "random" {
		t.Fatalf("EffectiveStrategy override invalid = %q, want random", got)
	}

	// A combo's own valid strategy is honored; the global default does NOT override it.
	if got := h.EffectiveStrategy("other", "priority"); got != "priority" {
		t.Fatalf("EffectiveStrategy own strategy = %q, want priority", got)
	}

	// Global default is only used when the combo has no valid strategy of its own.
	if got := h.EffectiveStrategy("other", ""); got != "round-robin" {
		t.Fatalf("EffectiveStrategy global default = %q, want round-robin", got)
	}

	// Fusion combo is not overridden by a global priority default.
	if got := h.EffectiveStrategy("fusion", "fusion"); got != "fusion" {
		t.Fatalf("EffectiveStrategy fusion = %q, want fusion", got)
	}
}

func TestPickConnection_EligibleAndBreaker(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{
		ID:     "conn-1",
		Prefix: "openai",
		Status: connstate.StatusReady,
	}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	if id, ok := h.PickConnection(db.ComboStep{ModelID: "openai/gpt-4o"}); !ok || id != "conn-1" {
		t.Fatalf("PickConnection eligible = (%q, %v), want (conn-1, true)", id, ok)
	}

	cb := h.fallback.GetBreaker("conn-1")
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != connstate.CBOpen {
		t.Fatalf("breaker state = %v, want open", cb.GetState())
	}
	if id, ok := h.PickConnection(db.ComboStep{ModelID: "openai/gpt-4o"}); ok {
		t.Fatalf("PickConnection with open breaker = (%q, %v), want false", id, ok)
	}
}

func TestPickConnections_RespectsCooldown(t *testing.T) {
	database := newComboTestDB(t)
	ids := []string{"conn-a", "conn-b", "conn-c", "conn-d", "conn-e"}
	for _, id := range ids {
		seedConnectionForCombo(t, database, id)
	}

	store := connstate.NewStore()
	future := time.Now().Add(time.Hour)

	ready := func(id string) *connstate.ConnectionState {
		return &connstate.ConnectionState{ID: id, Prefix: "openai", Status: connstate.StatusReady}
	}

	store.Set("conn-a", ready("conn-a"))

	csB := ready("conn-b")
	csB.SetCooldown(future)
	store.Set("conn-b", csB)

	csC := ready("conn-c")
	csC.SetModelCooldown("openai/gpt-4o", future)
	store.Set("conn-c", csC)

	store.Set("conn-d", &connstate.ConnectionState{ID: "conn-d", Prefix: "openai", Status: connstate.StatusDisabled})
	store.Set("conn-e", ready("conn-e"))

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	cb := h.fallback.GetBreaker("conn-e")
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != connstate.CBOpen {
		t.Fatalf("breaker state = %v, want open", cb.GetState())
	}

	got := h.PickConnections("openai", "openai/gpt-4o")
	if len(got) != 1 || got[0] != "conn-a" {
		t.Fatalf("PickConnections = %v, want [conn-a]", got)
	}
}

func TestRefreshFromDB_ReflectsExternalChanges(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{ID: "conn-1", Prefix: "openai", Status: connstate.StatusReady}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("refreshable", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	if _, ok := h.Resolve("refreshable"); !ok {
		t.Fatalf("Resolve before refresh returned false")
	}

	if _, err := database.Exec(`UPDATE combos SET is_active = 0 WHERE id = ?`, combo.ID); err != nil {
		t.Fatalf("update combo is_active: %v", err)
	}

	if err := h.RefreshFromDB(); err != nil {
		t.Fatalf("RefreshFromDB failed: %v", err)
	}

	if _, ok := h.Resolve("refreshable"); ok {
		t.Fatalf("Resolve returned true after deactivating combo in DB")
	}
}

func TestSnapshotFromDB_ExcludesStepsForDeletedCombo(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{ID: "conn-1", Prefix: "openai", Status: connstate.StatusReady}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("to-delete", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	if _, err := database.Exec(`DELETE FROM combo_steps WHERE combo_id = ?`, combo.ID); err != nil {
		t.Fatalf("delete combo steps: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM combos WHERE id = ?`, combo.ID); err != nil {
		t.Fatalf("delete combo: %v", err)
	}

	combos, _, _, steps, err := h.snapshotFromDB()
	if err != nil {
		t.Fatalf("snapshotFromDB failed: %v", err)
	}
	if _, ok := combos[combo.ID]; ok {
		t.Fatalf("deleted combo still present in snapshot")
	}
	if _, ok := steps[combo.ID]; ok {
		t.Fatalf("steps for deleted combo still present in snapshot")
	}
}

func TestSnapshotFromDB_NoOrphanedStepsUnderConcurrentDelete(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	store := connstate.NewStore()
	cs := &connstate.ConnectionState{ID: "conn-1", Prefix: "openai", Status: connstate.StatusReady}
	store.Set("conn-1", cs)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("race-combo", "priority", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("CreateCombo failed: %v", err)
	}

	// Serialize test-side operations to keep the SQLite test database stable
	// while still racing RefreshFromDB against mutations from two goroutines.
	var mu sync.Mutex
	var wg sync.WaitGroup
	done := make(chan struct{})
	errs := make(chan string, 100)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				err := h.RefreshFromDB()
				mu.Unlock()
				if err != nil {
					errs <- fmt.Sprintf("refresh: %v", err)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				err := h.DeleteCombo(combo.ID)
				if err == nil {
					newCombo, createErr := h.CreateCombo("race-combo", "priority", 30000, 1, false, "", "", []CreateStepInput{
						{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
					})
					if createErr == nil {
						combo = newCombo
					} else {
						err = createErr
					}
				}
				mu.Unlock()
				if err != nil {
					errs <- fmt.Sprintf("mutate: %v", err)
				}
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()
	close(errs)

	for msg := range errs {
		t.Errorf("concurrent worker error: %s", msg)
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for comboID := range h.steps {
		if _, ok := h.combos[comboID]; !ok {
			t.Fatalf("orphan steps in cache for combo %s", comboID)
		}
	}
}
