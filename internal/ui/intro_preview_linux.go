package ui

import (
	"os/exec"
	"syscall"
)

// configureChild has the kernel send the renderer SIGTERM if the thread
// that started it dies (the window's process going, or that thread
// exiting: run keeps it locked from Start to Wait). The renderer removes
// its folder on SIGTERM (handleChildSignals).
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

// endStartupFeedback: no launch feedback to end off Windows.
func endStartupFeedback() {}
