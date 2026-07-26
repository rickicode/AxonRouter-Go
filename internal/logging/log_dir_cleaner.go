package logging

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const logDirCleanerInterval = time.Minute

var (
	cleanerMu           sync.Mutex
	logDirCleanerCancel context.CancelFunc
)

// StartLogDirCleaner starts a background goroutine that periodically enforces
// a maximum total size for the log directory. Oldest .log/.log.gz files are
// removed until the directory is within the limit; protectedPath (the currently
// active log file) is never deleted. Pass maxTotalSizeMB <= 0 to disable.
//
// Each call stops any previously started cleaner, so it is safe to call multiple
// times (e.g. during reconfiguration).
func StartLogDirCleaner(logDir string, maxTotalSizeMB int, protectedPath string) {
	cleanerMu.Lock()
	defer cleanerMu.Unlock()

	stopLogDirCleanerLocked()

	if maxTotalSizeMB <= 0 {
		return
	}

	maxBytes := int64(maxTotalSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		return
	}

	dir := strings.TrimSpace(logDir)
	if dir == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	logDirCleanerCancel = cancel
	go runLogDirCleaner(ctx, filepath.Clean(dir), maxBytes, strings.TrimSpace(protectedPath))
}

// StopLogDirCleaner stops the background log-directory cleaner.
func StopLogDirCleaner() {
	cleanerMu.Lock()
	defer cleanerMu.Unlock()
	stopLogDirCleanerLocked()
}

func stopLogDirCleanerLocked() {
	if logDirCleanerCancel == nil {
		return
	}
	logDirCleanerCancel()
	logDirCleanerCancel = nil
}

func runLogDirCleaner(ctx context.Context, logDir string, maxBytes int64, protectedPath string) {
	cleanOnce := func() {
		deleted, errClean := enforceLogDirSizeLimit(logDir, maxBytes, protectedPath)
		if errClean != nil {
			log.Printf("WARN: logging: failed to enforce log directory size limit: %v", errClean)
			return
		}
		if deleted > 0 {
			log.Printf("logging: removed %d old log file(s) to enforce log directory size limit", deleted)
		}
	}

	// Clean immediately on startup so stale log files from previous runs are
	// trimmed before the first periodic tick.
	cleanOnce()

	ticker := time.NewTicker(logDirCleanerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanOnce()
		}
	}
}

func enforceLogDirSizeLimit(logDir string, maxBytes int64, protectedPath string) (int, error) {
	if maxBytes <= 0 {
		return 0, nil
	}

	dir := strings.TrimSpace(logDir)
	if dir == "" {
		return 0, nil
	}
	dir = filepath.Clean(dir)

	entries, errRead := os.ReadDir(dir)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return 0, nil
		}
		return 0, errRead
	}

	protected := strings.TrimSpace(protectedPath)
	if protected != "" {
		protected = filepath.Clean(protected)
	}

	type logFile struct {
		path    string
		size    int64
		modTime time.Time
	}

	var (
		files []logFile
		total int64
	)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isLogFileName(name) {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, name)
		files = append(files, logFile{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		total += info.Size()
	}

	if total <= maxBytes {
		return 0, nil
	}

	// Oldest first so stale files are removed before newer ones.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	deleted := 0
	for _, file := range files {
		if total <= maxBytes {
			break
		}
		if protected != "" && filepath.Clean(file.path) == protected {
			continue
		}
		if errRemove := os.Remove(file.path); errRemove != nil {
			log.Printf("WARN: logging: failed to remove old log file %s: %v", filepath.Base(file.path), errRemove)
			continue
		}
		total -= file.size
		deleted++
	}

	return deleted, nil
}

func isLogFileName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".log.gz")
}
