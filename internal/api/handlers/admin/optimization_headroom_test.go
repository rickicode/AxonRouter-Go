package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/headroom"
)

func TestHeadroomStatus_Defaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newOptimizationTestDB(t)
	h := NewOptimizationHandler(database, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/headroom/status", nil)
	h.GetHeadroomStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["enabled"] != false {
		t.Errorf("enabled = %v, want false", resp["enabled"])
	}
	if resp["endpoint"] != headroom.DefaultEndpoint {
		t.Errorf("endpoint = %v, want default", resp["endpoint"])
	}
	if resp["running"] != "disabled" {
		t.Errorf("running = %v, want disabled", resp["running"])
	}
	if resp["headroom_total"] != float64(0) {
		t.Errorf("headroom_total = %v, want 0", resp["headroom_total"])
	}
	if resp["headroom_bytes_saved"] != float64(0) {
		t.Errorf("headroom_bytes_saved = %v, want 0", resp["headroom_bytes_saved"])
	}
	if resp["headroom_errors"] != float64(0) {
		t.Errorf("headroom_errors = %v, want 0", resp["headroom_errors"])
	}
}

func TestHeadroomStatus_EnabledAndCounters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newOptimizationTestDB(t)
	h := NewOptimizationHandler(database, nil)

	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "headroom_enabled", "true", 1); err != nil {
		t.Fatalf("seed headroom_enabled: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "headroom_endpoint", "https://headroom.test/v1", 1); err != nil {
		t.Fatalf("seed headroom_endpoint: %v", err)
	}
	headroom.SetEnabled(true)
	headroom.IncTotal()
	headroom.AddBytesSaved(42)
	headroom.IncErrors()
	headroom.SetRunning()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/headroom/status", nil)
	h.GetHeadroomStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["enabled"] != true {
		t.Errorf("enabled = %v, want true", resp["enabled"])
	}
	if resp["endpoint"] != "https://headroom.test/v1" {
		t.Errorf("endpoint = %v, want https://headroom.test/v1", resp["endpoint"])
	}
	if resp["running"] != "running" {
		t.Errorf("running = %v, want running", resp["running"])
	}
	if resp["headroom_total"] != float64(1) {
		t.Errorf("headroom_total = %v, want 1", resp["headroom_total"])
	}
	if resp["headroom_bytes_saved"] != float64(42) {
		t.Errorf("headroom_bytes_saved = %v, want 42", resp["headroom_bytes_saved"])
	}
	if resp["headroom_errors"] != float64(1) {
		t.Errorf("headroom_errors = %v, want 1", resp["headroom_errors"])
	}
}
