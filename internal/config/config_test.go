package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestParseCloakMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "auto"},
		{"auto", "auto"},
		{"AUTO", "auto"},
		{"always", "always"},
		{"ALWAYS", "always"},
		{"never", "never"},
		{"invalid", "auto"},
	}
	for _, c := range cases {
		if got := parseCloakMode(c.in); got != c.want {
			t.Errorf("parseCloakMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseStringSliceEnv(t *testing.T) {
	if got := parseStringSliceEnv(""); got != nil {
		t.Errorf("parseStringSliceEnv(\"\") = %v, want nil", got)
	}
	got := parseStringSliceEnv("foo, bar ,")
	want := []string{"foo", "bar"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parseStringSliceEnv(\"foo, bar ,\") = %v, want %v", got, want)
	}
}

func TestGetEnvBool(t *testing.T) {
	if got := getEnvBool("AXON_TEST_BOOL_DEFAULT", true); !got {
		t.Errorf("getEnvBool missing fallback = %v, want true", got)
	}
	t.Setenv("AXON_TEST_BOOL_FALSE", "false")
	if got := getEnvBool("AXON_TEST_BOOL_FALSE", true); got {
		t.Errorf("getEnvBool false = %v, want false", got)
	}
	t.Setenv("AXON_TEST_BOOL_TRUE", "1")
	if got := getEnvBool("AXON_TEST_BOOL_TRUE", false); !got {
		t.Errorf("getEnvBool true = %v, want true", got)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	// config.Get uses sync.Once and Init uses sync.Once; for unit tests on the
	// resolve helper we only need to ensure AXONROUTER_DIR is not left over.
	t.Setenv("AXONROUTER_DIR", "")
}
