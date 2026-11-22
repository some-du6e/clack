//go:build windows

package desktop

import (
	"os"
	"testing"
	"time"
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

// Windows ending the session sends every top-level window
// WM_QUERYENDSESSION, then WM_ENDSESSION, and ends the process once those
// return: the tray's hidden window answers yes and hands OnSessionEnd the
// reason, which Clack logs as its exit line before it goes. A cancelled
// end (WM_ENDSESSION with FALSE) says nothing.
func TestTraySessionEnd(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TRAY_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	// an earlier test's Close posts WM_CLOSE: its tray thread lets go a
	// moment later
	var tr *Tray
	var err error
	for deadline := time.Now().Add(10 * time.Second); ; {
		if tr, err = StartTray("Clack test", nil, nil); err != errTrayAlreadyRunning || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("StartTray: %v", err)
	}
	defer tr.Close()
	got := make(chan string, 4)
	tr.OnSessionEnd(func(why string) { got <- why })
	tr.mu.Lock()
	hwnd := uintptr(tr.hwnd)
	tr.mu.Unlock()
	const closeApp, logoff = 0x1, 0x80000000
	if r, _, _ := procSendMessageW.Call(hwnd, wmQueryEndSess, 0, logoff); r != 1 {
		t.Errorf("WM_QUERYENDSESSION answered %d, want TRUE", r)
	}
	procSendMessageW.Call(hwnd, wmEndSession, 0, logoff) // cancelled
	for _, c := range []struct {
		lp   uintptr
		want string
	}{
		{logoff, "Windows is signing the user out (WM_ENDSESSION)"},
		{0, "Windows is shutting down or restarting (WM_ENDSESSION)"},
		{closeApp, "an installer or update is replacing Clack's files (Windows Restart Manager closed it) (WM_ENDSESSION)"},
	} {
		procSendMessageW.Call(hwnd, wmEndSession, 1, c.lp)
		select {
		case why := <-got:
			if why != c.want {
				t.Errorf("lParam %#x: %q, want %q", c.lp, why, c.want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("lParam %#x: OnSessionEnd wasn't called", c.lp)
		}
	}
	select {
	case why := <-got:
		t.Errorf("an extra call: %q (the cancelled end?)", why)
	default:
	}
}
