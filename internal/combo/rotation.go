package combo

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
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
	mu       sync.RWMutex
	db       *sql.DB
	counters map[string]int
	// pending holds the latest in-memory counter per combo awaiting a DB flush.
	pending map[string]int
	// flushScheduled is true while a debounced flush goroutine is pending.
	flushScheduled bool
	// usageCache memoizes request_log counts for the least-used strategy.
	usageCache usageCacheSnapshot
	uMu        sync.Mutex
}

const usageCacheTTL = 30 * time.Second

// usageCacheSnapshot holds per-model usage counts for least-used ordering.
type usageCacheSnapshot struct {
	key     string
	counts  map[string]int
	expires time.Time
}

// NewRotationManager creates a new rotation manager.
func NewRotationManager(database *sql.DB) *RotationManager {
	return &RotationManager{
		db:       database,
		counters: make(map[string]int),
		pending:  make(map[string]int),
	}
}

// GetRotatedSteps returns combo steps in the order they should be attempted for
// the given strategy:
//   - "round-robin": rotated by the persistent counter (sticky for stickyLimit hits)
//
// - "weighted": a weighted-random shuffle (probability ∝ step.Weight)
// - "random": an unweighted random shuffle
// - "least-used": steps ordered by recent successful usage (lowest first)
// - "priority" (default) and single-step combos: steps unchanged (priority order)
func (rm *RotationManager) GetRotatedSteps(comboID string, strategy string, stickyLimit int, steps []db.ComboStep) []db.ComboStep {
	if len(steps) <= 1 {
		return steps
	}

	switch strategy {
	case "round-robin":
		limit := stickyLimit
		if limit <= 0 {
			limit = 1
		}
		counter := rm.nextCounter(comboID)
		effectiveIndex := (counter / limit) % len(steps)
		rotated := make([]db.ComboStep, len(steps))
		for i := range steps {
			rotated[i] = steps[(effectiveIndex+i)%len(steps)]
		}
		return rotated
	case "weighted":
		return weightedShuffle(steps)
	case "random":
		return randomShuffle(steps)
	case "least-used":
		return rm.leastUsedOrder(steps)
	default: // priority
		return steps
	}
}

// randomShuffle returns steps in an unweighted random order. Each call may return
// a different permutation; all steps are kept exactly once.
func randomShuffle(steps []db.ComboStep) []db.ComboStep {
	out := make([]db.ComboStep, len(steps))
	copy(out, steps)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// leastUsedOrder reorders steps so models with the lowest recent successful usage
// counts are tried first. Counts are pulled from request_logs and cached briefly
// to avoid querying SQLite on every combo resolution.
func (rm *RotationManager) leastUsedOrder(steps []db.ComboStep) []db.ComboStep {
	modelIDs := make([]string, len(steps))
	for i, s := range steps {
		modelIDs[i] = s.ModelID
	}
	counts := rm.cachedUsageCounts(modelIDs)

	out := make([]db.ComboStep, len(steps))
	copy(out, steps)
	sort.SliceStable(out, func(i, j int) bool {
		return counts[out[i].ModelID] < counts[out[j].ModelID]
	})
	return out
}

// cachedUsageCounts returns recent successful request counts per model ID.
// Results are cached for usageCacheTTL because the value is recomputed from
// request_logs and the query cost does not need to be paid on every request.
func (rm *RotationManager) cachedUsageCounts(modelIDs []string) map[string]int {
	sort.Strings(modelIDs)
	key := strings.Join(modelIDs, "\x00")

	rm.uMu.Lock()
	defer rm.uMu.Unlock()

	if rm.usageCache.key == key && time.Now().Before(rm.usageCache.expires) {
		return rm.usageCache.counts
	}

	counts := rm.usageCounts(modelIDs)
	rm.usageCache = usageCacheSnapshot{
		key:     key,
		counts:  counts,
		expires: time.Now().Add(usageCacheTTL),
	}
	return counts
}

// usageCounts queries request_logs for successful calls per provider+model over
// the last 7 days. Steps with no usage get a count of 0. Counts are keyed by
// the original step model ID so callers can look them up without re-parsing.
func (rm *RotationManager) usageCounts(modelIDs []string) map[string]int {
	counts := make(map[string]int, len(modelIDs))
	providers := make([]string, 0, len(modelIDs))
	models := make([]string, 0, len(modelIDs))
	pmToIDs := make(map[string][]string, len(modelIDs))
	for _, id := range modelIDs {
		provider, model, ok := SplitProviderModel(id)
		if !ok {
			continue
		}
		providers = append(providers, provider)
		models = append(models, model)
		counts[id] = 0
		key := provider + "/" + model
		pmToIDs[key] = append(pmToIDs[key], id)
	}
	if len(providers) == 0 || rm.db == nil {
		return counts
	}

	minTs := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	q := fmt.Sprintf(""+
		"SELECT provider_type_id, model_id, COUNT(*) FROM request_logs "+
		"WHERE provider_type_id IN (%s) AND model_id IN (%s) "+
		"AND status_code >= 200 AND status_code < 400 AND timestamp > ? "+
		"GROUP BY provider_type_id, model_id",
		placeholders(len(providers)), placeholders(len(models)))

	args := make([]any, 0, len(providers)+len(models)+1)
	for _, p := range providers {
		args = append(args, p)
	}
	for _, m := range models {
		args = append(args, m)
	}
	args = append(args, minTs)

	rows, err := rm.db.Query(q, args...)
	if err != nil {
		// Fail open: return zero counts so order stays deterministic.
		return counts
	}
	defer rows.Close()

	for rows.Next() {
		var provider, model string
		var n int
		if err := rows.Scan(&provider, &model, &n); err != nil {
			continue
		}
		for _, id := range pmToIDs[provider+"/"+model] {
			counts[id] = n
		}
	}
	return counts
}

// placeholders returns "?" repeated n times joined by commas.
func placeholders(n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = "?"
	}
	return strings.Join(p, ",")
}

// weightedShuffle returns steps in a weighted-random order so that higher-weight
// steps are attempted first more often, while still allowing failover to lower
// ones. Sampling without replacement guarantees every step appears exactly once.
func weightedShuffle(steps []db.ComboStep) []db.ComboStep {
	pool := make([]db.ComboStep, len(steps))
	copy(pool, steps)
	out := make([]db.ComboStep, 0, len(steps))
	for len(pool) > 0 {
		total := 0
		for _, s := range pool {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			total += w
		}
		pick := rand.IntN(total) + 1
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
