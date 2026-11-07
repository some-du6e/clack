package desktop

import "golang.org/x/sys/windows"

var procAllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowForeground lets the running instance take the foreground when this
// (just launched, so foreground) process hands it a request; without it
// Windows only flashes the taskbar button.
func allowForeground() {
	const asfwAny = ^uintptr(0) // ASFW_ANY (DWORD -1)
	_, _, _ = procAllowSetForegroundWindow.Call(asfwAny & 0xffffffff)
}
