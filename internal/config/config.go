package config

import (
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Port        string
	DBPath      string
	PIDFile     string
	LogDir      string
	AdminAPIKey string
	Debug       bool
	JWTSecret   string
}

var (
	global Config
	once   sync.Once
)

// Init sets the global config. Call once at startup.
func Init(cfg Config) {
	once.Do(func() {
		global = cfg
	})
}

// Get returns the global config. Initializes with defaults if not explicitly set.
func Get() Config {
	once.Do(func() {
		dataDir := os.Getenv("AXON_DATA_DIR")
		if dataDir == "" {
			home, _ := os.UserHomeDir()
			dataDir = filepath.Join(home, ".axonrouter")
		}
		global = Config{
			Port:          getEnv("AXON_PORT", "3777"),
			DBPath:        filepath.Join(dataDir, "axonrouter.db"),
			PIDFile:       filepath.Join(dataDir, "axonrouter.pid"),
			LogDir:        filepath.Join(dataDir, "logs"),
			AdminAPIKey:   os.Getenv("AXON_ADMIN_KEY"),
		}
		os.MkdirAll(dataDir, 0755)
		os.MkdirAll(global.LogDir, 0755)
	})
	return global
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
