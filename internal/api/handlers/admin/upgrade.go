package admin

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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
	logs    []string
}

// NewUpgradeHandler creates a handler that downloads the latest release.
func NewUpgradeHandler(checker *version.Checker) *UpgradeHandler {
	return &UpgradeHandler{
		checker: checker,
		client:  &http.Client{Timeout: upgradeTimeout},
		baseURL: "https://github.com/rickicode/AxonRouter-Go/releases/download",
	}
}

// Upgrade downloads the latest binary for the current platform after
// verifying its SHA256 checksum.
func (h *UpgradeHandler) Upgrade(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logs = nil
	h.logs = append(h.logs, "Checking latest version...")

	info, ok := h.checker.LatestVersion()
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to determine latest version"})
		return
	}
	if !h.checker.UpdateAvailable() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no newer version available"})
		return
	}

	asset := assetName()
	h.logs = append(h.logs, fmt.Sprintf("Downloading %s...", asset))

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

	h.logs = append(h.logs, "Verifying checksum...")

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

	h.logs = append(h.logs, "Writing new binary...")

	path, err := h.writeBinary(asset, binary)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("write binary: %v", err)})
		return
	}

	h.logs = append(h.logs, "Upgrade complete")

	restartCmd, restartHint := restartInstructions()

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"path":            path,
		"version":         info.Version,
		"asset":           asset,
		"restart_command": restartCmd,
		"restart_hint":    restartHint,
		"logs":            h.logs,
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

func (h *UpgradeHandler) upgradeBinaryPath() (string, error) {
	if h.binDir != "" {
		name := "axonrouter"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		return filepath.Join(h.binDir, name), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate running executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}
	return exe, nil
}

// copyFile copies the contents and permissions of src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}

func (h *UpgradeHandler) writeBinary(asset string, binary []byte) (string, error) {
	path, err := h.upgradeBinaryPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	backupPath := path + ".bak"
	targetExisted := false
	if _, err := os.Stat(path); err == nil {
		targetExisted = true
		h.logs = append(h.logs, fmt.Sprintf("Backing up existing binary to %s...", backupPath))
		if err := copyFile(path, backupPath); err != nil {
			return "", fmt.Errorf("backup existing binary: %w", err)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, binary, 0o755); err != nil {
		if targetExisted {
			_ = copyFile(backupPath, path)
		}
		return "", fmt.Errorf("write new binary: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(path)
			err = os.Rename(tmp, path)
		}
		if err != nil {
			_ = os.Remove(tmp)
			if targetExisted {
				if rerr := copyFile(backupPath, path); rerr != nil {
					return "", fmt.Errorf("replace failed and restore failed: %v (original: %w)", rerr, err)
				}
			}
			return "", fmt.Errorf("replace binary: %w", err)
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

// RestartHandler initiates a service restart when AxonRouter is managed by systemd.
type RestartHandler struct {
	checkActive func(name string, arg ...string) error
	restart     func(name string, arg ...string) error
}

// NewRestartHandler creates a handler that restarts the systemd service.
func NewRestartHandler() *RestartHandler {
	return &RestartHandler{
		checkActive: func(name string, arg ...string) error {
			return exec.Command(name, arg...).Run()
		},
		restart: func(name string, arg ...string) error {
			return exec.Command(name, arg...).Start()
		},
	}
}

// Restart checks whether the axonrouter systemd unit is active and, if so,
// starts a non-blocking restart. It prefers a user-scoped unit and falls back
// to a system-scoped unit.
func (h *RestartHandler) Restart(c *gin.Context) {
	userActive := h.checkActive("systemctl", "--user", "is-active", "axonrouter") == nil
	systemActive := false
	if !userActive {
		systemActive = h.checkActive("systemctl", "is-active", "axonrouter") == nil
	}

	if !userActive && !systemActive {
		cmd, _ := restartInstructions()
		c.JSON(http.StatusBadRequest, gin.H{
			"error":           "service not managed by systemd",
			"restart_command": cmd,
		})
		return
	}

	args := []string{"restart", "axonrouter"}
	if userActive {
		args = []string{"--user", "restart", "axonrouter"}
	}

	if err := h.restart("systemctl", args...); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start restart: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "restart initiated",
	})
}
