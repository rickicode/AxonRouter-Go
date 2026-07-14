// Package service wraps github.com/kardianos/service for AxonRouter-Go.
package service

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	kardianos "github.com/kardianos/service"
)

const (
	Name        = "axonrouter"
	DisplayName = "AxonRouter-Go"
	Description = "Universal API proxy for coding agents"
)

// Program implements kardianos/service.Interface. It wraps the long-running
// server function and its shutdown callback so the binary can run as a service
// on Linux, macOS, and Windows.
type Program struct {
	Run      func() error
	Shutdown func() error
}

// Start launches the server in the background and returns immediately.
func (p *Program) Start(s kardianos.Service) error {
	go func() {
		if err := p.Run(); err != nil {
			log.Fatalf("service run failed: %v", err)
		}
	}()
	return nil
}

// Stop cleans up resources before the service manager terminates the process.
func (p *Program) Stop(s kardianos.Service) error {
	if p.Shutdown != nil {
		return p.Shutdown()
	}
	return nil
}

// ServiceConfig returns a service configuration for the current binary.
// It resolves the executable path and chooses the invoking user so installed
// services run with the same privileges and data directory as an interactive
// start. When root is true the service is configured to run as the root/system
// account instead of the invoking user.
func ServiceConfig(root bool) (*kardianos.Config, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}

	svcUser := "root"
	if !root {
		svcUser = os.Getenv("SUDO_USER")
		if svcUser == "" {
			svcUser = os.Getenv("DOAS_USER")
		}
		if svcUser == "" {
			if u, err := user.Current(); err == nil {
				svcUser = u.Username
			}
		}
		if svcUser == "" {
			svcUser = "root"
		}
	}

	cfg := &kardianos.Config{
		Name:        Name,
		DisplayName: DisplayName,
		Description: Description,
		UserName:    svcUser,
		Executable:  execPath,
	}

	if runtime.GOOS == "windows" {
		// On Windows the working directory defaults to the service root;
		// leave it empty so the data directory is resolved relative to the
		// service profile.
		return cfg, nil
	}

	homeDir := ""
	if u, err := user.Lookup(svcUser); err == nil {
		homeDir = u.HomeDir
	}
	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
	}
	if homeDir == "" {
		homeDir = "/"
	}
	cfg.WorkingDirectory = homeDir
	// Ensure HOME resolves to the target user so the data directory stays in
	// the original user's home under sudo, but uses root's home for install-root.
	if cfg.EnvVars == nil {
		cfg.EnvVars = make(map[string]string)
	}
	cfg.EnvVars["HOME"] = homeDir

	return cfg, nil
}

// ControlAction returns the kardianos service action for a user-supplied
// action. It maps "uninstall" to the library's removal action and returns an
// error for unsupported actions.
func ControlAction(action string) (string, error) {
	switch action {
	case "install", "start", "stop", "restart", "uninstall":
		return action, nil
	case "remove":
		return "uninstall", nil
	}
	return "", fmt.Errorf("unsupported service action: %s", action)
}
