package proxypool

import (
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"time"
)

type TestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status"`
	Error      string `json:"error"`
	ElapsedMs  int64  `json:"elapsedMs"`
	TestedAt   string `json:"testedAt"`
	IP         string `json:"ip,omitempty"`
	Country    string `json:"country,omitempty"`
	Region     string `json:"region,omitempty"`
	City       string `json:"city,omitempty"`
	Org        string `json:"org,omitempty"`
}

func TestPool(db *sql.DB, id string) (TestResult, error) {
	return testPool(db, id, false)
}

// TestPoolWithGeo tests a pool and records the egress geo in the GeoCache.
// The geo result includes IP history (flapping detection) and a datacenter
// classification. The probe falls back across multiple geo endpoints.
func TestPoolWithGeo(db *sql.DB, id string) (TestResult, error) {
	return testPool(db, id, true)
}

func testPool(db *sql.DB, id string, recordGeo bool) (TestResult, error) {
	var typ, proxyURL, proxyUsername, proxyPassword, relayAuth string
	if err := db.QueryRow("SELECT type, proxy_url, COALESCE(proxy_username,''), COALESCE(proxy_password,''), relay_auth FROM proxy_pools WHERE id = ?", id).Scan(&typ, &proxyURL, &proxyUsername, &proxyPassword, &relayAuth); err != nil {
		return TestResult{}, err
	}
	// Reconstruct URL with credentials for testing (stored URL has them stripped)
	if proxyUsername != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			if proxyPassword != "" {
				u.User = url.UserPassword(proxyUsername, proxyPassword)
			} else {
				u.User = url.User(proxyUsername)
			}
			proxyURL = u.String()
		}
	}
	start := time.Now()
	var res TestResult
	if typ == TypeHTTP || typ == "" {
		res = TestHTTPProxy(proxyURL, 8*time.Second)
	} else {
		res = TestRelay(proxyURL, relayAuth, 30*time.Second)
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	res.TestedAt = time.Now().Format(time.RFC3339)

	status := "active"
	var lastErr any = nil
	if !res.OK {
		status = "error"
		lastErr = res.Error
	}
	_, _ = db.Exec("UPDATE proxy_pools SET test_status = ?, last_tested_at = ?, last_error = ?, response_time_ms = ?, proxy_ip = ?, proxy_country = ?, proxy_city = ?, proxy_org = ?, updated_at = ? WHERE id = ?",
		status, res.TestedAt, lastErr, res.ElapsedMs, res.IP, res.Country, res.City, res.Org, time.Now().Unix(), id)

	// Optionally record egress geo in the GeoCache for fitness/flapping UI.
	if recordGeo {
		if res.OK {
			Geo().Record(id, res.IP, res.Country, "", res.City, res.Org, time.Now())
		} else {
			Geo().RecordError(id, res.Error, time.Now())
		}
	}

	return res, nil
}

// TestHTTPProxy tests whether an HTTP proxy is reachable via the geo endpoint
// chain and returns IP/country/ISP info.
func TestHTTPProxy(proxyURL string, timeout time.Duration) TestResult {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return TestResult{Error: "invalid proxy URL: " + err.Error()}
	}
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	ip, country, region, city, org, err := probeGeoOnce(client, geoEndpointsFor())
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	return TestResult{OK: true, StatusCode: http.StatusOK, IP: ip, Country: country, City: city, Org: org, Region: region}
}

// TestRelay tests whether a relay endpoint is reachable via the geo endpoint
// chain and returns IP/country/ISP info. The relay's target is overridden per
// endpoint in the chain; the first endpoint is used as the default target.
func TestRelay(relayURL, relayAuth string, timeout time.Duration) TestResult {
	endpoints := geoEndpointsFor()
	// Relay endpoints proxy an HTTP fetch to a target; iterate the chain by
	// pointing the relay at each endpoint until one yields parseable geo.
	var lastErr error
	for _, endpoint := range endpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodGet, relayURL, nil)
		if err != nil {
			return TestResult{Error: "invalid relay URL: " + err.Error()}
		}
		if relayAuth != "" {
			req.Header.Set("x-relay-auth", relayAuth)
		}
		req.Header.Set("x-relay-target", u.Scheme+"://"+u.Host)
		req.Header.Set("x-relay-path", u.RequestURI())
		resp, err := (&http.Client{Timeout: timeout}).Do(req)
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
		return TestResult{OK: true, StatusCode: resp.StatusCode, IP: ip, Country: country, City: city, Org: org, Region: region}
	}
	return TestResult{Error: lastErr.Error()}
}

// TestProxy dispatches to the appropriate health check for the proxy type.
func TestProxy(proxyURL, typ, relayAuth string) TestResult {
	if typ == TypeHTTP || typ == "" {
		return TestHTTPProxy(proxyURL, 8*time.Second)
	}
	return TestRelay(proxyURL, relayAuth, 30*time.Second)
}

// Healthy reports whether a test result is good enough to persist.
// A result must have succeeded and must not exceed maxMs when maxMs > 0.
func Healthy(res TestResult, maxMs int64) bool {
	if !res.OK {
		return false
	}
	if maxMs > 0 && res.ElapsedMs > maxMs {
		return false
	}
	return true
}
