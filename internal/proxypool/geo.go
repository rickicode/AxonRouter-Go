package proxypool

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// GeoEntry is the in-memory egress-geo record for one proxy pool, populated by
// the health-check probe. IPHistory holds the most recent distinct egress IPs
// seen (bounded), and IsUnstable reports whether the pool flaps across ≥2
// distinct egress IPs (typical for serverless relays).
type GeoEntry struct {
	IP           string    `json:"ip"`
	Country      string    `json:"country"`
	Region       string    `json:"region"`
	City         string    `json:"city"`
	Org          string    `json:"org"`
	IsDatacenter bool      `json:"isDatacenter"`
	IPHistory    []string  `json:"ipHistory"`
	IsUnstable   bool      `json:"isUnstable"`
	UpdatedAt    time.Time `json:"updatedAt"`
	LastError    string    `json:"lastError,omitempty"`
}

// geoHistoryCap bounds IPHistory so the cache cannot grow unbounded.
const geoHistoryCap = 8

// GeoCache stores the most recent egress-geo probe result per pool id.
// It is a process-local cache (not persisted) — the authoritative row
// (proxy_ip/country/city/org) already lives in proxy_pools.
type GeoCache struct {
	mu    sync.RWMutex
	byID  map[string]GeoEntry
	order []string // insertion order for stable iteration
}

var (
	geoOnce   sync.Once
	geoCache  *GeoCache
)

// Geo returns the package singleton geo cache.
func Geo() *GeoCache {
	geoOnce.Do(func() {
		geoCache = &GeoCache{byID: map[string]GeoEntry{}}
	})
	return geoCache
}

// NewGeoCache returns a fresh cache (used by tests).
func NewGeoCache() *GeoCache {
	return &GeoCache{byID: map[string]GeoEntry{}}
}

// Record ingests one probe result for a pool, appending to IPHistory when the
// egress IP changed and updating the derived flapping/datacenter flags.
func (g *GeoCache) Record(poolID, ip, country, region, city, org string, at time.Time) {
	if poolID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	entry := g.byID[poolID]
	entry.IP = ip
	entry.Country = country
	entry.Region = region
	entry.City = city
	entry.Org = org
	entry.UpdatedAt = at
	entry.LastError = ""
	if ip != "" {
		// Maintain the history: append the new IP if it differs from the most
		// recent one, dedupe identical consecutive probes, and cap the list.
		if len(entry.IPHistory) == 0 || entry.IPHistory[len(entry.IPHistory)-1] != ip {
			entry.IPHistory = append(entry.IPHistory, ip)
			if len(entry.IPHistory) > geoHistoryCap {
				entry.IPHistory = entry.IPHistory[len(entry.IPHistory)-geoHistoryCap:]
			}
		}
	}
	entry.IsUnstable = distinctIPs(entry.IPHistory) >= 2
	entry.IsDatacenter = isDatacenterOrg(entry.Org)

	if _, exists := g.byID[poolID]; !exists {
		g.order = append(g.order, poolID)
	}
	g.byID[poolID] = entry
}

// RecordError records a probe failure (e.g. all endpoints unreachable) without
// clobbering the last successful geo data.
func (g *GeoCache) RecordError(poolID, errMsg string, at time.Time) {
	if poolID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	entry := g.byID[poolID]
	entry.LastError = errMsg
	entry.UpdatedAt = at
	g.byID[poolID] = entry
}

// Get returns the geo entry for a pool.
func (g *GeoCache) Get(poolID string) (GeoEntry, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	e, ok := g.byID[poolID]
	return e, ok
}

// ClearAll empties the cache (used by tests; harmless in prod).
func (g *GeoCache) ClearAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.byID = map[string]GeoEntry{}
	g.order = g.order[:0]
}

// Snapshot returns a deep copy of all entries keyed by pool id.
func (g *GeoCache) Snapshot() map[string]GeoEntry {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]GeoEntry, len(g.byID))
	for k, v := range g.byID {
		v.IPHistory = append([]string(nil), v.IPHistory...)
		out[k] = v
	}
	return out
}

func distinctIPs(ips []string) int {
	seen := map[string]bool{}
	for _, ip := range ips {
		if ip != "" {
			seen[ip] = true
		}
	}
	return len(seen)
}

// datacenterRe matches org names that identify datacenter / hosting networks.
var datacenterRe = regexp.MustCompile(`(?i)amazon|google|microsoft|azure|digitalocean|hetzner|ovh|linode|vultr|cloudflare|aws|oracle|alibaba|tencent|contabo|scaleway|leaseweb|datacamp|colocation|hosting`)

// isDatacenterOrg reports whether an org string looks like a datacenter/hosting
// network rather than a residential ISP.
func isDatacenterOrg(org string) bool {
	return datacenterRe.MatchString(org)
}

// geoEndpoints is the ordered fallback chain of public egress-geo echo
// endpoints. The probe tries each in turn until one succeeds (non-2xx and
// unparseable responses count as failures). A user-provided GEO_PROBE_URL
// overrides the chain entirely.
var geoEndpoints = []string{
	"https://ifconfig.co/json",
	"https://ipwho.is/",
	"http://ip-api.com/json/",
	"https://ipapi.co/json/",
	"https://ipinfo.io/json",
}

// geoEndpointsFor returns the endpoint chain to use, honoring the optional
// GEO_PROBE_URL override.
func geoEndpointsFor() []string {
	if v := strings.TrimSpace(os.Getenv("GEO_PROBE_URL")); v != "" {
		return []string{v}
	}
	return geoEndpoints
}

// probeGeoOnce tries the endpoint chain over the given transport (the pool's
// proxy or a direct client) and returns the first parseable geo result.
func probeGeoOnce(client *http.Client, endpoints []string) (ip, country, region, city, org string, err error) {
	var lastErr error
	for _, endpoint := range endpoints {
		resp, err := client.Get(endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			lastErr = &geoHTTPError{status: resp.StatusCode, body: string(body)}
			continue
		}
		ip, country, region, city, org, ok := parseGeo(body)
		if !ok || ip == "" {
			lastErr = &geoParseError{body: string(body)}
			continue
		}
		return ip, country, region, city, org, nil
	}
	return "", "", "", "", "", lastErr
}

type geoHTTPError struct {
	status int
	body   string
}

func (e *geoHTTPError) Error() string {
	return "geo endpoint HTTP " + itoa(e.status)
}

type geoParseError struct {
	body string
}

func (e *geoParseError) Error() string {
	return "geo endpoint returned unparseable body"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// parseGeo tolerantly extracts ip/country/region/city/org from the common
// shapes returned by ifconfig.co, ipwho.is, ip-api.com, ipapi.co and ipinfo.io.
// It returns ok=false when the body is not recognizable JSON with an ip field.
func parseGeo(body []byte) (ip, country, region, city, org string, ok bool) {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return "", "", "", "", "", false
	}
	ip = jsonStr(m, "ip", "IPv4", "query")
	if ip == "" {
		return "", "", "", "", "", false
	}

	// Country: most endpoints use "country"; ifconfig.co uses "country" too.
	country = jsonStr(m, "country", "country_name")
	if country == "" {
		country = jsonStr(m, "country_code")
	}
	region = jsonStr(m, "region", "regionName", "state", "province")
	city = jsonStr(m, "city")

	// Org: ipinfo.io uses "org" ("AS15169 Google LLC"), ip-api.com "isp"/"org",
	// ifconfig.co "asn_org"/"org"/"organization_name", ipwho.is "connection.org" / "connection.isp".
	org = jsonStr(m, "org", "organization", "organization_name", "asn_org", "isp", "asn")
	if org == "" {
		if conn, ok := m["connection"].(map[string]any); ok {
			org = jsonStr(conn, "org", "isp", "asn", "organization")
		}
	}
	return ip, country, region, city, org, true
}

// jsonStr returns the first non-empty string value among the given keys.
func jsonStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
