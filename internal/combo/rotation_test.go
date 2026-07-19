package combo

import (
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

func TestLeastUsedStrategy_HandlesAtPrefix(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")
	seedConnectionForCombo(t, database, "conn-2")

	// Seed some request logs under the stripped provider/model.
	now := testingTimeNowMs()
	_, err := database.Exec(`
		INSERT INTO request_logs (id, timestamp, connection_id, provider_type_id, model_id, modality, stream, status_code, created_at)
		VALUES
			('r1', ?, 'conn-1', 'cx', 'gpt-5.4', 'chat', 0, 200, ?),
			('r2', ?, 'conn-1', 'cx', 'gpt-5.4', 'chat', 0, 200, ?),
			('r3', ?, 'conn-1', 'cx', 'gpt-5.4', 'chat', 0, 200, ?),
			('r4', ?, 'conn-2', 'cx', 'gpt-5.4-mini', 'chat', 0, 200, ?)
	`, now, now, now, now, now, now, now, now)
	if err != nil {
		t.Fatalf("seed request logs: %v", err)
	}

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	combo, err := h.CreateCombo("least-used-at", "least-used", 30000, 1, false, "", "", []CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "@cx/gpt-5.4", Priority: 1, Weight: 100},
		{ConnectionID: "conn-2", ModelID: "@cx/gpt-5.4-mini", Priority: 2, Weight: 100},
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
	if result.Steps[0].ModelID != "@cx/gpt-5.4-mini" {
		t.Fatalf("least-used should prefer lower-usage @-prefixed model, got %v", result.Steps)
	}
}

func testingTimeNowMs() int64 {
	return time.Now().UnixMilli()
}
