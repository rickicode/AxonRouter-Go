package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

func newComboHandlerForTest(t *testing.T) (*ComboHandler, *combo.Handler) {
	t.Helper()
	database := newConnectionHandlerTestDB(t)
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	ch := combo.NewHandler(database, store, elig)
	return NewComboHandler(database, ch), ch
}

func seedCombo(t *testing.T, h *combo.Handler) string {
	return seedComboNamed(t, h, "test-combo")
}

func seedComboNamed(t *testing.T, h *combo.Handler, name string) string {
	t.Helper()
	c, err := h.CreateCombo(name, "priority", 30000, 1, false, "", "", []combo.CreateStepInput{})
	if err != nil {
		t.Fatalf("seed combo: %v", err)
	}
	return c.ID
}

func TestComboUpdate_PersistsSmartAndStickyFields(t *testing.T) {
	handler, comboH := newComboHandlerForTest(t)
	id := seedCombo(t, comboH)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"sticky_limit":5,"is_smart":true,"smart_goal":"balanced"}`
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/combos/"+id, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: id}}

	handler.Update(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Verify by re-loading the combo. The in-memory copy returned by GetCombo reflects RefreshFromDB.
	got, ok := comboH.GetCombo(id)
	if !ok {
		t.Fatalf("combo not found after update")
	}
	if got.Combo.StickyLimit != 5 {
		t.Fatalf("StickyLimit = %d, want 5", got.Combo.StickyLimit)
	}
	if !got.Combo.IsSmart {
		t.Fatalf("IsSmart = false, want true")
	}
	if !got.Combo.SmartGoal.Valid || got.Combo.SmartGoal.String != "balanced" {
		t.Fatalf("SmartGoal = %v, want balanced", got.Combo.SmartGoal)
	}
}

func TestComboMetrics_AggregatesRequestLogs(t *testing.T) {
	handler, comboH := newComboHandlerForTest(t)
	combo1 := seedComboNamed(t, comboH, "metrics-combo-1")
	combo2 := seedComboNamed(t, comboH, "metrics-combo-2")

	// Seed usage logs for combo1 and combo2.
	now := time.Now().UnixMilli()
	db := handler.db
	_, err := db.Exec(`
		INSERT INTO request_logs (id, timestamp, combo_id, provider_type_id, model_id, modality, stream, status_code, input_tokens, output_tokens, latency_ms, created_at)
		VALUES
			('m1', ?, ?, 'oc', 'hy3-free', 'chat', 0, 200, 10, 20, 120, ?),
			('m2', ?, ?, 'oc', 'hy3-free', 'chat', 0, 200, 5, 15, 80, ?),
			('m3', ?, ?, 'oc', 'hy3-free', 'chat', 0, 500, 0, 0, 0, ?),
			('m4', ?, ?, 'cf', 'kimi-k2.6', 'chat', 0, 200, 30, 40, 200, ?)
	`, now, combo1, now, now, combo1, now, now, combo1, now, now, combo2, now)
	if err != nil {
		t.Fatalf("seed request_logs: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/combos/metrics?window=86400", nil)

	handler.Metrics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Data   []map[string]any `json:"data"`
		Totals map[string]any   `json:"totals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if int(body.Totals["requests"].(float64)) != 4 {
		t.Fatalf("total requests = %v, want 4", body.Totals["requests"])
	}
	if int(body.Totals["successes"].(float64)) != 3 {
		t.Fatalf("total successes = %v, want 3", body.Totals["successes"])
	}
	if int(body.Totals["errors"].(float64)) != 1 {
		t.Fatalf("total errors = %v, want 1", body.Totals["errors"])
	}
	if len(body.Data) < 2 {
		t.Fatalf("expected metrics for at least 2 combos, got %d", len(body.Data))
	}
}

func TestComboMetrics_WindowExactlyThirtyDays(t *testing.T) {
	handler, comboH := newComboHandlerForTest(t)
	comboID := seedComboNamed(t, comboH, "metrics-window-combo")

	// Seed a request just inside the 30-day boundary.
	db := handler.db
	now := time.Now().UnixMilli()
	justInside := now - 30*24*60*60*1000 + 1000
	_, err := db.Exec(`
		INSERT INTO request_logs (id, timestamp, combo_id, provider_type_id, model_id, modality, stream, status_code, input_tokens, output_tokens, latency_ms, created_at)
		VALUES ('w1', ?, ?, 'oc', 'hy3-free', 'chat', 0, 200, 1, 2, 50, ?)
	`, justInside, comboID, now)
	if err != nil {
		t.Fatalf("seed request_logs: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/combos/metrics?window=2592000", nil)

	handler.Metrics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Totals map[string]any `json:"totals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if int(body.Totals["requests"].(float64)) != 1 {
		t.Fatalf("total requests = %v, want 1", body.Totals["requests"])
	}
}
