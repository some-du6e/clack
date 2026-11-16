package ui

import (
	"os/exec"
	"syscall"
)

// hideChildWindow starts cmd without a console window (CREATE_NO_WINDOW).
func hideChildWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
