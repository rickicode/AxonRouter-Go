package combo

import (
	"database/sql"
	"strings"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// resolveDefaultConnection finds an active connection whose provider matches the
// model prefix (e.g. "openai/gpt-4o" -> "openai"). Falls back to empty string so
// the step is skipped if no matching connection is configured yet.
func resolveDefaultConnection(database *sql.DB, modelID string) string {
	idx := strings.IndexByte(modelID, '/')
	if idx <= 0 {
		return ""
	}
	prefix := modelID[:idx]
	var connID string
	database.QueryRow(`
		SELECT c.id FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE pt.id = ? AND c.is_active = 1
		ORDER BY c.created_at ASC
		LIMIT 1
	`, prefix).Scan(&connID)
	return connID
}

// DefaultCombos returns the built-in combos that should exist on first run.
func DefaultCombos() []DefaultComboDef {
	return []DefaultComboDef{
		{
			Name:     "balanced",
			Strategy: "priority",
			Steps: []DefaultStepDef{
				{ModelID: "mimo/mimo-v2-pro", Priority: 1, Weight: 100},
				{ModelID: "openai/gpt-4o", Priority: 2, Weight: 80},
				{ModelID: "deepseek/deepseek-chat", Priority: 3, Weight: 60},
			},
		},
		{
			Name:     "economy",
			Strategy: "priority",
			Steps: []DefaultStepDef{
				{ModelID: "mimo/mimo-v2-flash", Priority: 1, Weight: 100},
				{ModelID: "deepseek/deepseek-chat", Priority: 2, Weight: 80},
				{ModelID: "groq/llama-3.3-70b", Priority: 3, Weight: 60},
			},
		},
		{
			Name:     "premium",
			Strategy: "priority",
			Steps: []DefaultStepDef{
				{ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
				{ModelID: "claude/claude-sonnet-4", Priority: 2, Weight: 90},
				{ModelID: "gemini/gemini-2.5-pro", Priority: 3, Weight: 80},
			},
		},
		{
			Name:     "round-robin",
			Strategy: "round-robin",
			Steps: []DefaultStepDef{
				{ModelID: "mimo/mimo-v2-pro", Priority: 1, Weight: 100},
				{ModelID: "deepseek/deepseek-chat", Priority: 2, Weight: 100},
				{ModelID: "openai/gpt-4o", Priority: 3, Weight: 100},
			},
		},
		{
			Name:      "smart-balanced",
			Strategy:  "priority",
			IsSmart:   true,
			SmartGoal: "balanced",
			Steps: []DefaultStepDef{
				{ModelID: "mimo/mimo-v2-pro", Priority: 1, Weight: 100},
				{ModelID: "openai/gpt-4o", Priority: 2, Weight: 80},
			},
		},
		{
			Name:      "smart-economy",
			Strategy:  "priority",
			IsSmart:   true,
			SmartGoal: "economy",
			Steps: []DefaultStepDef{
				{ModelID: "mimo/mimo-v2-flash", Priority: 1, Weight: 100},
				{ModelID: "deepseek/deepseek-chat", Priority: 2, Weight: 80},
			},
		},
		{
			Name:      "smart-premium",
			Strategy:  "priority",
			IsSmart:   true,
			SmartGoal: "premium",
			Steps: []DefaultStepDef{
				{ModelID: "openai/gpt-4o", Priority: 1, Weight: 100},
				{ModelID: "claude/claude-sonnet-4", Priority: 2, Weight: 90},
			},
		},
		{
			Name:      "smart-auto",
			Strategy:  "priority",
			IsSmart:   true,
			SmartGoal: "auto",
			Steps: []DefaultStepDef{
				{ModelID: "mimo/mimo-v2-pro", Priority: 1, Weight: 100},
				{ModelID: "openai/gpt-4o", Priority: 2, Weight: 90},
				{ModelID: "deepseek/deepseek-chat", Priority: 3, Weight: 70},
			},
		},
	}
}

// DefaultComboDef is a combo definition for seeding.
type DefaultComboDef struct {
	Name      string
	Strategy  string
	IsSmart   bool
	SmartGoal string
	Steps     []DefaultStepDef
}

// DefaultStepDef is a step definition for seeding.
type DefaultStepDef struct {
	ModelID  string
	Priority int
	Weight   int
}

// SeedDefaultCombos inserts default combos if none exist.
func SeedDefaultCombos(database *sql.DB) error {
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM combos`).Scan(&count)
	if count > 0 {
		return nil
	}

	now := db.UnixNow()
	for _, def := range DefaultCombos() {
		comboID := uuid.New().String()
		smartGoal := sql.NullString{}
		if def.SmartGoal != "" {
			smartGoal = sql.NullString{String: def.SmartGoal, Valid: true}
		}
		_, err := database.Exec(`
			INSERT INTO combos (id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal, is_active, created_at, updated_at)
			VALUES (?, ?, ?, 1, 30000, ?, ?, 1, ?, ?)
		`, comboID, def.Name, def.Strategy, boolToInt(def.IsSmart), smartGoal, now, now)
		if err != nil {
			continue
		}
	for _, step := range def.Steps {
		connectionID := resolveDefaultConnection(database, step.ModelID)
		database.Exec(`
		INSERT INTO combo_steps (id, combo_id, connection_id, model_id, priority, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`, uuid.New().String(), comboID, connectionID, step.ModelID, step.Priority, step.Weight, now)
	}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
