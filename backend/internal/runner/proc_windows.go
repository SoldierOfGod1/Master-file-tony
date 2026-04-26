//go:build windows

package runner

import (
	"os/exec"
	"strconv"
	"syscall"
)

// applyProcessAttrs puts the child in its own process group so
// ctrl-break signals + taskkill's /T walk stop at the right boundary.
// Without CREATE_NEW_PROCESS_GROUP the dev server's grandchildren
// (e.g. node.exe under npm.cmd) survive a naive kill.
func applyProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000200, // CREATE_NEW_PROCESS_GROUP
	}
}

// killTree uses `taskkill /T /F /PID` to terminate the target and
// every descendant. This is the only reliable way on Windows to kill
// an npm-run-spawned dev server — Process.Kill() only takes out npm.cmd
// and leaves node.exe holding the port.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	return exec.Command("taskkill", "/T", "/F", "/PID", pid).Run()
}
