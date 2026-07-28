package codexlivestore

import (
	"context"
	"database/sql"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// SQLite returns a persistent SQLite-backed session store using the supplied
// *sql.DB. ttl is the default duration applied when Put sees a zero ExpiresAt.
// Callers should run CleanExpired in a background goroutine.
func SQLite(db *sql.DB, ttl time.Duration) Store {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &sqliteStore{db: db, ttl: ttl}
}

type sqliteStore struct {
	db  *sql.DB
	ttl time.Duration
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS codex_live_sessions (
	call_id TEXT PRIMARY KEY,
	conn_id TEXT NOT NULL,
	conn_token TEXT NOT NULL,
	model TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_codex_live_sessions_expires_at
	ON codex_live_sessions(expires_at);
`

// InitSQLiteSchema creates the codex_live_sessions table and index.
func InitSQLiteSchema(db *sql.DB) error {
	_, err := db.Exec(sqliteSchema)
	return err
}

func (s *sqliteStore) Get(ctx context.Context, callID string) (Session, bool, error) {
	if callID == "" {
		return Session{}, false, ErrInvalidCallID
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT call_id, conn_id, conn_token, model, created_at, expires_at
		FROM codex_live_sessions
		WHERE call_id = ? AND expires_at > ?
	`, callID, time.Now().Unix())
	var sess Session
	var created, expires int64
	err := row.Scan(&sess.CallID, &sess.ConnID, &sess.ConnToken, &sess.Model, &created, &expires)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.ExpiresAt = time.Unix(expires, 0).UTC()
	return sess, true, nil
}

func (s *sqliteStore) Put(ctx context.Context, sess Session) error {
	if sess.CallID == "" {
		return ErrInvalidCallID
	}
	if sess.ExpiresAt.IsZero() {
		sess.ExpiresAt = time.Now().Add(s.ttl)
	}
	created := sess.CreatedAt.Unix()
	if created == 0 {
		created = time.Now().Unix()
		sess.CreatedAt = time.Unix(created, 0).UTC()
	}
	expires := sess.ExpiresAt.Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO codex_live_sessions (call_id, conn_id, conn_token, model, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(call_id) DO UPDATE SET
			conn_id = excluded.conn_id,
			conn_token = excluded.conn_token,
			model = excluded.model,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at
	`, sess.CallID, sess.ConnID, sess.ConnToken, sess.Model, created, expires)
	return err
}

func (s *sqliteStore) Delete(ctx context.Context, callID string) error {
	if callID == "" {
		return ErrInvalidCallID
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM codex_live_sessions WHERE call_id = ?`, callID)
	return err
}

// CleanExpired removes expired rows and returns the deleted count.
func (s *sqliteStore) CleanExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM codex_live_sessions WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	logging.Logger.Debug("codex live sqlite store cleanup completed", "removed", affected)
	return affected, nil
}
