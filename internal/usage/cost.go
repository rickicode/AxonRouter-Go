package usage

import (
	"database/sql"
	"time"
)

// CostThisMonth returns the total cost_usd spent from the start of the current
// UTC month until now, grouped by provider and as a total.
func CostThisMonth(db *sql.DB) (map[string]float64, float64, error) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return CostBetween(db, start.UnixMilli(), now.UnixMilli())
}

// CostBetween returns the total cost_usd between the given millisecond timestamps,
// grouped by provider_type_id.
func CostBetween(db *sql.DB, fromMs, toMs int64) (map[string]float64, float64, error) {
	rows, err := db.Query(`
		SELECT provider_type_id, COALESCE(SUM(CASE WHEN flat_rate = 1 THEN 0 ELSE cost_usd END), 0)
		FROM request_logs
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY provider_type_id
	`, fromMs, toMs)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make(map[string]float64)
	var total float64
	for rows.Next() {
		var providerID string
		var cost float64
		if err := rows.Scan(&providerID, &cost); err != nil {
			return nil, 0, err
		}
		out[providerID] = cost
		total += cost
	}
	return out, total, rows.Err()
}
