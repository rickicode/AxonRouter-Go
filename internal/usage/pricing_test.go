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
	got := EstimateCost("gpt-4o", "chat", 0, 1000, 1000, 0, 200, 0)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("EstimateCost cached = %.6f, want %.6f", got, want)
	}

	// Reasoning billed when reason_per_1k > 0.
	if err := UpsertPricing(ModelPricingRow{ModelID: "rtest", InputPer1K: 0.001, OutputPer1K: 0.002, ReasonPer1K: 0.015}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rp := GetPricing("rtest")
	rw := float64(1000)/1000*rp.InputPer1K + float64(1000)/1000*rp.OutputPer1K + float64(500)/1000*rp.ReasonPer1K
	rg := EstimateCost("rtest", "chat", 0, 1000, 1000, 500, 0, 0)
	if math.Abs(rg-rw) > 1e-9 {
		t.Fatalf("EstimateCost reasoning = %.6f, want %.6f", rg, rw)
	}

	// Cached never overcharges: when input is fully cached, nonCached clamps to 0.
	cg := EstimateCost("gpt-4o", "chat", 0, 1000, 1000, 0, 1000, 0)
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
	cg2 := EstimateCost("ctest", "chat", 0, 1000, 1000, 0, 200, 300)
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

func TestEstimateCostByModality(t *testing.T) {
	database := newTestPricingDB(t)
	InitPricing(database)

	// Image cost = quantity * image_per_unit.
	if err := UpsertPricing(ModelPricingRow{ModelID: "dall-e-3", DisplayName: "DALL-E 3", InputPer1K: 0.001, OutputPer1K: 0.001, ImagePerUnit: 0.04}); err != nil {
		t.Fatalf("upsert image: %v", err)
	}
	if got := EstimateCost("dall-e-3", "image", 3, 0, 0, 0, 0, 0); math.Abs(got-0.12) > 1e-9 {
		t.Fatalf("image cost = %.6f, want 0.12", got)
	}

	// Audio cost = minutes * audio_per_min. TTS/STT share the audio branch.
	if err := UpsertPricing(ModelPricingRow{ModelID: "whisper-1", DisplayName: "Whisper", InputPer1K: 0.001, OutputPer1K: 0.001, AudioPerMin: 0.006}); err != nil {
		t.Fatalf("upsert audio: %v", err)
	}
	if got := EstimateCost("whisper-1", "audio", 5, 0, 0, 0, 0, 0); math.Abs(got-0.03) > 1e-9 {
		t.Fatalf("audio cost = %.6f, want 0.03", got)
	}
	if got := EstimateCost("whisper-1", "stt", 2, 0, 0, 0, 0, 0); math.Abs(got-0.012) > 1e-9 {
		t.Fatalf("stt cost = %.6f, want 0.012", got)
	}
	if got := EstimateCost("whisper-1", "tts", 10, 0, 0, 0, 0, 0); math.Abs(got-0.06) > 1e-9 {
		t.Fatalf("tts cost = %.6f, want 0.06", got)
	}

	// Video cost = quantity * video_per_unit.
	if err := UpsertPricing(ModelPricingRow{ModelID: "sora-1", DisplayName: "Sora", InputPer1K: 0.001, OutputPer1K: 0.001, VideoPerUnit: 0.5}); err != nil {
		t.Fatalf("upsert video: %v", err)
	}
	if got := EstimateCost("sora-1", "video", 2, 0, 0, 0, 0, 0); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("video cost = %.6f, want 1.0", got)
	}

	// Embedding cost = input_tokens * input_per_1k / 1000.
	if err := UpsertPricing(ModelPricingRow{ModelID: "text-embedding-3-small", DisplayName: "Embedding 3 Small", InputPer1K: 0.02, OutputPer1K: 0.001}); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}
	if got := EstimateCost("text-embedding-3-small", "embedding", 0, 1500, 0, 0, 0, 0); math.Abs(got-0.03) > 1e-9 {
		t.Fatalf("embedding cost = %.6f, want 0.03", got)
	}

	// Unknown or zero-quantity modality pricing returns 0 instead of falling back to text tokens.
	if got := EstimateCost("dall-e-3", "image", 0, 1000, 1000, 0, 0, 0); got != 0 {
		t.Fatalf("zero-quantity image cost = %.6f, want 0", got)
	}
}
