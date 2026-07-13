package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultCacheTTL = 5 * time.Minute

var githubLatestURL = "https://api.github.com/repos/rickicode/AxonRouter-Go/releases/latest"

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

	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rr releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return ReleaseInfo{}, err
	}

	info := ReleaseInfo{
		Version: strings.TrimPrefix(rr.Tag, "v"),
		Tag: rr.Tag,
		PublishedAt: rr.PublishedAt,
		HTMLURL: rr.HTMLURL,
	}
	return info, nil
}

// Checker caches the latest release for a fixed TTL.
type Checker struct {
	mu sync.Mutex
	client *http.Client
	url string
	cached ReleaseInfo
	cachedAt time.Time
	ttl time.Duration
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
	}
}

// LatestVersion returns the cached release, refreshing if needed.
func (c *Checker) LatestVersion() (ReleaseInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.cachedAt.IsZero() && time.Since(c.cachedAt) <= c.ttl {
		return c.cached, true
	}

	info, err := checkLatest(c.client, c.url)
	if err != nil {
		return ReleaseInfo{}, false
	}

	c.cached = info
	c.cachedAt = time.Now()
	return c.cached, true
}

// UpdateAvailable reports whether the cached latest version is newer than the current binary.
func (c *Checker) UpdateAvailable() bool {
	info, ok := c.LatestVersion()
	if !ok {
		return false
	}
	return versionGreater(info.Version, String())
}

var defaultChecker = NewChecker(http.DefaultClient)

// LatestVersion returns the default cached release info.
func LatestVersion() (ReleaseInfo, bool) {
	return defaultChecker.LatestVersion()
}

// UpdateAvailable reports whether the default cached latest version is newer.
func UpdateAvailable() bool {
	return defaultChecker.UpdateAvailable()
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
