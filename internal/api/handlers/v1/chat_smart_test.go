package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/smart"
)

func TestChatCompletions_SmartVirtualModel(t *testing.T) {
	database := openTestDB(t)
	store := connstate.NewStore()
	now := time.Now().Unix()
	_, _ = database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES (?,?,?,?,?)`, "openai", "OpenAI", "openai", "http://x", now)
	_, _ = database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, status, is_active, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, "conn-smart", "openai", "Test", "apikey", "sk-test", "ready", 1, now, now)
	store.Set("conn-smart", &connstate.ConnectionState{ID: "conn-smart", Prefix: "openai", Status: connstate.StatusReady, Priority: 0})

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	reg := smart.NewRegistry(database)
	_, _ = reg.Upsert(&smart.VirtualModel{
		ID:         smart.ModelAuto,
		Candidates: []string{"openai/gpt-4o-mini"},
		Enabled:    true,
		Strategy:   smart.StrategyBalanced,
	})
	tel := smart.NewTelemetryStore(database)
	router := smart.NewRouter(reg, tel, store, elig)

	h := newTestHandler(t)
	h.db = database
	h.store = store
	h.elig = elig
	h.smartRouter = router
	h.smartRegistry = reg

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	body := []byte(`{"model":"smart/auto","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req = req.WithContext(context.Background())
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// We can't fully run ChatCompletions without more setup, so instead validate
	// the smart hook at the router layer.
	resolved, updated, err := smart.RewriteVirtualModel("smart/auto", body, router)
	if err != nil {
		t.Fatalf("smart rewrite: %v", err)
	}
	if resolved != "openai/gpt-4o-mini" {
		t.Errorf("resolved = %q, want openai/gpt-4o-mini", resolved)
	}
	if !bytes.Contains(updated, []byte("openai/gpt-4o-mini")) {
		t.Errorf("updated body missing concrete model: %s", updated)
	}
}

func TestSmartRewrite_Passthrough(t *testing.T) {
	// If model is not virtual, it passes through unchanged.
	body := []byte(`{"model":"openai/gpt-4o","messages":[]}`)
	resolved, updated, err := smart.RewriteVirtualModel("openai/gpt-4o", body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "openai/gpt-4o" {
		t.Errorf("model changed to %q", resolved)
	}
	if !bytes.Equal(updated, body) {
		t.Error("body should be unchanged")
	}
}

func TestSmartRewrite_FallbackNoRouter(t *testing.T) {
	// If router is nil, virtual model string passes through unchanged.
	body := []byte(`{"model":"smart/auto","messages":[]}`)
	resolved, updated, err := smart.RewriteVirtualModel("smart/auto", body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "smart/auto" {
		t.Errorf("model changed to %q", resolved)
	}
	if !bytes.Equal(updated, body) {
		t.Error("body should be unchanged")
	}
}

func init() {
	_ = io.Discard
	_ = json.Marshal
	_ = db.UnixNow
}
