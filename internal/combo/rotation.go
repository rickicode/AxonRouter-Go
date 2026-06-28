package combo

import (
	"database/sql"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// RotationManager manages round-robin rotation state per combo.
type RotationManager struct {
	mu sync.Mutex
	db *sql.DB
}

// NewRotationManager creates a new rotation manager.
func NewRotationManager(database *sql.DB) *RotationManager {
	return &RotationManager{db: database}
}

// GetRotatedSteps returns combo steps rotated according to the round-robin counter.
// For "priority" strategy or single-step combos, returns steps unchanged.
func (rm *RotationManager) GetRotatedSteps(comboID string, strategy string, stickyLimit int, steps []db.ComboStep) []db.ComboStep {
	if strategy != "round-robin" || len(steps) <= 1 {
		return steps
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	counter := rm.getCounter(comboID)
	effectiveIndex := (counter / stickyLimit) % len(steps)
	rm.incrementCounter(comboID)

	// Rotate: move first N elements to end
	rotated := make([]db.ComboStep, len(steps))
	copy(rotated, steps)
	for i := 0; i < effectiveIndex; i++ {
		rotated = append(rotated[1:], rotated[0])
	}
	return rotated
}

// getCounter reads the current rotation counter from SQLite.
func (rm *RotationManager) getCounter(comboID string) int {
	var counter int
	err := rm.db.QueryRow(`SELECT counter FROM rotation_state WHERE combo_id = ?`, comboID).Scan(&counter)
	if err != nil {
		return 0
	}
	return counter
}

// incrementCounter bumps the rotation counter and persists.
func (rm *RotationManager) incrementCounter(comboID string) {
	now := time.Now().Unix()
	_, err := rm.db.Exec(`
		INSERT INTO rotation_state (combo_id, counter, updated_at) VALUES (?, 1, ?)
		ON CONFLICT(combo_id) DO UPDATE SET counter = counter + 1, updated_at = ?
	`, comboID, now, now)
	if err != nil {
		// ponytail: silent fail — rotation is best-effort
	}
}

// ResetCounter resets the rotation counter for a combo.
func (rm *RotationManager) ResetCounter(comboID string) {
	rm.db.Exec(`DELETE FROM rotation_state WHERE combo_id = ?`, comboID)
}
