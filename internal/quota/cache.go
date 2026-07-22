package quota

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// SaveQuotaCache persists fetched quota data to the quota_cache table.
// Called by the background scheduler after each fetch cycle.
// Skips saving failed fetches if there's already valid cached data (preserves last good state).
func SaveQuotaCache(db *sql.DB, results []ProviderQuota) {
	saveQuotaCacheResults(db, results)
	if err := PruneQuotaCache(db); err != nil {
		log.Printf("quota cache: prune failed: %v", err)
	}
}

// saveQuotaCacheResults writes the fetched results to quota_cache.
func saveQuotaCacheResults(db *sql.DB, results []ProviderQuota) {
	now := time.Now().Unix()
	for _, provider := range results {
		for _, conn := range provider.Connections {
			// If fetch failed, check if there's existing good data — don't overwrite it
			if conn.Error != "" {
				var existingStatus string
				err := db.QueryRow(`SELECT status FROM quota_cache WHERE id = ?`, conn.ConnectionID).Scan(&existingStatus)
				if err == nil && existingStatus != "error" && existingStatus != "no_data" {
					db.Exec(`UPDATE quota_cache SET updated_at = ? WHERE id = ?`, now, conn.ConnectionID)
					continue
				}
			}

			// Split quotas by scope: codex (default) vs spark
			var codexQuotas, sparkQuotas []QuotaItem
			for _, q := range conn.Quotas {
				if q.Scope == "spark" {
					sparkQuotas = append(sparkQuotas, q)
				} else {
					codexQuotas = append(codexQuotas, q)
				}
			}

			// Save codex/default quotas under connID
			saveQuotaCacheEntry(db, conn.ConnectionID, conn.ConnectionID, provider.ProviderID,
				conn.ConnectionName, conn.Plan, codexQuotas, conn.Error, conn.FetchedAt, now)

			// Save spark quotas under connID:spark if any exist
			if len(sparkQuotas) > 0 {
				sparkKey := conn.ConnectionID + ":spark"
				saveQuotaCacheEntry(db, sparkKey, conn.ConnectionID, provider.ProviderID,
					conn.ConnectionName+" (Spark)", conn.Plan, sparkQuotas, conn.Error, conn.FetchedAt, now)
			}
		}
	}
}

// PruneQuotaCache removes quota_cache rows for connections that are no longer
// active OAuth connections. This prevents stale/incomplete OAuth attempts or
// deleted connections from appearing in the Quota dashboard.
func PruneQuotaCache(db *sql.DB) error {
	_, err := db.Exec(`
		DELETE FROM quota_cache
		WHERE connection_id NOT IN (
			SELECT id FROM connections WHERE auth_type = 'oauth' AND is_active = 1
		)
	`)
	return err
}

// saveQuotaCacheEntry persists a single quota cache entry to the DB.
func saveQuotaCacheEntry(db *sql.DB, cacheID, connID, providerID, connName, plan string,
	quotas []QuotaItem, connError string, fetchedAt, now int64,
) {
	status := evaluateCacheStatus(quotas, connError)
	quotasJSON, err := json.Marshal(quotas)
	if err != nil {
		quotasJSON = []byte("[]")
	}

	_, err = db.Exec(`
		INSERT INTO quota_cache (id, connection_id, provider_type_id, connection_name, plan, quotas, status, error, fetched_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			plan = excluded.plan,
			quotas = excluded.quotas,
			status = excluded.status,
			error = excluded.error,
			fetched_at = excluded.fetched_at,
			updated_at = excluded.updated_at
	`, cacheID, connID, providerID, connName,
		plan, string(quotasJSON), status, connError, fetchedAt, now)
	if err != nil {
		log.Printf("quota cache: save error for %s: %v", cacheID, err)
	}
}

// evaluateCacheStatus determines the display status for the cache entry.
func evaluateCacheStatus(quotas []QuotaItem, connError string) string {
	if connError != "" {
		return "error"
	}
	if len(quotas) == 0 {
		return "no_data"
	}
	hasExhausted := false
	allUnlimited := true
	for _, q := range quotas {
		if !q.Unlimited {
			allUnlimited = false
			if q.RemainingPct <= 0 {
				hasExhausted = true
			}
		}
	}
	if allUnlimited {
		return "unlimited"
	}
	if hasExhausted {
		return "exhausted"
	}
	return "ok"
}

// NextProviderResets returns the earliest future reset_at per provider from
// cached quota data. Reset times are returned as RFC3339 strings. Providers
// with no future reset are omitted.
func NextProviderResets(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT provider_type_id, quotas FROM quota_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	best := make(map[string]time.Time)
	for rows.Next() {
		var providerID, quotasJSON string
		if err := rows.Scan(&providerID, &quotasJSON); err != nil {
			continue
		}
		var quotas []QuotaItem
		if err := json.Unmarshal([]byte(quotasJSON), &quotas); err != nil {
			continue
		}
		for _, q := range quotas {
			t, err := parseResetAt(q.ResetAt)
			if err != nil || !t.After(now) {
				continue
			}
			if cur, ok := best[providerID]; !ok || t.Before(cur) {
				best[providerID] = t
			}
		}
	}

	out := make(map[string]string, len(best))
	for providerID, t := range best {
		out[providerID] = t.Format(time.RFC3339)
	}
	return out, rows.Err()
}

func parseResetAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty reset_at")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05", s)
}

// QuotaCacheEntry is a single row from quota_cache for the API response.
type QuotaCacheEntry struct {
	ID             string      `json:"id"`
	ConnectionID   string      `json:"connection_id"`
	ConnectionName string      `json:"connection_name"`
	ProviderID     string      `json:"provider_id"`
	ProviderName   string      `json:"provider_name"`
	DisplayName    string      `json:"display_name"`
	Color          string      `json:"color"`
	IconFile       string      `json:"icon_file"`
	Plan           string      `json:"plan,omitempty"`
	Quotas         []QuotaItem `json:"quotas"`
	Status         string      `json:"status"`
	Error          string      `json:"error,omitempty"`
	FetchedAt      int64       `json:"fetched_at"`
	AuthType       string      `json:"auth_type"`
	OAuthExpiresAt int64       `json:"oauth_expires_at,omitempty"`
}

// QuotaCacheResponse is the paginated API response.
type QuotaCacheResponse struct {
	Items      []QuotaCacheEntry `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
	TotalPages int               `json:"total_pages"`
}

// LoadQuotaCache reads cached quota data from DB with filters and pagination.
func LoadQuotaCache(db *sql.DB, providerID, search, status string, page, perPage int) (*QuotaCacheResponse, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}

	providerNames := loadProviderNames(db)

	where, args := buildCacheWhere(providerID, search, status)

	// Count total
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM quota_cache %s", where)
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count quota_cache: %w", err)
	}

	totalPages := total / perPage
	if total%perPage > 0 {
		totalPages++
	}

	selectQuery := fmt.Sprintf(`
		SELECT q.id, q.connection_id, q.provider_type_id, q.connection_name, q.plan, q.quotas, q.status, q.error, q.fetched_at,
		       COALESCE(c.auth_type, ''), COALESCE(c.oauth_expires_at, 0)
		FROM quota_cache q
		LEFT JOIN connections c ON q.connection_id = c.id
		%s
		ORDER BY q.provider_type_id, q.connection_name
		LIMIT ? OFFSET ?
	`, where)
	offset := (page - 1) * perPage
	args = append(args, perPage, offset)

	rows, err := db.Query(selectQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query quota_cache: %w", err)
	}
	defer rows.Close()

	var items []QuotaCacheEntry
	for rows.Next() {
		var e QuotaCacheEntry
		var quotasJSON string
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.ProviderID, &e.ConnectionName,
			&e.Plan, &quotasJSON, &e.Status, &e.Error, &e.FetchedAt,
			&e.AuthType, &e.OAuthExpiresAt); err != nil {
			log.Printf("quota cache: scan error: %v", err)
			continue
		}
		json.Unmarshal([]byte(quotasJSON), &e.Quotas)

		// Enrich with provider metadata
		if meta, ok := knownProviders[e.ProviderID]; ok {
			e.Color = meta.Color
			e.IconFile = meta.IconFile
		}
		e.ProviderName = e.ProviderID
		if dbName, ok := providerNames[e.ProviderID]; ok && dbName != "" {
			e.DisplayName = dbName
		} else if meta, ok := knownProviders[e.ProviderID]; ok {
			e.DisplayName = meta.DisplayName
		} else {
			e.DisplayName = e.ProviderID
		}

		items = append(items, e)
	}

	return &QuotaCacheResponse{
		Items:      items,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, nil
}

func buildCacheWhere(providerID, search, status string) (string, []any) {
	var conditions []string
	var args []any

	if providerID != "" {
		conditions = append(conditions, "provider_type_id = ?")
		args = append(args, providerID)
	}
	if search != "" {
		conditions = append(conditions, "connection_name LIKE ?")
		args = append(args, "%"+search+"%")
	}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}

	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

// UpdateConnectionQuotaStatus updates connection status in DB + connstate
// based on quota data. Called by the background scheduler.
// When a connection transitions to "ready", its ExhaustionCache entry is cleared
// so routing picks it up immediately instead of waiting for the TTL to expire.
func UpdateConnectionQuotaStatus(db *sql.DB, store *connstate.Store, exhaustion *ExhaustionCache, connID string, quotas []QuotaItem, connError string, changed *bool) {
	newStatus := "ready"
	if connError != "" {
		// On a clear auth/permission failure (token expired, access denied, missing
		// projectId, refresh failed) disable the connection so routing stops using it
		// and the dashboard surfaces the cause. Manual terminal states are preserved.
		if strings.Contains(connError, "token expired") || strings.Contains(connError, "access denied") ||
			strings.Contains(connError, "no projectId") || strings.Contains(connError, "token refresh failed") {
			var cur string
			if err := db.QueryRow(`SELECT status FROM connections WHERE id = ?`, connID).Scan(&cur); err == nil {
				switch cur {
				case "disabled", "suspended", "balance_empty":
					// already in a terminal state; leave it
				default:
					if _, derr := db.Exec(`UPDATE connections SET is_active = 0, status = 'disabled', updated_at = ? WHERE id = ?`, time.Now().Unix(), connID); derr == nil {
						if cs := store.Get(connID); cs != nil {
							cs.SetStatus(connstate.Status("disabled"), connError)
						}
						*changed = true
						log.Printf("quota: connection %s disabled: %s", connID, connError)
					}
				}
			}
		}
		return // don't change status on fetch errors
	}
	if len(quotas) > 0 {
		hasExhausted := false
		for _, q := range quotas {
			if !q.Unlimited && q.RemainingPct <= 0 {
				hasExhausted = true
				break
			}
		}
		if hasExhausted {
			newStatus = "quota_exhausted"
		}

		// Cache the minimum remaining percentage for quota-aware routing.
		minRemaining := 100.0
		hasQuota := false
		for _, q := range quotas {
			hasQuota = true
			if !q.Unlimited && q.RemainingPct < minRemaining {
				minRemaining = q.RemainingPct
			}
		}
		if !hasQuota {
			minRemaining = 100
		}
		if cs := store.Get(connID); cs != nil {
			cs.SetRemainingPct(minRemaining)
		}
	}

	// Get current DB status
	var currentStatus string
	err := db.QueryRow(`SELECT status FROM connections WHERE id = ?`, connID).Scan(&currentStatus)
	if err != nil {
		return
	}

	// Only update if status actually changed
	if currentStatus == newStatus {
		return
	}

	// Skip if connection is in a manual-only state
	switch currentStatus {
	case "disabled", "auth_failed", "suspended", "balance_empty":
		return
	}

	_, err = db.Exec(`UPDATE connections SET status = ?, updated_at = ? WHERE id = ?`,
		newStatus, time.Now().Unix(), connID)
	if err != nil {
		log.Printf("quota: failed to update connection %s status: %v", connID, err)
		return
	}

	// Sync connstate
	if cs := store.Get(connID); cs != nil {
		cs.SetStatus(connstate.Status(newStatus), "")
	}

	// Clear exhaustion cache when connection recovers so routing picks it up immediately
	if newStatus == "ready" && exhaustion != nil {
		exhaustion.Clear(connID)
	}

	*changed = true
	log.Printf("quota: connection %s status: %s → %s", connID, currentStatus, newStatus)
}
