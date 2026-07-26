package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RotatingFileWriter is a size-based rotating file writer. It rotates the
// active log file when it exceeds maxSizeBytes, keeps at most maxFiles
// numbered backups, and removes files older than retentionDays.
type RotatingFileWriter struct {
	path          string
	maxSize       int64
	retentionDays int
	maxFiles      int

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewRotatingFileWriter opens (or creates) the log file at path and returns a
// writer that rotates the file based on the supplied limits.
// A non-positive maxSizeBytes disables size-based rotation.
func NewRotatingFileWriter(path string, maxSizeBytes int64, retentionDays, maxFiles int) (*RotatingFileWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &RotatingFileWriter{
		path:          path,
		maxSize:       maxSizeBytes,
		retentionDays: retentionDays,
		maxFiles:      maxFiles,
		file:          f,
		size:          info.Size(),
	}, nil
}

// Write appends p to the active log file, rotating first if the write would
// exceed the configured maximum size.
func (w *RotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.maxSize > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Rotate closes the current file, shifts numbered backups, and reopens a fresh
// log file. It also applies retention cleanup. It is safe to call concurrently
// with Write.
func (w *RotatingFileWriter) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rotateLocked()
}

func (w *RotatingFileWriter) rotateLocked() error {
	if w.file == nil {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}

	// Remove oldest backup if it would exceed maxFiles after rotation.
	if w.maxFiles > 0 {
		oldest := backupPath(w.path, w.maxFiles)
		_ = os.Remove(oldest)
		// Shift backups upward: .maxFiles-1 -> .maxFiles, ... .1 -> .2
		for i := w.maxFiles - 1; i >= 1; i-- {
			old := backupPath(w.path, i)
			newPath := backupPath(w.path, i+1)
			if _, err := os.Stat(old); err == nil {
				if err := os.Rename(old, newPath); err != nil {
					return err
				}
			}
		}
	}

	// Rename current file to .1
	if err := os.Rename(w.path, backupPath(w.path, 1)); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Reopen fresh log file
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0

	// Apply retention cleanup to all log files in the directory.
	_ = w.cleanupRetentionLocked()

	return nil
}

// Truncate clears the active log file. It is used by the admin clear endpoint.
func (w *RotatingFileWriter) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	if err := os.Truncate(w.path, 0); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0
	return nil
}

// Close closes the active log file.
func (w *RotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// cleanupRetentionLocked removes any log file in the same directory older than
// retentionDays. It is called with the mutex held.
func (w *RotatingFileWriter) cleanupRetentionLocked() error {
	if w.retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -w.retentionDays)
	dir := filepath.Dir(w.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	base := filepath.Base(w.path)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isRotatedLogFile(base, name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

// CleanupRetention removes log files older than retentionDays. It can be called
// periodically from a background goroutine.
func (w *RotatingFileWriter) CleanupRetention() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cleanupRetentionLocked()
}

func backupPath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

func isRotatedLogFile(baseName, candidate string) bool {
	if candidate == baseName {
		return true
	}
	if !strings.HasPrefix(candidate, baseName) {
		return false
	}
	suffix := candidate[len(baseName):]
	if suffix == "" {
		return true
	}
	if !strings.HasPrefix(suffix, ".") {
		return false
	}
	rest := suffix[1:]
	// Accept numbered backups (e.g. .1, .2) or timestamped backups (e.g. .20260101-120000).
	if rest == "" {
		return false
	}
	return true
}

// totalBackupSize returns the total size of all log files in the same
// directory that share the base name. Used by tests and size-based cleaners.
func totalBackupSize(path string) int64 {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isRotatedLogFile(base, entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

// oldestRotatedFile returns the oldest rotated file (not the active file) for
// the given log path. It is used by tests.
func oldestRotatedFile(path string) (string, time.Time, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, err
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == base || !isRotatedLogFile(base, name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(dir, name), modTime: info.ModTime()})
	}
	if len(files) == 0 {
		return "", time.Time{}, os.ErrNotExist
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})
	return files[0].path, files[0].modTime, nil
}
