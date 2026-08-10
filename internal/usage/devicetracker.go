package usage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/config"
)

const (
	cleanupInterval        = 60 * time.Second
	maxStoredUALength      = 256
	fingerprintDisplayLen  = 12
)

// deviceRecord stores a masked IP, a truncated user agent, and a fingerprint.
// It never stores a raw client IP.
type deviceRecord struct {
	Fingerprint string
	IP          string
	UserAgent   string
	LastSeen    time.Time
}

// DeviceDetail is the public view of a tracked device.
type DeviceDetail struct {
	Fingerprint string
	IP          string
	UserAgent   string
	LastSeen    time.Time
}

// DeviceTracker tracks unique client devices per API key in memory.
type DeviceTracker struct {
	mu       sync.RWMutex
	devices  map[string]map[string]deviceRecord
	cfg      config.Config
	ticker   *time.Ticker
	stopCh   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewDeviceTracker creates a new in-memory device tracker.
func NewDeviceTracker(cfg config.Config) *DeviceTracker {
	return &DeviceTracker{
		devices: make(map[string]map[string]deviceRecord),
		cfg:     cfg,
	}
}

// Track records a device for the given API key from request headers and user agent.
// It returns the full fingerprint, or an empty string when apiKeyID is empty.
func (dt *DeviceTracker) Track(apiKeyID string, headers http.Header, userAgent string) string {
	if apiKeyID == "" {
		return ""
	}

	now := time.Now()
	dt.expireDevices(now)

	ip := extractIP(headers)
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		ua = "unknown"
	}

	fp := fingerprint(ip, ua)

	dt.mu.Lock()
	defer dt.mu.Unlock()

	devices, ok := dt.devices[apiKeyID]
	if !ok {
		devices = make(map[string]deviceRecord)
		dt.devices[apiKeyID] = devices
	}

	if rec, ok := devices[fp]; ok {
		rec.LastSeen = now
		devices[fp] = rec
		return fp
	}

	dt.enforceLimits(apiKeyID)

	// enforceLimits may delete the devices map if it evicted everything.
	devices, ok = dt.devices[apiKeyID]
	if !ok {
		devices = make(map[string]deviceRecord)
		dt.devices[apiKeyID] = devices
	}

	devices[fp] = deviceRecord{
		Fingerprint: fp,
		IP:          maskIP(ip),
		UserAgent:   truncateUA(ua),
		LastSeen:    now,
	}
	return fp
}

// GetDeviceCount returns the number of distinct devices currently tracked for an API key.
func (dt *DeviceTracker) GetDeviceCount(apiKeyID string) int {
	dt.expireDevices(time.Now())
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return len(dt.devices[apiKeyID])
}

// GetDeviceDetails returns the device records for an API key, newest first.
func (dt *DeviceTracker) GetDeviceDetails(apiKeyID string) []DeviceDetail {
	dt.expireDevices(time.Now())

	dt.mu.RLock()
	devices := dt.devices[apiKeyID]
	dt.mu.RUnlock()

	out := make([]DeviceDetail, 0, len(devices))
	for _, rec := range devices {
		out = append(out, DeviceDetail{
			Fingerprint: truncateFingerprint(rec.Fingerprint),
			IP:          rec.IP,
			UserAgent:   rec.UserAgent,
			LastSeen:    rec.LastSeen,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// StartCleanup starts a background goroutine that periodically removes expired records.
func (dt *DeviceTracker) StartCleanup() {
	dt.startOnce.Do(func() {
		dt.ticker = time.NewTicker(cleanupInterval)
		dt.stopCh = make(chan struct{})
		go dt.cleanupLoop()
	})
}

// Stop stops the background cleanup goroutine.
func (dt *DeviceTracker) Stop() {
	dt.stopOnce.Do(func() {
		if dt.ticker != nil {
			dt.ticker.Stop()
			close(dt.stopCh)
		}
	})
}

func (dt *DeviceTracker) cleanupLoop() {
	for {
		select {
		case <-dt.ticker.C:
			dt.expireDevices(time.Now())
		case <-dt.stopCh:
			return
		}
	}
}

func (dt *DeviceTracker) enforceLimits(apiKeyID string) {
	for {
		if len(dt.devices[apiKeyID]) < dt.cfg.DeviceTrackerMaxPerKey {
			break
		}
		if !dt.deleteOldest(apiKeyID) {
			break
		}
	}

	for {
		if dt.totalCount() < dt.cfg.DeviceTrackerMaxTotal {
			break
		}
		if !dt.deleteOldest("") {
			break
		}
	}
}

func (dt *DeviceTracker) totalCount() int {
	count := 0
	for _, devices := range dt.devices {
		count += len(devices)
	}
	return count
}

func (dt *DeviceTracker) deleteOldest(apiKeyID string) bool {
	var oldestKey string
	var oldestFP string
	var oldestSeen time.Time

	keys := apiKeyID
	if keys == "" {
		for k := range dt.devices {
			for fp, rec := range dt.devices[k] {
				if oldestFP == "" || rec.LastSeen.Before(oldestSeen) {
					oldestKey = k
					oldestFP = fp
					oldestSeen = rec.LastSeen
				}
			}
		}
	} else {
		for fp, rec := range dt.devices[apiKeyID] {
			if oldestFP == "" || rec.LastSeen.Before(oldestSeen) {
				oldestKey = apiKeyID
				oldestFP = fp
				oldestSeen = rec.LastSeen
			}
		}
	}

	if oldestFP == "" {
		return false
	}

	delete(dt.devices[oldestKey], oldestFP)
	if len(dt.devices[oldestKey]) == 0 {
		delete(dt.devices, oldestKey)
	}
	return true
}

func (dt *DeviceTracker) expireDevices(now time.Time) {
	ttl := time.Duration(dt.cfg.DeviceTrackerTTLMs) * time.Millisecond

	dt.mu.Lock()
	defer dt.mu.Unlock()

	for apiKeyID, devices := range dt.devices {
		for fp, rec := range devices {
			if now.Sub(rec.LastSeen) > ttl {
				delete(devices, fp)
			}
		}
		if len(devices) == 0 {
			delete(dt.devices, apiKeyID)
		}
	}
}

// extractIP returns the client IP from request headers following the priority
// cf-connecting-ip, x-real-ip, fastly-client-ip, then the first hop of x-forwarded-for.
func extractIP(headers http.Header) string {
	if headers == nil {
		return "unknown"
	}
	for _, key := range []string{"CF-Connecting-IP", "X-Real-IP", "Fastly-Client-IP"} {
		if v := headers.Get(key); v != "" {
			return strings.TrimSpace(v)
		}
	}
	if v := headers.Get("X-Forwarded-For"); v != "" {
		parts := strings.SplitN(v, ",", 2)
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	return "unknown"
}

// maskIP masks an IP address so the stored/reported value never reveals the full address.
func maskIP(ip string) string {
	if ip == "" || ip == "unknown" {
		return "unknown"
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "masked"
	}
	if ipv4 := parsed.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.x.x", ipv4[0], ipv4[1])
	}

	groups := []string{
		fmt.Sprintf("%x", binary.BigEndian.Uint16(parsed[0:2])),
		fmt.Sprintf("%x", binary.BigEndian.Uint16(parsed[2:4])),
		fmt.Sprintf("%x", binary.BigEndian.Uint16(parsed[4:6])),
		fmt.Sprintf("%x", binary.BigEndian.Uint16(parsed[6:8])),
	}
	return strings.Join(groups, ":") + ":..."
}

func fingerprint(ip, userAgent string) string {
	h := sha256.Sum256([]byte(ip + "|" + userAgent))
	return hex.EncodeToString(h[:])
}

func truncateFingerprint(fp string) string {
	if len(fp) <= fingerprintDisplayLen {
		return fp
	}
	return fp[:fingerprintDisplayLen]
}

func truncateUA(ua string) string {
	if len(ua) <= maxStoredUALength {
		return ua
	}
	return ua[:maxStoredUALength] + "..."
}
