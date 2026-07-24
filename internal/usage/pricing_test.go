package usage

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func newTestPricingDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "pricing-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestGetPricingExact(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	p := GetPricing("gpt-4o")
	if p.InputPer1K != 0.0025 || p.OutputPer1K != 0.01 {
		t.Fatalf("gpt-4o pricing wrong: got in=%.5f out=%.5f", p.InputPer1K, p.OutputPer1K)
	}
}

func TestGetPricingStripsProviderPrefix(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	// openai/gpt-4o must resolve to the gpt-4o row after prefix stripping.
	p := GetPricing("openai/gpt-4o")
	if p.InputPer1K != 0.0025 {
		t.Fatalf("prefixed model resolved wrong: got in=%.5f", p.InputPer1K)
	}
}

func TestGetPricingDeterministicPrefix(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	// gpt-4o-2024-08-06 matches both "gpt-4o" and "gpt-4" via substring, but the
	// longest matching key must win deterministically (gpt-4o: $2.5/$10 per 1M),
	// never the random-map-order result (gpt-4: $30/$60 per 1M).
	for i := 0; i < 1000; i++ {
		p := GetPricing("gpt-4o-2024-08-06")
		if p.InputPer1K != 0.0025 {
			t.Fatalf("iteration %d: non-deterministic/incorrect match, got in=%.5f (expected 0.0025)", i, p.InputPer1K)
		}
	}
}

func TestEstimateCostTokenBreakdown(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	// gpt-4o seeded: in 0.0025, out 0.01, cached_read 0.00125, reason 0.
	// 1000 input (200 cached) + 1000 output, 0 reasoning.
	p := GetPricing("gpt-4o")
	want := float64(800)/1000*p.InputPer1K + float64(200)/1000*p.CachedReadPer1K + float64(1000)/1000*p.OutputPer1K
	got := EstimateCost("gpt-4o", 1000, 1000, 0, 200, 0)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("EstimateCost cached = %.6f, want %.6f", got, want)
	}

	// Reasoning billed when reason_per_1k > 0.
	if err := UpsertPricing(ModelPricingRow{ModelID: "rtest", InputPer1K: 0.001, OutputPer1K: 0.002, ReasonPer1K: 0.015}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rp := GetPricing("rtest")
	rw := float64(1000)/1000*rp.InputPer1K + float64(1000)/1000*rp.OutputPer1K + float64(500)/1000*rp.ReasonPer1K
	rg := EstimateCost("rtest", 1000, 1000, 500, 0, 0)
	if math.Abs(rg-rw) > 1e-9 {
		t.Fatalf("EstimateCost reasoning = %.6f, want %.6f", rg, rw)
	}

	// Cached never overcharges: when input is fully cached, nonCached clamps to 0.
	cg := EstimateCost("gpt-4o", 1000, 1000, 0, 1000, 0)
	cw := float64(1000)/1000*p.CachedReadPer1K + float64(1000)/1000*p.OutputPer1K
	if math.Abs(cg-cw) > 1e-9 {
		t.Fatalf("EstimateCost cached-clamp = %.6f, want %.6f", cg, cw)
	}
	// Cache creation billed at write rate (falls back to input rate when write rate is 0).
	if err := UpsertPricing(ModelPricingRow{ModelID: "ctest", InputPer1K: 0.003, OutputPer1K: 0.015, CachedReadPer1K: 0.0003, CachedWritePer1K: 0.00375}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	cp := GetPricing("ctest")
	// cache-inclusive input 1000 = base 500 + read 200 + creation 300.
	cg2 := EstimateCost("ctest", 1000, 1000, 0, 200, 300)
	cw2 := float64(500)/1000*cp.InputPer1K + float64(200)/1000*cp.CachedReadPer1K + float64(300)/1000*cp.CachedWritePer1K + float64(1000)/1000*cp.OutputPer1K
	if math.Abs(cg2-cw2) > 1e-9 {
		t.Fatalf("EstimateCost cache-creation = %.6f, want %.6f", cg2, cw2)
	}
}

func TestGetPricingUnknownFallsBackToDefault(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	p := GetPricing("some-unknown-model-xyz")
	if p.InputPer1K != defaultPricing.InputPer1K || p.OutputPer1K != defaultPricing.OutputPer1K {
		t.Fatalf("unknown model did not fall back to default: got in=%.5f out=%.5f", p.InputPer1K, p.OutputPer1K)
	}
}

func TestUpsertAndDeletePricing(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	if err := UpsertPricing(ModelPricingRow{
		ModelID:     "custom-model",
		DisplayName: "Custom",
		InputPer1K:  0.009,
		OutputPer1K: 0.018,
		Currency:    "USD",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if p := GetPricing("custom-model"); p.InputPer1K != 0.009 {
		t.Fatalf("upserted price not visible: got in=%.5f", p.InputPer1K)
	}

	if err := DeletePricing("custom-model"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if p := GetPricing("custom-model"); p.InputPer1K != defaultPricing.InputPer1K {
		t.Fatalf("deleted price still present: got in=%.5f", p.InputPer1K)
	}
}

func TestEstimateCostServiceTierMultipliers(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	// gpt-4o seeded: in 0.0025, out 0.01.
	base := EstimateCost("gpt-4o", 1000, 1000, 0, 0, 0)

	cases := []struct {
		tier string
		mult float64
	}{
		{"", 1.0},
		{"standard", 1.0},
		{"default", 1.0},
		{"flex", 0.5},
		{"priority", 1.5},
		{"fast", 2.5},
		{"FAST", 2.5}, // case-insensitive
	}
	for _, tc := range cases {
		got := EstimateCostWithServiceTier("gpt-4o", tc.tier, 1000, 1000, 0, 0, 0)
		want := base * tc.mult
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("tier %q: got %.6f, want %.6f", tc.tier, got, want)
		}
	}
}

func TestEstimateCostConfigurableTierMultiplier(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	// Configure a custom fast multiplier for this model.
	if err := UpsertPricing(ModelPricingRow{
		ModelID:                "tiered-model",
		DisplayName:            "Tiered",
		InputPer1K:             0.001,
		OutputPer1K:            0.002,
		TierFastMultiplier:     3.0,
		TierPriorityMultiplier: 1.5,
		TierFlexMultiplier:     0.5,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	base := EstimateCost("tiered-model", 1000, 1000, 0, 0, 0)
	fast := EstimateCostWithServiceTier("tiered-model", "fast", 1000, 1000, 0, 0, 0)
	if math.Abs(fast-base*3.0) > 1e-9 {
		t.Fatalf("custom fast multiplier = %.6f, want %.6f", fast, base*3.0)
	}

	flex := EstimateCostWithServiceTier("tiered-model", "flex", 1000, 1000, 0, 0, 0)
	if math.Abs(flex-base*0.5) > 1e-9 {
		t.Fatalf("custom flex multiplier = %.6f, want %.6f", flex, base*0.5)
	}
}
