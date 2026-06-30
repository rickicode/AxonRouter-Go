package quota

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// QuotaItem represents a single quota window.
type QuotaItem struct {
	Name         string  `json:"name"`
	Used         float64 `json:"used"`
	Total        float64 `json:"total"`
	RemainingPct float64 `json:"remaining_pct"`
	ResetAt      string  `json:"reset_at,omitempty"`
	Unlimited    bool    `json:"unlimited"`
	ModelKey     string  `json:"model_key,omitempty"`
	Family       string  `json:"family,omitempty"`
}

// ConnectionQuota holds quota data for a single connection.
type ConnectionQuota struct {
	ConnectionID   string      `json:"connection_id"`
	ConnectionName string      `json:"connection_name"`
	ProviderID     string      `json:"provider_id"`
	ProviderName   string      `json:"provider_name"`
	Plan           string      `json:"plan,omitempty"`
	Quotas         []QuotaItem `json:"quotas"`
	Message        string      `json:"message,omitempty"`
	Error          string      `json:"error,omitempty"`
	FetchedAt      int64       `json:"fetched_at"`
}

// ProviderQuota groups connections under a provider.
type ProviderQuota struct {
	ProviderID   string            `json:"provider_id"`
	ProviderName string            `json:"provider_name"`
	DisplayName  string            `json:"display_name"`
	Color        string            `json:"color"`
	IconFile     string            `json:"icon_file"`
	Connections  []ConnectionQuota `json:"connections"`
}

// connRow is the DB row we need for quota fetching.
type connRow struct {
	ID                   string
	ProviderTypeID       string
	Name                 string
	OAuthToken           sql.NullString
	ProviderSpecificData sql.NullString
}

// providerMeta holds display info for a provider type.
type providerMeta struct {
	DisplayName string
	Color       string
	IconFile    string
}

// knownProviders maps provider_type_id to display metadata (colors, icons).
// Display names are loaded from the DB provider_types table at runtime.
var knownProviders = map[string]providerMeta{
	"cx":  {DisplayName: "Codex", Color: "#10a37f", IconFile: "codex.svg"},
	"ag":  {DisplayName: "Antigravity", Color: "#4285f4", IconFile: "antigravity.svg"},
	"kiro": {DisplayName: "Kiro", Color: "#ff9900", IconFile: "kiro.svg"},
}
// FetchAllQuota fetches quota for all OAuth connections across all providers.
func FetchAllQuota(db *sql.DB) ([]ProviderQuota, error) {
	// Load provider display names from DB
	providerNames := loadProviderNames(db)

	rows, err := db.Query(`
		SELECT id, provider_type_id, name, oauth_token, provider_specific_data
		FROM connections
		WHERE auth_type = 'oauth' AND is_active = 1
		ORDER BY provider_type_id, name
	`)
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer rows.Close()

	var conns []connRow
	for rows.Next() {
		var c connRow
		if err := rows.Scan(&c.ID, &c.ProviderTypeID, &c.Name, &c.OAuthToken, &c.ProviderSpecificData); err != nil {
			log.Printf("quota: scan connection: %v", err)
			continue
		}
		conns = append(conns, c)
	}

	if len(conns) == 0 {
		return []ProviderQuota{}, nil
	}

	// Group by provider
	grouped := make(map[string][]connRow)
	for _, c := range conns {
		grouped[c.ProviderTypeID] = append(grouped[c.ProviderTypeID], c)
	}

	var mu sync.Mutex
	var results []ProviderQuota

	for providerID, providerConns := range grouped {
		meta, ok := knownProviders[providerID]
		if !ok {
			meta = providerMeta{DisplayName: providerID, Color: "#888888", IconFile: ""}
		}
		// Use DB display name if available
		if dbName, ok := providerNames[providerID]; ok && dbName != "" {
			meta.DisplayName = dbName
		}

		pq := ProviderQuota{
			ProviderID:   providerID,
			ProviderName: providerID,
			DisplayName:  meta.DisplayName,
			Color:        meta.Color,
			IconFile:     meta.IconFile,
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, 5) // max 5 parallel fetches

		for _, conn := range providerConns {
			wg.Add(1)
			sem <- struct{}{}
			go func(c connRow) {
				defer wg.Done()
				defer func() { <-sem }()

				cq := fetchConnectionQuota(c, providerID)

				mu.Lock()
				pq.Connections = append(pq.Connections, cq)
				mu.Unlock()
			}(conn)
		}

		wg.Wait()
		mu.Lock()
		results = append(results, pq)
		mu.Unlock()
	}

	return results, nil
}

// loadProviderNames loads display_name from provider_types table.
func loadProviderNames(db *sql.DB) map[string]string {
	names := make(map[string]string)
	rows, err := db.Query(`SELECT id, display_name FROM provider_types`)
	if err != nil {
		return names
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err == nil {
			names[id] = name
		}
	}
	return names
}

// FetchConnectionQuota fetches quota for a single connection.
func FetchConnectionQuota(db *sql.DB, connectionID string) (*ConnectionQuota, error) {
	var c connRow
	err := db.QueryRow(`
		SELECT id, provider_type_id, name, oauth_token, provider_specific_data
		FROM connections
		WHERE id = ? AND auth_type = 'oauth' AND is_active = 1
	`, connectionID).Scan(&c.ID, &c.ProviderTypeID, &c.Name, &c.OAuthToken, &c.ProviderSpecificData)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	cq := fetchConnectionQuota(c, c.ProviderTypeID)
	return &cq, nil
}

// parseProviderSpecificData parses the JSON provider_specific_data column.
func parseProviderSpecificData(raw sql.NullString) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw.String), &m); err != nil {
		return nil
	}
	return m
}

// fetchConnectionQuota dispatches to the right provider fetcher.
func fetchConnectionQuota(c connRow, providerID string) ConnectionQuota {
	cq := ConnectionQuota{
		ConnectionID:   c.ID,
		ConnectionName: c.Name,
		ProviderID:     providerID,
		ProviderName:   providerID,
		FetchedAt:      time.Now().UnixMilli(),
	}

	if !c.OAuthToken.Valid || c.OAuthToken.String == "" {
		cq.Error = "No OAuth token available"
		return cq
	}

	token := c.OAuthToken.String
	psd := parseProviderSpecificData(c.ProviderSpecificData)

	type fetchResult struct {
		quotas []QuotaItem
		plan   string
		msg    string
		err    error
	}

	ch := make(chan fetchResult, 1)
	go func() {
		var r fetchResult
		switch providerID {
		case "cx":
			r.quotas, r.plan, r.err = fetchCodexQuota(token, psd)
		case "ag":
			r.quotas, r.plan, r.err = fetchAntigravityQuota(token, psd)
		case "kiro":
			r.quotas, r.plan, r.err = fetchKiroQuota(token, psd)
		default:
			r.msg = "Quota fetching not supported for this provider"
		}
		ch <- r
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			cq.Error = r.err.Error()
		} else {
			cq.Quotas = r.quotas
			cq.Plan = r.plan
			cq.Message = r.msg
		}
	case <-time.After(15 * time.Second):
		cq.Error = "Quota fetch timed out (15s)"
	}

	return cq
}
