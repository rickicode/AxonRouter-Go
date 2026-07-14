package usage

import (
	"database/sql"
	"time"
)

// SavingsBetween estimates how much was saved versus paying list price for
// input/output/reasoning tokens with no cache or routing discounts.
// fromMs/toMs are request_logs timestamps in milliseconds.
func SavingsBetween(db *sql.DB, fromMs, toMs int64) (float64, error) {
	rows, err := db.Query(`
		SELECT model_id,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM request_logs
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY model_id
	`, fromMs, toMs)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total float64
	for rows.Next() {
		var modelID string
		var inputTokens, outputTokens, reasoningTokens int64
		var actualCost float64
		if err := rows.Scan(&modelID, &inputTokens, &outputTokens, &reasoningTokens, &actualCost); err != nil {
			return 0, err
		}
		p := GetPricing(modelID)
		baseline := float64(inputTokens)/1000.0*p.InputPer1K +
			float64(outputTokens)/1000.0*p.OutputPer1K +
			float64(reasoningTokens)/1000.0*p.ReasonPer1K
		saved := baseline - actualCost
		if saved > 0 {
			total += saved
		}
	}
	return total, rows.Err()
}

// SavingsByProvider returns per-provider estimated savings for the given window.
func SavingsByProvider(db *sql.DB, fromMs, toMs int64) (map[string]float64, float64, error) {
	rows, err := db.Query(`
		SELECT provider_type_id, model_id,
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM request_logs
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY provider_type_id, model_id
	`, fromMs, toMs)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make(map[string]float64)
	var total float64
	for rows.Next() {
		var providerID, modelID string
		var inputTokens, outputTokens, reasoningTokens int64
		var actualCost float64
		if err := rows.Scan(&providerID, &modelID, &inputTokens, &outputTokens, &reasoningTokens, &actualCost); err != nil {
			return nil, 0, err
		}
		p := GetPricing(modelID)
		baseline := float64(inputTokens)/1000.0*p.InputPer1K +
			float64(outputTokens)/1000.0*p.OutputPer1K +
			float64(reasoningTokens)/1000.0*p.ReasonPer1K
		saved := baseline - actualCost
		if saved > 0 {
			out[providerID] += saved
			total += saved
		}
	}
	return out, total, rows.Err()
}

// SavingsThisMonth returns estimated savings from the start of the current UTC month until now.
func SavingsThisMonth(db *sql.DB) (map[string]float64, float64, error) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return SavingsByProvider(db, start.UnixMilli(), now.UnixMilli())
}
