package db

import (
	"database/sql"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var (
	globalDB *sql.DB
	dbOnce   sync.Once
)

// isRemoteDSN reports whether dsn points to a remote libsql/Turso database
// rather than a local SQLite file.
func isRemoteDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "libsql://") ||
		strings.HasPrefix(dsn, "https://") ||
		strings.HasPrefix(dsn, "http://") ||
		strings.HasPrefix(dsn, "wss://")
}

// appendAuthToken injects authToken into a remote libsql DSN when a token is
// configured and the DSN does not already contain one.
func appendAuthToken(dsn, token string) (string, error) {
	if token == "" || strings.Contains(dsn, "authToken=") {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("authToken", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Open opens (or creates) the SQLite database and runs migrations.
// This is the primary initialization function used by the application.
//
// It supports two modes:
//   - Local file: any path without a remote scheme (e.g. "/data/axonrouter.db"
//     or ":memory:"). Uses the embedded modernc.org/sqlite driver.
//   - Remote libsql/Turso: dsn starting with libsql://, https://, http://, or
//     wss://. Uses github.com/tursodatabase/libsql-client-go.
//     AXON_DB_TOKEN is appended automatically if the DSN lacks an authToken.
func Open(dsn string, token string) (*sql.DB, error) {
	var initErr error
	dbOnce.Do(func() {
		driverName := "sqlite"
		remote := isRemoteDSN(dsn)
		if remote {
			driverName = "libsql"
			var err error
			dsn, err = appendAuthToken(dsn, token)
			if err != nil {
				initErr = err
				return
			}
		}

		d, err := sql.Open(driverName, dsn)
		if err != nil {
			initErr = err
			return
		}

		// Base pragmas that are safe for both local and remote databases.
		pragmas := []string{
			"PRAGMA foreign_keys=ON",
		}
		if !remote {
			// Local-only tuning. These PRAGMAs manage the SQLite engine on the
			// local filesystem and are not applicable (or rejected) by a remote
			// libsql server.
			pragmas = append(pragmas,
				"PRAGMA journal_mode=WAL",
				"PRAGMA busy_timeout=5000",
				"PRAGMA synchronous=NORMAL",
				"PRAGMA cache_size=-65536",
				"PRAGMA mmap_size=268435456",
				"PRAGMA temp_store=MEMORY",
				"PRAGMA wal_autocheckpoint=1000",
			)
		}
		for _, p := range pragmas {
			if _, err := d.Exec(p); err != nil {
				d.Close()
				initErr = err
				return
			}
		}

		// ── Connection pool: allow many concurrent readers ──
		// WAL mode permits unlimited concurrent readers; only writers serialize.
		// The async WriteQueue ensures there is a single writer, so no write-lock
		// contention ever reaches the pool. 50 open conns is conservative for Go
		// (each is a lightweight goroutine-friendly handle in modernc.org/sqlite).
		d.SetMaxOpenConns(50)
		d.SetMaxIdleConns(25)
		d.SetConnMaxLifetime(30 * time.Minute)
		d.SetConnMaxIdleTime(5 * time.Minute)

		// Run migrations (idempotent: CREATE TABLE IF NOT EXISTS + INSERT OR IGNORE
		// seed + provider-id normalization). Must run on every startup so seeded
		// provider_types and connection renames (e.g. opencode -> oc) stay in sync.
		if err := RunMigrations(d); err != nil {
			d.Close()
			initErr = err
			return
		}

		globalDB = d
	})

	if initErr != nil {
		return nil, initErr
	}
	return globalDB, nil
}

// Get returns the already-opened database. Panics if Open was not called.
func Get() *sql.DB {
	if globalDB == nil {
		panic("db.Open() must be called before db.Get()")
	}
	return globalDB
}

// UnixNow returns current unix timestamp (seconds).
func UnixNow() int64 {
	return time.Now().Unix()
}

// UnixMilliNow returns current unix timestamp (milliseconds).
func UnixMilliNow() int64 {
	return time.Now().UnixMilli()
}

// Store is a convenience wrapper around *sql.DB.
type Store struct {
	db *sql.DB
}

var (
	defaultStore *Store
	storeOnce    sync.Once
)

// Init opens the database and returns a Store wrapper.
func Init(path string) (*Store, error) {
	var err error
	storeOnce.Do(func() {
		var d *sql.DB
		d, err = Open(path, "")
		if err == nil {
			defaultStore = &Store{db: d}
		}
	})
	return defaultStore, err
}

// Default returns the global store. Call Init first.
func Default() *Store {
	if defaultStore == nil {
		log.Fatal("db: Init() must be called before Default()")
	}
	return defaultStore
}

// DB returns the raw *sql.DB.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetSetting returns a setting value from the database, falling back to default
// if the row does not exist or an error occurs.
func GetSetting(key, defaultValue string) string {
	if globalDB == nil {
		return defaultValue
	}
	var value string
	err := globalDB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil || value == "" {
		return defaultValue
	}
	return value
}

// SetSetting persists a setting value to the database, creating or replacing
// the row. It does not use the global store wrapper, mirroring GetSetting.
func SetSetting(key, value string) error {
	if globalDB == nil {
		return nil
	}
	_, err := globalDB.Exec(`
INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, key, value, UnixNow())
	return err
}
