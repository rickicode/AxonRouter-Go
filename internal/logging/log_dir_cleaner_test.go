package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path string, size int64, modTime time.Time) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if size > 0 {
		if _, err := f.Write(make([]byte, size)); err != nil {
			f.Close()
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

func TestEnforceLogDirSizeLimit_NoDeletionWhenUnderCap(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "axonrouter.log")
	writeFile(t, active, 100, time.Now())

	deleted, err := enforceLogDirSizeLimit(dir, 1024, active)
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected no deletions, got %d", deleted)
	}
	if !fileExists(t, active) {
		t.Fatalf("active log should still exist")
	}
}

func TestEnforceLogDirSizeLimit_DeletesOldestFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	old1 := filepath.Join(dir, "old1.log")
	old2 := filepath.Join(dir, "old2.log")
	newest := filepath.Join(dir, "newest.log")

	writeFile(t, old1, 400, now.Add(-3*time.Hour))
	writeFile(t, old2, 400, now.Add(-2*time.Hour))
	writeFile(t, newest, 400, now.Add(-1*time.Hour))

	// Cap is 700 bytes; total is 1200. Oldest two (800 bytes) must go,
	// newest stays even though target after deleting oldest two is 400 <= 700.
	deleted, err := enforceLogDirSizeLimit(dir, 700, "")
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deletions, got %d", deleted)
	}
	if fileExists(t, old1) || fileExists(t, old2) {
		t.Fatalf("oldest files should be deleted")
	}
	if !fileExists(t, newest) {
		t.Fatalf("newest file should remain")
	}
}

func TestEnforceLogDirSizeLimit_ProtectsActiveLogFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	active := filepath.Join(dir, "axonrouter.log")
	old := filepath.Join(dir, "old.log")

	writeFile(t, active, 600, now.Add(-2*time.Hour))
	writeFile(t, old, 400, now.Add(-3*time.Hour))

	// Cap is 500 bytes; total is 1000. Without protection, the cleaner
	// would delete the active file (it is the oldest), but it must be kept.
	deleted, err := enforceLogDirSizeLimit(dir, 500, active)
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}
	if !fileExists(t, active) {
		t.Fatalf("active log file should be protected")
	}
	if fileExists(t, old) {
		t.Fatalf("old log file should be deleted")
	}
}

func TestEnforceLogDirSizeLimit_CleansGzippedLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	plain := filepath.Join(dir, "current.log")
	gzipped := filepath.Join(dir, "archived.log.gz")

	writeFile(t, plain, 300, now.Add(-1*time.Hour))
	writeFile(t, gzipped, 300, now.Add(-2*time.Hour))

	// Cap is 400 bytes; total is 600. Oldest gzipped file is removed first,
	// and after that the remaining plain log is within the cap.
	deleted, err := enforceLogDirSizeLimit(dir, 400, "")
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}
	if fileExists(t, gzipped) {
		t.Fatalf("gzipped log should be deleted first (oldest)")
	}
	if !fileExists(t, plain) {
		t.Fatalf("plain log should remain")
	}
}

func TestEnforceLogDirSizeLimit_IgnoresNonLogFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	bad := filepath.Join(dir, "notes.txt")
	good := filepath.Join(dir, "app.log")

	writeFile(t, bad, 1000, now.Add(-24*time.Hour))
	writeFile(t, good, 100, now)

	deleted, err := enforceLogDirSizeLimit(dir, 50, "")
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}
	if !fileExists(t, bad) {
		t.Fatalf("non-log file should be ignored")
	}
	if fileExists(t, good) {
		t.Fatalf("log file should be deleted to meet cap")
	}
}

func TestEnforceLogDirSizeLimit_DisabledWithZeroCap(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.log")
	writeFile(t, big, 1024, time.Now())

	deleted, err := enforceLogDirSizeLimit(dir, 0, "")
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected disabled cleaner to delete nothing, got %d", deleted)
	}
	if !fileExists(t, big) {
		t.Fatalf("file should remain when cap is 0")
	}
}

func TestIsLogFileName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"app.log", true},
		{"app.LOG", true},
		{"archived.log.gz", true},
		{"ARCHIVED.LOG.GZ", true},
		{"notes.txt", false},
		{"", false},
		{"dir.log", true},
		{"access.log.1", false},
	}
	for _, c := range cases {
		if got := isLogFileName(c.name); got != c.want {
			t.Errorf("isLogFileName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStartLogDirCleaner_ReplacesPreviousCleaner(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Start a cleaner with a cap that does nothing.
	StartLogDirCleaner(dir, 10000, "")
	if logDirCleanerCancel == nil {
		t.Fatal("expected cleaner to be running")
	}

	// Start another cleaner; the previous context should be cancelled and a
	// new one installed.
	StartLogDirCleaner(dir, 10000, "")
	if logDirCleanerCancel == nil {
		t.Fatal("expected new cleaner to be running")
	}

	StopLogDirCleaner()

	// Verify a real cleanup pass still works after the restart dance.
	f := filepath.Join(dir, "stale.log")
	writeFile(t, f, 100, now.Add(-time.Hour))
	deleted, err := enforceLogDirSizeLimit(dir, 50, "")
	if err != nil {
		t.Fatalf("enforceLogDirSizeLimit: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}
}
