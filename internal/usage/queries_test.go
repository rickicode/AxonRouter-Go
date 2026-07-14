package usage

import (
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func TestQueryLogs_ProxyPoolName(t *testing.T) {
	database := openTestDB(t)

	now := db.UnixNow()
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, is_active, created_at, updated_at)
		VALUES ('conn-pool', 'openai', 'Conn', 'api_key', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO proxy_pools (id, name, type, proxy_url, is_active, created_at, updated_at)
		VALUES ('pool-1', 'US East', 'http', 'http://10.0.0.1:8080', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert proxy pool: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO request_logs (id, timestamp, connection_id, provider_type_id, model_id, proxy_pool_id, modality, input_tokens, output_tokens, stream, tokens_estimated, cost_usd, created_at)
		VALUES ('log-1', ?, 'conn-pool', 'openai', 'gpt-4o', 'pool-1', 'chat', 1, 1, 0, 0, 0, ?)`, time.Now().UnixMilli(), now); err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	resp, err := QueryLogs(database, 1, 50, LogFilter{})
	if err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if resp.Pagination.Total != 1 {
		t.Fatalf("expected 1 log, got %d", resp.Pagination.Total)
	}
	logs, ok := resp.Data.([]db.RequestLog)
	if !ok {
		t.Fatalf("unexpected data type %T", resp.Data)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log row, got %d", len(logs))
	}
	log := logs[0]
	if log.ProxyPoolID.String != "pool-1" {
		t.Errorf("proxy_pool_id = %q, want pool-1", log.ProxyPoolID.String)
	}
	if log.ProxyPoolName.String != "US East" {
		t.Errorf("proxy_pool_name = %q, want US East", log.ProxyPoolName.String)
	}
}
