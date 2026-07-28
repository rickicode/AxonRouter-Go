package codexlivestore

import (
	"context"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// Memory returns a process-local in-memory store with explicit TTL.
// It requires ttl > 0; callers may also invoke Cleanup to delete expired
// entries immediately. A background janitor is not started to keep the
// surface simple; expired entries are evicted on every read/write.
func Memory(ttl time.Duration) Store {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &memoryStore{
		ttl:      ttl,
		sessions: make(map[string]*entry),
	}
}

type entry struct {
	sess  Session
	ttl   time.Duration
	added time.Time
}

type memoryStore struct {
	mu       sync.RWMutex
	ttl      time.Duration
	sessions map[string]*entry
}

func (m *memoryStore) Get(ctx context.Context, callID string) (Session, bool, error) {
	if callID == "" {
		return Session{}, false, ErrInvalidCallID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evict(time.Now())
	e, ok := m.sessions[callID]
	if !ok {
		return Session{}, false, nil
	}
	if isExpired(time.Now(), e.sess) {
		delete(m.sessions, callID)
		return Session{}, false, nil
	}
	return e.sess, true, nil
}

func (m *memoryStore) Put(ctx context.Context, sess Session) error {
	if sess.CallID == "" {
		return ErrInvalidCallID
	}
	if sess.ExpiresAt.IsZero() {
		sess.ExpiresAt = time.Now().Add(m.ttl)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evict(time.Now())
	m.sessions[sess.CallID] = &entry{sess: sess, ttl: m.ttl, added: time.Now()}
	return nil
}

func (m *memoryStore) Delete(ctx context.Context, callID string) error {
	if callID == "" {
		return ErrInvalidCallID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, callID)
	return nil
}

// Cleanup removes all expired sessions and returns how many were removed.
func (m *memoryStore) Cleanup(ctx context.Context) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := m.evict(time.Now())
	logging.Logger.Debug("codex live memory store cleanup completed", "removed", removed, "remaining", len(m.sessions))
	return removed
}

func (m *memoryStore) evict(now time.Time) int {
	removed := 0
	for id, e := range m.sessions {
		if isExpired(now, e.sess) || time.Since(e.added) > e.ttl*2 {
			// Be defensive: drop anything that's expired or has lived
			// more than twice its TTL (protects against clock skew).
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}
