//go:build windows

package huddleui

import (
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32TestFindWindow  = windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowW")
	user32TestPostMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")
)

// closeHuddleWindowLikeYou sends the huddle window WM_CLOSE, what its
// caption's close button sends.
func closeHuddleWindowLikeYou(t *testing.T, _ *window) {
	t.Helper()
	title, _ := windows.UTF16PtrFromString("Huddle - " + AppName)
	var hwnd uintptr
	deadline := time.Now().Add(5 * time.Second)
	for hwnd == 0 {
		hwnd, _, _ = user32TestFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
		if hwnd == 0 && time.Now().After(deadline) {
			t.Fatal("no top-level window titled Huddle - " + AppName)
		}
		time.Sleep(20 * time.Millisecond)
	}
	const wmClose = 0x0010
	if r, _, err := user32TestPostMessage.Call(hwnd, wmClose, 0, 0); r == 0 {
		t.Fatalf("PostMessage(WM_CLOSE): %v", err)
	}
}
