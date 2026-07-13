package combo

import (
	"database/sql"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// rotationFlushInterval bounds how often pending rotation counters are flushed
// to SQLite. Instead of one UPSERT goroutine per resolve (which serializes on
// SQLite's single writer under load), increments accumulate in memory and a
// single debounced goroutine writes them in batches.
const rotationFlushInterval = 250 * time.Millisecond

// RotationManager manages round-robin rotation state per combo.
// Counters are kept in memory so combo resolution never blocks on SQLite I/O.
// Best-effort async writes flush state to the DB for continuity across restarts.
type RotationManager struct {
	mu sync.RWMutex
	db *sql.DB
	counters map[string]int
	// pending holds the latest in-memory counter per combo awaiting a DB flush.
	pending map[string]int
	// flushScheduled is true while a debounced flush goroutine is pending.
	flushScheduled bool
}

// NewRotationManager creates a new rotation manager.
func NewRotationManager(database *sql.DB) *RotationManager {
	return &RotationManager{
		db: database,
		counters: make(map[string]int),
		pending: make(map[string]int),
	}
}

// GetRotatedSteps returns combo steps in the order they should be attempted for
// the given strategy:
//   - "round-robin": rotated by the persistent counter (sticky for stickyLimit hits)
//   - "weighted": a weighted-random shuffle (probability ∝ step.Weight)
//   - "priority" (default) and single-step combos: steps unchanged (priority order)
func (rm *RotationManager) GetRotatedSteps(comboID string, strategy string, stickyLimit int, steps []db.ComboStep) []db.ComboStep {
	if len(steps) <= 1 {
		return steps
	}

	switch strategy {
	case "round-robin":
		counter := rm.nextCounter(comboID)
		effectiveIndex := (counter / stickyLimit) % len(steps)
		rotated := make([]db.ComboStep, len(steps))
		for i := range steps {
			rotated[i] = steps[(effectiveIndex+i)%len(steps)]
		}
		return rotated
	case "weighted":
		return weightedShuffle(steps)
	default: // priority
		return steps
	}
}

// weightedShuffle returns steps in a weighted-random order so that higher-weight
// steps are attempted first more often, while still allowing failover to lower
// ones. Sampling without replacement guarantees every step appears exactly once.
func weightedShuffle(steps []db.ComboStep) []db.ComboStep {
	pool := make([]db.ComboStep, len(steps))
	copy(pool, steps)
	out := make([]db.ComboStep, 0, len(steps))
	r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
	for len(pool) > 0 {
		total := 0
		for _, s := range pool {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			total += w
		}
		pick := r.IntN(total) + 1
		acc := 0
		idx := 0
		for i, s := range pool {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			acc += w
			if pick <= acc {
				idx = i
				break
			}
		}
		out = append(out, pool[idx])
		pool = append(pool[:idx], pool[idx+1:]...)
	}
	return out
}

// nextCounter returns the current counter for comboID and advances it in memory.
// The first call lazily seeds the in-memory value from the DB so restarts keep
// approximate position. DB writes are coalesced: the new value lands in `pending`
// and a single debounced goroutine flushes all pending counters in batches, so a
// high request rate does not spawn a SQLite UPSERT per resolve.
func (rm *RotationManager) nextCounter(comboID string) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	counter, ok := rm.counters[comboID]
	if !ok {
		counter = rm.loadCounter(comboID)
		rm.counters[comboID] = counter
	}
	rm.counters[comboID] = counter + 1

	rm.pending[comboID] = counter + 1
	if !rm.flushScheduled {
		rm.flushScheduled = true
		go rm.flushPending()
	}

	return counter
}

// flushPending waits one interval, then writes all pending counters to SQLite in
// a single batch and clears the pending set. Further increments that arrive
// while a flush is scheduled simply update `pending`; they are picked up by the
// next scheduled flush rather than spawning additional goroutines.
func (rm *RotationManager) flushPending() {
	time.Sleep(rotationFlushInterval)
	rm.mu.Lock()
	batch := rm.pending
	rm.pending = make(map[string]int)
	rm.flushScheduled = false
	rm.mu.Unlock()

	for comboID, counter := range batch {
		rm.persistCounter(comboID, counter)
	}
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
	delete(rm.pending, comboID)
	rm.mu.Unlock()

	rm.db.Exec(`DELETE FROM rotation_state WHERE combo_id = ?`, comboID)
}
