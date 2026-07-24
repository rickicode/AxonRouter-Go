package usage

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"sync"
	"time"
)

type Pricing struct {
	InputPer1K       float64
	OutputPer1K      float64
	ReasonPer1K      float64
	ImagePerUnit     float64
	AudioPerMin      float64
	VideoPerUnit     float64
	CachedReadPer1K  float64
	CachedWritePer1K float64
}

var defaultPricing = Pricing{InputPer1K: 0.001, OutputPer1K: 0.002}

type ModelPricingRow struct {
	ModelID string `json:"model_id"`
	DisplayName string `json:"display_name"`
	InputPer1K float64 `json:"input_per_1k"`
	OutputPer1K float64 `json:"output_per_1k"`
	ReasonPer1K float64 `json:"reason_per_1k"`
	CachedReadPer1K float64 `json:"cached_read_per_1k"`
	CachedWritePer1K float64 `json:"cached_write_per_1k"`
	ImagePerUnit float64 `json:"image_per_unit"`
	AudioPerMin float64 `json:"audio_per_min"`
	VideoPerUnit float64 `json:"video_per_unit"`
	Currency string `json:"currency"`
	UpdatedAt int64 `json:"updated_at"`
	// ServiceKinds is populated from the model catalog for display; not persisted.
	ServiceKinds []string `json:"service_kinds,omitempty"`
}

var (
	pricingDB   *sql.DB
	pricingMu   sync.RWMutex
	pricingRows = map[string]ModelPricingRow{}
)

// InitPricing loads pricing from DB.
func InitPricing(db *sql.DB) {
	pricingDB = db
	ReloadPricing()
}

// ReloadPricing refreshes the in-memory cache from the DB.
func ReloadPricing() {
	if pricingDB == nil {
		return
	}
	rows, err := pricingDB.Query(`SELECT model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, image_per_unit, audio_per_min, video_per_unit, currency, updated_at FROM model_pricing`)
	if err != nil {
		return
	}
	defer rows.Close()
	fresh := map[string]ModelPricingRow{}
	for rows.Next() {
		var r ModelPricingRow
		if err := rows.Scan(
			&r.ModelID, &r.DisplayName, &r.InputPer1K, &r.OutputPer1K, &r.ReasonPer1K,
			&r.CachedReadPer1K, &r.CachedWritePer1K, &r.ImagePerUnit, &r.AudioPerMin, &r.VideoPerUnit,
			&r.Currency, &r.UpdatedAt,
		); err != nil {
			continue
		}
		fresh[r.ModelID] = r
	}
	pricingMu.Lock()
	pricingRows = fresh
	pricingMu.Unlock()
}

// splitModel splits a model id after the first slash.
// StartPeriodicReload refreshes the pricing cache at the given interval.
func StartPeriodicReload(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ReloadPricing()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func splitModel(s string) (string, string) {
	for i, c := range s {
		if c == '/' {
			prefix := strings.TrimPrefix(s[:i], "@")
			return prefix, s[i+1:]
		}
	}
	return "", s
}

// GetPricing returns the pricing for a model. Provider prefixes are stripped.
func GetPricing(modelID string) Pricing {
	_, model := splitModel(modelID)

	pricingMu.RLock()
	rows := pricingRows
	pricingMu.RUnlock()

	if r, ok := rows[model]; ok {
		return Pricing{
			InputPer1K:       r.InputPer1K,
			OutputPer1K:      r.OutputPer1K,
			ReasonPer1K:      r.ReasonPer1K,
			ImagePerUnit:     r.ImagePerUnit,
			AudioPerMin:      r.AudioPerMin,
			VideoPerUnit:     r.VideoPerUnit,
			CachedReadPer1K:  r.CachedReadPer1K,
			CachedWritePer1K: r.CachedWritePer1K,
		}
	}

	// Deterministic longest-substring fallback.
	var matches []string
	for key := range rows {
		if strings.Contains(model, key) {
			matches = append(matches, key)
		}
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			if len(matches[i]) != len(matches[j]) {
				return len(matches[i]) > len(matches[j])
			}
			return matches[i] < matches[j]
		})
		r := rows[matches[0]]
		return Pricing{
			InputPer1K:       r.InputPer1K,
			OutputPer1K:      r.OutputPer1K,
			ReasonPer1K:      r.ReasonPer1K,
			ImagePerUnit:     r.ImagePerUnit,
			AudioPerMin:      r.AudioPerMin,
			VideoPerUnit:     r.VideoPerUnit,
			CachedReadPer1K:  r.CachedReadPer1K,
			CachedWritePer1K: r.CachedWritePer1K,
		}
	}
	return defaultPricing
}

// EstimateCost returns the estimated cost in USD.
// Token convention: inputTokens is cache-inclusive (input + cache_read + cache_creation),
// matching the per-provider usage reports after extraction. Cache-read tokens are billed at the
// read rate and cache-creation tokens at the write rate (falling back to the input rate when no
// dedicated write rate is configured). This mirrors OmniRoute's computeCostFromPricing.
//
// For non-text modalities, cost is driven by Quantity:
//   - image: quantity * image_per_unit
//   - audio/stt/tts: quantity * audio_per_min
//   - video: quantity * video_per_unit
//   - embedding: input_tokens * input_per_1k / 1000
func EstimateCost(modelID, modality string, quantity, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens int64) float64 {
	p := GetPricing(modelID)
	switch modality {
	case "image":
		if quantity > 0 && p.ImagePerUnit > 0 {
			return float64(quantity) * p.ImagePerUnit
		}
		return 0
	case "audio", "stt", "tts":
		if quantity > 0 && p.AudioPerMin > 0 {
			return float64(quantity) * p.AudioPerMin
		}
		return 0
	case "video":
		if quantity > 0 && p.VideoPerUnit > 0 {
			return float64(quantity) * p.VideoPerUnit
		}
		return 0
	case "embedding":
		if inputTokens > 0 && p.InputPer1K > 0 {
			return float64(inputTokens) / 1000.0 * p.InputPer1K
		}
		return 0
	}

	nonCached := inputTokens - cachedTokens - cacheCreationTokens
	if nonCached < 0 {
		nonCached = 0
	}
	writeRate := p.CachedWritePer1K
	if writeRate == 0 {
		writeRate = p.InputPer1K
	}
	cost := float64(nonCached) / 1000.0 * p.InputPer1K
	cost += float64(cachedTokens) / 1000.0 * p.CachedReadPer1K
	cost += float64(cacheCreationTokens) / 1000.0 * writeRate
	cost += float64(outputTokens) / 1000.0 * p.OutputPer1K
	cost += float64(reasoningTokens) / 1000.0 * p.ReasonPer1K
	return cost
}

// ListPricing returns all rows from DB.
func ListPricing() []ModelPricingRow {
	if pricingDB == nil {
		return nil
	}
	rows, err := pricingDB.Query(`SELECT model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, image_per_unit, audio_per_min, video_per_unit, currency, updated_at FROM model_pricing ORDER BY model_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ModelPricingRow{}
	for rows.Next() {
		var r ModelPricingRow
		if err := rows.Scan(
			&r.ModelID, &r.DisplayName, &r.InputPer1K, &r.OutputPer1K, &r.ReasonPer1K,
			&r.CachedReadPer1K, &r.CachedWritePer1K, &r.ImagePerUnit, &r.AudioPerMin, &r.VideoPerUnit,
			&r.Currency, &r.UpdatedAt,
		); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

// UpsertPricing inserts or replaces a row.
func UpsertPricing(row ModelPricingRow) error {
	if pricingDB == nil {
		return sql.ErrConnDone
	}
	if row.Currency == "" {
		row.Currency = "USD"
	}
	if row.UpdatedAt == 0 {
		row.UpdatedAt = time.Now().Unix()
	}
	_, err := pricingDB.Exec(
		`
INSERT INTO model_pricing (model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, image_per_unit, audio_per_min, video_per_unit, currency, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(model_id) DO UPDATE SET
display_name = excluded.display_name,
input_per_1k = excluded.input_per_1k,
output_per_1k = excluded.output_per_1k,
reason_per_1k = excluded.reason_per_1k,
cached_read_per_1k = excluded.cached_read_per_1k,
cached_write_per_1k = excluded.cached_write_per_1k,
image_per_unit = excluded.image_per_unit,
audio_per_min = excluded.audio_per_min,
video_per_unit = excluded.video_per_unit,
currency = excluded.currency,
updated_at = excluded.updated_at`,
		row.ModelID, row.DisplayName, row.InputPer1K, row.OutputPer1K, row.ReasonPer1K,
		row.CachedReadPer1K, row.CachedWritePer1K, row.ImagePerUnit, row.AudioPerMin, row.VideoPerUnit,
		row.Currency, row.UpdatedAt,
	)
	if err != nil {
		return err
	}
	ReloadPricing()
	return nil
}

// DeletePricing removes a row.
func DeletePricing(modelID string) error {
	if pricingDB == nil {
		return sql.ErrConnDone
	}
	if _, err := pricingDB.Exec(`DELETE FROM model_pricing WHERE model_id = ?`, modelID); err != nil {
		return err
	}
	ReloadPricing()
	return nil
}
