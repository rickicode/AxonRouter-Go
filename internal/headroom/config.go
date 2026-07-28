package headroom

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config is the env-driven configuration for Headroom.
type Config struct {
	Enabled         bool
	Endpoint        string
	Timeout         time.Duration
	MaxPayloadBytes int
}

var (
	defaultConfig Config
	once          sync.Once
)

// DefaultConfig returns a singleton config parsed from environment variables.
func DefaultConfig() Config {
	once.Do(func() {
		defaultConfig = Config{
			Enabled:         getEnvBool("AXON_HEADROOM_ENABLED", false),
			Endpoint:        getEnvStr("AXON_HEADROOM_ENDPOINT", "http://127.0.0.1:9123"),
			Timeout:         time.Duration(getEnvInt("AXON_HEADROOM_TIMEOUT_MS", 30000)) * time.Millisecond,
			MaxPayloadBytes: getEnvInt("AXON_HEADROOM_MAX_PAYLOAD_BYTES", 512*1024),
		}
	})
	return defaultConfig
}

// NewConfig parses configuration explicitly from a map (useful for tests).
func NewConfig(env map[string]string) Config {
	lookup := func(key, fallback string) string {
		if v, ok := env[key]; ok && v != "" {
			return v
		}
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	return Config{
		Enabled:         parseBool(lookup("AXON_HEADROOM_ENABLED", "false")),
		Endpoint:        lookup("AXON_HEADROOM_ENDPOINT", "http://127.0.0.1:9123"),
		Timeout:         time.Duration(parseInt(lookup("AXON_HEADROOM_TIMEOUT_MS", "30000"))) * time.Millisecond,
		MaxPayloadBytes: parseInt(lookup("AXON_HEADROOM_MAX_PAYLOAD_BYTES", "524288")),
	}
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	return parseBool(getEnvStr(key, strconv.FormatBool(fallback)))
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseInt(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n
}
