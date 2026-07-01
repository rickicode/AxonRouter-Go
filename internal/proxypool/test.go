package proxypool

import (
	"database/sql"
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
	_, _ = db.Exec("UPDATE proxy_pools SET test_status = ?, last_tested_at = ?, last_error = ?, response_time_ms = ?, updated_at = ? WHERE id = ?", status, res.TestedAt, lastErr, res.ElapsedMs, time.Now().Unix(), id)
	return res, nil
}

func TestHTTPProxy(proxyURL string, timeout time.Duration) TestResult {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return TestResult{Error: "invalid proxy URL: " + err.Error()}
	}
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	resp, err := client.Get("https://google.com/")
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	return TestResult{OK: resp.StatusCode >= 200 && resp.StatusCode < 400, StatusCode: resp.StatusCode}
}

func TestRelay(relayURL, relayAuth string, timeout time.Duration) TestResult {
	req, err := http.NewRequest(http.MethodGet, relayURL, nil)
	if err != nil {
		return TestResult{Error: "invalid relay URL: " + err.Error()}
	}
	if relayAuth != "" {
		req.Header.Set("x-relay-auth", relayAuth)
	}
	req.Header.Set("x-relay-target", "https://api.openai.com")
	req.Header.Set("x-relay-path", "/v1/models")
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	return TestResult{OK: resp.StatusCode >= 200 && resp.StatusCode < 400, StatusCode: resp.StatusCode}
}
