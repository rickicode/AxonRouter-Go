package combo

import (
	"database/sql"
	"path/filepath"
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
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('combo-test','Combo Test','openai','http://x',?)`, now); err != nil {
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

	combo, err := h.CreateCombo("auto-conn", "priority", 30000, 1, false, "", []CreateStepInput{
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

	combo, err := h.CreateCombo("premium-smart", "priority", 15000, 3, true, "premium", []CreateStepInput{
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

	combo, err := h.CreateCombo("rr-zero", "round-robin", 30000, 0, false, "", []CreateStepInput{
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

	regular, err := h.CreateCombo("balanced", "priority", 30000, 1, false, "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("create regular combo: %v", err)
	}
	_, err = h.CreateCombo("smart-balanced", "priority", 30000, 1, true, "balanced", []CreateStepInput{
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

	_, err := h.CreateCombo("z-economy", "priority", 30000, 1, true, "economy", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("create z-economy: %v", err)
	}
	_, err = h.CreateCombo("a-economy", "priority", 30000, 1, true, "economy", []CreateStepInput{
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
