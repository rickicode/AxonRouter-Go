package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCacheTTL  = 5 * time.Minute
	maxResponseBytes = 1 << 20 // 1 MiB
)

var githubLatestURL = "https://api.github.com/repos/rickicode/AxonRouter-Go/releases/latest"
var rawChangelogURL = "https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/CHANGELOG.md"

// userAgent is sent with all outbound version/GitHub requests.
var userAgent = "AxonRouter-Go/" + String()

// ReleaseInfo describes the latest GitHub release.
type ReleaseInfo struct {
	Version     string
	Tag         string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type releaseResponse struct {
	Tag         string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

// CheckLatest fetches the latest release from GitHub and parses it.
func CheckLatest(client *http.Client) (ReleaseInfo, error) {
	return checkLatest(client, githubLatestURL)
}

func checkLatest(client *http.Client, url string) (ReleaseInfo, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rr releaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&rr); err != nil {
		return ReleaseInfo{}, err
	}

	info := ReleaseInfo{
		Version:     strings.TrimPrefix(rr.Tag, "v"),
		Tag:         rr.Tag,
		PublishedAt: rr.PublishedAt,
		HTMLURL:     rr.HTMLURL,
	}
	return info, nil
}

// Checker caches the latest release and refreshes it in the background.
type Checker struct {
	mu sync.RWMutex
	client *http.Client
	url string
	ttl time.Duration
	cached ReleaseInfo
	cachedAt time.Time
	changelog string
	changelogAt time.Time
	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup
}

// NewChecker creates a Checker with the provided HTTP client.
func NewChecker(client *http.Client) *Checker {
	return NewCheckerWithURL(client, githubLatestURL)
}

// NewCheckerWithURL creates a Checker that fetches from a custom URL.
func NewCheckerWithURL(client *http.Client, url string) *Checker {
	if client == nil {
		client = http.DefaultClient
	}
	return &Checker{
		client: client,
		url: url,
		ttl: defaultCacheTTL,
		stop: make(chan struct{}),
	}
}

func (c *Checker) startLoop() {
	c.wg.Add(1)
	go c.loop()
}

func (c *Checker) loop() {
	defer c.wg.Done()
	c.Refresh()
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.Refresh()
		case <-c.stop:
			return
		}
	}
}

// Refresh fetches the latest release synchronously and updates the cache.
// HTTP I/O happens outside of the write lock so readers are never blocked.
func (c *Checker) Refresh() error {
	info, err := checkLatest(c.client, c.url)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.cached = info
	c.cachedAt = time.Now()
	c.mu.Unlock()
	return nil
}

// Changelog returns the project's CHANGELOG markdown, fetched from GitHub and
// cached for the configured TTL. Callers get previously cached content even if
// a background refresh fails, so UI pages never block on transient network issues.
func (c *Checker) Changelog() (string, error) {
	c.mu.RLock()
	if c.changelog != "" && time.Since(c.changelogAt) < c.ttl {
		md := c.changelog
		c.mu.RUnlock()
		return md, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changelog != "" && time.Since(c.changelogAt) < c.ttl {
		return c.changelog, nil
	}

	req, err := http.NewRequest(http.MethodGet, rawChangelogURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("changelog returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", err
	}
	c.changelog = string(body)
	c.changelogAt = time.Now()
	return c.changelog, nil
}

// LatestVersion returns the cached release. It never blocks on network I/O.
func (c *Checker) LatestVersion() (ReleaseInfo, bool) {
	c.startOnce.Do(c.startLoop)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cachedAt.IsZero() {
		return ReleaseInfo{}, false
	}
	return c.cached, true
}

// UpdateAvailable reports whether the cached latest version is newer than the current binary.
func (c *Checker) UpdateAvailable() bool {
	return c.updateAvailableFor(String())
}

// updateAvailableFor is the testable internal implementation of UpdateAvailable.
func (c *Checker) updateAvailableFor(current string) bool {
	c.mu.RLock()
	info := c.cached
	ok := !c.cachedAt.IsZero()
	c.mu.RUnlock()
	if !ok {
		return false
	}
	return versionGreater(info.Version, current)
}

// Stop halts the background refresh goroutine. It is safe to call more than once.
func (c *Checker) Stop() {
	c.startOnce.Do(func() {}) // prevent a future LatestVersion from starting a goroutine
	c.stopOnce.Do(func() {
		close(c.stop)
	})
	c.wg.Wait()
}

func versionGreater(latest, current string) bool {
	a, ok := versionParts(latest)
	if !ok {
		return false
	}
	b, ok := versionParts(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func versionParts(v string) ([3]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		parts = append(parts, "0", "0")
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
