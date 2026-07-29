package search

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKnownProviders(t *testing.T) {
	want := []Provider{
		ProviderTavily, ProviderBrave, ProviderExa,
		ProviderSerper, ProviderGooglePSE, ProviderSearXNG,
	}
	got := KnownProviders()
	if len(got) != len(want) {
		t.Fatalf("KnownProviders() len = %d, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("KnownProviders()[%d] = %s, want %s", i, got[i], p)
		}
	}
}

func TestIsKnownProvider(t *testing.T) {
	if !IsKnownProvider(ProviderTavily) {
		t.Errorf("ProviderTavily should be known")
	}
	if IsKnownProvider(Provider("unknown")) {
		t.Errorf("unknown provider should not be known")
	}
}

func TestSearchRequestWithDefaults(t *testing.T) {
	req := SearchRequest{Query: "hello"}.WithDefaults()
	if req.Provider != DefaultProvider {
		t.Errorf("default provider = %s, want %s", req.Provider, DefaultProvider)
	}
	if req.MaxResults != DefaultMaxResults {
		t.Errorf("default max_results = %d, want %d", req.MaxResults, DefaultMaxResults)
	}

	req2 := SearchRequest{Query: "hello", MaxResults: 50}.WithDefaults()
	if req2.MaxResults != 50 {
		t.Errorf("preserved max_results = %d, want 50", req2.MaxResults)
	}

	req3 := SearchRequest{Query: "hello", MaxResults: 200}.WithDefaults()
	if req3.MaxResults != MaxMaxResults {
		t.Errorf("capped max_results = %d, want %d", req3.MaxResults, MaxMaxResults)
	}
}

func TestSearchRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     SearchRequest
		wantErr string
	}{
		{"empty query", SearchRequest{Query: "   "}, "query is required"},
		{"ok", SearchRequest{Query: "hello", MaxResults: 10}, ""},
		{"too long", SearchRequest{Query: strings.Repeat("a", MaxQueryLength+1)}, "query exceeds"},
		{"unknown provider", SearchRequest{Query: "x", Provider: "bing"}, "unsupported provider"},
		{"negative max_results", SearchRequest{Query: "x", MaxResults: -1}, "max_results cannot be negative"},
		{"max_results too high", SearchRequest{Query: "x", MaxResults: MaxMaxResults + 1}, "max_results cannot exceed"},
		{"negative offset", SearchRequest{Query: "x", Offset: -1}, "offset cannot be negative"},
		{"invalid time_range", SearchRequest{Query: "x", TimeRange: "fortnight"}, "invalid time_range"},
		{"invalid response_format", SearchRequest{Query: "x", ResponseFormat: " essay "}, "invalid response_format"},
		{"bogus api_key", SearchRequest{Query: "x", APIKey: "http://not-a-key"}, "does not look like a valid key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestUnmarshalSearchRequest(t *testing.T) {
	body := []byte(`{
		"query": "go testing",
		"provider": "brave",
		"max_results": 5,
		"time_range": "week",
		"include_images": true,
		"extra": {"foo": "bar"}
	}`)
	req, err := UnmarshalSearchRequest(body)
	if err != nil {
		t.Fatalf("UnmarshalSearchRequest error: %v", err)
	}
	if req.Query != "go testing" {
		t.Errorf("query = %q, want %q", req.Query, "go testing")
	}
	if req.Provider != ProviderBrave {
		t.Errorf("provider = %s, want brave", req.Provider)
	}
	if req.MaxResults != 5 {
		t.Errorf("max_results = %d, want 5", req.MaxResults)
	}
	if req.TimeRange != TimeRangeWeek {
		t.Errorf("time_range = %s, want week", req.TimeRange)
	}
	if !req.IncludeImages {
		t.Errorf("include_images should be true")
	}
	if req.Extra["foo"] != "bar" {
		t.Errorf("extra.foo = %v, want bar", req.Extra["foo"])
	}
}

func TestUnmarshalSearchRequestInvalidJSON(t *testing.T) {
	_, err := UnmarshalSearchRequest([]byte(`{not json}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSearchResponseIsEmpty(t *testing.T) {
	empty := SearchResponse{}
	if !empty.IsEmpty() {
		t.Error("empty response should be empty")
	}
	resp := SearchResponse{Results: []SearchResult{{Title: "x"}}}
	if resp.IsEmpty() {
		t.Error("non-empty response should not be empty")
	}
}

func TestMarshalSearchResponse(t *testing.T) {
	resp := SearchResponse{
		Query:    "q",
		Provider: ProviderTavily,
		Results: []SearchResult{
			{Rank: 1, Title: "T", URL: "https://example.com"},
		},
		Usage: &SearchUsage{Provider: ProviderTavily, Requests: 1, LatencyMs: 12},
	}
	data, err := MarshalSearchResponse(resp)
	if err != nil {
		t.Fatalf("MarshalSearchResponse error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal own output: %v", err)
	}
	if parsed["query"] != "q" {
		t.Errorf("query in json = %v", parsed["query"])
	}
}

func TestMustParseInt(t *testing.T) {
	if MustParseInt("42", 0) != 42 {
		t.Errorf("MustParseInt(42) != 42")
	}
	if MustParseInt("", 7) != 7 {
		t.Errorf("MustParseInt(empty, 7) != 7")
	}
	if MustParseInt("abc", 7) != 7 {
		t.Errorf("MustParseInt(abc, 7) != 7")
	}
}
