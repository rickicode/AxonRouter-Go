package db

import (
	"database/sql"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	globalDB *sql.DB
	dbOnce   sync.Once
)

// Open opens (or creates) the SQLite database and runs migrations.
// This is the primary initialization function used by the application.
func Open(dbPath string) (*sql.DB, error) {
	var initErr error
	dbOnce.Do(func() {
		d, err := sql.Open("sqlite", dbPath)
		if err != nil {
			initErr = err
			return
		}

		// WAL mode + busy timeout for concurrent reads
		if _, err := d.Exec("PRAGMA journal_mode=WAL"); err != nil {
			d.Close()
			initErr = err
			return
		}
		if _, err := d.Exec("PRAGMA busy_timeout=5000"); err != nil {
			d.Close()
			initErr = err
			return
		}
		if _, err := d.Exec("PRAGMA foreign_keys=ON"); err != nil {
			d.Close()
			initErr = err
			return
		}

		d.SetMaxOpenConns(1) // SQLite: single writer
		d.SetMaxIdleConns(1)
		d.SetConnMaxLifetime(0)

		if err := RunMigrations(d); err != nil {
			initErr = err
			d.Close()
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
		d, err = Open(path)
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
