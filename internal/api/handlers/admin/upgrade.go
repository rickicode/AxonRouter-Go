package admin

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/version"
)

const (
	upgradeTimeout   = 60 * time.Second
	maxDownloadBytes = 128 << 20 // 128 MiB
)

// UpgradeHandler downloads a newer binary release for the current platform.
type UpgradeHandler struct {
	checker *version.Checker
	client  *http.Client
	baseURL string
	binDir  string
	mu      sync.Mutex
}

// NewUpgradeHandler creates a handler that downloads the latest release.
func NewUpgradeHandler(checker *version.Checker) *UpgradeHandler {
	return &UpgradeHandler{
		checker: checker,
		client:  &http.Client{Timeout: upgradeTimeout},
		baseURL: "https://github.com/rickicode/AxonRouter-Go/releases/download",
		binDir:  filepath.Join(config.Get().DataDir, "bin"),
	}
}

// Upgrade downloads the latest binary for the current platform after
// verifying its SHA256 checksum.
func (h *UpgradeHandler) Upgrade(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	info, ok := h.checker.LatestVersion()
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to determine latest version"})
		return
	}

	asset := assetName()
	assetURL := fmt.Sprintf("%s/%s/%s", h.baseURL, info.Tag, asset)
	checksumURL := fmt.Sprintf("%s/%s/checksums.txt", h.baseURL, info.Tag)

	binary, err := h.download(assetURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("download binary: %v", err)})
		return
	}

	checksums, err := h.download(checksumURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("download checksums: %v", err)})
		return
	}

	expected, err := findChecksum(checksums, asset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("checksum lookup: %v", err)})
		return
	}

	got := fmt.Sprintf("%x", sha256.Sum256(binary))
	if !strings.EqualFold(got, expected) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "checksum mismatch"})
		return
	}

	path, err := h.writeBinary(asset, binary)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("write binary: %v", err)})
		return
	}

	restartCmd, restartHint := restartInstructions()

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"path":            path,
		"version":         info.Version,
		"asset":           asset,
		"restart_command": restartCmd,
		"restart_hint":    restartHint,
	})
}

func restartInstructions() (command, hint string) {
	hint = "Run this command to restart the service and start using the new binary."
	switch runtime.GOOS {
	case "linux":
		return "systemctl restart axonrouter", hint
	case "darwin":
		return "launchctl unload ~/Library/LaunchAgents/axonrouter.plist && launchctl load ~/Library/LaunchAgents/axonrouter.plist", hint
	case "windows":
		return "sc stop axonrouter && sc start axonrouter", hint
	default:
		return "restart the 'axonrouter' service", "Restart the 'axonrouter' service to start using the new binary."
	}
}

func (h *UpgradeHandler) download(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AxonRouter-Go/"+version.String())

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes))
}

func (h *UpgradeHandler) writeBinary(asset string, binary []byte) (string, error) {
	if err := os.MkdirAll(h.binDir, 0o755); err != nil {
		return "", err
	}
	// Always install as the canonical service binary name so the upgrade
	// replaces the executable the service manager already points to.
	targetName := "axonrouter"
	if runtime.GOOS == "windows" {
		targetName += ".exe"
	}
	path := filepath.Join(h.binDir, targetName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, binary, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		if runtime.GOOS != "windows" {
			return "", err
		}
		// Windows Rename does not overwrite an existing file.
		_ = os.Remove(path)
		if err := os.Rename(tmp, path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func assetName() string {
	name := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func findChecksum(data []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hash, filename, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		filename = strings.TrimSpace(filename)
		filename = strings.TrimPrefix(filename, "*")
		if filepath.Base(filename) == asset {
			return strings.TrimSpace(hash), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", asset)
}
