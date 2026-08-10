package usage

import (
	"testing"
	"time"
)

func TestGetTodaySummary(t *testing.T) {
	database := openTestDB(t)
	now := time.Now()

	todayMs := now.Truncate(24 * time.Hour).Add(2 * time.Hour).UnixMilli()
	yesterdayMs := now.Truncate(24 * time.Hour).Add(-2 * time.Hour).UnixMilli()

	if _, err := database.Exec(`INSERT INTO request_logs (id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, latency_ms, error_message, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-today", todayMs, "openai", "gpt-4o", "chat", 10, 20, 100, "", 0.05, todayMs); err != nil {
		t.Fatalf("insert today log: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO request_logs (id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, latency_ms, error_message, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-err", todayMs+1, "openai", "gpt-4o", "chat", 5, 5, 200, "boom", 0.01, todayMs+1); err != nil {
		t.Fatalf("insert today error log: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO request_logs (id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, latency_ms, error_message, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-yest", yesterdayMs, "openai", "gpt-4o", "chat", 100, 100, 50, "", 0.50, yesterdayMs); err != nil {
		t.Fatalf("insert yesterday log: %v", err)
	}

	agg := NewAggregator(database)
	s, err := agg.GetTodaySummary()
	if err != nil {
		t.Fatalf("GetTodaySummary: %v", err)
	}
	if s.Requests != 2 {
		t.Errorf("Requests = %d, want 2", s.Requests)
	}
	if s.Tokens != 40 {
		t.Errorf("Tokens = %d, want 40", s.Tokens)
	}
	if s.Errors != 1 {
		t.Errorf("Errors = %d, want 1", s.Errors)
	}
	if s.CostUsd < 0.059 || s.CostUsd > 0.061 {
		t.Errorf("CostUsd = %f, want ~0.06", s.CostUsd)
	}
	if s.AvgLatencyMs < 149 || s.AvgLatencyMs > 151 {
		t.Errorf("AvgLatencyMs = %f, want ~150", s.AvgLatencyMs)
	}
}

func TestGetTodaySummary_EmptyDB(t *testing.T) {
	database := openTestDB(t)
	agg := NewAggregator(database)
	s, err := agg.GetTodaySummary()
	if err != nil {
		t.Fatalf("GetTodaySummary: %v", err)
	}
	if s.Requests != 0 || s.Tokens != 0 || s.CostUsd != 0 || s.Errors != 0 || s.AvgLatencyMs != 0 {
		t.Errorf("expected zero summary, got %+v", s)
	}
}
