package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAntigravityCreditsMode(t *testing.T) {
	cases := []struct {
		in   string
		want AntigravityCreditsMode
	}{
		{"off", AntigravityCreditsModeOff},
		{"OFF", AntigravityCreditsModeOff},
		{"", AntigravityCreditsModeOff},
		{"retry", AntigravityCreditsModeRetry},
		{"RETRY", AntigravityCreditsModeRetry},
		{"always", AntigravityCreditsModeAlways},
		{"ALWAYS", AntigravityCreditsModeAlways},
		{"invalid", AntigravityCreditsModeOff},
	}
	for _, c := range cases {
		if got := parseAntigravityCreditsMode(c.in); got != c.want {
			t.Errorf("parseAntigravityCreditsMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveDataDir_Default(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	clearEnv(t)
	got := resolveDataDir("")
	want := filepath.Join(home, "axonrouter")
	if got != want {
		t.Errorf("resolveDataDir() = %q, want %q", got, want)
	}
}

func TestResolveDataDir_AxonRouterDirEnv(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	clearEnv(t)
	t.Setenv("AXONROUTER_DIR", "/tmp/axon-test")
	got := resolveDataDir("")
	want := "/tmp/axon-test"
	if got != want {
		t.Errorf("resolveDataDir() = %q, want %q", got, want)
	}
	t.Setenv("AXONROUTER_DIR", "custom-data")
	got = resolveDataDir("")
	want = filepath.Join(home, "custom-data")
	if got != want {
		t.Errorf("resolveDataDir() relative = %q, want %q", got, want)
	}
}

func TestResolveDataDir_ExplicitWins(t *testing.T) {
	clearEnv(t)
	t.Setenv("AXONROUTER_DIR", "/tmp/from-env")
	got := resolveDataDir("/tmp/explicit")
	want := "/tmp/explicit"
	if got != want {
		t.Errorf("resolveDataDir() = %q, want %q", got, want)
	}
}

func TestParseAntigravityObfuscationWords(t *testing.T) {
	defaultLen := len(defaultAntigravityObfuscationWords)
	cases := []struct {
		in   string
		want []string
	}{
		{"", defaultAntigravityObfuscationWords},
		{"   ", defaultAntigravityObfuscationWords},
		{"cursor, opencode", []string{"cursor", "opencode"}},
		{" Cursor ,  OPENCODE ", []string{"cursor", "opencode"}},
		{"cursor,,opencode", []string{"cursor", "opencode"}},
	}
	for _, c := range cases {
		got := parseAntigravityObfuscationWords(c.in)
		if c.in == "" && len(got) != defaultLen {
			t.Errorf("parseAntigravityObfuscationWords(%q) returned %d words, want defaults", c.in, len(got))
			continue
		}
		if c.in != "" {
			if len(got) != len(c.want) {
				t.Errorf("parseAntigravityObfuscationWords(%q) length = %d, want %d", c.in, len(got), len(c.want))
				continue
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("parseAntigravityObfuscationWords(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
				}
			}
		}
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	// config.Get uses sync.Once and Init uses sync.Once; for unit tests on the
	// resolve helper we only need to ensure AXONROUTER_DIR is not left over.
	t.Setenv("AXONROUTER_DIR", "")
}
