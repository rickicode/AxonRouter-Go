package usage

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// LogFilter holds filter parameters for log queries.
type LogFilter struct {
	ProviderTypeID string
	ConnectionID   string
	ModelID        string
	ComboID        string
	Modality       string
	StatusFilter   string // "success", "error", or ""
	StatusCode     int    // exact HTTP status (500 means >=500 i.e. 5xx)
	Search         string // search in error_message
	Since          int64  // unix ms timestamp
	Until          int64  // unix ms timestamp
}

// QueryLogs returns paginated request logs with filters.
func QueryLogs(database *sql.DB, page, perPage int, filter LogFilter) (*db.PaginatedResponse, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	where, args := buildWhereClause(filter)

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs r LEFT JOIN connections c ON r.connection_id = c.id WHERE %s", where)
	database.QueryRow(countQuery, args...).Scan(&total)

	offset := (page - 1) * perPage
	dataQuery := fmt.Sprintf(`
SELECT r.id, r.timestamp, r.connection_id, c.name AS connection_name, r.provider_type_id, r.model_id, r.combo_id,
r.proxy_pool_id, p.name AS proxy_pool_name,
COALESCE(NULLIF(k.name,''), NULLIF(k.key_value,''), r.api_key_id) AS api_key,
r.api_type, r.modality, r.input_tokens, r.output_tokens, r.reasoning_tokens, r.cached_tokens, r.cache_creation_tokens,
  r.stream, r.tokens_estimated,
  r.latency_ms, r.status_code, r.error_message, r.cost_usd, r.service_tier, r.client_ip, r.user_agent, r.created_at
  FROM request_logs r
LEFT JOIN connections c ON r.connection_id = c.id
LEFT JOIN proxy_pools p ON r.proxy_pool_id = p.id
LEFT JOIN api_keys k ON r.api_key_id = k.id
WHERE %s
ORDER BY r.timestamp DESC
LIMIT ? OFFSET ?
`, where)
	args = append(args, perPage, offset)

	rows, err := database.Query(dataQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []db.RequestLog
	for rows.Next() {
		l := db.RequestLog{}
		rows.Scan(&l.ID, &l.Timestamp, &l.ConnectionID, &l.ConnectionName, &l.ProviderTypeID,
			&l.ModelID, &l.ComboID, &l.ProxyPoolID, &l.ProxyPoolName, &l.ApiKey, &l.ApiType, &l.Modality,
			&l.InputTokens, &l.OutputTokens, &l.ReasoningTokens, &l.CachedTokens, &l.CacheCreationTokens,
			&l.Stream, &l.TokensEstimated,
			&l.LatencyMs, &l.StatusCode, &l.ErrorMessage,
			&l.CostUsd, &l.ServiceTier, &l.ClientIP, &l.UserAgent, &l.CreatedAt)
		logs = append(logs, l)
	}

	totalPages := total / perPage
	if total%perPage > 0 {
		totalPages++
	}

	return &db.PaginatedResponse{
		Data: logs,
		Pagination: db.Pagination{
			Page:       page,
			PerPage:    perPage,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

func buildWhereClause(f LogFilter) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	conditions = append(conditions, "1=1")

	if f.ProviderTypeID != "" {
		conditions = append(conditions, "r.provider_type_id = ?")
		args = append(args, f.ProviderTypeID)
	}
	if f.ConnectionID != "" {
		conditions = append(conditions, "r.connection_id = ?")
		args = append(args, f.ConnectionID)
	}
	if f.ModelID != "" {
		conditions = append(conditions, "r.model_id = ?")
		args = append(args, f.ModelID)
	}
	if f.ComboID != "" {
		conditions = append(conditions, "r.combo_id = ?")
		args = append(args, f.ComboID)
	}
	if f.Modality != "" {
		conditions = append(conditions, "r.modality = ?")
		args = append(args, f.Modality)
	}
	if f.StatusFilter == "success" {
		conditions = append(conditions, "(r.error_message IS NULL OR r.error_message = '')")
	} else if f.StatusFilter == "error" {
		conditions = append(conditions, "r.error_message IS NOT NULL AND r.error_message != ''")
	}
	if f.StatusCode > 0 {
		// The dashboard "Error" pill sends status_code=500 to mean any
		// client or server error except the dedicated 401/429 filters.
		// Exact 200/401/429/xxx pills use status_code = ? below.
		if f.StatusCode == 500 {
			conditions = append(conditions, "((r.status_code >= 400 AND r.status_code NOT IN (401, 429)) OR (r.status_code = 0 AND r.error_message IS NOT NULL AND r.error_message != ''))")
		} else {
			conditions = append(conditions, "r.status_code = ?")
			args = append(args, f.StatusCode)
		}
	}
	if f.Search != "" {
		conditions = append(conditions, "r.error_message LIKE ?")
		args = append(args, "%"+f.Search+"%")
	}
	if f.Since > 0 {
		conditions = append(conditions, "r.timestamp >= ?")
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		conditions = append(conditions, "r.timestamp <= ?")
		args = append(args, f.Until)
	}

	return strings.Join(conditions, " AND "), args
}
