package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func newComboHandlerForTest(t *testing.T) (*ComboHandler, *combo.Handler, *connstate.Store, *connstate.EligibilityManager) {
	t.Helper()
	database := newConnectionHandlerTestDB(t)
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	ch := combo.NewHandler(database, nil, store, elig)
	return NewComboHandler(database, nil, ch), ch, store, elig
}

func seedConnectionForAdminCombo(t *testing.T, database *sql.DB, store *connstate.Store, elig *connstate.EligibilityManager, id, prefix string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`
		INSERT INTO provider_types (id, display_name, format, base_url, created_at)
		VALUES (?, 'Admin Combo Test', 'openai', 'http://x', ?)
		ON CONFLICT(id) DO NOTHING
	`, prefix, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at)
		VALUES (?, ?, 'c', 'none', 'ready', 1, ?, ?)
	`, id, prefix, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	store.Set(id, &connstate.ConnectionState{ID: id, Prefix: prefix, Status: connstate.StatusReady})
	if elig != nil {
		elig.RecomputeAll()
	}
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
	handler, comboH, _, _ := newComboHandlerForTest(t)
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
	handler, comboH, _, _ := newComboHandlerForTest(t)
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
	handler, comboH, _, _ := newComboHandlerForTest(t)
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

func TestComboCreate_ValidatesFusion(t *testing.T) {
	handler, _, store, elig := newComboHandlerForTest(t)
	seedConnectionForAdminCombo(t, handler.db, store, elig, "conn-fusion", "openai")

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"fusion-bad","strategy":"fusion","fusion_config":"{\"panel_hard_timeout_ms\":500}","steps":[{"model_id":"openai/gpt-4o"},{"model_id":"openai/gpt-4o-mini"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/combos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestComboDelete_Returns404IfMissing(t *testing.T) {
	handler, _, _, _ := newComboHandlerForTest(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/admin/combos/not-there", nil)
	c.Params = []gin.Param{{Key: "id", Value: "not-there"}}

	handler.Delete(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestComboList_Paginated(t *testing.T) {
	handler, comboH, _, _ := newComboHandlerForTest(t)
	for i := 1; i <= 3; i++ {
		seedComboNamed(t, comboH, "list-combo-"+strconv.Itoa(i))
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/combos?page=1&per_page=2", nil)

	handler.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Data       []map[string]any `json:"data"`
		Pagination struct {
			Page       int `json:"page"`
			PerPage    int `json:"per_page"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Pagination.Total != 3 {
		t.Fatalf("total = %d, want 3", resp.Pagination.Total)
	}
	if resp.Pagination.PerPage != 2 {
		t.Fatalf("per_page = %d, want 2", resp.Pagination.PerPage)
	}
	if resp.Pagination.TotalPages != 2 {
		t.Fatalf("total_pages = %d, want 2", resp.Pagination.TotalPages)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(resp.Data))
	}
}

func TestComboUpdate_ResetsRotationCounter(t *testing.T) {
	handler, comboH, store, elig := newComboHandlerForTest(t)
	seedConnectionForAdminCombo(t, handler.db, store, elig, "conn-update", "openai")
	id := seedComboNamed(t, comboH, "rotation-reset")

	now := time.Now().Unix()
	if _, err := handler.db.Exec(`
		INSERT INTO rotation_state (combo_id, counter, updated_at) VALUES (?, 5, ?)
	`, id, now); err != nil {
		t.Fatalf("seed rotation_state: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"strategy":"round-robin"}`
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/combos/"+id, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: id}}

	handler.Update(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var count int
	err := handler.db.QueryRow(`SELECT COUNT(*) FROM rotation_state WHERE combo_id = ?`, id).Scan(&count)
	if err != nil {
		t.Fatalf("query rotation_state: %v", err)
	}
	if count != 0 {
		t.Fatalf("rotation_state rows = %d, want 0 after strategy update", count)
	}
}

func newComboHandlerWithQueueForTest(t *testing.T) (*ComboHandler, *combo.Handler, *connstate.Store, *connstate.EligibilityManager) {
	t.Helper()
	database := newConnectionHandlerTestDB(t)
	queue := db.NewWriteQueue(database)
	t.Cleanup(func() { queue.Stop() })
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	ch := combo.NewHandler(database, queue, store, elig)
	return NewComboHandler(database, queue, ch), ch, store, elig
}

func TestComboCreate_WithWriteQueue_QueueStateConsistent(t *testing.T) {
	handler, comboH, store, elig := newComboHandlerWithQueueForTest(t)
	seedConnectionForAdminCombo(t, handler.db, store, elig, "conn-queue", "openai")

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"queue-combo","strategy":"priority","steps":[{"model_id":"openai/gpt-4o"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/combos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}

	// The handler must have waited for the queued write and updated in-memory
	// state before returning, so the combo resolves immediately.
	if _, ok := comboH.Resolve("queue-combo"); !ok {
		t.Fatalf("combo not resolvable immediately after Create returned")
	}

	var dbCount int
	if err := handler.db.QueryRow(`SELECT COUNT(*) FROM combos WHERE name = ?`, "queue-combo").Scan(&dbCount); err != nil {
		t.Fatalf("query combos: %v", err)
	}
	if dbCount != 1 {
		t.Fatalf("combos row count = %d, want 1", dbCount)
	}
}
