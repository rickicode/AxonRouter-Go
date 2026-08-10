//go:build windows

package executor

import (
	"os/exec"
	"strconv"
	"time"
)

func setupProcessGroup(cmd *exec.Cmd) {
	// Windows has no Unix-style process groups. We rely on taskkill /T to
	// terminate the process tree when cancelling a CLI invocation.
}

func terminateProcessGroup(pid int, gracefulShutdown time.Duration) {
	if gracefulShutdown > 0 {
		// Generic Windows CLI tools don't have a common graceful signal.
		time.Sleep(gracefulShutdown)
	}
	// Kill the process and its children.
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}
