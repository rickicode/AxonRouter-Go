package headroom

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// ProcessManager is a lifecycle manager stub for future external binary support.
type ProcessManager struct {
	path    string
	args    []string
	cmd     *exec.Cmd
	running bool
}

// NewProcessManager creates a process manager for an external headroom binary.
func NewProcessManager(path string, args ...string) *ProcessManager {
	if path == "" {
		path = "headroom"
	}
	return &ProcessManager{path: path, args: args}
}

// Start launches the external process. It currently returns an error indicating
// external binary support is planned but not yet implemented.
func (p *ProcessManager) Start(ctx context.Context) error {
	if p == nil {
		return errors.New("headroom: nil process manager")
	}
	if p.running {
		return nil
	}
	_ = ctx
	return fmt.Errorf("headroom: external binary support not yet implemented (would run %s %v)", p.path, p.args)
}

// Stop terminates the external process.
func (p *ProcessManager) Stop(ctx context.Context) error {
	if p == nil || !p.running || p.cmd == nil {
		return nil
	}
	_ = ctx
	return p.cmd.Process.Kill()
}

// Running reports whether the process is currently running.
func (p *ProcessManager) Running() bool {
	return p != nil && p.running
}
