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

func clearEnv(t *testing.T) {
	t.Helper()
	// config.Get uses sync.Once and Init uses sync.Once; for unit tests on the
	// resolve helper we only need to ensure AXONROUTER_DIR is not left over.
	t.Setenv("AXONROUTER_DIR", "")
}
