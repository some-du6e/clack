package main

import (
	"os"
	"syscall"
)

// attachConsole gives `Clack.exe --update` (a GUI-subsystem exe, so it
// starts without a console) the terminal it was run from, unless its
// output is already redirected (`| Out-Host`, a file).
func attachConsole() {
	if _, err := os.Stdout.Stat(); err == nil {
		return // redirected: keep it
	}
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParentProcess)
	if r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
}
