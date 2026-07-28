package codexlivestore

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Provider names.
const (
	ProviderMemory = "memory"
	ProviderSQLite = "sqlite"
	ProviderRedis  = "redis"
)

// Options selects and configures a Store backend.
type Options struct {
	// Provider is one of memory, sqlite, or redis. Empty defaults to memory.
	Provider string
	// TTL for each session. Zero uses the package default (1 hour).
	TTL time.Duration
	// SQLite database handle. Required when Provider == sqlite.
	DB *sql.DB
	// Redis client. Required when Provider == redis.
	RedisClient *redis.Client
}

// New creates a Store from Options.
func New(opts Options) (Store, error) {
	provider := opts.Provider
	if provider == "" {
		provider = ProviderMemory
	}
	switch provider {
	case ProviderMemory:
		return Memory(opts.TTL), nil
	case ProviderSQLite:
		if opts.DB == nil {
			return nil, errors.New("codex live sqlite store requires *sql.DB")
		}
		if err := InitSQLiteSchema(opts.DB); err != nil {
			return nil, fmt.Errorf("codex live sqlite schema: %w", err)
		}
		return SQLite(opts.DB, opts.TTL), nil
	case ProviderRedis:
		if opts.RedisClient == nil {
			return nil, errors.New("codex live redis store requires *redis.Client")
		}
		return Redis(opts.RedisClient, opts.TTL), nil
	default:
		return nil, fmt.Errorf("unsupported codex live session provider: %q", provider)
	}
}

// ProviderFromEnv selects the provider from environment variables, defaulting
// to memory. It does not create the Redis client; callers can read
// CODEX_LIVE_STORE_PROVIDER plus CODEX_LIVE_STORE_TTL.
func ProviderFromEnv() string {
	return "" // Placeholder for callers to read env directly; see config package.
}
