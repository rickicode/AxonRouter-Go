package usage

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		DeviceTrackerTTLMs:     30 * 60 * 1000,
		DeviceTrackerMaxPerKey: 1000,
		DeviceTrackerMaxTotal:  10000,
	}
}

func TestTrack_SameIPAndUACountsAsOneDevice(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	ua := "Mozilla/5.0 test-agent"
	dt.Track("key-1", headerWithIP("203.0.113.5"), ua)
	dt.Track("key-1", headerWithIP("203.0.113.5"), ua)
	dt.Track("key-1", headerWithIP("203.0.113.5"), ua)

	if got := dt.GetDeviceCount("key-1"); got != 1 {
		t.Errorf("GetDeviceCount = %d, want 1", got)
	}
}

func TestTrack_DifferentUserAgentCountsAsNewDevice(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")
	dt.Track("key-1", headerWithIP("203.0.113.5"), "python-requests/2.31")

	if got := dt.GetDeviceCount("key-1"); got != 2 {
		t.Errorf("GetDeviceCount = %d, want 2", got)
	}
}

func TestTrack_DifferentIPCountsAsNewDevice(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")
	dt.Track("key-1", headerWithIP("198.51.100.9"), "curl/8.0")

	if got := dt.GetDeviceCount("key-1"); got != 2 {
		t.Errorf("GetDeviceCount = %d, want 2", got)
	}
}

func TestTrack_RepeatRefreshesLastSeen(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")
	before := dt.GetDeviceDetails("key-1")[0].LastSeen

	time.Sleep(5 * time.Millisecond)
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")
	after := dt.GetDeviceDetails("key-1")[0].LastSeen

	if dt.GetDeviceCount("key-1") != 1 {
		t.Errorf("GetDeviceCount = %d, want 1", dt.GetDeviceCount("key-1"))
	}
	if !after.After(before) {
		t.Errorf("lastSeen did not refresh: before=%v after=%v", before, after)
	}
}

func TestTrack_NoOpWhenKeyMissing(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	fp := dt.Track("", headerWithIP("203.0.113.5"), "curl/8.0")
	if fp != "" {
		t.Errorf("Track returned %q, want empty", fp)
	}
	if dt.GetDeviceCount("") != 0 {
		t.Errorf("GetDeviceCount(\"\") = %d, want 0", dt.GetDeviceCount(""))
	}
}

func TestTrack_DevicesScopedPerAPIKey(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")
	dt.Track("key-2", headerWithIP("203.0.113.5"), "curl/8.0")

	if got := dt.GetDeviceCount("key-1"); got != 1 {
		t.Errorf("key-1 count = %d, want 1", got)
	}
	if got := dt.GetDeviceCount("key-2"); got != 1 {
		t.Errorf("key-2 count = %d, want 1", got)
	}
}

func TestTrack_ExpiresRecordsPastTTL(t *testing.T) {
	cfg := testConfig()
	cfg.DeviceTrackerTTLMs = 50
	dt := NewDeviceTracker(cfg)
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")

	time.Sleep(60 * time.Millisecond)
	if got := dt.GetDeviceCount("key-1"); got != 0 {
		t.Errorf("GetDeviceCount after TTL = %d, want 0", got)
	}
}

func TestTrack_KeepsRecordsWithinTTL(t *testing.T) {
	cfg := testConfig()
	cfg.DeviceTrackerTTLMs = 60000
	dt := NewDeviceTracker(cfg)
	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")

	if got := dt.GetDeviceCount("key-1"); got != 1 {
		t.Errorf("GetDeviceCount within TTL = %d, want 1", got)
	}
}

func TestTrack_EnforcesMaxPerKeyByEvictingOldest(t *testing.T) {
	cfg := testConfig()
	cfg.DeviceTrackerMaxPerKey = 2
	dt := NewDeviceTracker(cfg)

	dt.Track("key-1", headerWithIP("203.0.113.1"), "ua-1")
	time.Sleep(2 * time.Millisecond)
	dt.Track("key-1", headerWithIP("203.0.113.2"), "ua-2")
	time.Sleep(2 * time.Millisecond)
	dt.Track("key-1", headerWithIP("203.0.113.3"), "ua-3")

	if got := dt.GetDeviceCount("key-1"); got != 2 {
		t.Errorf("GetDeviceCount = %d, want 2", got)
	}
	uas := make(map[string]bool)
	for _, d := range dt.GetDeviceDetails("key-1") {
		uas[d.UserAgent] = true
	}
	if uas["ua-1"] {
		t.Error("oldest device (ua-1) should have been evicted")
	}
	if !uas["ua-2"] || !uas["ua-3"] {
		t.Errorf("expected ua-2 and ua-3 to remain, got %v", uas)
	}
}

func TestTrack_EnforcesMaxTotalAcrossKeys(t *testing.T) {
	cfg := testConfig()
	cfg.DeviceTrackerMaxTotal = 2
	dt := NewDeviceTracker(cfg)

	dt.Track("key-1", headerWithIP("203.0.113.1"), "ua-1")
	time.Sleep(2 * time.Millisecond)
	dt.Track("key-2", headerWithIP("203.0.113.2"), "ua-2")
	time.Sleep(2 * time.Millisecond)
	dt.Track("key-3", headerWithIP("203.0.113.3"), "ua-3")

	if dt.GetDeviceCount("key-1") != 0 {
		t.Errorf("key-1 count = %d, want 0 (oldest evicted)", dt.GetDeviceCount("key-1"))
	}
	if dt.GetDeviceCount("key-2") != 1 {
		t.Errorf("key-2 count = %d, want 1", dt.GetDeviceCount("key-2"))
	}
	if dt.GetDeviceCount("key-3") != 1 {
		t.Errorf("key-3 count = %d, want 1", dt.GetDeviceCount("key-3"))
	}
}

func TestMaskIP_MasksIPv4LastTwoOctets(t *testing.T) {
	if got := maskIP("203.0.113.42"); got != "203.0.x.x" {
		t.Errorf("maskIP(IPv4) = %q, want %q", got, "203.0.x.x")
	}
}

func TestMaskIP_MasksIPv6ToFirst64Bits(t *testing.T) {
	if got := maskIP("2001:db8:85a3:0:0:8a2e:370:7334"); got != "2001:db8:85a3:0:..." {
		t.Errorf("maskIP(IPv6) = %q, want %q", got, "2001:db8:85a3:0:...")
	}
}

func TestMaskIP_UnknownInput(t *testing.T) {
	cases := []string{"", "unknown"}
	for _, c := range cases {
		if got := maskIP(c); got != "unknown" {
			t.Errorf("maskIP(%q) = %q, want unknown", c, got)
		}
	}
}

func TestTrack_StoresMaskedIPNotRawIP(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	dt.Track("key-1", headerWithIP("203.0.113.42"), "curl/8.0")

	details := dt.GetDeviceDetails("key-1")
	if len(details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(details))
	}
	if details[0].IP != "203.0.x.x" {
		t.Errorf("stored IP = %q, want masked 203.0.x.x", details[0].IP)
	}
	if details[0].IP == "203.0.113.42" {
		t.Error("stored IP equals raw IP")
	}
}

func TestGetDeviceDetails_TruncatesFingerprint(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	dt.Track("key-1", headerWithIP("203.0.113.42"), "curl/8.0")

	details := dt.GetDeviceDetails("key-1")
	if len(details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(details))
	}
	if got := len(details[0].Fingerprint); got != 12 {
		t.Errorf("fingerprint length = %d, want 12", got)
	}
}

func TestExtractIP_PrefersCFConnectingIP(t *testing.T) {
	h := make(http.Header)
	h.Set("CF-Connecting-IP", "203.0.113.5")
	h.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")
	if got := extractIP(h); got != "203.0.113.5" {
		t.Errorf("extractIP = %q, want 203.0.113.5", got)
	}
}

func TestExtractIP_FallsBackToFirstXForwardedFor(t *testing.T) {
	h := make(http.Header)
	h.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")
	if got := extractIP(h); got != "198.51.100.9" {
		t.Errorf("extractIP = %q, want 198.51.100.9", got)
	}
}

func TestExtractIP_FallsBackToXRealIP(t *testing.T) {
	h := make(http.Header)
	h.Set("X-Real-IP", "192.0.2.7")
	if got := extractIP(h); got != "192.0.2.7" {
		t.Errorf("extractIP = %q, want 192.0.2.7", got)
	}
}

func TestExtractIP_FastlyClientIPPriorityOverXForwardedFor(t *testing.T) {
	h := make(http.Header)
	h.Set("Fastly-Client-IP", "10.0.0.5")
	h.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")
	if got := extractIP(h); got != "10.0.0.5" {
		t.Errorf("extractIP = %q, want 10.0.0.5", got)
	}
}

func TestExtractIP_ReturnsUnknownWhenNoHeader(t *testing.T) {
	if got := extractIP(http.Header{}); got != "unknown" {
		t.Errorf("extractIP = %q, want unknown", got)
	}
}

func TestTracker_TruncatesLongUserAgent(t *testing.T) {
	dt := NewDeviceTracker(testConfig())
	longUA := strings.Repeat("a", 300)
	dt.Track("key-1", headerWithIP("203.0.113.5"), longUA)

	details := dt.GetDeviceDetails("key-1")
	if len(details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(details))
	}
	if details[0].UserAgent == longUA {
		t.Error("long user agent was not truncated")
	}
	if got := len(details[0].UserAgent); got != 259 {
		t.Errorf("truncated UA length = %d, want 259", got)
	}
}

func TestTracker_CleanupTimerRemovesExpiredRecords(t *testing.T) {
	cfg := testConfig()
	cfg.DeviceTrackerTTLMs = 1
	dt := NewDeviceTracker(cfg)
	dt.StartCleanup()
	defer dt.Stop()

	dt.Track("key-1", headerWithIP("203.0.113.5"), "curl/8.0")
	if dt.GetDeviceCount("key-1") != 1 {
		t.Fatalf("expected 1 device before cleanup")
	}

	time.Sleep(20 * time.Millisecond)
	if dt.GetDeviceCount("key-1") != 0 {
		t.Errorf("GetDeviceCount after cleanup = %d, want 0", dt.GetDeviceCount("key-1"))
	}
}

func headerWithIP(ip string) http.Header {
	h := make(http.Header)
	h.Set("X-Forwarded-For", ip)
	return h
}
