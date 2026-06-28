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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs WHERE %s", where)
	database.QueryRow(countQuery, args...).Scan(&total)

	offset := (page - 1) * perPage
	dataQuery := fmt.Sprintf(`
		SELECT id, timestamp, connection_id, provider_type_id, model_id, combo_id,
		       modality, input_tokens, output_tokens, reasoning_tokens,
		       latency_ms, status_code, error_message, cost_usd, created_at
		FROM request_logs WHERE %s
		ORDER BY timestamp DESC
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
		rows.Scan(&l.ID, &l.Timestamp, &l.ConnectionID, &l.ProviderTypeID,
			&l.ModelID, &l.ComboID, &l.Modality,
			&l.InputTokens, &l.OutputTokens, &l.ReasoningTokens,
			&l.LatencyMs, &l.StatusCode, &l.ErrorMessage,
			&l.CostUsd, &l.CreatedAt)
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
		conditions = append(conditions, "provider_type_id = ?")
		args = append(args, f.ProviderTypeID)
	}
	if f.ConnectionID != "" {
		conditions = append(conditions, "connection_id = ?")
		args = append(args, f.ConnectionID)
	}
	if f.ModelID != "" {
		conditions = append(conditions, "model_id = ?")
		args = append(args, f.ModelID)
	}
	if f.ComboID != "" {
		conditions = append(conditions, "combo_id = ?")
		args = append(args, f.ComboID)
	}
	if f.Modality != "" {
		conditions = append(conditions, "modality = ?")
		args = append(args, f.Modality)
	}
	if f.StatusFilter == "success" {
		conditions = append(conditions, "(error_message IS NULL OR error_message = '')")
	} else if f.StatusFilter == "error" {
		conditions = append(conditions, "error_message IS NOT NULL AND error_message != ''")
	}
	if f.Search != "" {
		conditions = append(conditions, "error_message LIKE ?")
		args = append(args, "%"+f.Search+"%")
	}
	if f.Since > 0 {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, f.Until)
	}

	return strings.Join(conditions, " AND "), args
}
