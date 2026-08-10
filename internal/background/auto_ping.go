package background

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Settings keys for auto-ping configuration and runtime metrics.
const (
	claudeAutoPingSettingKey  = "claude_auto_ping"
	codexAutoPingSettingKey   = "codex_auto_ping"
	autoPingMetricsSettingKey = "auto_ping_metrics"

	defaultAutoPingIntervalMin         = 1
	defaultAutoPingFallbackIntervalMin = 60
	autoPingRequestTimeout             = 15 * time.Second
	autoPingResetDriftSec              = 60
	autoPingFallbackMaxAgeForResetMin  = 10
)

// autoPingSettings is the per-provider configuration stored in settings.
type autoPingSettings struct {
	Enabled     bool            `json:"enabled"`
	Connections map[string]bool `json:"connections"`
}

// autoPingConnMetrics tracks per-connection timing.
type autoPingConnMetrics struct {
	LastPingAt  int64 `json:"last_ping_at,omitempty"`
	LastResetAt int64 `json:"last_reset_at,omitempty"`
}

// autoPingProviderMetrics tracks aggregate ping metrics per provider.
type autoPingProviderMetrics struct {
	Count            int64                            `json:"count"`
	LastPingAt       int64                            `json:"last_ping_at,omitempty"`
	LastConnectionID string                           `json:"last_connection_id,omitempty"`
	Connections      map[string]*autoPingConnMetrics `json:"connections,omitempty"`
}

func (m *autoPingProviderMetrics) connMetrics(connID string) *autoPingConnMetrics {
	if m.Connections == nil {
		m.Connections = make(map[string]*autoPingConnMetrics)
	}
	cm := m.Connections[connID]
	if cm == nil {
		cm = &autoPingConnMetrics{}
		m.Connections[connID] = cm
	}
	return cm
}

// autoPingMetrics is persisted so counts and last ping times survive restarts.
type autoPingMetrics struct {
	Claude *autoPingProviderMetrics `json:"claude,omitempty"`
	Codex  *autoPingProviderMetrics `json:"codex,omitempty"`
}

func (m *autoPingMetrics) provider(providerID string) *autoPingProviderMetrics {
	switch providerID {
	case "claude":
		if m.Claude == nil {
			m.Claude = &autoPingProviderMetrics{}
		}
		return m.Claude
	case "cx":
		if m.Codex == nil {
			m.Codex = &autoPingProviderMetrics{}
		}
		return m.Codex
	default:
		return nil
	}
}

// autoPingConnection is the DB row used by the scheduler.
type autoPingConnection struct {
	ID                   string
	ProviderTypeID       string
	AuthType             string
	APIKey               string
	AccessToken          string
	ProviderSpecificData string
	BaseURL              string
}

// autoPingProviderConfig holds provider-specific ping behaviour.
type autoPingProviderConfig struct {
	SettingKey   string
	ProviderID   string
	DefaultURL   string
	PingURL      func(baseURL string) string
	BuildHeaders func(conn autoPingConnection, psd map[string]string) map[string]string
}

var autoPingProviderConfigs = map[string]autoPingProviderConfig{
	"claude": {
		SettingKey: claudeAutoPingSettingKey,
		ProviderID: "claude",
		DefaultURL: "https://api.anthropic.com/v1/models",
		PingURL: func(baseURL string) string {
			if baseURL == "" {
				return "https://api.anthropic.com/v1/models"
			}
			baseURL = strings.TrimSuffix(baseURL, "/")
			return baseURL + "/models"
		},
		BuildHeaders: func(conn autoPingConnection, psd map[string]string) map[string]string {
			h := map[string]string{
				"Accept":            "application/json",
				"anthropic-version": "2023-06-01",
			}
			if conn.APIKey != "" {
				h["x-api-key"] = conn.APIKey
			}
			if conn.AccessToken != "" {
				h["Authorization"] = "Bearer " + conn.AccessToken
			}
			return h
		},
	},
	"cx": {
		SettingKey: codexAutoPingSettingKey,
		ProviderID: "cx",
		DefaultURL: "https://chatgpt.com/backend-api/wham/usage",
		PingURL: func(baseURL string) string {
			return "https://chatgpt.com/backend-api/wham/usage"
		},
		BuildHeaders: func(conn autoPingConnection, psd map[string]string) map[string]string {
			token := conn.AccessToken
			if token == "" {
				token = conn.APIKey
			}
			h := map[string]string{
				"Accept":        "application/json",
				"Authorization": "Bearer " + token,
			}
			if wid := psd["workspaceId"]; wid != "" {
				h["chatgpt-account-id"] = wid
			} else if tid := psd["teamId"]; tid != "" {
				h["chatgpt-account-id"] = tid
			} else if aid := psd["accountId"]; aid != "" {
				h["chatgpt-account-id"] = aid
			}
			return h
		},
	},
}

// autoPingSender performs the actual HTTP ping. It is swappable for tests.
type autoPingSender func(ctx context.Context, cfg autoPingProviderConfig, conn autoPingConnection) (int, error)

func defaultAutoPingSender(ctx context.Context, cfg autoPingProviderConfig, conn autoPingConnection, client *http.Client) (int, error) {
	url := cfg.PingURL(conn.BaseURL)
	var psd map[string]string
	if conn.ProviderSpecificData != "" {
		_ = json.Unmarshal([]byte(conn.ProviderSpecificData), &psd)
	}
	headers := cfg.BuildHeaders(conn, psd)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// AutoPingScheduler sends minimal, fail-silent pings after quota reset windows.
type AutoPingScheduler struct {
	once     sync.Once
	db       *sql.DB
	client   *http.Client
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	sender   autoPingSender

	mu      sync.Mutex
	metrics autoPingMetrics
	dirty   bool
}

// NewAutoPingScheduler creates a scheduler that ticks every intervalMin minutes.
func NewAutoPingScheduler(database *sql.DB, intervalMin int) *AutoPingScheduler {
	if intervalMin <= 0 {
		intervalMin = defaultAutoPingIntervalMin
	}
	s := &AutoPingScheduler{
		db:       database,
		client:   &http.Client{Timeout: autoPingRequestTimeout},
		interval: time.Duration(intervalMin) * time.Minute,
		stopCh:   make(chan struct{}),
	}
	s.sender = func(ctx context.Context, cfg autoPingProviderConfig, conn autoPingConnection) (int, error) {
		return defaultAutoPingSender(ctx, cfg, conn, s.client)
	}
	return s
}

// Start launches the background goroutine (sync.Once).
func (s *AutoPingScheduler) Start(ctx context.Context) {
	s.once.Do(func() {
		s.loadMetrics()
		go s.run(ctx)
	})
}

func (s *AutoPingScheduler) run(ctx context.Context) {
	log.Println("background: auto-ping scheduler started")
	s.check()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.check()
		case <-ctx.Done():
			log.Println("background: auto-ping scheduler stopped")
			return
		case <-s.stopCh:
			log.Println("background: auto-ping scheduler stopped")
			return
		}
	}
}

// Stop signals the scheduler to stop.
func (s *AutoPingScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *AutoPingScheduler) check() {
	now := time.Now().Unix()
	settings := s.loadSettings()
	s.loadMetrics()

	dirty := false
	for providerID, cfg := range autoPingProviderConfigs {
		st, ok := settings[cfg.SettingKey]
		if !ok || !st.Enabled {
			continue
		}
		conns, err := s.activeConnections(providerID)
		if err != nil {
			log.Printf("background: auto-ping failed to load %s connections: %v", providerID, err)
			continue
		}
		for _, conn := range conns {
			if !st.Connections[conn.ID] {
				continue
			}
			if s.pingIfDue(now, cfg, conn) {
				dirty = true
			}
		}
	}

	if dirty {
		s.saveMetrics()
	}
}

// pingIfDue sends a ping when a reset window has drifted forward or when the
// fallback interval has elapsed for providers without quota-cache reset data.
func (s *AutoPingScheduler) pingIfDue(now int64, cfg autoPingProviderConfig, conn autoPingConnection) bool {
	m := s.metrics.provider(cfg.ProviderID)
	if m == nil {
		return false
	}
	cm := m.connMetrics(conn.ID)

	resetAt, resetFound := s.latestReset(conn.ID, cfg.ProviderID)
	if resetFound {
		if cm.LastResetAt == 0 {
			cm.LastResetAt = resetAt
			s.dirty = true
			return false
		}
		if resetAt <= cm.LastResetAt+autoPingResetDriftSec {
			return false
		}
		cm.LastResetAt = resetAt
	}

	fallbackIntervalSec := int64(defaultAutoPingFallbackIntervalMin * 60)
	if !resetFound && cm.LastPingAt > 0 && now-cm.LastPingAt < fallbackIntervalSec {
		return false
	}
	if !resetFound && cm.LastPingAt == 0 {
		cm.LastPingAt = now
		s.dirty = true
		return false
	}

	status, err := s.sender(context.Background(), cfg, conn)
	if err != nil {
		log.Printf("background: auto-ping failed for %s/%s: status=%d err=%v", cfg.ProviderID, conn.ID, status, err)
	} else {
		log.Printf("background: auto-ping sent for %s/%s: status=%d", cfg.ProviderID, conn.ID, status)
	}

	m.Count++
	m.LastPingAt = now
	m.LastConnectionID = conn.ID
	cm.LastPingAt = now
	s.dirty = true
	return true
}

// latestReset returns the newest quota reset timestamp from quota_cache for the
// connection, if available. It tolerates malformed JSON so one bad cache row never
// breaks the scheduler for other connections.
func (s *AutoPingScheduler) latestReset(connectionID, providerTypeID string) (int64, bool) {
	var raw string
	var fetchedAt int64
	err := s.db.QueryRow(`
		SELECT quotas, fetched_at FROM quota_cache
		WHERE connection_id = ? AND provider_type_id = ?
		ORDER BY updated_at DESC LIMIT 1
	`, connectionID, providerTypeID).Scan(&raw, &fetchedAt)
	if err != nil {
		return 0, false
	}
	if raw == "" {
		return 0, false
	}
	if time.Since(time.Unix(fetchedAt, 0)) > autoPingFallbackMaxAgeForResetMin*time.Minute {
		return 0, false
	}

	var items []struct {
		ResetAt string `json:"reset_at"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return 0, false
	}

	var latest int64
	for _, it := range items {
		if it.ResetAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, it.ResetAt)
		if err != nil {
			continue
		}
		u := t.Unix()
		if u > latest {
			latest = u
		}
	}
	if latest == 0 {
		return 0, false
	}
	return latest, true
}

func (s *AutoPingScheduler) activeConnections(providerTypeID string) ([]autoPingConnection, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.provider_type_id, COALESCE(c.auth_type,''), COALESCE(c.api_key,''),
		       COALESCE(c.oauth_token,''), COALESCE(c.provider_specific_data,''),
		       COALESCE(pt.base_url,'')
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.provider_type_id = ? AND c.is_active = 1
	`, providerTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []autoPingConnection
	for rows.Next() {
		var conn autoPingConnection
		if err := rows.Scan(&conn.ID, &conn.ProviderTypeID, &conn.AuthType,
			&conn.APIKey, &conn.AccessToken, &conn.ProviderSpecificData, &conn.BaseURL); err != nil {
			continue
		}
		out = append(out, conn)
	}
	return out, nil
}

func (s *AutoPingScheduler) loadSettings() map[string]autoPingSettings {
	out := make(map[string]autoPingSettings)
	for _, cfg := range autoPingProviderConfigs {
		value := s.settingValue(cfg.SettingKey)
		st := autoPingSettings{Connections: make(map[string]bool)}
		if value != "" {
			_ = json.Unmarshal([]byte(value), &st)
		}
		if st.Connections == nil {
			st.Connections = make(map[string]bool)
		}
		out[cfg.SettingKey] = st
	}
	return out
}

func (s *AutoPingScheduler) settingValue(key string) string {
	var value string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value); err != nil {
		return ""
	}
	return value
}

func (s *AutoPingScheduler) loadMetrics() {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.settingValue(autoPingMetricsSettingKey)
	if value == "" {
		s.metrics = autoPingMetrics{}
		return
	}
	var m autoPingMetrics
	if err := json.Unmarshal([]byte(value), &m); err != nil {
		s.metrics = autoPingMetrics{}
		return
	}
	s.metrics = m
}

func (s *AutoPingScheduler) saveMetrics() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	data, err := json.Marshal(s.metrics)
	if err != nil {
		log.Printf("background: auto-ping failed to marshal metrics: %v", err)
		return
	}
	_, err = s.db.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, autoPingMetricsSettingKey, string(data), time.Now().Unix())
	if err != nil {
		log.Printf("background: auto-ping failed to save metrics: %v", err)
		return
	}
	s.dirty = false
}
