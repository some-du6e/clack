//go:build js

package desktop

import (
	"os/exec"
	"time"
)

// The browser demo has no processes of its own: nothing to wait for and
// nothing to detach (proc_unix.go).
func waitPID(int, time.Duration) bool { return true }

func detach(*exec.Cmd) {}
