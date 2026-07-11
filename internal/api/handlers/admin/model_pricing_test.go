package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func TestModelPricingListEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := filepath.Join(t.TempDir(), "admin-pricing-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	usage.InitPricing(database)

	h := NewModelPricingHandler()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("List status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []usage.ModelPricingRow `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected seeded pricing rows, got none")
	}
	found := false
	for _, r := range resp.Data {
		if r.ModelID == "gpt-4o" && r.InputPer1K == 0.0025 {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeded gpt-4o row not returned: %+v", resp.Data)
	}
}

func TestModelPricingCreateUpdateDeleteEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := filepath.Join(t.TempDir(), "admin-pricing-crud.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	usage.InitPricing(database)
	h := NewModelPricingHandler()

	// Create
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/model-pricing", jsonBody(t, map[string]any{
		"model_id": "smoke-model", "display_name": "Smoke", "input_per_1k": 0.007, "output_per_1k": 0.014,
	}))
	h.Create(c)
	if w.Code != http.StatusOK {
		t.Fatalf("Create status = %d, body=%s", w.Code, w.Body.String())
	}

	// List should now contain it
	if !hasModel(t, h, "smoke-model") {
		t.Fatal("created model not found in list")
	}

	// Delete
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "smoke-model"}}
	h.Delete(c)
	if w.Code != http.StatusOK {
		t.Fatalf("Delete status = %d, body=%s", w.Code, w.Body.String())
	}
	if hasModel(t, h, "smoke-model") {
		t.Fatal("model still present after delete")
	}
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func hasModel(t *testing.T, h *ModelPricingHandler, id string) bool {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.List(c)
	var resp struct {
		Data []usage.ModelPricingRow `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, r := range resp.Data {
		if r.ModelID == id {
			return true
		}
	}
	return false
}
