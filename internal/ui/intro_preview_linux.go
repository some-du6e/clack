package ui

import (
	"os/exec"
	"syscall"
)

// configureChild has the kernel kill the renderer if the window's process
// dies first (it would otherwise run on to its deadline).
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// endStartupFeedback: no launch feedback to end off Windows.
func endStartupFeedback() {}
