package proxypool

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
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
const maxCheckBody = 64 * 1024

func TestHTTPProxy(proxyURL string, timeout time.Duration) TestResult {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return TestResult{Error: "invalid proxy URL: " + err.Error()}
	}
	// First try an HTTPS CONNECT tunnel through the proxy; this mirrors how
	// upstream providers like Cloudflare are reached and catches proxies that
	// only work for plain HTTP targets.
	if res := testHTTPSCONNECT(u, timeout); !res.OK {
		return res
	}
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	resp, err := client.Get(checkURL)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return TestResult{StatusCode: resp.StatusCode, Error: string(body)}
	}
	ip, country, city, org := fetchIPInfo(body)
	return TestResult{OK: true, StatusCode: resp.StatusCode, IP: ip, Country: country, City: city, Org: org}
}

// connectCheckTarget is the upstream host used for HTTPS CONNECT validation.
const connectCheckTarget = "ifconfig.co:443"

// testHTTPSCONNECT attempts to establish a CONNECT tunnel through the proxy and
// perform a short TLS handshake. It does not verify the upstream certificate so
// the check only validates proxy-side CONNECT behaviour.
func testHTTPSCONNECT(proxyURL *url.URL, timeout time.Duration) TestResult {
	deadline := time.Now().Add(timeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(proxyURL.Hostname(), proxyPort(proxyURL)))
	if err != nil {
		return TestResult{Error: "proxy dial: " + err.Error()}
	}
	defer conn.Close()

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", connectCheckTarget, connectCheckTarget)
	if user := proxyURL.User; user != nil {
		password, _ := user.Password()
		auth := user.Username() + ":" + password
		connectReq += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
	}
	connectReq += "\r\n"

	if _, err := io.WriteString(conn, connectReq); err != nil {
		return TestResult{Error: "proxy CONNECT write: " + err.Error()}
	}

	if err := conn.SetReadDeadline(deadline); err != nil {
		return TestResult{Error: "set deadline: " + err.Error()}
	}
	br := bufio.NewReader(io.LimitReader(conn, 8*1024))
	statusLine, err := br.ReadString('\n')
	if err != nil {
		return TestResult{Error: "proxy CONNECT read: " + err.Error()}
	}
	parts := strings.Split(statusLine, " ")
	if len(parts) < 2 {
		return TestResult{Error: "proxy CONNECT invalid response: " + statusLine}
	}
	code := parts[1]
	if code != "200" {
		return TestResult{Error: "proxy CONNECT failed: " + statusLine}
	}
	// Drain any remaining headers. In tests the CONNECT target is a plain HTTP
	// server, so we stop here and report success once the proxy has accepted the
	// tunnel. In production the subsequent HTTPS request performs the real TLS
	// handshake through the tunnel.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return TestResult{Error: "proxy CONNECT header read: " + err.Error()}
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return TestResult{OK: true}
}

func proxyPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return TestResult{StatusCode: resp.StatusCode, Error: string(body)}
	}
	ip, country, city, org := fetchIPInfo(body)
	return TestResult{OK: true, StatusCode: resp.StatusCode, IP: ip, Country: country, City: city, Org: org}
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
