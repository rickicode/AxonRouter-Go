package v1

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// codexLiveSessionStore persists Codex Live sessions to SQLite when a database
// is available, falling back to an in-memory map otherwise. Sessions are
// validated by a TTL at read time and expired rows are pruned periodically.
type codexLiveSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]codexLiveSession
	db       *sql.DB
	ttl      time.Duration
}

func newCodexLiveSessionStoreImpl(database ...*sql.DB) *codexLiveSessionStore {
	var d *sql.DB
	if len(database) > 0 {
		d = database[0]
	}
	return &codexLiveSessionStore{
		sessions: make(map[string]codexLiveSession),
		db:       d,
		ttl:      codexLiveSessionTTL,
	}
}

func (s *codexLiveSessionStore) get(callID string) (codexLiveSession, bool) {
	if s == nil || !codexLiveCallIDPattern.MatchString(callID) {
		return codexLiveSession{}, false
	}
	if s.db != nil {
		return s.getDB(callID)
	}
	return s.getMemory(callID)
}

func (s *codexLiveSessionStore) getMemory(callID string) (codexLiveSession, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[callID]
	s.mu.RUnlock()
	if !ok || time.Since(sess.createdAt) > s.ttl {
		return codexLiveSession{}, false
	}
	return sess, true
}

func (s *codexLiveSessionStore) getDB(callID string) (codexLiveSession, bool) {
	var sess codexLiveSession
	var createdAt int64
	err := s.db.QueryRow(`
		SELECT call_id, conn_id, conn_token, model, created_at
		FROM codex_live_sessions
		WHERE call_id = ?
	`, callID).Scan(&sess.callID, &sess.connID, &sess.connToken, &sess.model, &createdAt)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logging.Logger.Warn("codex live session db read failed", "error", err.Error())
		}
		return codexLiveSession{}, false
	}
	sess.createdAt = time.Unix(createdAt, 0)
	if time.Since(sess.createdAt) > s.ttl {
		// Best-effort cleanup of expired row.
		_, _ = s.db.Exec(`DELETE FROM codex_live_sessions WHERE call_id = ?`, callID)
		return codexLiveSession{}, false
	}
	return sess, true
}

func (s *codexLiveSessionStore) put(callID, connID, connToken, model string) {
	if s == nil || !codexLiveCallIDPattern.MatchString(callID) {
		return
	}
	sess := codexLiveSession{
		callID:    callID,
		connID:    connID,
		connToken: connToken,
		model:     model,
		createdAt: time.Now(),
	}
	if s.db != nil {
		if _, err := s.db.Exec(`
			INSERT INTO codex_live_sessions (call_id, conn_id, conn_token, model, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(call_id) DO UPDATE SET
				conn_id = excluded.conn_id,
				conn_token = excluded.conn_token,
				model = excluded.model,
				created_at = excluded.created_at
		`, sess.callID, sess.connID, sess.connToken, sess.model, sess.createdAt.Unix()); err != nil {
			logging.Logger.Warn("codex live session db write failed", "error", err.Error())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[callID] = sess
}

func (s *codexLiveSessionStore) delete(callID string) {
	if s == nil {
		return
	}
	if s.db != nil {
		if _, err := s.db.Exec(`DELETE FROM codex_live_sessions WHERE call_id = ?`, callID); err != nil {
			logging.Logger.Warn("codex live session db delete failed", "error", err.Error())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, callID)
}

// cleanup removes expired sessions from the backing database and the in-memory
// fallback. It is exported for tests and background maintenance, and should be
// invoked periodically.
func (s *codexLiveSessionStore) cleanup() int {
	if s == nil {
		return 0
	}
	cutoff := time.Now().Add(-s.ttl).Unix()
	if s.db != nil {
		if _, err := s.db.Exec(`DELETE FROM codex_live_sessions WHERE created_at < ?`, cutoff); err != nil {
			logging.Logger.Warn("codex live session db cleanup failed", "error", err.Error())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	memRows := 0
	for id, sess := range s.sessions {
		if now.Sub(sess.createdAt) > s.ttl {
			delete(s.sessions, id)
			memRows++
		}
	}
	return memRows
}

// isPersistent reports whether the store has a database-backed persistence
// layer. Callers can use this to document survival guarantees.
func (s *codexLiveSessionStore) isPersistent() bool {
	return s != nil && s.db != nil
}
