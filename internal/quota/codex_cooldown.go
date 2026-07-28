package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const codexProviderID = "cx"
const codexCooldownSubdir = "auths" + string(os.PathSeparator) + codexProviderID

// CodexCooldownState is the persisted runtime cooldown snapshot for one Codex connection.
// It mirrors CLIProxyAPI's CooldownStateRecord but is scoped to the connection-level
// Codex quota cooldown calculated by CodexQuotaCooldown.
type CodexCooldownState struct {
	ConnectionID string    `json:"connection_id"`
	ProviderID   string    `json:"provider_id"`
	Until        time.Time `json:"until"`
	Reason       string    `json:"reason"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// codexCooldownStore persists Codex cooldown state as .cds files next to the
// (logical) auth file inside the configured data directory.
// It keeps an in-memory cache and uses the filesystem as the durable default source.
type codexCooldownStore struct {
	mu       sync.RWMutex
	dataDir  string
	mem      map[string]CodexCooldownState
	loadOnce sync.Once
}

var (
	globalCodexCooldownStore = &codexCooldownStore{mem: make(map[string]CodexCooldownState)}
	// cooldownFileNameUnsafe matches any character that is not safe in a cross-platform file name.
	cooldownFileNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// SetCodexCooldownDataDir configures the root directory where .cds files are stored.
// The files are written under <dir>/auths/cx/<connectionID>.cds to mirror CLIProxyAPI's
// convention of placing cooldown state next to the auth file.
func SetCodexCooldownDataDir(dir string) {
	globalCodexCooldownStore.mu.Lock()
	defer globalCodexCooldownStore.mu.Unlock()
	globalCodexCooldownStore.dataDir = strings.TrimSpace(dir)
}

// codexCooldownDir returns the directory that holds Codex .cds files.
func codexCooldownDir(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, codexCooldownSubdir)
}

// codexCooldownPath returns the .cds file path for a connection when no data directory
// is configured it returns an empty string and no error.
func codexCooldownPathWithDir(dataDir, connID string) (string, error) {
	if dataDir == "" {
		return "", nil
	}
	base := sanitizeCooldownFileName(connID)
	if base == "" {
		return "", fmt.Errorf("invalid connection id for cooldown file: %q", connID)
	}
	return filepath.Join(codexCooldownDir(dataDir), base), nil
}

// codexCooldownPath reads the current data directory under the store lock and returns
// the .cds file path for a connection.
func codexCooldownPath(connID string) (string, error) {
	globalCodexCooldownStore.mu.RLock()
	dataDir := globalCodexCooldownStore.dataDir
	globalCodexCooldownStore.mu.RUnlock()
	return codexCooldownPathWithDir(dataDir, connID)
}

// sanitizeCooldownFileName mirrors CLIProxyAPI's file-name sanitizer.
func sanitizeCooldownFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	ext := filepath.Ext(name)
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	name = cooldownFileNameUnsafe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._-")
	if name == "" {
		return ""
	}
	return name + ".cds"
}

// SaveCodexCooldown persists an active Codex cooldown for a connection.
// When the cooldown is not active any stale .cds file for the connection is removed.
// Errors are logged and swallowed; callers continue with in-memory behaviour.
func SaveCodexCooldown(connID string, until time.Time, reason string) {
	globalCodexCooldownStore.save(connID, until, reason)
}

func (s *codexCooldownStore) save(connID string, until time.Time, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	active := now.Before(until)

	if !active {
		delete(s.mem, connID)
		if s.dataDir == "" {
			return
		}
		path, err := codexCooldownPathWithDir(s.dataDir, connID)
		if err != nil {
			log.Printf("quota: codex cooldown path error for %s: %v", connID, err)
			return
		}
		if path == "" {
			return
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("quota: failed to remove stale codex cooldown %s: %v", path, err)
		}
		return
	}

	state := CodexCooldownState{
		ConnectionID: connID,
		ProviderID:   codexProviderID,
		Until:        until.UTC(),
		Reason:       reason,
		UpdatedAt:    now.UTC(),
	}
	s.mem[connID] = state

	if s.dataDir == "" {
		return
	}
	path, err := codexCooldownPathWithDir(s.dataDir, connID)
	if err != nil {
		log.Printf("quota: codex cooldown path error for %s: %v", connID, err)
		return
	}
	if path == "" {
		return
	}
	if err := writeCodexCooldownFile(path, state); err != nil {
		log.Printf("quota: failed to write codex cooldown %s: %v", path, err)
	}
}

// writeCodexCooldownFile writes a single cooldown state atomically via a temp file + rename.
func writeCodexCooldownFile(path string, state CodexCooldownState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cooldown state: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cooldown dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// LoadCodexCooldownStates reads all .cds files and returns non-expired cooldown states.
// Errors reading individual files are logged and skipped so that one corrupt file does
// not prevent other cooldowns from loading.
func LoadCodexCooldownStates() []CodexCooldownState {
	return globalCodexCooldownStore.load()
}

func (s *codexCooldownStore) load() []CodexCooldownState {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	merged := make(map[string]CodexCooldownState, len(s.mem))
	for _, state := range s.mem {
		if now.Before(state.Until) {
			merged[state.ConnectionID] = state
		} else {
			delete(s.mem, state.ConnectionID)
		}
	}

	if s.dataDir == "" {
		return mapToCooldownSlice(merged)
	}

	dir := codexCooldownDir(s.dataDir)
	errWalk := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".cds") {
			return nil
		}
		state, err := readCodexCooldownFile(path)
		if err != nil {
			log.Printf("quota: skipped corrupt codex cooldown file %s: %v", path, err)
			return nil
		}
		if now.Before(state.Until) {
			if existing, ok := merged[state.ConnectionID]; !ok || state.UpdatedAt.After(existing.UpdatedAt) {
				merged[state.ConnectionID] = state
			}
			s.mem[state.ConnectionID] = merged[state.ConnectionID]
		} else {
			delete(s.mem, state.ConnectionID)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("quota: failed to remove expired codex cooldown %s: %v", path, err)
			}
		}
		return nil
	})
	if errWalk != nil && !errors.Is(errWalk, os.ErrNotExist) {
		log.Printf("quota: failed to walk codex cooldown dir %s: %v", dir, errWalk)
	}
	return mapToCooldownSlice(merged)
}

// mapToCooldownSlice returns the values of a connection-id keyed map in a stable
// but unspecified order.
func mapToCooldownSlice(m map[string]CodexCooldownState) []CodexCooldownState {
	out := make([]CodexCooldownState, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// readCodexCooldownFile parses a single cooldown state JSON file.
func readCodexCooldownFile(path string) (CodexCooldownState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CodexCooldownState{}, fmt.Errorf("read file: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return CodexCooldownState{}, errors.New("empty file")
	}
	var state CodexCooldownState
	if err := json.Unmarshal(data, &state); err != nil {
		return CodexCooldownState{}, fmt.Errorf("parse file: %w", err)
	}
	if state.ConnectionID == "" {
		return CodexCooldownState{}, errors.New("missing connection_id")
	}
	return state, nil
}

// RestoreCodexCooldownStates loads persisted cooldowns and applies them to the
// in-memory connection store. Errors are logged, not returned, so startup cannot
// be aborted by a corrupt .cds file.
func RestoreCodexCooldownStates(store *connstate.Store) {
	if store == nil {
		return
	}
	for _, state := range LoadCodexCooldownStates() {
		store.UpdateCooldown(state.ConnectionID, state.Until)
		log.Printf("quota: restored codex cooldown for %s until %v", state.ConnectionID, state.Until)
	}
}

// CodexCooldownStore exports a thin facade for callers outside this package that need
// to initialise the data directory or inspect state.
type CodexCooldownStore struct{}

// NewCodexCooldownStore creates a new in-memory-only cooldown store for tests.
func NewCodexCooldownStore() *CodexCooldownStore {
	return &CodexCooldownStore{}
}

// CodexQuotaCooldown checks whether any quota window is exhausted and returns
// the cooldown deadline and reason. If no window reports a reset time, the
// cooldown defaults to 60 seconds from now.
// A window is considered exhausted when usage reaches 95% of its limit, matching
// OmniRoute dual-window behavior. This prevents over-blocking healthy accounts
// while still cooling down accounts that are near their limit.
func CodexQuotaCooldown(quotas []QuotaItem) (active bool, until time.Time, reason string) {
	now := time.Now()
	var earliestReset time.Time
	var exhausted []string
	for _, q := range quotas {
		// Unlimited or healthy windows do not trigger cooldown.
		if q.Unlimited || q.RemainingPct > 5 {
			continue
		}
		exhausted = append(exhausted, q.Name)
		reset := now.Add(60 * time.Second)
		if q.ResetAt != "" {
			// Prefer explicit reset time.
			if t, err := time.Parse(time.RFC3339, q.ResetAt); err == nil {
				reset = t
			}
		}
		if earliestReset.IsZero() || reset.Before(earliestReset) {
			earliestReset = reset
		}
	}
	if len(exhausted) == 0 {
		return false, time.Time{}, ""
	}
	return true, earliestReset, fmt.Sprintf("Codex quota near limit (>=95%%): %s", strings.Join(exhausted, ", "))
}
