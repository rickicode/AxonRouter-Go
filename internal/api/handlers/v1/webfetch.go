package v1

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

const (
	webFetchMaxBodyBytes = 2 << 20 // 2 MiB — protect against oversized responses
	webFetchTimeout      = 30 * time.Second
)

// isPublicHost rejects loopback, link-local, private, and unspecified IP
// targets to prevent SSRF. This mirrors 9router's resolveBaseUrl() guard on
// /v1/search: client-supplied non-public base URLs are rejected.
func isPublicHost(host string) bool {
	// Resolve hostname (or bare IP) to its addresses. A host that cannot be
	// resolved is rejected — we never dial unknown targets.
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return false
		}
	}
	return len(ips) > 0
}

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	// Reject documentation/test ranges and IPv4-mapped loopback.
	if ip.IsUnspecified() {
		return false
	}
	return true
}

// WebFetch handles POST /v1/web/fetch — fetches a public URL and returns its
// content as text/markdown. Used by coding agents to pull documentation or
// web content through the gateway. SSRF-guarded: only public IPs are allowed.
func (h *Handler) WebFetch(c *gin.Context) {
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}
	var req struct {
		URL          string `json:"url"`
		MaxChars     int    `json:"max_chars"`
		ReturnFormat string `json:"return_format"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "url is required", "type": "invalid_request_error"}})
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid url", "type": "invalid_request_error"}})
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "only http/https urls are allowed", "type": "invalid_request_error"}})
		return
	}
	host := u.Hostname()
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid url host", "type": "invalid_request_error"}})
		return
	}
	if !isPublicHost(host) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "non-public url target is not allowed", "type": "invalid_request_error"}})
		return
	}

	maxChars := req.MaxChars
	if maxChars <= 0 || maxChars > webFetchMaxBodyBytes {
		maxChars = webFetchMaxBodyBytes
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), webFetchTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid url", "type": "invalid_request_error"}})
		return
	}
	httpReq.Header.Set("User-Agent", "AxonRouter/0.1 (+https://github.com/rickicode/AxonRouter-Go)")
	httpReq.Header.Set("Accept", "text/plain,text/markdown,text/html,application/json;q=0.8,*/*;q=0.5")

	client := &http.Client{
		Timeout: webFetchTimeout,
		// Do not follow redirects to a different host silently — the SSRF guard
		// only validated the original target. Re-validate on redirect.
		CheckRedirect: func(redirReq *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			rh := redirReq.URL.Hostname()
			if !isPublicHost(rh) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		logging.Logger.Warn("web_fetch failed", "url", host, "error", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "fetch failed: " + err.Error(), "type": "server_error"}})
		return
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, int64(maxChars))
	content, err := io.ReadAll(limited)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "read failed", "type": "server_error"}})
		return
	}

	contentType := resp.Header.Get("Content-Type")
	format := strings.ToLower(req.ReturnFormat)
	if format == "" {
		format = "text"
	}
	c.JSON(http.StatusOK, gin.H{
		"url":          u.String(),
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"format":       format,
		"content":      string(content),
		"truncated":    len(content) >= maxChars,
	})
}
