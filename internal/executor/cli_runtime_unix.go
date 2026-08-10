//go:build !windows

package executor

import (
	"os/exec"
	"syscall"
	"time"
)

func setupProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func terminateProcessGroup(pid int, gracefulShutdown time.Duration) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	if gracefulShutdown > 0 {
		time.Sleep(gracefulShutdown)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
