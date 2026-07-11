package compression

import (
	"sort"
	"sync"
)

var (
	engines = make(map[string]Engine)
	mu      sync.RWMutex
)

// Register adds an engine to the global registry.
func Register(e Engine) {
	mu.Lock()
	engines[e.ID()] = e
	mu.Unlock()
}

// Get looks up an engine by ID.
func Get(id string) (Engine, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := engines[id]
	return e, ok
}

// List returns sorted engine IDs.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(engines))
	for k := range engines {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
