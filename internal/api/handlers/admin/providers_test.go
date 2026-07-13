package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

// countingMockExecutor counts the maximum number of concurrent ExecuteStream calls.
type countingMockExecutor struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (m *countingMockExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *countingMockExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	m.mu.Lock()
	m.active++
	if m.active > m.maxActive {
		m.maxActive = m.active
	}
	m.mu.Unlock()

	time.Sleep(m.delay)

	m.mu.Lock()
	m.active--
	m.mu.Unlock()

	ch := make(chan executor.StreamChunk, 1)
	ch <- executor.StreamChunk{Payload: []byte(`{"ok":true}`)}
	close(ch)
	return &executor.StreamResult{Chunks: ch}, nil
}

func (m *countingMockExecutor) MaxActive() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxActive
}

// TestAll_BatchesToMaxTen proves that TestAll never runs more than 10 connection
// tests concurrently, even when a provider has many connections.
func TestAll_BatchesToMaxTen(t *testing.T) {
	database := newConnectionHandlerTestDB(t)

	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('batchp','Batch Provider','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}

	const totalConns = 25
	for i := range totalConns {
		id := "conn-" + string(rune('a'+i))
		if _, err := database.Exec(`
			INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at)
			VALUES (?,'batchp',?,'none','ready',1,?,?)
		`, id, id, now, now); err != nil {
			t.Fatalf("seed connection %d: %v", i, err)
		}
	}

	mock := &countingMockExecutor{delay: 50 * time.Millisecond}
	registry := executor.GetRegistry()
	registry.Register("batchp", executor.FormatOpenAI, mock)

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	providerCfg := providercfg.NewManager("")
	h := NewProviderHandler(database, registry, store, elig, providerCfg)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/batchp/test", nil)
	c.Params = []gin.Param{{Key: "id", Value: "batchp"}}

	h.TestAll(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Count(body, `"status":"ok"`) != totalConns {
		t.Fatalf("expected %d ok results, body=%s", totalConns, body)
	}
	if mock.MaxActive() > testAllBatchSize {
		t.Fatalf("max concurrent = %d, want <= %d", mock.MaxActive(), testAllBatchSize)
	}
	if mock.MaxActive() == 0 {
		t.Fatalf("mock was never called concurrently")
	}
}
