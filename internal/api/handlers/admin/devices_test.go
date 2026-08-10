package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func newDevicesHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "devices-handler-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func deviceTrackerConfig() config.Config {
	return config.Config{
		DeviceTrackerTTLMs:     30 * 60 * 1000,
		DeviceTrackerMaxPerKey: 1000,
		DeviceTrackerMaxTotal:  10000,
	}
}

func TestDevicesHandler_GetDevices_KeyNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newDevicesHandlerTestDB(t)
	dt := usage.NewDeviceTracker(deviceTrackerConfig())
	h := NewDevicesHandler(database, dt)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/keys/missing/devices", nil)
	c.Params = gin.Params{{Key: "id", Value: "missing"}}
	h.GetDevices(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestDevicesHandler_GetDevices_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newDevicesHandlerTestDB(t)
	if _, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-empty", "hash", "raw-empty", "Empty Key", 60, 1000, 1, time.Now().Unix(), 0); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	dt := usage.NewDeviceTracker(deviceTrackerConfig())
	h := NewDevicesHandler(database, dt)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/keys/key-empty/devices", nil)
	c.Params = gin.Params{{Key: "id", Value: "key-empty"}}
	h.GetDevices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		KeyID   string `json:"keyId"`
		Name    string `json:"name"`
		Count   int    `json:"count"`
		Devices []struct {
			Fingerprint string `json:"fingerprint"`
			IP          string `json:"ip"`
			UserAgent   string `json:"userAgent"`
			LastSeen    int64  `json:"lastSeen"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.KeyID != "key-empty" || resp.Name != "Empty Key" || resp.Count != 0 || len(resp.Devices) != 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestDevicesHandler_GetDevices_WithDevices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newDevicesHandlerTestDB(t)
	if _, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-dev", "hash", "raw-dev", "Tracked Key", 60, 1000, 1, time.Now().Unix(), 0); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	dt := usage.NewDeviceTracker(deviceTrackerConfig())
	headers := http.Header{}
	headers.Set("X-Forwarded-For", "203.0.113.5")
	dt.Track("key-dev", headers, "curl/8.0")

	h := NewDevicesHandler(database, dt)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/keys/key-dev/devices", nil)
	c.Params = gin.Params{{Key: "id", Value: "key-dev"}}
	h.GetDevices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		KeyID   string `json:"keyId"`
		Name    string `json:"name"`
		Count   int    `json:"count"`
		Devices []struct {
			Fingerprint string `json:"fingerprint"`
			IP          string `json:"ip"`
			UserAgent   string `json:"userAgent"`
			LastSeen    int64  `json:"lastSeen"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.KeyID != "key-dev" || resp.Name != "Tracked Key" || resp.Count != 1 || len(resp.Devices) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	dev := resp.Devices[0]
	if !strings.HasSuffix(dev.IP, ".x.x") {
		t.Errorf("ip %q not masked", dev.IP)
	}
	if dev.UserAgent != "curl/8.0" {
		t.Errorf("userAgent = %q, want curl/8.0", dev.UserAgent)
	}
	if len(dev.Fingerprint) == 0 {
		t.Errorf("fingerprint empty")
	}
	if len(dev.Fingerprint) != 12 {
		t.Errorf("fingerprint length = %d, want 12", len(dev.Fingerprint))
	}
	if dev.LastSeen == 0 {
		t.Errorf("lastSeen not set")
	}
}

func headerWithIP(ip string) http.Header {
	h := http.Header{}
	h.Set("X-Forwarded-For", ip)
	return h
}

func TestDevicesHandler_GetDevices_DifferentKeysAreIsolated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newDevicesHandlerTestDB(t)
	if _, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-a", "hash", "raw-a", "Key A", 60, 1000, 1, time.Now().Unix(), 0); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-b", "hash-b", "raw-b", "Key B", 60, 1000, 1, time.Now().Unix(), 0); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	dt := usage.NewDeviceTracker(deviceTrackerConfig())
	dt.Track("key-a", headerWithIP("203.0.113.1"), "client-a")
	dt.Track("key-a", headerWithIP("203.0.113.2"), "client-b")
	dt.Track("key-b", headerWithIP("203.0.113.3"), "client-c")

	h := NewDevicesHandler(database, dt)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/keys/key-a/devices", nil)
	c.Params = gin.Params{{Key: "id", Value: "key-a"}}
	h.GetDevices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Count   int `json:"count"`
		Devices []struct {
			IP string `json:"ip"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 2 || len(resp.Devices) != 2 {
		t.Fatalf("expected 2 devices for key-a, got %d", resp.Count)
	}
}
