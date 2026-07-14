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
