package config

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type Config struct {
	Port                   string
	DBPath                 string
	DBURL                  string
	DBToken                string
	PIDFile                string
	LogDir                 string
	DataDir                string
	Debug                  bool
	JWTSecret              string
	DeviceTrackerTTLMs     int
	DeviceTrackerMaxPerKey int
	DeviceTrackerMaxTotal  int
}

var (
	global Config
	once   sync.Once
)

// resolveDataDir picks the data directory: explicit value, then AXONROUTER_DIR,
// then the default ~/axonrouter. Relative paths are resolved against $HOME.
func resolveDataDir(explicit string) string {
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			return explicit
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, explicit)
		}
		return explicit
	}

	if env := os.Getenv("AXONROUTER_DIR"); env != "" {
		if filepath.IsAbs(env) {
			return env
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, env)
		}
		return env
	}

	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "axonrouter")
	}
	return "axonrouter"
}

// Init sets the global config. Call once at startup.
func Init(cfg Config) {
	once.Do(func() {
		cfg.DataDir = resolveDataDir(cfg.DataDir)
		global = cfg
	})
}

// Get returns the global config. Initializes with defaults if not explicitly set.
func Get() Config {
	once.Do(func() {
		dataDir := resolveDataDir("")
		global = Config{
			Port:                   getEnv("AXON_PORT", "3777"),
			DBPath:                 filepath.Join(dataDir, "axonrouter.db"),
			DBURL:                  getEnv("AXON_DB_URL", ""),
			DBToken:                getEnv("AXON_DB_TOKEN", ""),
			PIDFile:                filepath.Join(dataDir, "axonrouter.pid"),
			LogDir:                 filepath.Join(dataDir, "logs"),
			DataDir:                dataDir,
			DeviceTrackerTTLMs:     getIntEnv("DEVICE_TRACKER_TTL_MS", 30*60*1000),
			DeviceTrackerMaxPerKey: getIntEnv("DEVICE_TRACKER_MAX_PER_KEY", 1000),
			DeviceTrackerMaxTotal:  getIntEnv("DEVICE_TRACKER_MAX_TOTAL_DEVICES", 10000),
		}
		os.MkdirAll(dataDir, 0o755)
		os.MkdirAll(global.LogDir, 0o755)
	})
	return global
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
