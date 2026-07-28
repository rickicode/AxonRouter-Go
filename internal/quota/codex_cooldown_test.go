package quota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

func resetGlobalCooldownStore() {
	globalCodexCooldownStore.mu.Lock()
	globalCodexCooldownStore.dataDir = ""
	globalCodexCooldownStore.mem = make(map[string]CodexCooldownState)
	globalCodexCooldownStore.mu.Unlock()
}

func tempDataDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	SetCodexCooldownDataDir(dir)
	return dir
}

func TestCodexCooldown_RoundTrip(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	connID := "conn-cx-1"
	until := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)
	reason := "Codex quota near limit (>=95%): Session"

	SaveCodexCooldown(connID, until, reason)

	loaded := LoadCodexCooldownStates()
	if len(loaded) != 1 {
		t.Fatalf("expected 1 loaded cooldown, got %d", len(loaded))
	}
	if loaded[0].ConnectionID != connID {
		t.Errorf("connection id = %q, want %q", loaded[0].ConnectionID, connID)
	}
	if !loaded[0].Until.Equal(until) {
		t.Errorf("until = %v, want %v", loaded[0].Until, until)
	}
	if loaded[0].Reason != reason {
		t.Errorf("reason = %q, want %q", loaded[0].Reason, reason)
	}

	// File should exist
	path, err := codexCooldownPath(connID)
	if err != nil {
		t.Fatalf("codexCooldownPath error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected .cds file to exist: %v", err)
	}
}

func TestCodexCooldown_ExpiredCooldownIgnored(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	connID := "conn-expired"
	past := time.Now().Add(-time.Hour).UTC()
	SaveCodexCooldown(connID, past, "old")

	loaded := LoadCodexCooldownStates()
	if len(loaded) != 0 {
		t.Fatalf("expected expired cooldown to be ignored, got %d", len(loaded))
	}

	path, _ := codexCooldownPath(connID)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected expired .cds file to be removed")
	}
}

func TestCodexCooldown_CorruptedFileSkipped(t *testing.T) {
	resetGlobalCooldownStore()
	dir := tempDataDir(t)
	defer resetGlobalCooldownStore()

	goodConn := "good-conn"
	badConn := "bad-conn"

	SaveCodexCooldown(goodConn, time.Now().Add(time.Hour), "ok")

	// Write a corrupt .cds file directly.
	badPath := filepath.Join(dir, codexCooldownSubdir, sanitizeCooldownFileName(badConn))
	if err := os.MkdirAll(filepath.Dir(badPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(badPath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	loaded := LoadCodexCooldownStates()
	if len(loaded) != 1 || loaded[0].ConnectionID != goodConn {
		t.Fatalf("expected only good cooldown to load, got %+v", loaded)
	}
}

func TestCodexCooldown_ReasonIsOptional(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	connID := "conn-no-reason"
	until := time.Now().Add(time.Minute)
	SaveCodexCooldown(connID, until, "")

	loaded := LoadCodexCooldownStates()
	if len(loaded) != 1 || loaded[0].ConnectionID != connID {
		t.Fatalf("expected cooldown with empty reason to persist, got %+v", loaded)
	}
}

func TestCodexCooldown_NoDataDirInMemoryOnly(t *testing.T) {
	resetGlobalCooldownStore()
	defer resetGlobalCooldownStore()

	connID := "conn-memory"
	until := time.Now().Add(time.Minute)
	SaveCodexCooldown(connID, until, "memory")

	loaded := LoadCodexCooldownStates()
	if len(loaded) != 1 || loaded[0].ConnectionID != connID {
		t.Fatalf("expected in-memory cooldown without data dir, got %+v", loaded)
	}

	// No file should exist anywhere.
	path, err := codexCooldownPath(connID)
	if err != nil || path != "" {
		t.Fatalf("expected empty path when no data dir, got %q err=%v", path, err)
	}
}

func TestCodexCooldown_Clear(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	connID := "conn-clear"
	until := time.Now().Add(time.Hour)
	SaveCodexCooldown(connID, until, "clear")

	SaveCodexCooldown(connID, time.Now().Add(-time.Second), "")

	loaded := LoadCodexCooldownStates()
	if len(loaded) != 0 {
		t.Fatalf("expected cooldown to be cleared, got %d", len(loaded))
	}
	path, _ := codexCooldownPath(connID)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected .cds file to be removed after clear")
	}
}

func TestSanitizeCooldownFileName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"auth-1", "auth-1.cds"},
		{"auth/id 5", "auth_id_5.cds"},
		{"../escape", "escape.cds"},
		{"", ""},
	}
	for _, tc := range cases {
		got := sanitizeCooldownFileName(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeCooldownFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCodexCooldown_FileContentLayout(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	connID := "conn-layout"
	until := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	SaveCodexCooldown(connID, until, "layout reason")

	path, _ := codexCooldownPath(connID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .cds: %v", err)
	}
	wantSub := []string{
		`"connection_id": "conn-layout"`,
		`"provider_id": "cx"`,
		`"until": "2026-07-28T12:00:00Z"`,
		`"reason": "layout reason"`,
	}
	for _, w := range wantSub {
		if !strings.Contains(string(data), w) {
			t.Errorf("missing %q in file content:\n%s", w, string(data))
		}
	}
}

func TestCodexCooldown_SanitizeFileNameExtension(t *testing.T) {
	// Connection IDs that happen to end in .cds should not duplicate the suffix.
	got := sanitizeCooldownFileName("foo.cds")
	want := "foo.cds"
	if got != want {
		t.Errorf("sanitizeCooldownFileName(\"foo.cds\") = %q, want %q", got, want)
	}
}

func TestCodexCooldown_RestoreSkipsUnknownConnection(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	store := connstate.NewStore()
	knownID := "conn-known"
	unknownID := "conn-unknown"
	store.GetOrCreate(knownID)

	SaveCodexCooldown(unknownID, time.Now().Add(time.Hour), "stale")
	RestoreCodexCooldownStates(store)

	if cs := store.Get(unknownID); cs != nil && cs.IsInCooldown() {
		t.Fatalf("expected unknown connection to have no cooldown")
	}
	path, _ := codexCooldownPath(unknownID)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected .cds file for unknown connection to be removed")
	}
}

func TestCodexCooldown_RestoreSkipsExpiredState(t *testing.T) {
	resetGlobalCooldownStore()
	_ = tempDataDir(t)
	defer resetGlobalCooldownStore()

	store := connstate.NewStore()
	knownID := "conn-expired-restore"
	store.GetOrCreate(knownID)

	SaveCodexCooldown(knownID, time.Now().Add(-time.Hour), "old")
	RestoreCodexCooldownStates(store)

	if cs := store.Get(knownID); cs != nil && cs.IsInCooldown() {
		t.Fatalf("expected expired state not to be restored")
	}
}
