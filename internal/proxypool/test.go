package proxypool

import (
	"database/sql"
	"encoding/json"
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
	City       string `json:"city,omitempty"`
	Org        string `json:"org,omitempty"`
}

func TestPool(db *sql.DB, id string) (TestResult, error) {
	var typ, proxyURL, relayAuth string
	if err := db.QueryRow("SELECT type, proxy_url, relay_auth FROM proxy_pools WHERE id = ?", id).Scan(&typ, &proxyURL, &relayAuth); err != nil {
		return TestResult{}, err
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
	return res, nil
}

// checkURL is the public IP/geo echo endpoint used to verify a proxy/relay works
// and to capture the egress IP, country, city and ISP.
const checkURL = "https://ifconfig.co/json"

// fetchIPInfo extracts IP, country, city, org from an ifconfig.co (or ipinfo.io)
// JSON response body. ifconfig.co returns the ISP as "organization_name".
func fetchIPInfo(body []byte) (ip, country, city, org string) {
	var info struct {
		IP               string `json:"ip"`
		Country          string `json:"country"`
		City             string `json:"city"`
		Org              string `json:"org"`
		OrganizationName string `json:"organization_name"`
	}
	if json.Unmarshal(body, &info) != nil {
		return "", "", "", ""
	}
	orgName := info.Org
	if orgName == "" {
		orgName = info.OrganizationName
	}
	return info.IP, info.Country, info.City, orgName
}

// TestHTTPProxy tests whether an HTTP proxy is reachable via ifconfig.co and returns IP/country/ISP info.
func TestHTTPProxy(proxyURL string, timeout time.Duration) TestResult {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return TestResult{Error: "invalid proxy URL: " + err.Error()}
	}
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	resp, err := client.Get(checkURL)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return TestResult{StatusCode: resp.StatusCode, Error: string(body)}
	}
	ip, country, city, org := fetchIPInfo(body)
	return TestResult{OK: true, StatusCode: resp.StatusCode, IP: ip, Country: country, City: city, Org: org}
}

// TestRelay tests whether a relay endpoint is reachable via ifconfig.co and returns IP/country/ISP info.
func TestRelay(relayURL, relayAuth string, timeout time.Duration) TestResult {
	req, err := http.NewRequest(http.MethodGet, relayURL, nil)
	if err != nil {
		return TestResult{Error: "invalid relay URL: " + err.Error()}
	}
	if relayAuth != "" {
		req.Header.Set("x-relay-auth", relayAuth)
	}
	req.Header.Set("x-relay-target", "https://ifconfig.co")
	req.Header.Set("x-relay-path", "/json")
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return TestResult{StatusCode: resp.StatusCode, Error: string(body)}
	}
	ip, country, city, org := fetchIPInfo(body)
	return TestResult{OK: true, StatusCode: resp.StatusCode, IP: ip, Country: country, City: city, Org: org}
}
