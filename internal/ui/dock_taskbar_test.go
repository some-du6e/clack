package ui

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"gioui.org/widget"
)

// taskbarRec records the Windows taskbar calls dock.go makes, with the
// window and the system's idle time faked (taskbarAnyOS).
type taskbarRec struct {
	mu      sync.Mutex
	calls   []string
	visible bool
	idle    int
	toasts  bool
}

func (r *taskbarRec) add(f string, args ...any) {
	r.mu.Lock()
	r.calls = append(r.calls, fmt.Sprintf(f, args...))
	r.mu.Unlock()
}

func (r *taskbarRec) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.calls
	r.calls = nil
	return c
}

const testHWND = 0x1234

func fakeTaskbar(t *testing.T) *taskbarRec {
	r := &taskbarRec{visible: true, toasts: true}
	old := []any{taskbarOverlay, frameFlash, winVisible, winShowInactive, winMinimize, systemIdleTime, toastsEnabled}
	taskbarAnyOS = true
	taskbarHWNDHook = func() uintptr { return testHWND }
	taskbarOverlay = func(h uintptr, n int, desc string) error { r.add("overlay %#x %d %q", h, n, desc); return nil }
	frameFlash = func(h uintptr, on bool) { r.add("flash %#x %v", h, on) }
	winVisible = func(uintptr) bool { r.mu.Lock(); defer r.mu.Unlock(); return r.visible }
	winShowInactive = func(h uintptr) { r.add("showInactive %#x", h); r.mu.Lock(); r.visible = true; r.mu.Unlock() }
	winMinimize = func(h uintptr) { r.add("minimize %#x", h) }
	systemIdleTime = func() int { r.mu.Lock(); defer r.mu.Unlock(); return r.idle }
	toastsEnabled = func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.toasts }
	t.Cleanup(func() {
		taskbarAnyOS, taskbarHWNDHook = false, nil
		taskbarOverlay = old[0].(func(uintptr, int, string) error)
		frameFlash = old[1].(func(uintptr, bool))
		winVisible = old[2].(func(uintptr) bool)
		winShowInactive = old[3].(func(uintptr))
		winMinimize = old[4].(func(uintptr))
		systemIdleTime = old[5].(func() int)
		toastsEnabled = old[6].(func() bool)
	})
	return r
}

// SET_BADGE_EPIC (win32 main.bundle.cjs @1171101): the overlay follows the
// summed unread highlights, set only when the count changes, cleared at 0
// with no text, set again on FOREGROUND_APP (Raise), which also stops a
// flash (rj's flashFrame(false)).
func TestTaskbarOverlayFollowsCount(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	r := fakeTaskbar(t)
	m := a.model()
	m.HomeCount = 3
	a.updateDock()
	a.updateDock() // unchanged: nothing
	m.HomeCount = 120
	a.updateDock()
	m.HomeCount = 0
	a.updateDock()
	want := []string{
		`overlay 0x1234 3 "You have unread messages"`,
		`overlay 0x1234 120 "You have unread messages"`,
		`overlay 0x1234 0 "You have unread messages"`, // desktop.SetTaskbarOverlay drops the text at 0
	}
	if got := r.take(); !reflect.DeepEqual(got, want) {
		t.Fatalf("overlay calls\n%q\nwant\n%q", got, want)
	}
	a.foregrounded()
	if got, want := r.take(), []string{"flash 0x1234 false", `overlay 0x1234 0 "You have unread messages"`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FOREGROUND_APP: %q, want %q", got, want)
	}
	// a new window (Win32ViewEvent's HWND) gets the badge again
	taskbarHWNDHook = func() uintptr { return 0x99 }
	m.HomeCount = 2
	a.syncTaskbar()
	a.syncTaskbar()
	if got, want := r.take(), []string{`overlay 0x99 2 "You have unread messages"`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("new window: %q, want %q", got, want)
	}
	// no window yet: nothing
	taskbarHWNDHook = func() uintptr { return 0 }
	m.HomeCount = 5
	a.updateDock()
	if got := r.take(); len(got) != 0 {
		t.Fatalf("no window: %q", got)
	}
}

// FLASH_WINDOW_ON_NOTIFICATION (@1168880) per win32.windowFlashBehavior:
// always flashes; idle (the default) flashes only after more than 20 s
// without input, first showing a window hidden to the tray inactive and
// minimized; never doesn't. A notification Windows won't show (the app's
// toasts turned off) is no NEW_NOTIFICATION, so no flash.
func TestFlashOnNotification(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	r := fakeTaskbar(t)
	a.notify = &recNotifier{}
	notify := func() []string {
		t.Helper()
		r.take()
		a.Notify("t", "b")
		time.Sleep(100 * time.Millisecond)
		drainPosted(a)
		return r.take()
	}
	if a.windowFlashBehavior() != flashIdle {
		t.Fatalf("default windowFlashBehavior %q, want idle", a.windowFlashBehavior())
	}
	r.idle = 20 // getSystemIdleTime() 20: still active
	if got := notify(); len(got) != 0 {
		t.Fatalf("idle mode, active user: %q", got)
	}
	r.idle = 21
	if got, want := notify(), []string{"flash 0x1234 true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("idle mode, idle user: %q, want %q", got, want)
	}
	r.visible = false // hidden to the tray
	if got, want := notify(), []string{"showInactive 0x1234", "minimize 0x1234", "flash 0x1234 true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("idle mode, hidden window: %q, want %q", got, want)
	}
	a.prefs.WindowFlashBehavior = flashAlways
	r.idle, r.visible = 0, false
	if got, want := notify(), []string{"flash 0x1234 true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("always: %q, want %q (no revive: always never hides the window)", got, want)
	}
	r.toasts = false
	if got := notify(); len(got) != 0 {
		t.Fatalf("toasts off in Windows: %q", got)
	}
	r.toasts = true
	a.prefs.WindowFlashBehavior = flashNever
	r.idle = 100
	if got := notify(); len(got) != 0 {
		t.Fatalf("never: %q", got)
	}
}

// drainPosted runs what was posted (the UI goroutine's pending work).
func drainPosted(a *App) {
	a.mu.Lock()
	p := a.pending
	a.pending = nil
	a.mu.Unlock()
	for _, f := range p {
		f()
	}
}

// MAIN_WINDOW_CLOSE on win32 (@1149121, gP @1151399): "always" flashing
// is keepAppInTaskbar, so a close minimizes the window even with
// runFromTray off, and the first one shows the WindowFlash balloon
// instead of RunFromTray's.
func TestCloseKeepsAppInTaskbar(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	r := fakeTaskbar(t)
	hides := 0
	a.hideHook = func() bool { hides++; return true }
	tray := &fakeTray{}
	a.SetTray(tray)
	a.prefs.WindowFlashBehavior = flashAlways
	a.setRunFromTray(false)
	if !a.closeToTray() || hides != 0 {
		t.Fatalf("always flash, runFromTray off: closeToTray didn't keep the window (%d hides)", hides)
	}
	if got, want := r.take(), []string{"minimize 0x1234"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("close: %q, want %q", got, want)
	}
	want := [2]string{"Why didn’t Clack quit?", "Clack’s set to always flash when you get notifications. To change this, visit Preferences > Notifications."}
	waitFor(t, a, "the WindowFlash balloon", func() bool { return len(tray.got()) == 1 })
	if got := tray.got(); got[0] != want {
		t.Errorf("balloon %q, want %q", got[0], want)
	}
	if !a.prefs.HasExplainedWindowFlash || a.prefs.HasRunFromTray {
		t.Errorf("hasExplainedWindowFlash %v hasRunFromTray %v, want true false", a.prefs.HasExplainedWindowFlash, a.prefs.HasRunFromTray)
	}
	if !a.closeToTray() {
		t.Fatal("second close quit")
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(tray.got()); n != 1 {
		t.Errorf("%d balloons after the second close, want 1", n)
	}
	// without a tray icon the minimize still keeps Clack running
	a.SetTray(nil)
	if !a.closeToTray() {
		t.Error("no tray, always flash: the close quit")
	}
	// back to idle with runFromTray off: a quit
	a.prefs.WindowFlashBehavior = flashIdle
	if a.closeToTray() {
		t.Error("idle flash, runFromTray off, no tray: the close was aborted")
	}
	a.SetTray(tray)
	if a.closeToTray() {
		t.Error("idle flash, runFromTray off: the close was aborted")
	}
	a.setRunFromTray(true)
	if !a.closeToTray() || hides != 1 {
		t.Fatalf("runFromTray on: no hide (%d hides)", hides)
	}
}

// WindowsAppearancePref (prefs-dialog b5a3ec9f): the flash radios, the
// delivery select (winrt by default, html adds the position selects), and
// none of the selects under force_native_notifications.
func TestWindowsAppearancePref(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	winAppearanceAnyOS = true
	t.Cleanup(func() { winAppearanceAnyOS = false })
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	tail := func() []prefsDrawn {
		rows := pageRows(t, a, p, "notifications")
		for i, r := range rows {
			if r.Label == "Flash window when a notification is received" {
				return rows[i:]
			}
		}
		t.Fatalf("no Windows block in %v", rows)
		return nil
	}
	got := tail()
	want := []prefsDrawn{
		{Kind: "label", Label: "Flash window when a notification is received", Enabled: true},
		{Kind: "radio", Key: "flashwindow=never", Label: "Never", Enabled: true},
		{Kind: "radio", Key: "flashwindow=idle", Label: "When left idle", On: true, Enabled: true},
		{Kind: "radio", Key: "flashwindow=always", Label: "Always", Enabled: true},
		{Kind: "text", Label: "Deliver notifications via…", Enabled: true},
		{Kind: "select", Key: "sel.winssb_notification_method", Label: "Windows Action Center", Enabled: true},
	}
	if len(got) < len(want) || !reflect.DeepEqual(got[:len(want)], want) {
		t.Fatalf("Windows block\n%+v\nwant\n%+v", got, want)
	}
	if len(got) != len(want) {
		t.Fatalf("rows after the select: %+v", got[len(want):])
	}
	p.clicks["flashwindow=always"].Click()
	tail()
	if a.prefs.WindowFlashBehavior != flashAlways {
		t.Fatalf("windowFlashBehavior %q after Always", a.prefs.WindowFlashBehavior)
	}
	a.setDesktopSetting(func(u *prefs) { u.NotificationMethod = "html" })
	got = tail()
	var labels []string
	for _, r := range got[len(want)-1:] {
		labels = append(labels, r.Kind+":"+r.Label)
	}
	if w := []string{"select:Slack’s built-in notifications", "label:Notification position", "select:bottom-right", "label:Display", "select:same display as app"}; !reflect.DeepEqual(labels, w) {
		t.Fatalf("html: %q, want %q", labels, w)
	}
	ca.SetExperimentGroup("force_native_notifications", "on")
	got = tail()
	if len(got) != 4 {
		t.Fatalf("force_native_notifications on: %+v, want only the flash radios", got)
	}
}
