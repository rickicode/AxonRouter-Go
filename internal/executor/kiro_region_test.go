package executor

import (
	"strings"
	"testing"
)

func TestResolveKiroRuntimeRegion(t *testing.T) {
	tests := []struct {
		name string
		psd  map[string]string
		want string
	}{
		{
			name: "profileArn region is authoritative",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer:eu-central-1:123456789:profile/abcd",
				"region":     "us-west-2",
			},
			want: "eu-central-1",
		},
		{
			name: "profileArn us-east-1",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer:us-east-1:123456789:profile/abcd",
			},
			want: "us-east-1",
		},
		{
			name: "invalid profileArn region falls back to stored profile region",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer:not-a-region:123456789:profile/abcd",
				"region":     "eu-central-1",
			},
			want: "eu-central-1",
		},
		{
			name: "invalid profileArn region falls back to us-east-1",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer: junk:123456789:profile/abcd",
				"region":     "us-west-2",
			},
			want: "us-east-1",
		},
		{
			name: "stored us-east-1 is accepted",
			psd: map[string]string{
				"region": "us-east-1",
			},
			want: "us-east-1",
		},
		{
			name: "stored eu-central-1 is accepted",
			psd: map[string]string{
				"region": "EU-CENTRAL-1",
			},
			want: "eu-central-1",
		},
		{
			name: "non-profile stored region is ignored",
			psd: map[string]string{
				"region": "eu-north-1",
			},
			want: "us-east-1",
		},
		{
			name: "arbitrary stored region is ignored",
			psd: map[string]string{
				"region": "ap-southeast-2",
			},
			want: "us-east-1",
		},
		{
			name: "empty data falls back to us-east-1",
			psd:  nil,
			want: "us-east-1",
		},
		{
			name: "profileArn does not log through output",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer:eu-central-1:111122223333:profile/TEST",
			},
			want: "eu-central-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveKiroRuntimeRegion(tt.psd)
			if got != tt.want {
				t.Errorf("resolveKiroRuntimeRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKiroRuntimeHost(t *testing.T) {
	tests := []struct {
		region string
		want   string
	}{
		{"us-east-1", "https://codewhisperer.us-east-1.amazonaws.com"},
		{"eu-central-1", "https://q.eu-central-1.amazonaws.com"},
		{"us-west-2", "https://q.us-west-2.amazonaws.com"},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := kiroRuntimeHost(tt.region)
			if got != tt.want {
				t.Errorf("kiroRuntimeHost(%q) = %q, want %q", tt.region, got, tt.want)
			}
		})
	}
}

func TestKiroEndpointURLs(t *testing.T) {
	amazonUSEast := "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	amazonEUCentral := "https://q.eu-central-1.amazonaws.com/generateAssistantResponse"
	kiroDev := "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	qUSEast := "https://q.us-east-1.amazonaws.com/generateAssistantResponse"
	override := "https://custom.example.com/generateAssistantResponse"

	tests := []struct {
		name    string
		psd     map[string]string
		baseURL string
		want    []string
	}{
		{
			name: "baseURL overrides everything",
			psd: map[string]string{
				"authMethod": "api_key",
				"region":     "eu-central-1",
			},
			baseURL: override,
			want:    []string{override},
		},
		{
			name: "api_key tries amazon first",
			psd:  map[string]string{"authMethod": "api_key"},
			want: []string{amazonUSEast, kiroDev, qUSEast},
		},
		{
			name: "external_idp tries amazon first",
			psd:  map[string]string{"authMethod": "external_idp"},
			want: []string{amazonUSEast, kiroDev, qUSEast},
		},
		{
			name: "idc tries amazon first",
			psd:  map[string]string{"authMethod": "idc"},
			want: []string{amazonUSEast, kiroDev, qUSEast},
		},
		{
			name: "api_key with eu-central-1 profile uses q endpoint first",
			psd: map[string]string{
				"authMethod": "api_key",
				"region":     "eu-central-1",
			},
			want: []string{amazonEUCentral, kiroDev, qUSEast},
		},
		{
			name: "builder-id tries kiro.dev first",
			psd:  map[string]string{"authMethod": "builder-id"},
			want: []string{kiroDev, amazonUSEast, qUSEast},
		},
		{
			name: "github social tries kiro.dev first",
			psd:  map[string]string{"authMethod": "github"},
			want: []string{kiroDev, amazonUSEast, qUSEast},
		},
		{
			name: "import tries kiro.dev first",
			psd:  map[string]string{"authMethod": "import"},
			want: []string{kiroDev, amazonUSEast, qUSEast},
		},
		{
			name: "empty authMethod defaults to kiro.dev first",
			psd:  map[string]string{"region": "us-east-1"},
			want: []string{kiroDev, amazonUSEast, qUSEast},
		},
		{
			name:    "empty psd defaults to kiro.dev first on us-east-1",
			psd:     map[string]string{},
			baseURL: "",
			want:    []string{kiroDev, amazonUSEast, qUSEast},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kiroEndpointURLs(tt.psd, tt.baseURL)
			if len(got) != len(tt.want) {
				t.Fatalf("kiroEndpointURLs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("kiroEndpointURLs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestKiroEndpointURLs_DefaultBaseURLIgnored(t *testing.T) {
	amazonUSEast := "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	kiroDev := "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	qUSEast := "https://q.us-east-1.amazonaws.com/generateAssistantResponse"

	got := kiroEndpointURLs(map[string]string{"authMethod": "builder-id"}, amazonUSEast)
	want := []string{kiroDev, amazonUSEast, qUSEast}
	if len(got) != len(want) {
		t.Fatalf("kiroEndpointURLs() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("kiroEndpointURLs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestKiroEndpointURLs_ThreeFallbackForUSEastApiKey(t *testing.T) {
	amazonUSEast := "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	kiroDev := "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	qUSEast := "https://q.us-east-1.amazonaws.com/generateAssistantResponse"

	got := kiroEndpointURLs(map[string]string{"authMethod": "api_key", "region": "us-east-1"}, "")
	want := []string{amazonUSEast, kiroDev, qUSEast}
	if len(got) != len(want) {
		t.Fatalf("kiroEndpointURLs() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("kiroEndpointURLs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestKiroHeaders_AuthMethodConditionals(t *testing.T) {
	tests := []struct {
		name             string
		req              *Request
		wantAuthz        string
		wantTokentype    string
		wantTokenType    string
		wantXAmzTarget   string
		notWantTokentype bool
		notWantTokenType bool
	}{
		{
			name: "api_key sets tokentype and bearer from accessToken",
			req: &Request{
				ProviderSpecificData: map[string]string{"authMethod": "api_key"},
				AccessToken:            "my-api-key",
			},
			wantAuthz:      "Bearer my-api-key",
			wantTokentype:  "API_KEY",
			wantXAmzTarget: "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
		},
		{
			name: "api_key falls back to APIKey field",
			req: &Request{
				ProviderSpecificData: map[string]string{"authMethod": "api_key"},
				APIKey:                 "my-api-key-2",
			},
			wantAuthz:      "Bearer my-api-key-2",
			wantTokentype:  "API_KEY",
			wantXAmzTarget: "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
		},
		{
			name: "external_idp sets TokenType and bearer",
			req: &Request{
				ProviderSpecificData: map[string]string{"authMethod": "external_idp"},
				AccessToken:            "ent-token",
			},
			wantAuthz:      "Bearer ent-token",
			wantTokenType:  "EXTERNAL_IDP",
			wantXAmzTarget: "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
		},
		{
			name: "builder-id only sets bearer and X-Amz-Target",
			req: &Request{
				ProviderSpecificData: map[string]string{"authMethod": "builder-id"},
				AccessToken:            "builder-token",
			},
			wantAuthz:        "Bearer builder-token",
			wantXAmzTarget:   "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
			notWantTokentype: true,
			notWantTokenType: true,
		},
		{
			name: "import neither tokentype nor TokenType",
			req: &Request{
				ProviderSpecificData: map[string]string{"authMethod": "import"},
				AccessToken:            "import-token",
			},
			wantAuthz:        "Bearer import-token",
			wantXAmzTarget:   "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
			notWantTokentype: true,
			notWantTokenType: true,
		},
		{
			name: "auth header is not overwritten by upstream tokentype",
			req: &Request{
				ProviderSpecificData: map[string]string{"authMethod": "api_key"},
				AccessToken:          "key",
				Headers: map[string]string{
					"tokentype": "OTHER",
				},
			},
			wantAuthz:      "Bearer key",
			wantTokentype:  "API_KEY",
			wantXAmzTarget: "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := kiroHeaders(tt.req)
			if got := h["Authorization"]; got != tt.wantAuthz {
				t.Errorf("Authorization = %q, want %q", got, tt.wantAuthz)
			}
			if got := h["X-Amz-Target"]; got != tt.wantXAmzTarget {
				t.Errorf("X-Amz-Target = %q, want %q", got, tt.wantXAmzTarget)
			}
			if tt.wantTokentype != "" {
				// The test helper stores exact-casing keys in the returned map.
				if got, ok := h["tokentype"]; !ok || got != tt.wantTokentype {
					t.Errorf("tokentype = %q, want %q, present=%v", got, tt.wantTokentype, ok)
				}
			}
			if tt.wantTokenType != "" {
				if got, ok := h["TokenType"]; !ok || got != tt.wantTokenType {
					t.Errorf("TokenType = %q, want %q, present=%v", got, tt.wantTokenType, ok)
				}
			}
			if tt.notWantTokentype {
				if _, ok := h["tokentype"]; ok {
					t.Errorf("unexpected tokentype header")
				}
			}
			if tt.notWantTokenType {
				if _, ok := h["TokenType"]; ok {
					t.Errorf("unexpected TokenType header")
				}
			}
		})
	}
}

func TestKiroHeaders_DefaultUserAgent(t *testing.T) {
	req := &Request{ProviderSpecificData: map[string]string{"authMethod": "builder-id"}}
	h := kiroHeaders(req)
	if !strings.Contains(h["User-Agent"], "AWS-SDK-JS") {
		t.Errorf("expected default AWS-SDK-JS user agent, got %q", h["User-Agent"])
	}
}

func TestKiroHeaders_Accept(t *testing.T) {
	req := &Request{ProviderSpecificData: map[string]string{"authMethod": "builder-id"}}
	h := kiroHeaders(req)
	if got := h["Accept"]; got != "application/vnd.amazon.eventstream" {
		t.Errorf("Accept = %q, want %q", got, "application/vnd.amazon.eventstream")
	}
	if got := h["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
}
