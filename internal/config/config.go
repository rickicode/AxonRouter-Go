package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// AntigravityCreditsMode controls whether Antigravity requests include
// Google One AI credits via enabledCreditTypes.
type AntigravityCreditsMode string

const (
	AntigravityCreditsModeOff    AntigravityCreditsMode = "off"
	AntigravityCreditsModeRetry  AntigravityCreditsMode = "retry"
	AntigravityCreditsModeAlways AntigravityCreditsMode = "always"
)

// defaultAntigravityObfuscationWords masks competitor/client names before they
// reach the Google Antigravity API. Matches OmniRoute's DEFAULT_WORDS.
var defaultAntigravityObfuscationWords = []string{
	"opencode", "open-code", "cline", "roo-cline", "roo_cline",
	"cursor", "windsurf", "aider", "continue.dev", "copilot",
	"avante", "codecompanion", "claude code", "claude-code",
	"kilo code", "kilocode", "omniroute",
}

type Config struct {
	Port                        string
	DBPath                      string
	DBURL                       string
	DBToken                     string
	PIDFile                     string
	LogDir                      string
	LogsMaxTotalSizeMB          int
	DataDir                     string
	Debug                       bool
	JWTSecret                   string
	DeviceTrackerTTLMs          int
	DeviceTrackerMaxPerKey      int
	DeviceTrackerMaxTotal       int
	AntigravityCredits          AntigravityCreditsMode
	AntigravityObfuscationWords []string
	// Claude cloaking / CCH signing controls
	DisableClaudeCloakMode       bool
	ClaudeCloakMode              string // "auto" (default), "always", "never"
	ClaudeCloakSensitiveWords    []string
	ClaudeExperimentalCCHSigning bool
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
			Port:                         getEnv("AXON_PORT", "3777"),
			DBPath:                       filepath.Join(dataDir, "axonrouter.db"),
			DBURL:                        getEnv("AXON_DB_URL", ""),
			DBToken:                      getEnv("AXON_DB_TOKEN", ""),
			PIDFile:                      filepath.Join(dataDir, "axonrouter.pid"),
			LogDir:                       filepath.Join(dataDir, "logs"),
			LogsMaxTotalSizeMB:           getIntEnv("AXON_LOGS_MAX_TOTAL_SIZE_MB", 0),
			DataDir:                      dataDir,
			DeviceTrackerTTLMs:           getIntEnv("DEVICE_TRACKER_TTL_MS", 30*60*1000),
			DeviceTrackerMaxPerKey:       getIntEnv("DEVICE_TRACKER_MAX_PER_KEY", 1000),
			DeviceTrackerMaxTotal:        getIntEnv("DEVICE_TRACKER_MAX_TOTAL_DEVICES", 10000),
			AntigravityCredits:           parseAntigravityCreditsMode(getEnv("ANTIGRAVITY_CREDITS", "")),
			AntigravityObfuscationWords:  parseAntigravityObfuscationWords(getEnv("ANTIGRAVITY_OBFUSCATION_WORDS", "")),
			DisableClaudeCloakMode:       getEnvBool("AXON_DISABLE_CLAUDE_CLOAK", false),
			ClaudeCloakMode:              parseCloakMode(getEnv("AXON_CLAUDE_CLOAK_MODE", "auto")),
			ClaudeCloakSensitiveWords:    parseStringSliceEnv(getEnv("AXON_CLAUDE_CLOAK_SENSITIVE_WORDS", "")),
			ClaudeExperimentalCCHSigning: getEnvBool("AXON_CLAUDE_CCH_SIGNING", false),
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

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return fallback
}

func parseStringSliceEnv(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseCloakMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "always", "never":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "auto"
	}
}

func parseAntigravityCreditsMode(v string) AntigravityCreditsMode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "retry":
		return AntigravityCreditsModeRetry
	case "always":
		return AntigravityCreditsModeAlways
	default:
		return AntigravityCreditsModeOff
	}
}

func parseAntigravityObfuscationWords(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultAntigravityObfuscationWords
	}
	seen := make(map[string]bool)
	var out []string
	for _, raw := range strings.Split(v, ",") {
		word := strings.TrimSpace(raw)
		if word == "" {
			continue
		}
		word = strings.ToLower(word)
		if seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	if len(out) == 0 {
		return defaultAntigravityObfuscationWords
	}
	return out
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
