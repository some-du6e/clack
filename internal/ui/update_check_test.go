package ui

import (
	"context"
	"errors"
	"image"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/menubar"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// rateLimited is desktop.RateLimitError's shape.
type rateLimited struct{ until time.Time }

func (e rateLimited) Error() string      { return "403 rate limited" }
func (e rateLimited) RetryAt() time.Time { return e.until }

// statusErr is desktop.StatusError's shape.
type statusErr string

func (e statusErr) Error() string      { return "update check: " + string(e) }
func (e statusErr) HTTPStatus() string { return string(e) }

var offline = &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: network is unreachable")}

// The schedule is wall clock: an hour between automatic checks, counted
// across sleeps (the "slept 10 h" jump), errors retried sooner, GitHub's
// rate limit waited out, a clock set back not freezing checks forever.
func TestUpdateSchedWallClock(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	var s updateSched
	if !s.due(t0) {
		t.Fatal("never checked: not due")
	}
	s.record(t0, nil)
	for _, d := range []time.Duration{0, 5 * time.Minute, 59 * time.Minute} {
		if s.due(t0.Add(d)) {
			t.Errorf("due %v after a check", d)
		}
	}
	if !s.due(t0.Add(time.Hour)) {
		t.Error("not due an hour later")
	}
	// A monotonic reading would say only minutes passed across a sleep;
	// Round(0) compares the wall clock, which says 10 h.
	woke := time.Now().Add(10 * time.Hour) // carries a monotonic reading
	var s2 updateSched
	s2.record(time.Now(), nil)
	if !s2.due(woke) {
		t.Error("not due after sleeping 10 h")
	}
	if s2.due(time.Now().Add(30 * time.Minute)) {
		t.Error("due 30 min after a check")
	}
	if strings.Contains(s2.Last.String(), "m=") {
		t.Errorf("the last check keeps a monotonic reading: %v", s2.Last)
	}

	// a failed check: retried after 5, 10, 20… minutes, at most hourly
	s3 := updateSched{Last: t0}
	now := t0.Add(2 * time.Hour)
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		s3.record(now, offline)
		if s3.due(now.Add(w - time.Second)) {
			t.Errorf("failure %d: due before %v", i+1, w)
		}
		if !s3.due(now.Add(w)) {
			t.Errorf("failure %d: not due after %v", i+1, w)
		}
		now = now.Add(w)
	}
	s3.record(now, nil)
	if s3.fails != 0 || !s3.retry.IsZero() || s3.due(now.Add(10*time.Minute)) {
		t.Errorf("success didn't reset the backoff: %+v", s3)
	}

	// rate limited for 40 min: nothing before, due after
	s4 := updateSched{Last: t0}
	now = t0.Add(3 * time.Hour)
	s4.record(now, rateLimited{now.Add(40 * time.Minute)})
	if s4.due(now.Add(39 * time.Minute)) {
		t.Error("due while rate limited")
	}
	if until, on := s4.limited(now.Add(time.Minute)); !on || !until.Equal(now.Add(40*time.Minute)) {
		t.Errorf("limited: %v %v", until, on)
	}
	if !s4.due(now.Add(40 * time.Minute)) {
		t.Error("not due once the limit ends")
	}
	// a reset in the past still waits a minute
	s4.record(now, rateLimited{now.Add(-time.Hour)})
	if s4.due(now.Add(30*time.Second)) || !s4.due(now.Add(time.Minute)) {
		t.Error("past reset")
	}

	// the clock was set back a day: check rather than wait a day
	s5 := updateSched{Last: t0.Add(24 * time.Hour)}
	if !s5.due(t0) {
		t.Error("clock set back: not due")
	}
	if s5.due(t0.Add(24*time.Hour - time.Minute)) {
		t.Error("a minute of skew counts as set back")
	}
}

// The schedule survives a restart through ui.json: a relaunch right after
// a check doesn't ask GitHub again.
func TestUpdateSchedPersists(t *testing.T) {
	dir := t.TempDir()
	a := New(Options{ConfigDir: dir, NoRealtime: true}, nil)
	at := time.Now().Round(0).Truncate(time.Second)
	s := updateSched{}
	s.record(at, nil)
	a.updateChecked(updateResult{at: at, sched: s})
	a.flushPrefs()
	b := New(Options{ConfigDir: dir, NoRealtime: true}, nil)
	got := b.updateSchedule()
	if !got.Last.Equal(at) || got.due(at.Add(10*time.Minute)) || !got.due(at.Add(time.Hour)) {
		t.Fatalf("after a restart: %+v", got)
	}
	lim := at.Add(30 * time.Minute)
	s.record(at, rateLimited{lim})
	b.updateChecked(updateResult{at: at, err: rateLimited{lim}, sched: s})
	b.flushPrefs()
	c := New(Options{ConfigDir: dir, NoRealtime: true}, nil)
	if got := c.updateSchedule(); !got.Limited.Equal(lim) || got.due(at.Add(29*time.Minute)) {
		t.Fatalf("rate limit after a restart: %+v", got)
	}
}

// fakeCheck is Options.CheckUpdate answering from a script.
type fakeCheck struct {
	calls  atomic.Int32
	mu     sync.Mutex
	answer func() (UpdateInfo, bool, error)
	gate   chan struct{} // non-nil: each call waits for a value
}

func (f *fakeCheck) set(info UpdateInfo, ok bool, err error) {
	f.mu.Lock()
	f.answer = func() (UpdateInfo, bool, error) { return info, ok, err }
	f.mu.Unlock()
}

func (f *fakeCheck) check(ctx context.Context) (UpdateInfo, bool, error) {
	f.calls.Add(1)
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.answer()
}

func (f *fakeCheck) setGate(g chan struct{}) {
	f.mu.Lock()
	f.gate = g
	f.mu.Unlock()
}

// fakeClock is updateLoop's clock in a test: wall time set by the test.
type fakeClock struct {
	mu   sync.Mutex
	t    time.Time
	tick chan time.Time
	stop chan struct{}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// fire is a tick; the loop has handled it once a second one is taken.
func (c *fakeClock) fire() {
	c.tick <- c.now()
	c.tick <- c.now()
}

func startLoop(t *testing.T, a *App, s updateSched, t0 time.Time) *fakeClock {
	c := &fakeClock{t: t0, tick: make(chan time.Time), stop: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		a.updateLoop(s, updateClock{now: c.now, tick: c.tick, stop: c.stop})
		close(done)
	}()
	t.Cleanup(func() { close(c.stop); <-done })
	return c
}

func waitUpd(t *testing.T, a *App, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.drain()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The loop against a scripted GitHub: ticks check only when due by the
// wall clock, a 10 h sleep is noticed on the next tick, "Check for
// updates" off stops automatic checks but not Check now, and a rate limit
// holds every request (Check now says so without asking).
func TestUpdateLoopSchedule(t *testing.T) {
	SetVersion("v0.1.0-alpha.12")
	t.Cleanup(func() { SetVersion("") })
	f := &fakeCheck{}
	f.set(UpdateInfo{}, false, nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true, CheckUpdate: f.check}, nil)
	t0 := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	clk := startLoop(t, a, updateSched{}, t0)

	clk.fire() // never checked: due
	waitUpd(t, a, "the first check", func() bool { return a.update.check.upToDate })
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("%d checks on the first tick", n)
	}
	if a.prefs.UpdateCheckedAt != t0.Unix() {
		t.Errorf("last check not persisted: %d", a.prefs.UpdateCheckedAt)
	}
	for _, d := range []time.Duration{5 * time.Minute, 30 * time.Minute, 55 * time.Minute} {
		clk.set(t0.Add(d))
		clk.fire()
	}
	if n := f.calls.Load(); n != 1 {
		t.Fatalf("%d checks within the hour", n)
	}
	// slept 10 h: the next tick (or the focus nudge) checks
	clk.set(t0.Add(10 * time.Hour))
	a.nudgeUpdates()
	waitUpd(t, a, "the check after sleeping", func() bool { return f.calls.Load() == 2 })
	clk.fire()
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("%d checks after waking", n)
	}

	// checks off: ticks don't ask, Check now does
	a.toggleUpdateChecks()
	clk.set(t0.Add(20 * time.Hour))
	clk.fire()
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("checked automatically with checks off (%d)", n)
	}
	f.set(UpdateInfo{Tag: "v0.1.0-alpha.14", URL: "https://github.com/r/v0.1.0-alpha.14"}, true, nil)
	a.checkForUpdates(false)
	waitUpd(t, a, "Check now with checks off", func() bool { return a.update.info.Tag == "v0.1.0-alpha.14" })
	if a.update.phase == updNone || !a.update.manual {
		t.Errorf("Check now found an update with checks off: phase %d, manual %v", a.update.phase, a.update.manual)
	}
	a.toggleUpdateChecks()

	// rate limited for 40 min: no automatic check, and Check now answers
	// without asking GitHub (asking only pushes the limit out)
	calls := f.calls.Load()
	T := t0.Add(20*time.Hour + 20*time.Minute)
	lim := T.Add(40 * time.Minute)
	f.set(UpdateInfo{}, false, rateLimited{lim})
	clk.set(T)
	a.checkForUpdates(false)
	waitUpd(t, a, "the rate-limited check", func() bool { return !a.update.check.checking })
	if f.calls.Load() != calls+1 || a.prefs.UpdateLimitedUntil != lim.Unix() {
		t.Fatalf("rate limit: %d calls, limited until %d (want %d)", f.calls.Load()-calls, a.prefs.UpdateLimitedUntil, lim.Unix())
	}
	if !strings.Contains(a.update.check.err, "Try again in 40 minutes") {
		t.Errorf("rate limited: %q", a.update.check.err)
	}
	f.set(UpdateInfo{}, false, nil)
	clk.set(T.Add(10 * time.Minute))
	clk.fire()
	a.checkForUpdates(false)
	waitUpd(t, a, "Check now while limited", func() bool { return !a.update.check.checking })
	if f.calls.Load() != calls+1 {
		t.Fatal("asked GitHub while rate limited")
	}
	if !strings.Contains(a.update.check.err, "Try again in 30 minutes") {
		t.Errorf("rate-limited Check now: %q", a.update.check.err)
	}
	// the limit is over (and the last answer over an hour old): checks resume
	clk.set(lim.Add(5 * time.Minute))
	clk.fire()
	waitUpd(t, a, "the check after the limit", func() bool { return f.calls.Load() == calls+2 })
	waitUpd(t, a, "the limit cleared", func() bool { return a.prefs.UpdateLimitedUntil == 0 && a.update.check.upToDate })
}

func checkApp(t *testing.T, f *fakeCheck, dl *fakeUpdater) *App {
	t.Helper()
	opts := Options{ConfigDir: t.TempDir(), NoRealtime: true, CheckUpdate: f.check}
	if dl != nil {
		opts.DownloadUpdate = func(ctx context.Context, u UpdateInfo, _ func(UpdateProgress)) error {
			dl.downloads.Add(1)
			select {
			case err := <-dl.release:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		opts.InstallUpdate = func(u UpdateInfo) error { dl.installs.Add(1); return dl.installErr }
	}
	a := New(opts, nil)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	if dl != nil {
		a.update.quit = func() { dl.quits++ }
	}
	return a
}

// prefsRow lays out Preferences > Clack > Updates (handling its clicks)
// and returns what the row says.
func prefsRow(a *App, p *prefsModal) updateStatus {
	a.drain()
	gtx := ttContext(new(op.Ops), image.Pt(523, 600))
	a.slackcFrame(gtx)
	p.updatesSection(p.form(gtx))
	a.drain()
	return a.updateStatusAt(time.Now())
}

// rowClick clicks the row's button, which must read label, and returns
// the row after it.
func rowClick(t *testing.T, a *App, p *prefsModal, label string) updateStatus {
	t.Helper()
	st := a.updateStatusAt(time.Now())
	if st.button != label {
		t.Fatalf("the row's button is %q, want %q (%+v)", st.button, label, st)
	}
	b, ok := p.clicks["update_row"]
	if !ok {
		t.Fatalf("no row button laid out (have %v)", keysOf(p.clicks))
	}
	b.Click()
	return prefsRow(a, p)
}

// Check for Updates' states: Checking for Update, You’re all good (with
// when), a newer release with Download → Downloading Update → Restart to
// Apply Update, and errors in plain words, all driven by clicks on the
// Preferences row's one button.
func TestUpdateCheckNowStates(t *testing.T) {
	SetVersion("v0.1.0-alpha.12")
	t.Cleanup(func() { SetVersion("") })
	f := &fakeCheck{}
	gate := make(chan struct{})
	f.setGate(gate)
	dl := &fakeUpdater{release: make(chan error, 1)}
	a := checkApp(t, f, dl)
	a.toggleUpdateDownloads() // "Download updates automatically" off: the Download button
	startLoop(t, a, updateSched{}, time.Now())
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: clackTabID}
	click := func(label string) updateStatus { t.Helper(); return rowClick(t, a, p, label) }
	answer := func(info UpdateInfo, ok bool, err error) {
		f.set(info, ok, err)
		gate <- struct{}{}
		waitUpd(t, a, "the answer", func() bool { return !a.update.check.checking })
	}

	if st := prefsRow(a, p); st.title != "Clack v0.1.0-alpha.12" || st.desc != "Not checked yet." || st.button != "Check for Updates" || !st.enabled {
		t.Fatalf("before any check: %+v", st)
	}
	// Checking for Update: the button is disabled, a second click doesn't ask twice
	if st := click("Check for Updates"); st.title != "Checking for Update" || st.enabled {
		t.Fatalf("checking: %+v", st)
	}
	a.checkForUpdates(false)
	answer(UpdateInfo{}, false, nil)
	if st := prefsRow(a, p); st.title != "You’re all good" || !strings.HasSuffix(st.desc, "Checked just now.") || !st.enabled {
		t.Fatalf("up to date: %+v", st)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("%d checks for a double click", n)
	}
	if got := a.updateStatusAt(time.Now().Add(42 * time.Minute)).desc; !strings.HasSuffix(got, "Checked 42 minutes ago.") {
		t.Errorf("later: %q", got)
	}

	// errors, in plain words
	for _, c := range []struct {
		err  error
		want string
	}{
		{offline, "Couldn’t reach GitHub. Check your internet connection and try again."},
		{context.DeadlineExceeded, "GitHub didn’t answer in time. Check your internet connection and try again."},
		{statusErr("502 Bad Gateway"), "GitHub couldn’t answer right now (502 Bad Gateway). Try again later."},
	} {
		click("Check for Updates")
		answer(UpdateInfo{}, false, c.err)
		st := prefsRow(a, p)
		if st.title != "Couldn’t update Clack" || st.desc != c.want || st.button != "Check for Updates" || !st.enabled {
			t.Errorf("%v: %+v", c.err, st)
		}
	}
	// (TestUpdateLoopSchedule drives a rate limit through the loop)
	now := time.Now()
	if got := updateErrText(rateLimited{now.Add(12*time.Minute + 5*time.Second)}, now); got != "GitHub is limiting update checks from your network. Try again in 13 minutes." {
		t.Errorf("rate limited: %q", got)
	}
	if got := updateErrText(errors.New("update check: no published release"), now); got != "Couldn’t check for updates: update check: no published release" {
		t.Errorf("other: %q", got)
	}

	// alpha.14 is out: available, Download, downloading, ready, restart
	f.setGate(nil)
	f.set(newerInfo, true, nil)
	click("Check for Updates")
	waitUpd(t, a, "the newer release", func() bool { return a.update.info.Tag == newerInfo.Tag })
	st := prefsRow(a, p)
	if st.title != "An Update Is Available" || st.desc != "Clack v0.1.0-alpha.14 is ready to download." || !st.notes || st.button != "Download" || !st.primary {
		t.Fatalf("available: %+v", st)
	}
	if dl.downloads.Load() != 0 {
		t.Fatal("downloaded with downloads off")
	}
	if a.prefs.UpdateFound != newerInfo.Tag {
		t.Errorf("the found release isn't kept for the next launch: %q", a.prefs.UpdateFound)
	}
	if st := click("Download"); st.title != "Downloading Update" || st.button != "Cancel" || st.primary || !st.bar || st.frac >= 0 {
		t.Fatalf("downloading: %+v", st)
	}
	dl.release <- errors.New("update: the download doesn't match the release's SHA256SUMS")
	waitUpd(t, a, "the failed download", func() bool { return a.update.phase == updAvailable })
	if st := prefsRow(a, p); st.title != "Couldn’t update Clack" || !strings.Contains(st.desc, "SHA256SUMS") || st.button != "Try Again" {
		t.Fatalf("download failed: %+v", st)
	}
	click("Try Again")
	dl.release <- nil
	waitUpd(t, a, "the download", func() bool { return a.update.phase == updReady })
	if st := prefsRow(a, p); st.title != "Restart to Apply Update" || !strings.Contains(st.desc, "Just a quick restart") || !st.notes || st.button != "Restart Clack" || !st.primary {
		t.Fatalf("ready: %+v", st)
	}
	if dl.installs.Load() != 0 {
		t.Fatal("installed without the click")
	}
	click("Restart Clack")
	waitUpd(t, a, "the install", func() bool { return dl.quits == 1 })
	if dl.installs.Load() != 1 {
		t.Errorf("%d installs", dl.installs.Load())
	}
}

// newerInfo is the release Karim's alpha.12 should have been offered.
var newerInfo = UpdateInfo{Tag: "v0.1.0-alpha.14", URL: "https://github.com/some-du6e/clack/releases/tag/v0.1.0-alpha.14"}

func keysOf(m map[string]*widget.Clickable) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// Clack > Check for Updates… (macOS) and the workspace menu's item
// (Linux, Windows) run the same check and answer like Slack's manual
// check (yI, yR): a message box when there's nothing new or it failed;
// a newer release downloads (even with downloads off), the item says
// Downloading Update, then Restart to Apply Update, which installs.
func TestCheckForUpdatesMenus(t *testing.T) {
	SetVersion("v0.1.0-alpha.12")
	t.Cleanup(func() { SetVersion("") })
	f := &fakeCheck{}
	f.set(UpdateInfo{}, false, nil)
	dl := &fakeUpdater{release: make(chan error, 1)}
	a := checkApp(t, f, dl)
	startLoop(t, a, updateSched{}, time.Now())
	var item *menubar.Item
	eachItem(a.macMenus(), func(it menubar.Item) {
		if it.ID == "menuitem-update-status" {
			item = &it
		}
	})
	if item == nil || item.Title != "Check for Updates…" || item.Do == nil {
		t.Fatalf("app menu item: %+v", item)
	}
	if app := a.macMenus()[0]; app.Kind != "app" || app.Items[1].ID != "menuitem-update-status" {
		t.Error("Check for Updates… isn't right after About Clack")
	}
	menuTitle := func() (string, bool) {
		t.Helper()
		title, ok := a.updateMenuItem()
		st := a.menuState(menuKey{update: title, updateOff: !ok}) // signed out: the update item is the shell's own
		return st.Titles[menuUpdate], !st.Disabled[menuUpdate]
	}
	item.Do() // menubar runs it on its own goroutine: it only posts
	waitUpd(t, a, "the menu's check", func() bool { return f.calls.Load() == 1 && !a.update.check.checking })
	if got := a.updateBoxShown(); got != "You’re all good" {
		t.Errorf("up to date: message box %q", got)
	}
	// an error: "Couldn’t update Clack", saying why; it replaces the first box
	f.set(UpdateInfo{}, false, offline)
	item.Do()
	waitUpd(t, a, "the failing check", func() bool { return f.calls.Load() == 2 && !a.update.check.checking })
	if got := a.updateBoxShown(); got != "Couldn’t update Clack" || !strings.HasPrefix(a.update.check.err, "Couldn’t reach GitHub") {
		t.Errorf("error: message box %q (%q)", got, a.update.check.err)
	}
	if n := len(a.overlays); n != 2 || !a.overlays[0].Dismissed() {
		t.Errorf("%d overlays, the first dismissed: %v", n, n > 0 && a.overlays[0].Dismissed())
	}
	a.update.box.(dismisser).Dismiss()

	// a newer release with downloads and checks off: the menu's check
	// downloads it anyway, without a message box
	a.toggleUpdateDownloads()
	a.toggleUpdateChecks()
	f.set(newerInfo, true, nil)
	item.Do()
	waitUpd(t, a, "the newer release", func() bool { return a.update.phase == updDownloading })
	if got := a.updateBoxShown(); got != "" {
		t.Errorf("found: message box %q", got)
	}
	if title, ok := menuTitle(); title != "Downloading Update" || ok {
		t.Errorf("downloading: %q enabled %v", title, ok)
	}
	if a.updatePending() {
		t.Error("the Help dot before the download finished")
	}
	dl.release <- nil
	waitUpd(t, a, "the download", func() bool { return a.update.phase == updReady })
	if title, ok := menuTitle(); title != "Restart to Apply Update" || !ok || !a.updatePending() {
		t.Errorf("ready: %q enabled %v, pending %v", title, ok, a.updatePending())
	}
	if dl.installs.Load() != 0 {
		t.Fatal("installed without the user choosing to")
	}

	// Linux and Windows: the workspace menu's item; macOS has the menu bar
	if a.menuCheckForUpdatesOn("darwin") != nil || a.menuCheckForUpdatesOn("linux") == nil || a.menuCheckForUpdatesOn("windows") == nil {
		t.Error("workspace menu item per OS")
	}
	if (&App{}).menuCheckForUpdatesOn("linux") != nil {
		t.Error("an item without update checks (dev build)")
	}
	// Restart to Apply Update restarts right away (Slack's restart-to-apply)
	a.menuCheckForUpdatesOn("linux")()
	waitUpd(t, a, "the restart", func() bool { return dl.quits == 1 })
	if dl.installs.Load() != 1 {
		t.Errorf("%d installs", dl.installs.Load())
	}
	// no update checks: no menu bar item either
	if hasItem((&App{}).macMenus(), "menuitem-update-status") {
		t.Error("menu bar item without update checks")
	}
}

func hasItem(menus []menubar.Menu, id string) bool {
	found := false
	eachItem(menus, func(it menubar.Item) { found = found || it.ID == id })
	return found
}

// Preferences > Clack > Updates in each state, light and dark
// (UPDATE_SHOTS_DIR, the updater workflow's prefs-shots job): one row, the
// label and description on the left and its one button on the right.
func TestUpdatePrefsShots(t *testing.T) {
	SetVersion("v0.1.0-alpha.12")
	t.Cleanup(func() { SetVersion("") })
	f := &fakeCheck{}
	f.set(UpdateInfo{}, false, nil)
	dl := &fakeUpdater{release: make(chan error, 1)}
	a := checkApp(t, f, dl)
	shots := os.Getenv("UPDATE_SHOTS_DIR")
	r := &ttRenderer{t: t, a: a, sz: image.Pt(600, 330), dir: shots}
	if shots != "" {
		w, err := pixeltest.NewWindow(r.sz.X, r.sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		r.win = w
		defer w.Release()
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: clackTabID}
	draw := func(gtx layout.Context) {
		op.Offset(image.Pt(24, 20)).Push(gtx.Ops)
		gtx.Constraints = layout.Exact(image.Pt(523, 300))
		p.updatesSection(p.form(gtx))
	}
	now := time.Now().Round(0)
	info := UpdateInfo{Tag: "v0.1.0-alpha.14", URL: "https://github.com/x"}
	u := &a.update
	reset := func() {
		u.info, u.phase, u.err, u.dlErr, u.prog = UpdateInfo{}, updNone, "", "", UpdateProgress{}
		u.check = updateCheckState{}
		a.prefs.UpdateCheckedAt = 0
	}
	states := []struct {
		name          string
		set           func()
		title, button string
	}{
		{"idle", func() {}, "Clack v0.1.0-alpha.12", "Check for Updates"},
		{"last-checked", func() { a.prefs.UpdateCheckedAt = now.Add(-2 * time.Hour).Unix() }, "Clack v0.1.0-alpha.12", "Check for Updates"},
		{"checking", func() { u.check.checking = true }, "Checking for Update", "Check for Updates"},
		{"up-to-date", func() { u.check.upToDate, u.check.at = true, now.Add(-3*time.Minute) }, "You’re all good", "Check for Updates"},
		{"error-offline", func() { u.check.err = updateErrText(offline, now) }, "Couldn’t update Clack", "Check for Updates"},
		{"error-rate-limited", func() { u.check.err = updateErrText(rateLimited{now.Add(25 * time.Minute)}, now) }, "Couldn’t update Clack", "Check for Updates"},
		{"available", func() { u.info, u.phase = info, updAvailable }, "An Update Is Available", "Download"},
		{"download-failed", func() {
			u.info, u.phase, u.dlErr = info, updAvailable, "update: the download doesn't match the release's SHA256SUMS"
		}, "Couldn’t update Clack", "Try Again"},
		{"downloading", func() { u.info, u.phase = info, updDownloading }, "Downloading Update", "Cancel"},
		// a third of the way in (Karim asked to see this)
		{"downloading-progress", func() {
			u.info, u.phase = info, updDownloading
			u.prog = UpdateProgress{Stage: UpdateDownloading, Done: 12_700_000, Total: 37_330_000}
		}, "Downloading Update", "Cancel"},
		{"downloading-unknown-size", func() {
			u.info, u.phase = info, updDownloading
			u.prog = UpdateProgress{Stage: UpdateDownloading, Done: 5 << 20}
		}, "Downloading Update", "Cancel"},
		{"verifying", func() {
			u.info, u.phase = info, updDownloading
			u.prog = UpdateProgress{Stage: UpdateVerifying, Done: 30 << 20, Total: 40 << 20}
		}, "Downloading Update", "Cancel"},
		{"staging", func() {
			u.info, u.phase = info, updDownloading
			u.prog = UpdateProgress{Stage: UpdateStaging, Done: 40 << 20, Total: 40 << 20}
		}, "Downloading Update", "Cancel"},
		{"ready", func() { u.info, u.phase = info, updReady }, "Restart to Apply Update", "Restart Clack"},
		{"installing", func() { u.info, u.phase = info, updInstalling }, "Restart to Apply Update", "Restart Clack"},
		{"installer-open", func() { u.info, u.phase = info, updManual }, "Restart to Apply Update", ""},
		{"install-failed", func() {
			u.info, u.phase, u.err = info, updFailed, "update: moving the old copy aside: permission denied"
		}, "Couldn’t update Clack", "Try Again"},
	}
	for _, dark := range []bool{false, true} {
		a.setDark(dark)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, s := range states {
			reset()
			s.set()
			if st := a.updateStatusAt(now); st.title != s.title || st.button != s.button {
				t.Errorf("%s: %q [%s], want %q [%s]", s.name, st.title, st.button, s.title, s.button)
			}
			r.frame(draw)
			r.save("updates-"+s.name+"-"+mode, r.frame(draw), r.sz.Y)
		}
	}
}
