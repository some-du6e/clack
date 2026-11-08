//go:build !windows

package desktop

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// waitPID waits up to d for process pid to exit.
func waitPID(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// detach starts c in its own session, so it outlives Clack and its
// terminal.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
