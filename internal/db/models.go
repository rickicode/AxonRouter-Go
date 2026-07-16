package db

import (
	"database/sql"
	"encoding/json"
	"time"
)

// ProviderType represents a provider type (e.g., "openai", "claude", "gemini").
type ProviderType struct {
	ID           string         `json:"id"`
	DisplayName  string         `json:"display_name"`
	Format       string         `json:"format"`
	BaseURL      string         `json:"base_url"`
	IsCustom     bool           `json:"is_custom"`
	CustomHeaders sql.NullString `json:"custom_headers,omitempty"`
	Category     string         `json:"category"`
	ServiceKinds []string       `json:"service_kinds"`
	CreatedAt    int64          `json:"created_at"`
}

// Connection represents a single API key/token instance for a provider.
type Connection struct {
	ID                   string         `json:"id"`
	ProviderTypeID       string         `json:"provider_type_id"`
	Name                 string         `json:"name"`
	AuthType             string         `json:"auth_type"`
	APIKey               sql.NullString `json:"-"`
	OAuthToken           sql.NullString `json:"-"`
	OAuthRefreshToken    sql.NullString `json:"-"`
	OAuthExpiresAt       sql.NullInt64  `json:"-"`
	ProviderSpecificData sql.NullString `json:"-"`
	Priority             int            `json:"priority"`
	Status               string         `json:"status"`
	CooldownUntil        sql.NullInt64  `json:"cooldown_until,omitempty"`
	LastError            sql.NullString `json:"last_error,omitempty"`
	LastErrorCode        sql.NullString `json:"last_error_code,omitempty"`
	LastSuccessAt        sql.NullInt64  `json:"last_success_at,omitempty"`
	LastFailureAt        sql.NullInt64  `json:"last_failure_at,omitempty"`
	FailureCount         int            `json:"failure_count"`
	Capabilities         sql.NullString `json:"capabilities,omitempty"`
	IsActive             bool           `json:"is_active"`
	CreatedAt            int64          `json:"created_at"`
	UpdatedAt            int64          `json:"updated_at"`
}

// ModelRateLimit tracks per-model rate limits on a connection.
type ModelRateLimit struct {
	ID            string        `json:"id"`
	ConnectionID  string        `json:"connection_id"`
	ModelID       string        `json:"model_id"`
	TPMRemaining  sql.NullInt64 `json:"tpm_remaining,omitempty"`
	TPMLimit      sql.NullInt64 `json:"tpm_limit,omitempty"`
	RPMRemaining  sql.NullInt64 `json:"rpm_remaining,omitempty"`
	RPMLimit      sql.NullInt64 `json:"rpm_limit,omitempty"`
	CooldownUntil sql.NullInt64 `json:"cooldown_until,omitempty"`
	LastUpdatedAt int64         `json:"last_updated_at"`
}

// APIKey is a client-facing API key for authenticating to the proxy.
type APIKey struct {
	ID              string         `json:"id"`
	KeyHash         string         `json:"key_hash"`
	Name            sql.NullString `json:"name,omitempty"`
	RateLimitPerMin int            `json:"rate_limit_per_min"`
	IsActive        bool           `json:"is_active"`
	CreatedAt       int64          `json:"created_at"`
}

// Combo is a named ordered list of model steps with a routing strategy.
type Combo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Strategy    string         `json:"strategy"`
	StickyLimit int            `json:"sticky_limit"`
	TimeoutMs   int            `json:"timeout_ms"`
	IsSmart     bool           `json:"is_smart"`
	SmartGoal   sql.NullString `json:"smart_goal,omitempty"`
	IsActive    bool           `json:"is_active"`
	CreatedAt   int64          `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
}

// ComboStep is a single step inside a combo.
type ComboStep struct {
	ID           string `json:"id"`
	ComboID      string `json:"combo_id"`
	ConnectionID string `json:"connection_id"`
	ModelID      string `json:"model_id"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
	CreatedAt    int64  `json:"created_at"`
}

// RequestLog is a single request log entry.
type RequestLog struct {
	ID              string         `json:"id"`
	Timestamp       int64          `json:"timestamp"`
	ConnectionID    sql.NullString `json:"connection_id,omitempty"`
	ConnectionName  sql.NullString `json:"connection_name,omitempty"`
	ProviderTypeID  sql.NullString `json:"provider_type_id,omitempty"`
	ModelID         sql.NullString `json:"model_id,omitempty"`
	ComboID         sql.NullString `json:"combo_id,omitempty"`
	ProxyPoolID     sql.NullString `json:"proxy_pool_id,omitempty"`
	ProxyPoolName   sql.NullString `json:"proxy_pool_name,omitempty"`
	ApiKey          sql.NullString `json:"api_key,omitempty"`
	ApiType         sql.NullString `json:"api_type,omitempty"`
	Modality        string         `json:"modality"`
	InputTokens     int64          `json:"input_tokens"`
	OutputTokens    int64          `json:"output_tokens"`
	ReasoningTokens int64          `json:"reasoning_tokens"`
	CachedTokens    int64          `json:"cached_tokens"`
	CacheCreationTokens int64      `json:"cache_creation_tokens"`
	Stream          bool           `json:"stream"`
	TokensEstimated bool           `json:"tokens_estimated"`
	LatencyMs       sql.NullInt64  `json:"latency_ms,omitempty"`
	StatusCode      sql.NullInt64  `json:"status_code,omitempty"`
	ErrorMessage    sql.NullString `json:"error_message,omitempty"`
	CostUsd         float64        `json:"cost_usd"`
	CreatedAt       int64          `json:"created_at"`
}

// MarshalJSON emits plain strings/numbers instead of database/sql null
// wrapper objects ({"String":..,"Valid":..}). Consumers such as the
// dashboard expect scalar values; the wrapper objects crash JSON-based
// rendering (e.g. calling .slice on an object).
func (r RequestLog) MarshalJSON() ([]byte, error) {
	getStr := func(n sql.NullString) string {
		if n.Valid {
			return n.String
		}
		return ""
	}
	getInt := func(n sql.NullInt64) int64 {
		if n.Valid {
			return n.Int64
		}
		return 0
	}
	type plain struct {
		ID              string `json:"id"`
		Timestamp       int64  `json:"timestamp"`
		ConnectionID    string `json:"connection_id,omitempty"`
		ConnectionName  string `json:"connection_name,omitempty"`
		ProviderTypeID  string `json:"provider_type_id,omitempty"`
		ModelID         string `json:"model_id,omitempty"`
		ComboID         string `json:"combo_id,omitempty"`
		ProxyPoolID     string `json:"proxy_pool_id,omitempty"`
		ProxyPoolName   string `json:"proxy_pool_name,omitempty"`
		ApiKey          string `json:"api_key,omitempty"`
		ApiType         string `json:"api_type,omitempty"`
		Modality        string `json:"modality"`
		InputTokens     int64  `json:"input_tokens"`
		OutputTokens    int64  `json:"output_tokens"`
		ReasoningTokens int64  `json:"reasoning_tokens"`
		CachedTokens    int64  `json:"cached_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_tokens"`
		Stream          bool   `json:"stream"`
		TokensEstimated bool   `json:"tokens_estimated"`
		LatencyMs       int64  `json:"latency_ms,omitempty"`
		StatusCode      int64  `json:"status_code,omitempty"`
		ErrorMessage    string `json:"error_message,omitempty"`
		CostUsd         float64 `json:"cost_usd"`
		CreatedAt       int64  `json:"created_at"`
	}
	return json.Marshal(plain{
		ID:              r.ID,
		Timestamp:       r.Timestamp,
		ConnectionID:    getStr(r.ConnectionID),
		ConnectionName:  getStr(r.ConnectionName),
		ProviderTypeID:  getStr(r.ProviderTypeID),
		ModelID:         getStr(r.ModelID),
		ComboID:         getStr(r.ComboID),
		ProxyPoolID:     getStr(r.ProxyPoolID),
		ProxyPoolName:   getStr(r.ProxyPoolName),
		ApiKey:          getStr(r.ApiKey),
		ApiType:         getStr(r.ApiType),
		Modality:        r.Modality,
		InputTokens:     r.InputTokens,
		OutputTokens:    r.OutputTokens,
		ReasoningTokens: r.ReasoningTokens,
		CachedTokens:    r.CachedTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		Stream:          r.Stream,
		TokensEstimated: r.TokensEstimated,
		LatencyMs:       getInt(r.LatencyMs),
		StatusCode:      getInt(r.StatusCode),
		ErrorMessage:    getStr(r.ErrorMessage),
		CostUsd:         r.CostUsd,
		CreatedAt:       r.CreatedAt,
	})
}

// Setting is a key-value configuration pair.
type Setting struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updated_at"`
}

// RotationState tracks round-robin rotation counter per combo.
type RotationState struct {
	ComboID   string `json:"combo_id"`
	Counter   int    `json:"counter"`
	UpdatedAt int64  `json:"updated_at"`
}

// Pagination holds pagination metadata for API responses.
type Pagination struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// PaginatedResponse wraps data with pagination metadata.
type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

// DashboardStats holds aggregated stats for the dashboard.
type DashboardStats struct {
	TotalProviders   int            `json:"total_providers"`
	TotalConnections int            `json:"total_connections"`
	TotalCombos      int            `json:"total_combos"`
	StatusCounts     map[string]int `json:"status_counts"`
	RequestsToday    int64          `json:"requests_today"`
	TokensToday      int64          `json:"tokens_today"`
	CostToday        float64        `json:"cost_today"`
	Uptime           time.Duration  `json:"uptime"`
}

// ProviderWithCounts is a provider type with its connection count breakdown.
type ProviderWithCounts struct {
	ProviderType
	ConnectionCount int            `json:"connection_count"`
	StatusCounts    map[string]int `json:"status_counts"`
	Aliases         []string       `json:"aliases"`
}

// ProxyPool represents a proxy endpoint (HTTP proxy or relay).
type ProxyPool struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"` // http, vercel, deno, cloudflare
	ProxyURL       string         `json:"proxyUrl"`
	NoProxy        string         `json:"noProxy"`
	RelayAuth      string         `json:"relayAuth"` // auth token for relay types
	IsActive       bool           `json:"isActive"`
	TestStatus     string         `json:"testStatus"` // untested, active, error
	LastTestedAt   sql.NullString `json:"lastTestedAt"`
	LastError      sql.NullString `json:"lastError"`
	ResponseTimeMs sql.NullInt64  `json:"responseTimeMs"`
	ProxyIP        string         `json:"proxyIp"`
	ProxyCountry   string         `json:"proxyCountry"`
	ProxyCity      string         `json:"proxyCity"`
	ProxyOrg       string         `json:"proxyOrg"`
	CreatedAt      int64          `json:"createdAt"`
	UpdatedAt      int64          `json:"updatedAt"`
}

// ProxyGroup represents a named group of proxy pools with a selection strategy.
type ProxyGroup struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Mode         string   `json:"mode"` // roundrobin, sticky
	StickyLimit  int      `json:"stickyLimit"`
	StrictProxy  bool     `json:"strictProxy"`
	ProxyPoolIDs []string `json:"proxyPoolIds"` // ordered list of pool IDs
	IsActive     bool     `json:"isActive"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    int64    `json:"updatedAt"`
}
