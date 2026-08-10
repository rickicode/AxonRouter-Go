package admin

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rickicode/AxonRouter-Go/internal/version"
)

func TestUpgrade_DownloadsAndVerifiesBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	version.SetTestVersion("0.3.1")
	defer version.ClearTestVersion()

	binary := []byte("fake binary content")
	sum := fmt.Sprintf("%x", sha256.Sum256(binary))
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	t.Setenv("HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
		case "/download/v0.3.2/" + asset:
			w.WriteHeader(http.StatusOK)
			w.Write(binary)
		case "/download/v0.3.2/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", sum, asset)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL+"/latest")
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}
	h := NewUpgradeHandler(checker)
	h.client = server.Client()
	h.baseURL = server.URL + "/download"
	h.binDir = t.TempDir()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
	h.Upgrade(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	if resp["version"] != "0.3.2" {
		t.Errorf("version = %v, want 0.3.2", resp["version"])
	}
	if resp["asset"] != asset {
		t.Errorf("asset = %v, want %s", resp["asset"], asset)
	}

	wantPath := filepath.Join(h.binDir, installedName())
	if resp["path"] != wantPath {
		t.Errorf("path = %v, want %s", resp["path"], wantPath)
	}

	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read downloaded binary: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("downloaded binary = %q, want %q", got, binary)
	}

	wantCmd, wantHint := restartInstructions()
	if resp["restart_command"] != wantCmd {
		t.Errorf("restart_command = %v, want %s", resp["restart_command"], wantCmd)
	}
	if resp["restart_hint"] != wantHint {
		t.Errorf("restart_hint = %v, want %s", resp["restart_hint"], wantHint)
	}

	logs, ok := resp["logs"].([]any)
	if !ok || len(logs) == 0 {
		t.Fatalf("logs = %v, want non-empty []string", resp["logs"])
	}
	wantLogs := []string{
		"Checking latest version...",
		fmt.Sprintf("Downloading %s...", asset),
		"Verifying checksum...",
		"Writing new binary...",
		"Upgrade complete",
	}
	for _, want := range wantLogs {
		found := false
		for _, l := range logs {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected log: %q in %v", want, logs)
		}
	}
	backupLog := fmt.Sprintf("Backing up existing binary to %s.bak...", wantPath)
	for _, l := range logs {
		if l == backupLog {
			t.Errorf("unexpected backup log in fresh install: %q", l)
		}
	}
}

func TestRestartInstructions(t *testing.T) {
	command, hint := restartInstructions()
	if command == "" {
		t.Error("restart command is empty")
	}
	if hint == "" {
		t.Error("restart hint is empty")
	}

	switch runtime.GOOS {
	case "linux":
		if command != "systemctl restart axonrouter" {
			t.Errorf("linux command = %q, want systemctl restart axonrouter", command)
		}
	case "darwin":
		if !strings.Contains(command, "launchctl") {
			t.Errorf("darwin command = %q, want launchctl", command)
		}
	case "windows":
		if command != "sc stop axonrouter && sc start axonrouter" {
			t.Errorf("windows command = %q, want sc stop/start", command)
		}
	}
}

func TestUpgrade_BackupCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	version.SetTestVersion("0.3.1")
	defer version.ClearTestVersion()

	binary := []byte("fake binary content v0.3.2")
	sum := fmt.Sprintf("%x", sha256.Sum256(binary))
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
		case "/download/v0.3.2/" + asset:
			w.WriteHeader(http.StatusOK)
			w.Write(binary)
		case "/download/v0.3.2/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", sum, asset)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL+"/latest")
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}
	h := NewUpgradeHandler(checker)
	h.client = server.Client()
	h.baseURL = server.URL + "/download"
	h.binDir = t.TempDir()

	path := filepath.Join(h.binDir, installedName())
	original := []byte("original binary v0.3.1")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatalf("write existing binary: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
	h.Upgrade(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded binary: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("upgraded binary = %q, want %q", got, binary)
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup binary: %v", err)
	}
	if string(backup) != string(original) {
		t.Errorf("backup binary = %q, want %q", backup, original)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	logs, ok := resp["logs"].([]any)
	if !ok {
		t.Fatalf("logs = %v, want []string", resp["logs"])
	}
	wantBackupLog := fmt.Sprintf("Backing up existing binary to %s.bak...", path)
	found := false
	for _, l := range logs {
		if l == wantBackupLog {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing backup log: %q in %v", wantBackupLog, logs)
	}
}

func TestFindChecksum_MatchesBasenameWithDirectoryPrefix(t *testing.T) {
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	sum := "abcd1234"

	data := []byte(fmt.Sprintf("%s build/%s\n0000 other-file\n", sum, asset))
	got, err := findChecksum(data, asset)
	if err != nil {
		t.Fatalf("findChecksum returned error: %v", err)
	}
	if got != sum {
		t.Errorf("checksum = %q, want %q", got, sum)
	}
}

func TestUpgrade_ConcurrentCallsAreSerialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	version.SetTestVersion("0.3.1")
	defer version.ClearTestVersion()

	binary := []byte("fake binary content")
	sum := fmt.Sprintf("%x", sha256.Sum256(binary))
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
		case "/download/v0.3.2/" + asset:
			w.WriteHeader(http.StatusOK)
			w.Write(binary)
		case "/download/v0.3.2/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s %s\n", sum, asset)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL+"/latest")
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}
	h := NewUpgradeHandler(checker)
	h.client = server.Client()
	h.baseURL = server.URL + "/download"
	h.binDir = t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
			h.Upgrade(c)
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()

	got, err := os.ReadFile(filepath.Join(h.binDir, installedName()))
	if err != nil {
		t.Fatalf("read downloaded binary: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("downloaded binary = %q, want %q", got, binary)
	}
}

func TestUpgrade_ChecksumMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	version.SetTestVersion("0.3.1")
	defer version.ClearTestVersion()

	binary := []byte("fake binary content")
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
		case "/download/v0.3.2/" + asset:
			w.WriteHeader(http.StatusOK)
			w.Write(binary)
		case "/download/v0.3.2/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", asset)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL+"/latest")
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}
	h := NewUpgradeHandler(checker)
	h.client = server.Client()
	h.baseURL = server.URL + "/download"
	h.binDir = t.TempDir()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
	h.Upgrade(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestUpgrade_NoNewerVersionAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	version.SetTestVersion("0.3.3")
	defer version.ClearTestVersion()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL)
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}
	h := NewUpgradeHandler(checker)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
	h.Upgrade(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "no newer version available" {
		t.Errorf("error = %v, want no newer version available", resp["error"])
	}
}

func installedName() string {
	name := "axonrouter"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func TestRestart_UserServiceActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRestartHandler()
	var calls []string
	h.checkActive = func(name string, arg ...string) error {
		calls = append(calls, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		if name == "systemctl" && len(arg) == 3 && arg[0] == "--user" && arg[1] == "is-active" && arg[2] == "axonrouter" {
			return nil
		}
		return errors.New("inactive")
	}
	h.restart = func(name string, arg ...string) error {
		calls = append(calls, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		return nil
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	h.Restart(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	if resp["message"] != "restart initiated" {
		t.Errorf("message = %v, want restart initiated", resp["message"])
	}
	wantCalls := []string{"systemctl --user is-active axonrouter", "systemctl --user restart axonrouter"}
	if len(calls) != 2 || calls[0] != wantCalls[0] || calls[1] != wantCalls[1] {
		t.Errorf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestRestart_SystemServiceActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRestartHandler()
	var calls []string
	h.checkActive = func(name string, arg ...string) error {
		calls = append(calls, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		if name == "systemctl" && len(arg) == 2 && arg[0] == "is-active" && arg[1] == "axonrouter" {
			return nil
		}
		return errors.New("inactive")
	}
	h.restart = func(name string, arg ...string) error {
		calls = append(calls, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		return nil
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	h.Restart(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	wantCalls := []string{"systemctl --user is-active axonrouter", "systemctl is-active axonrouter", "systemctl restart axonrouter"}
	if len(calls) != 3 || calls[2] != wantCalls[2] {
		t.Errorf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestRestart_NotManagedBySystemd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRestartHandler()
	h.checkActive = func(name string, arg ...string) error {
		return errors.New("inactive")
	}
	h.restart = func(name string, arg ...string) error {
		t.Error("restart should not be called")
		return nil
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	h.Restart(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "service not managed by systemd" {
		t.Errorf("error = %v, want service not managed by systemd", resp["error"])
	}
	wantCmd, _ := restartInstructions()
	if resp["restart_command"] != wantCmd {
		t.Errorf("restart_command = %v, want %s", resp["restart_command"], wantCmd)
	}
}

func TestRestart_RestartStartFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRestartHandler()
	h.checkActive = func(name string, arg ...string) error {
		return nil
	}
	h.restart = func(name string, arg ...string) error {
		return errors.New("start error")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	h.Restart(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == nil || !strings.Contains(resp["error"].(string), "start error") {
		t.Errorf("error = %v, want containing start error", resp["error"])
	}
}
