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
			name: "invalid profileArn region falls back to stored valid region",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer:not-a-region:123456789:profile/abcd",
				"region":     "eu-central-1",
			},
			want: "eu-central-1",
		},
		{
			name: "invalid profileArn region falls back to stored region",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer: junk:123456789:profile/abcd",
				"region":     "us-west-2",
			},
			want: "us-west-2",
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
			name: "stored eu-north-1 is accepted",
			psd: map[string]string{
				"region": "eu-north-1",
			},
			want: "eu-north-1",
		},
		{
			name: "stored ap-southeast-2 is accepted",
			psd: map[string]string{
				"region": "ap-southeast-2",
			},
			want: "ap-southeast-2",
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
		{
			name: "eu-west-1 from profile arn is accepted",
			psd: map[string]string{
				"profileArn": "arn:aws:codewhisperer:eu-west-1:111122223333:profile/TEST",
			},
			want: "eu-west-1",
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
		{"eu-west-1", "https://q.eu-west-1.amazonaws.com"},
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
	amazonEUWest := "https://q.eu-west-1.amazonaws.com/generateAssistantResponse"
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
			name: "api_key with eu-west-1 profile uses q endpoint first",
			psd: map[string]string{
				"authMethod": "api_key",
				"region":     "eu-west-1",
			},
			want: []string{amazonEUWest, kiroDev, qUSEast},
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
			name: "google social tries kiro.dev first",
			psd:  map[string]string{"authMethod": "google"},
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
		{
			name:    "legacy codewhisperer.us-east-1 base url is treated as default",
			psd:     map[string]string{"authMethod": "builder-id"},
			baseURL: "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
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

func TestIsValidAWSRegion(t *testing.T) {
	tests := []struct {
		region string
		want   bool
	}{
		{"us-east-1", true},
		{"eu-central-1", true},
		{"eu-west-1", true},
		{"us-west-2", true},
		{"ap-southeast-2", true},
		{"", false},
		{"US-EAST-1", false},
		{"us east 1", false},
		{"not-a-region", false},
		{"us-east", false},
		{"../etc/passwd", false},
		{"q.us-east-1.amazonaws.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := isValidAWSRegion(tt.region)
			if got != tt.want {
				t.Errorf("isValidAWSRegion(%q) = %v, want %v", tt.region, got, tt.want)
			}
		})
	}
}

func TestRegionFromKiroProfileArn(t *testing.T) {
	tests := []struct {
		arn  string
		want string
	}{
		{"arn:aws:codewhisperer:us-east-1:123456789:profile/abcd", "us-east-1"},
		{"arn:aws:codewhisperer:eu-west-1:123456789:profile/abcd", "eu-west-1"},
		{"arn:aws:codewhisperer:eu-central-1:123456789:profile/abcd", "eu-central-1"},
		{"", ""},
		{"arn:aws:codewhisperer", ""},
		{"arn:aws:codewhisperer::123456789:profile/abcd", ""},
		{"arn:aws:something:us-east-1:123:profile/x", ""},
	}
	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.arn, ":", "_"), func(t *testing.T) {
			got := regionFromKiroProfileArn(tt.arn)
			if got != tt.want {
				t.Errorf("regionFromKiroProfileArn(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}
