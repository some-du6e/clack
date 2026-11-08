package desktop

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// waitPID waits up to d for process pid to exit.
func waitPID(pid int, d time.Duration) bool {
	p, err := os.FindProcess(pid) // OpenProcess: fails once it is gone
	if err != nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		_, _ = p.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		_ = p.Release()
		return false
	}
}

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach starts c without Clack's console or process group, so it outlives
// Clack.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
