package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestConnectionHandler_CleanupConnections(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newConnectionHandlerForTest(t, database, nil)

	now := time.Now().Unix()
	old := time.Now().Add(-8 * 24 * time.Hour).Unix()
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('test','Test','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	// Eligible for deletion (legacy terminal status)
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-old-auth','test','c1','none','auth_failed',0,?,?)`, now, old); err != nil {
		t.Fatalf("seed old auth_failed: %v", err)
	}
	// Should be kept (recent legacy terminal status)
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-recent','test','c2','none','auth_failed',0,?,?)`, now, now); err != nil {
		t.Fatalf("seed recent auth_failed: %v", err)
	}
	// Should be kept (disabled rows are never auto-deleted)
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('conn-disabled','test','c3','none','disabled',0,?,?)`, now, old); err != nil {
		t.Fatalf("seed old disabled: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.CleanupConnections(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["deleted"] != float64(1) {
		t.Fatalf("deleted = %v, want 1", body["deleted"])
	}

	var oldCount, recentCount, disabledCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM connections WHERE id = 'conn-old-auth'`).Scan(&oldCount); err != nil {
		t.Fatalf("count old: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM connections WHERE id = 'conn-recent'`).Scan(&recentCount); err != nil {
		t.Fatalf("count recent: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM connections WHERE id = 'conn-disabled'`).Scan(&disabledCount); err != nil {
		t.Fatalf("count disabled: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("conn-old-auth should be deleted")
	}
	if recentCount != 1 {
		t.Fatalf("conn-recent should be kept")
	}
	if disabledCount != 1 {
		t.Fatalf("conn-disabled should be kept")
	}
}
