//go:build windows

package desktop

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NOTIFYICONDATAW's size for this architecture (shellapi.h with
// NTDDI_VERSION >= Vista: 976 bytes on 64-bit, 956 on 32-bit).
func TestNotifyIconDataSize(t *testing.T) {
	want := uintptr(976)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = 956
	}
	if got := unsafe.Sizeof(notifyIconData{}); got != want {
		t.Errorf("notifyIconData is %d bytes, want %d", got, want)
	}
	n := notifyIconData{}
	if off := unsafe.Offsetof(n.BalloonIcon); off != want-unsafe.Sizeof(uintptr(0)) {
		t.Errorf("hBalloonIcon at %d", off)
	}
}

// The RunFromTray notice as Electron's Tray.displayBalloon sends it, on a
// real notification area (CLACK_WINDOWS_TRAY_TESTS, ci's windows-applog
// job): Shell_NotifyIcon(NIM_ADD) and then NIM_MODIFY with NIF_INFO
// succeed, with NIIF_USER | NIIF_LARGE_ICON, the app's large icon and
// uTimeout 0, and a later tooltip change doesn't send NIF_INFO again
// (the balloon would come back). It proves the shell took the call; it
// can't see the toast Windows 11 turns it into.
func TestTrayBalloon(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TRAY_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	tr, err := StartTray("Clack test", nil, nil)
	if err != nil {
		t.Fatalf("StartTray: %v", err)
	}
	defer tr.Close()
	title := "Why didn’t Clack quit?"
	content := "Clack’s set to stay running in the notification area. To change this, visit Preferences > Advanced."
	if err := tr.Balloon(title, content); err != nil {
		t.Fatalf("Balloon: %v", err)
	}
	tr.mu.Lock()
	n := tr.balloon
	tip := tr.nid
	tr.mu.Unlock()
	if n.Flags != nifInfo {
		t.Errorf("uFlags %#x, want NIF_INFO", n.Flags)
	}
	if n.InfoFlags != niifUser|niifLargeIcon {
		t.Errorf("dwInfoFlags %#x, want NIIF_USER|NIIF_LARGE_ICON", n.InfoFlags)
	}
	if n.BalloonIcon == 0 || n.Version != 0 {
		t.Errorf("hBalloonIcon %#x, uTimeout %d", n.BalloonIcon, n.Version)
	}
	if got := windows.UTF16ToString(n.InfoTitle[:]); got != title {
		t.Errorf("szInfoTitle %q", got)
	}
	if got := windows.UTF16ToString(n.Info[:]); got != content {
		t.Errorf("szInfo %q", got)
	}
	if n.Wnd != tip.Wnd || n.ID != tip.ID {
		t.Errorf("the balloon names icon %v/%d, the tray is %v/%d", n.Wnd, n.ID, tip.Wnd, tip.ID)
	}
	if tip.Flags&nifInfo != 0 {
		t.Error("the icon's own data has NIF_INFO: every tooltip change would show the balloon again")
	}
	tr.SetTooltip("Clack test 2")
}

// putUTF16 cuts like wcslcpy: the NUL always fits.
func TestPutUTF16(t *testing.T) {
	var b [4]uint16
	putUTF16(b[:], "abcdef")
	if got := windows.UTF16ToString(b[:]); got != "abc" || b[3] != 0 {
		t.Errorf("got %q %v", got, b)
	}
}
