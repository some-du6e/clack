//go:build windows

package ui

import (
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/ui/huddleui"
)

const canCloseHuddleWindowLikeYou = true

var (
	barTestFindWindow  = windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowW")
	barTestPostMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")
)

// closeHuddleWindowLikeYou sends the huddle window WM_CLOSE, what its
// caption's close button sends.
func closeHuddleWindowLikeYou(t *testing.T) {
	t.Helper()
	title, _ := windows.UTF16PtrFromString("Huddle - " + huddleui.AppName)
	var hwnd uintptr
	for dl := time.Now().Add(5 * time.Second); hwnd == 0; time.Sleep(20 * time.Millisecond) {
		hwnd, _, _ = barTestFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
		if hwnd == 0 && time.Now().After(dl) {
			t.Fatal("no top-level window titled Huddle - " + huddleui.AppName)
		}
	}
	const wmClose = 0x0010
	if r, _, err := barTestPostMessage.Call(hwnd, wmClose, 0, 0); r == 0 {
		t.Fatalf("PostMessage(WM_CLOSE): %v", err)
	}
}
