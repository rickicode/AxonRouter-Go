package combo

import (
	"database/sql"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// RotationManager manages round-robin rotation state per combo.
// Counters are kept in memory so combo resolution never blocks on SQLite I/O.
// Best-effort async writes flush state to the DB for continuity across restarts.
type RotationManager struct {
	mu       sync.RWMutex
	db       *sql.DB
	counters map[string]int
}

// NewRotationManager creates a new rotation manager.
func NewRotationManager(database *sql.DB) *RotationManager {
	return &RotationManager{
		db:       database,
		counters: make(map[string]int),
	}
}

// GetRotatedSteps returns combo steps rotated according to the round-robin counter.
// For "priority" strategy or single-step combos, returns steps unchanged.
func (rm *RotationManager) GetRotatedSteps(comboID string, strategy string, stickyLimit int, steps []db.ComboStep) []db.ComboStep {
	if strategy != "round-robin" || len(steps) <= 1 {
		return steps
	}

	counter := rm.nextCounter(comboID)
	effectiveIndex := (counter / stickyLimit) % len(steps)

	rotated := make([]db.ComboStep, len(steps))
	for i := range steps {
		rotated[i] = steps[(effectiveIndex+i)%len(steps)]
	}
	return rotated
}

// nextCounter returns the current counter for comboID and advances it in memory.
// The first call lazily seeds the in-memory value from the DB so restarts keep
// approximate position. DB writes are async and best-effort.
func (rm *RotationManager) nextCounter(comboID string) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	counter, ok := rm.counters[comboID]
	if !ok {
		counter = rm.loadCounter(comboID)
		rm.counters[comboID] = counter
	}
	rm.counters[comboID] = counter + 1

	// Flush counter asynchronously so the hot path never waits on SQLite.
	go rm.persistCounter(comboID, counter+1)

	return counter
}

// loadCounter reads the current rotation counter from SQLite.
func (rm *RotationManager) loadCounter(comboID string) int {
	var counter int
	err := rm.db.QueryRow(`SELECT counter FROM rotation_state WHERE combo_id = ?`, comboID).Scan(&counter)
	if err != nil {
		return 0
	}
	return counter
}

// persistCounter bumps the rotation counter in SQLite.
func (rm *RotationManager) persistCounter(comboID string, counter int) {
	now := time.Now().Unix()
	rm.db.Exec(`
		INSERT INTO rotation_state (combo_id, counter, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(combo_id) DO UPDATE SET counter = ?, updated_at = ?
	`, comboID, counter, now, counter, now)
}

// ResetCounter resets the rotation counter for a combo in memory and on disk.
func (rm *RotationManager) ResetCounter(comboID string) {
	rm.mu.Lock()
	delete(rm.counters, comboID)
	rm.mu.Unlock()

	rm.db.Exec(`DELETE FROM rotation_state WHERE combo_id = ?`, comboID)
}
