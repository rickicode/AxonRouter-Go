package v1

import (
	"database/sql"
	"sync"
	"time"
)

// codexLiveSessionTTL matches the public constant in codexlive.go. It is
// private here to avoid a circular depend-on-the-whole-file issue.

// codexLiveSession holds the runtime data needed to reconnect a Codex Live
// sideband WebSocket to the same upstream connection.
type codexLiveSession struct {
	callID    string
	connID    string
	connToken string
	model     string
	createdAt time.Time
}

// codexLiveSessionStore is the persistence-capable live session store. By
// default it keeps sessions in memory and purges expired entries on access.
// When a *sql.DB is supplied, sessions are persisted to the
// codex_live_sessions table and reloaded from it on creation, so live calls
// can survive process restart when a database is configured.
type codexLiveSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]codexLiveSession
	db       *sql.DB
}

func newCodexLiveSessionStore() *codexLiveSessionStore {
	return &codexLiveSessionStore{sessions: make(map[string]codexLiveSession)}
}

// newCodexLiveSessionStoreWithDB creates a session store backed by the provided
// database. Existing non-expired sessions are loaded into memory.
func newCodexLiveSessionStoreWithDB(db *sql.DB) *codexLiveSessionStore {
	s := &codexLiveSessionStore{
		sessions: make(map[string]codexLiveSession),
		db:       db,
	}
	if db != nil {
		s.loadFromDB()
	}
	return s
}

func (s *codexLiveSessionStore) get(callID string) (codexLiveSession, bool) {
	if s == nil || !codexLiveCallIDPattern.MatchString(callID) {
		return codexLiveSession{}, false
	}
	s.mu.RLock()
	sess, ok := s.sessions[callID]
	s.mu.RUnlock()
	if !ok || time.Since(sess.createdAt) > codexLiveSessionTTL {
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
	s.mu.Lock()
	s.sessions[callID] = sess
	s.mu.Unlock()
	if s.db != nil {
		expiresAt := sess.createdAt.Add(codexLiveSessionTTL)
		_, _ = s.db.Exec(`
			INSERT INTO codex_live_sessions (call_id, conn_id, conn_token, model, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(call_id) DO UPDATE SET
				conn_id = excluded.conn_id,
				conn_token = excluded.conn_token,
				model = excluded.model,
				created_at = excluded.created_at,
				expires_at = excluded.expires_at
		`, callID, connID, connToken, model, sess.createdAt.Unix(), expiresAt.Unix())
	}
}

func (s *codexLiveSessionStore) delete(callID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, callID)
	s.mu.Unlock()
	if s.db != nil {
		_, _ = s.db.Exec(`DELETE FROM codex_live_sessions WHERE call_id = ?`, callID)
	}
}

// purgeExpired removes in-memory entries older than the TTL and optionally
// deletes them from the database.
func (s *codexLiveSessionStore) purgeExpired(now time.Time) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []string
	for callID, sess := range s.sessions {
		if now.Sub(sess.createdAt) > codexLiveSessionTTL {
			delete(s.sessions, callID)
			expired = append(expired, callID)
		}
	}
	if s.db != nil {
		for _, callID := range expired {
			_, _ = s.db.Exec(`DELETE FROM codex_live_sessions WHERE call_id = ?`, callID)
		}
	}
	return len(expired)
}

// loadFromDB hydrates the in-memory store from persisted non-expired rows.
func (s *codexLiveSessionStore) loadFromDB() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query(`
		SELECT call_id, conn_id, conn_token, model, created_at
		FROM codex_live_sessions
		WHERE expires_at > ?
	`, time.Now().Unix())
	if err != nil {
		return
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var callID, connID, connToken, model string
		var createdAt int64
		if err := rows.Scan(&callID, &connID, &connToken, &model, &createdAt); err != nil {
			continue
		}
		s.sessions[callID] = codexLiveSession{
			callID:    callID,
			connID:    connID,
			connToken: connToken,
			model:     model,
			createdAt: time.Unix(createdAt, 0),
		}
	}
}

// count returns the number of sessions in memory, including any that may have
// expired but have not yet been purged.
func (s *codexLiveSessionStore) count() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}
