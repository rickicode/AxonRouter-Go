package auth

import "testing"

// TestProviderGrokCliConstant proves the grok-cli provider type identifier exists
// and matches the expected value used throughout routing and the dashboard.
func TestProviderGrokCliConstant(t *testing.T) {
	if ProviderGrokCli != "grok-cli" {
		t.Fatalf("ProviderGrokCli = %q, want %q", ProviderGrokCli, "grok-cli")
	}
}
