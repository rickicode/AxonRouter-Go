package usage

import (
	"testing"
	"time"
)

func TestSavingsBetween_CacheDiscount(t *testing.T) {
	db := newTestPricingDB(t)
	InitPricing(db)

	now := time.Now()
	start := now.Add(-time.Hour).UnixMilli()
	end := now.Add(time.Hour).UnixMilli()

	// 1000 cached input tokens on gpt-4o. Baseline input cost: $0.0025
	// actual cached-read cost: $0.00125 -> savings $0.00125
	_, err := db.Exec(`INSERT INTO request_logs
		(id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_creation_tokens, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-1", now.UnixMilli(), "openai", "openai/gpt-4o", "chat", 1000, 0, 0, 1000, 0, 0.00125, now.Unix())
	if err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	saved, err := SavingsBetween(db, start, end)
	if err != nil {
		t.Fatalf("SavingsBetween: %v", err)
	}
	want := 0.00125
	if saved < want-1e-9 || saved > want+1e-9 {
		t.Errorf("savings = %.6f, want %.6f", saved, want)
	}
}

func TestSavingsByProvider(t *testing.T) {
	db := newTestPricingDB(t)
	InitPricing(db)

	now := time.Now()
	start := now.Add(-time.Hour).UnixMilli()
	end := now.Add(time.Hour).UnixMilli()

	_, err := db.Exec(`INSERT INTO request_logs
		(id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_creation_tokens, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-2", now.UnixMilli(), "cx", "cx/gpt-4o", "chat", 1000, 0, 0, 1000, 0, 0.00125, now.Unix())
	if err != nil {
		t.Fatalf("insert request log: %v", err)
	}

	byProvider, total, err := SavingsByProvider(db, start, end)
	if err != nil {
		t.Fatalf("SavingsByProvider: %v", err)
	}
	if total < 0.00125-1e-9 || total > 0.00125+1e-9 {
		t.Errorf("total = %.6f, want 0.00125", total)
	}
	if byProvider["cx"] < 0.00125-1e-9 || byProvider["cx"] > 0.00125+1e-9 {
		t.Errorf("cx savings = %.6f, want 0.00125", byProvider["cx"])
	}
}
