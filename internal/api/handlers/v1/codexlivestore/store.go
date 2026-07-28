package codexlivestore

import (
	"context"
	"errors"
	"time"
)

// Session holds the state needed to reconnect a Codex Live sideband call.
type Session struct {
	CallID    string
	ConnID    string
	ConnToken string
	Model     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store is a Codex Live session backend with explicit TTL / cleanup.
type Store interface {
	// Get returns a non-expired session by call ID.
	Get(ctx context.Context, callID string) (Session, bool, error)
	// Put writes a session with the configured TTL.
	Put(ctx context.Context, sess Session) error
	// Delete removes a session.
	Delete(ctx context.Context, callID string) error
}

// Common errors.
var ErrInvalidCallID = errors.New("invalid codex live call id")
var ErrSessionNotFound = errors.New("codex live session not found")

func isExpired(now time.Time, s Session) bool {
	return s.ExpiresAt.Before(now) || s.ExpiresAt.Equal(now)
}
