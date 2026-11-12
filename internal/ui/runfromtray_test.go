package ui

import (
	"encoding/json"
	"errors"
	"image"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/widget"
)

// fakeTray records the balloons a TrayIcon was asked to show.
type fakeTray struct {
	mu       sync.Mutex
	balloons [][2]string
	err      error
}

func (f *fakeTray) Balloon(title, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balloons = append(f.balloons, [2]string{title, content})
	return f.err
}

func (f *fakeTray) got() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.balloons...)
}

// MAIN_WINDOW_CLOSE (main.bundle.cjs @1137077): a close hides the window
// when runFromTray is on, a workspace is signed in and the app isn't
// quitting, and the first such hide ever shows the RunFromTray balloon;
// anything else is a quit.
func TestCloseToTray(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	hides := 0
	hidden := true // what Window.Hide reports
	a.hideHook = func() bool { hides++; return hidden }

	// no tray icon (macOS, Linux, or StartTray failed): the close quits
	if a.closeToTray() || hides != 0 {
		t.Fatalf("no tray: closeToTray hid the window (%d hides)", hides)
	}
	tray := &fakeTray{}
	a.SetTray(tray)
	a.focused = true
	if !a.closeToTray() || hides != 1 {
		t.Fatalf("runFromTray on: the close wasn't a hide (%d hides)", hides)
	}
	if a.focused {
		t.Error("a hidden window still counts as focused (notifications for the open channel would be dropped)")
	}
	want := [2]string{"Why didn’t Clack quit?", "Clack’s set to stay running in the notification area. To change this, visit Preferences > Advanced."}
	waitFor(t, a, "the RunFromTray balloon", func() bool { return len(tray.got()) == 1 })
	if got := tray.got(); got[0] != want {
		t.Errorf("balloon %q, want %q", got[0], want)
	}
	if !a.prefs.HasRunFromTray {
		t.Error("hasRunFromTray not set after the notice")
	}
	a.flushPrefs()
	var saved prefs
	if b, err := os.ReadFile(filepath.Join(a.opts.ConfigDir, "ui.json")); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.HasRunFromTray || saved.NoRunFromTray {
		t.Errorf("ui.json: has_run_from_tray %v, no_run_from_tray %v; want true, false", saved.HasRunFromTray, saved.NoRunFromTray)
	}

	// the notice shows once ever
	if !a.closeToTray() || hides != 2 {
		t.Fatalf("second close: no hide (%d hides)", hides)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(tray.got()); n != 1 {
		t.Errorf("%d balloons after the second close, want 1", n)
	}

	// full screen is left first (EH: setFullScreen(false), then hide)
	a.winMode = app.Fullscreen // a.win is nil here: nothing to configure
	if !a.closeToTray() {
		t.Error("full screen: no hide")
	}
	a.winMode = app.Windowed

	// a platform that couldn't hide: the close goes on
	hidden = false
	if a.closeToTray() {
		t.Error("Window.Hide failed but the close was aborted")
	}
	hidden = true

	// signed out (0 workspaces): a quit
	login := a.login
	a.login = &loginScreen{}
	if a.closeToTray() {
		t.Error("signed out: the close hid the window")
	}
	a.login = login
	data := a.data
	a.data = nil
	if a.closeToTray() {
		t.Error("no backend: the close hid the window")
	}
	a.data = data

	// Advanced's checkbox off: a quit
	a.setRunFromTray(false)
	if a.closeToTray() {
		t.Error("runFromTray off: the close hid the window")
	}
	a.setRunFromTray(true)

	// Quit (the tray's Quit Clack, File > Quit, an update's restart)
	a.quitting.Store(true)
	if a.closeToTray() {
		t.Error("quitting: the close hid the window")
	}
	a.quitting.Store(false)

	// a balloon that fails is logged, nothing else
	a.prefs.HasRunFromTray = false
	tray.err = errors.New("Shell_NotifyIcon")
	if !a.closeToTray() {
		t.Error("a failing balloon stopped the hide")
	}
}

// DesktopPref's "Leave app running in notification area when the window
// is closed" (prefs-dialog GpYL): a real checkbox, on by default, where a
// tray icon runs; greyed and unticked without one.
func TestRunFromTrayRow(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	draw := func() {
		gtx := ttContext(new(op.Ops), image.Pt(523, 900))
		a.slackcFrame(gtx)
		p.runFromTrayRow(p.form(gtx))
	}
	draw()
	if _, ok := p.clicks["na.runFromTray"]; !ok {
		t.Fatalf("no tray: the row isn't the greyed one (%v)", clickKeys(p.clicks))
	}
	a.SetTray(&fakeTray{})
	draw()
	c, ok := p.clicks["run_from_tray"]
	if !ok {
		t.Fatalf("with a tray: no run_from_tray row (%v)", clickKeys(p.clicks))
	}
	if !a.runFromTrayOn() {
		t.Fatal("runFromTray isn't on by default")
	}
	c.Click()
	draw()
	draw()
	if a.runFromTrayOn() || !a.prefs.NoRunFromTray {
		t.Error("unticking the row left runFromTray on")
	}
	c.Click()
	draw()
	draw()
	if !a.runFromTrayOn() {
		t.Error("ticking the row again left runFromTray off")
	}
}

func clickKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Work posted while the window draws no frames (hidden to the tray,
// minimized) runs on Gio's wake-up event instead of waiting for a frame:
// the backend's events and notifications keep coming.
func TestDrainIdleRunsPosted(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	ran := false
	a.postLazy(func() { ran = true })
	a.drainIdle()
	if !ran {
		t.Error("drainIdle didn't run the posted work")
	}
	// before the fonts load, the first frames drain (as before); once
	// they have, the wake-up drains though no frame took them (a window
	// closed to the tray while they loaded: CI, TestCloseToTrayWindow)
	held, hold := make(chan struct{}), make(chan struct{})
	shaperReadyHook = func() { close(held); <-hold }
	t.Cleanup(func() { shaperReadyHook = nil })
	b := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, a.images)
	<-held
	ran = false
	b.postLazy(func() { ran = true })
	b.drainIdle()
	if ran {
		t.Error("drainIdle ran posted work before the shaper was ready")
	}
	close(hold)
	for deadline := time.Now().Add(10 * time.Second); len(b.shaperCh) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the shaper never came")
		}
	}
	b.drainIdle()
	if !ran {
		t.Error("drainIdle didn't run posted work once the shaper was ready (no frame had taken it)")
	}
}
