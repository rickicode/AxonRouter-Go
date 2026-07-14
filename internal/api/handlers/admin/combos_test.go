package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	t.Helper()
	c, err := h.CreateCombo("test-combo", "priority", 30000, 1, false, "", []combo.CreateStepInput{})
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
