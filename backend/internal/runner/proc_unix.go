//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// applyProcessAttrs puts the child in its own process group so
// a single SIGTERM on the group id reaches every descendant.
func applyProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree sends SIGTERM to the process group, then SIGKILL after
// a short grace period. The leading minus sign on the PID targets
// the whole group — essential for killing grandchildren like node
// spawned under npm.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Kill()
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	// Short grace, then SIGKILL. We don't wait here because Wait()
	// runs in another goroutine.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}
