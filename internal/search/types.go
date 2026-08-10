// Package search defines the shared domain model, provider registry, and
// configuration contract for the unified /v1/search endpoint.
//
// Credentials are deliberately not stored in this package. Callers resolve
// API keys using one of the supported CredentialMode strategies:
//   - CredentialModeConnection: use the selected connection's api_key or
//     access_token fields (default for most providers).
//   - CredentialModeEnv: read a provider-specific environment variable at
//     runtime (e.g. AXON_TAVILY_API_KEY).
//   - CredentialModeSettingsTable: read a settings key from the database.
//   - CredentialModeAPIKeyField: allow the client to pass an api_key in the
//     request body (intended only for self-hosted / searxng).
package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Provider identifies a search backend supported in Phase-1.
type Provider string

const (
	ProviderTavily    Provider = "tavily"
	ProviderBrave     Provider = "brave"
	ProviderExa       Provider = "exa"
	ProviderSerper    Provider = "serper"
	ProviderGooglePSE Provider = "googlepse"
	ProviderSearXNG   Provider = "searxng"
)

// KnownProviders returns the stable Phase-1 provider identifiers in priority
// order (general-purpose engines first, then niche / self-hosted options).
func KnownProviders() []Provider {
	return []Provider{
		ProviderTavily,
		ProviderBrave,
		ProviderExa,
		ProviderSerper,
		ProviderGooglePSE,
		ProviderSearXNG,
	}
}

// IsKnownProvider reports whether p is a registered Phase-1 provider.
func IsKnownProvider(p Provider) bool {
	switch p {
	case ProviderTavily, ProviderBrave, ProviderExa,
		ProviderSerper, ProviderGooglePSE, ProviderSearXNG:
		return true
	}
	return false
}

// SearchTimeRange restricts results to a relative time window.
type SearchTimeRange string

const (
	TimeRangeDay   SearchTimeRange = "day"
	TimeRangeWeek  SearchTimeRange = "week"
	TimeRangeMonth SearchTimeRange = "month"
	TimeRangeYear  SearchTimeRange = "year"
)

// Valid returns true for recognised time-range values.
func (t SearchTimeRange) Valid() bool {
	switch t {
	case "", TimeRangeDay, TimeRangeWeek, TimeRangeMonth, TimeRangeYear:
		return true
	}
	return false
}

// ResponseFormat controls whether a provider should synthesise a prose answer.
type ResponseFormat string

const (
	ResponseFormatDefault ResponseFormat = ""
	ResponseFormatList    ResponseFormat = "list"
	ResponseFormatSources ResponseFormat = "sources"
	ResponseFormatAnswer  ResponseFormat = "answer"
)

// CredentialMode describes how the caller should resolve the upstream
// credential for a provider.
type CredentialMode string

const (
	// CredentialModeConnection uses the selected connection's stored
	// api_key / access_token. This is the default for most providers.
	CredentialModeConnection CredentialMode = "connection"
	// CredentialModeEnv reads the API key from a provider-specific
	// environment variable at runtime.
	CredentialModeEnv CredentialMode = "env"
	// CredentialModeSettingsTable reads the API key from the settings table
	// by key.
	CredentialModeSettingsTable CredentialMode = "settings"
	// CredentialModeAPIKeyField permits the client to send an api_key in
	// the request body. This is suitable for self-hosted or no-auth
	// providers such as searxng.
	CredentialModeAPIKeyField CredentialMode = "api_key_field"
)

// Valid returns true for recognised credential modes.
func (c CredentialMode) Valid() bool {
	switch c {
	case "", CredentialModeConnection, CredentialModeEnv,
		CredentialModeSettingsTable, CredentialModeAPIKeyField:
		return true
	}
	return false
}

// SearchRequest is the unified request shape for /v1/search.
// Provider-specific parameters can be supplied under Extra.
type SearchRequest struct {
	// Query is the free-text search query.
	Query string `json:"query"`
	// Provider selects the upstream backend. When empty the registry returns
	// a sensible default (ProviderTavily).
	Provider Provider `json:"provider,omitempty"`
	// MaxResults caps the number of returned results (default 10, max 100).
	MaxResults int `json:"max_results,omitempty"`
	// Offset skips the first N results for pagination.
	Offset int `json:"offset,omitempty"`
	// TimeRange restricts results to a relative window.
	TimeRange SearchTimeRange `json:"time_range,omitempty"`
	// IncludeImages asks the provider to include image results when supported.
	IncludeImages bool `json:"include_images,omitempty"`
	// IncludeAnswer asks the provider to include a synthesized answer when
	// supported (e.g. Tavily answer).
	IncludeAnswer bool `json:"include_answer,omitempty"`
	// SafeSearch enables provider-level safe-search filters when supported.
	SafeSearch bool `json:"safe_search,omitempty"`
	// Market / locale hint such as "en-US".
	Market string `json:"market,omitempty"`
	// APIKey is only honoured when the selected provider's CredentialMode is
	// CredentialModeAPIKeyField.
	APIKey string `json:"api_key,omitempty"`
	// Extra holds provider-specific parameters that are passed through
	// verbatim to the upstream caller (e.g. Tavily's search_depth).
	Extra map[string]any `json:"extra,omitempty"`
	// ResponseFormat hints at the desired output shape.
	ResponseFormat ResponseFormat `json:"response_format,omitempty"`
}

// SearchResult is one row in a unified search response.
type SearchResult struct {
	// Rank is the 1-based position of this result in the result set.
	Rank int `json:"rank"`
	// Title is the page title.
	Title string `json:"title"`
	// URL is the result link.
	URL string `json:"url"`
	// Snippet is a short plaintext description.
	Snippet string `json:"snippet,omitempty"`
	// Content is the full retrieved page content when available.
	Content string `json:"content,omitempty"`
	// Source is the domain or provider-specific source label.
	Source string `json:"source,omitempty"`
	// PublishedAt is an optional ISO-8601 publication timestamp.
	PublishedAt string `json:"published_at,omitempty"`
	// ThumbnailURL is present for image-capable providers when requested.
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// SearchUsage reports upstream consumption for a single search call.
type SearchUsage struct {
	// Provider is the upstream provider identifier.
	Provider Provider `json:"provider"`
	// Requests is the number of upstream HTTP calls made.
	Requests int `json:"requests"`
	// Tokens is a provider-specific usage unit (queries, tokens, etc.).
	Tokens int `json:"tokens,omitempty"`
	// Credits is a provider-specific billing unit when exposed.
	Credits float64 `json:"credits,omitempty"`
	// LatencyMs is the upstream-facing round-trip latency.
	LatencyMs int64 `json:"latency_ms"`
}

// SearchMetrics contains response-level metadata.
type SearchMetrics struct {
	// TotalResults is the estimated total number of results available.
	TotalResults int `json:"total_results"`
	// ReturnedResults is the number of SearchResult items in this response.
	ReturnedResults int `json:"returned_results"`
	// LatencyMs is the total time spent inside the handler.
	LatencyMs int64 `json:"latency_ms"`
}

// SearchResponse is the unified response envelope for /v1/search.
type SearchResponse struct {
	// Query echoes the original query.
	Query string `json:"query"`
	// Provider echoes the upstream provider identifier.
	Provider Provider `json:"provider"`
	// Results is the ordered list of search results.
	Results []SearchResult `json:"results"`
	// Answer is a synthesized prose answer when requested and supported.
	Answer string `json:"answer,omitempty"`
	// Usage reports upstream consumption.
	Usage *SearchUsage `json:"usage,omitempty"`
	// Metrics reports response-level metadata.
	Metrics *SearchMetrics `json:"metrics,omitempty"`
	// Extra holds provider-specific response fields that are passed through
	// verbatim to the client.
	Extra map[string]any `json:"extra,omitempty"`
}

// Defaults.
const (
	DefaultMaxResults = 10
	DefaultProvider   = ProviderTavily
	MaxMaxResults     = 100
	MaxQueryLength    = 4000
)

// WithDefaults returns a copy of r with default values applied.
func (r SearchRequest) WithDefaults() SearchRequest {
	if r.Provider == "" {
		r.Provider = DefaultProvider
	}
	if r.MaxResults <= 0 {
		r.MaxResults = DefaultMaxResults
	}
	if r.MaxResults > MaxMaxResults {
		r.MaxResults = MaxMaxResults
	}
	if r.TimeRange == "" {
		// No default window; leave empty (provider decides).
	}
	return r
}

// Validate checks the request for obvious errors.
func (r SearchRequest) Validate() error {
	if strings.TrimSpace(r.Query) == "" {
		return errors.New("query is required")
	}
	if len(r.Query) > MaxQueryLength {
		return fmt.Errorf("query exceeds %d characters", MaxQueryLength)
	}
	if r.Provider != "" && !IsKnownProvider(r.Provider) {
		return fmt.Errorf("unsupported provider: %s", r.Provider)
	}
	if r.MaxResults < 0 {
		return errors.New("max_results cannot be negative")
	}
	if r.MaxResults > MaxMaxResults {
		return fmt.Errorf("max_results cannot exceed %d", MaxMaxResults)
	}
	if r.Offset < 0 {
		return errors.New("offset cannot be negative")
	}
	if !r.TimeRange.Valid() {
		return fmt.Errorf("invalid time_range: %s", r.TimeRange)
	}
	if r.ResponseFormat != "" {
		switch r.ResponseFormat {
		case ResponseFormatList, ResponseFormatSources, ResponseFormatAnswer:
		default:
			return fmt.Errorf("invalid response_format: %s", r.ResponseFormat)
		}
	}
	if r.APIKey != "" && !isProbablyAPIKey(r.APIKey) {
		return errors.New("api_key does not look like a valid key")
	}
	return nil
}

// isProbablyAPIKey rejects keys that are obviously malformed. It is intentionally
// permissive because providers use many key formats.
func isProbablyAPIKey(key string) bool {
	if len(key) < 4 {
		return false
	}
	if strings.Contains(key, " ") {
		return false
	}
	if _, err := url.Parse(key); err == nil && strings.Contains(key, "://") {
		return false
	}
	return true
}

// IsEmpty reports whether the response has no results.
func (s SearchResponse) IsEmpty() bool {
	return len(s.Results) == 0
}

// UnmarshalSearchRequest parses a JSON request body applying defaults and
// returning the raw error (if any). Defaults are not applied on parse error.
func UnmarshalSearchRequest(data []byte) (SearchRequest, error) {
	var req SearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return SearchRequest{}, fmt.Errorf("invalid search request: %w", err)
	}
	// JSON numbers may unmarshal as float64 inside Extra. Convert well-known
	// numeric extras to float64 so callers normalize them once.
	if req.Extra != nil {
		req.Extra = normalizeExtraNumbers(req.Extra)
	}
	return req.WithDefaults(), nil
}

// MarshalSearchResponse serialises the response envelope.
func MarshalSearchResponse(resp SearchResponse) ([]byte, error) {
	return json.Marshal(resp)
}

// normalizeExtraNumbers ensures integer-like values in Extra stay numbers.
func normalizeExtraNumbers(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case json.Number:
			if i, err := val.Int64(); err == nil {
				out[k] = i
			} else if f, err := val.Float64(); err == nil {
				out[k] = f
			} else {
				out[k] = val.String()
			}
		case map[string]any:
			out[k] = normalizeExtraNumbers(val)
		default:
			out[k] = v
		}
	}
	return out
}

// MustParseInt is a helper for config builders that need to coerce string
// defaults into ints. It returns the fallback on failure.
func MustParseInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n64, err := strconv.ParseInt(s, 10, 0)
	if err != nil {
		return fallback
	}
	return int(n64)
}
