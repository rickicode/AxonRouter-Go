package headroom

import (
	"os"
	"strconv"
	"strings"
)

const (
	DefaultEndpoint        = "127.0.0.1:9123"
	DefaultTimeoutMs       = 30000
	DefaultMaxPayloadBytes = 524288
)

// Config is the environment-driven configuration for headroom.
type Config struct {
	Enabled         bool
	Endpoint        string
	TimeoutMs       int
	MaxPayloadBytes int
}

// DefaultConfig returns a Config populated from environment variables.
func DefaultConfig() Config {
	return Config{
		Enabled:         getEnvBool("AXON_HEADROOM_ENABLED", false),
		Endpoint:        getEnv("AXON_HEADROOM_ENDPOINT", DefaultEndpoint),
		TimeoutMs:       getEnvInt("AXON_HEADROOM_TIMEOUT_MS", DefaultTimeoutMs),
		MaxPayloadBytes: getEnvInt("AXON_HEADROOM_MAX_PAYLOAD_BYTES", DefaultMaxPayloadBytes),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return fallback
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
