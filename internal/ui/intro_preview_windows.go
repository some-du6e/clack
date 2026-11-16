package ui

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// configureChild starts the renderer without a console window
// (CREATE_NO_WINDOW; it is a GUI-subsystem program in releases anyway).
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

var procPeekMessage = syscall.NewLazyDLL("user32.dll").NewProc("PeekMessageW")

// endStartupFeedback ends the app-starting cursor Windows shows over the
// window that launched the renderer: the feedback lasts until a started
// GUI program asks for its messages, and the renderer has no window or
// message loop, so it asks once (PM_NOREMOVE: nothing is taken).
func endStartupFeedback() {
	var msg [64]byte // MSG (48 bytes on amd64 and arm64)
	_, _, _ = procPeekMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
}
