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
		sessions: make(map[string]Session),
	}
}

type memoryStore struct {
	mu       sync.RWMutex
	ttl      time.Duration
	sessions map[string]Session
}

func (m *memoryStore) Get(ctx context.Context, callID string) (Session, bool, error) {
	if callID == "" {
		return Session{}, false, ErrInvalidCallID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.evict(now)
	sess, ok := m.sessions[callID]
	if !ok {
		return Session{}, false, nil
	}
	return sess, true, nil
}

func (m *memoryStore) Put(ctx context.Context, sess Session) error {
	if sess.CallID == "" {
		return ErrInvalidCallID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.evict(now)
	sess.ExpiresAt = now.Add(m.ttl)
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	m.sessions[sess.CallID] = sess
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
	for id, sess := range m.sessions {
		if isExpired(now, sess) {
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}
