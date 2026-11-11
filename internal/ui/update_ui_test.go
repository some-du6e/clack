package ui

import (
	"context"
	"errors"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"image"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type fakeUpdater struct {
	downloads, installs atomic.Int32
	release             chan error // each download waits for one value
	installErr          error
	quits               int
}

func updateApp(t *testing.T, f *fakeUpdater) *App {
	t.Helper()
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true,
		DownloadUpdate: func(ctx context.Context, u UpdateInfo, progress func(UpdateProgress)) error {
			f.downloads.Add(1)
			select {
			case err := <-f.release:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		InstallUpdate: func(u UpdateInfo) error {
			f.installs.Add(1)
			return f.installErr
		},
	}, nil)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	a.update.quit = func() { f.quits++ }
	return a
}

func waitPhase(t *testing.T, a *App, want updatePhase) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.update.phase != want {
		if time.Now().After(deadline) {
			t.Fatalf("phase %d, want %d", a.update.phase, want)
		}
		time.Sleep(5 * time.Millisecond)
		a.drain()
	}
}

// helpFrame lays out the secondary pane (the Help flexpane) and the
// overlays, handling their clicks, like App.frame.
func helpFrame(a *App) {
	a.drain()
	gtx := ttContext(new(op.Ops), image.Pt(1000, 700))
	if v := a.secondary.view; v != nil && a.secondary.open() {
		atExact(gtx, image.Pt(600, 0), image.Pt(400, 700), v.Layout)
	}
	for _, o := range a.overlays {
		if !o.Dismissed() {
			o.Layout(gtx)
		}
	}
	a.drain()
}

// press is a click on one of the update dialog's buttons.
func press(d *updateDialog, b *popups.ModalButton) {
	if b.Do == nil || b.Do() {
		d.modal.Dismiss()
	}
}

// Each phase in Slack's presentation: found but not downloaded (no dot,
// "An Update Is Available"), downloading (no dot, "Downloading Update"),
// downloaded (the Help dot, the Take action card, "Restart to Apply
// Update"), and nothing installs until the user picks Restart.
func TestUpdatePhasesPresentation(t *testing.T) {
	f := &fakeUpdater{release: make(chan error, 1)}
	a := updateApp(t, f)
	menu := func() string {
		t.Helper()
		title, ok := a.updateMenuItem()
		if !ok {
			title += " (greyed)"
		}
		return title
	}
	if got := menu(); got != "Check for Updates…" || a.updatePending() {
		t.Fatalf("idle: %q, pending %v", got, a.updatePending())
	}
	a.update.check.checking = true
	if got := menu(); got != "Checking for Update (greyed)" {
		t.Errorf("checking: %q", got)
	}
	a.update.check.checking = false

	// available: downloads off, so it waits for the user
	a.toggleUpdateDownloads()
	a.updateFound(UpdateInfo{Tag: "v0.2.0", URL: "https://github.com/x"}, false)
	if a.update.phase != updAvailable || a.updatePending() || menu() != "An Update Is Available" {
		t.Fatalf("available: phase %d, pending %v, menu %q", a.update.phase, a.updatePending(), menu())
	}
	a.helpClick() // no dot: Help is the Keyboard Shortcuts pane (shortcuts_pane.go)
	if a.secondary.kind != secShortcuts {
		t.Errorf("Help without an update: pane %d, toast %q", a.secondary.kind, a.toast.text)
	}
	a.CloseSecondary()
	a.updateMenuClick() // An Update Is Available: Preferences > Advanced (openPrefsModal)
	waitUpd(t, a, "Preferences", func() bool { return len(a.overlays) > 0 })
	if len(a.overlays) != 1 {
		t.Errorf("An Update Is Available opened %d overlays", len(a.overlays))
	}
	a.overlays = nil

	// downloading
	a.toggleUpdateDownloads()
	if a.update.phase != updDownloading || a.updatePending() || menu() != "Downloading Update (greyed)" {
		t.Fatalf("downloading: phase %d, pending %v, menu %q", a.update.phase, a.updatePending(), menu())
	}
	a.updateMenuClick() // greyed: does nothing
	// the same release found again doesn't download twice
	a.updateFound(UpdateInfo{Tag: "v0.2.0", URL: "https://github.com/x"}, false)
	f.release <- nil
	waitPhase(t, a, updReady)
	if n := f.downloads.Load(); n != 1 {
		t.Errorf("%d downloads", n)
	}

	// ready: the dot, the menu item, the Take action card
	if !a.updatePending() || menu() != "Restart to Apply Update" {
		t.Fatalf("ready: pending %v, menu %q", a.updatePending(), menu())
	}
	a.helpClick()
	if a.secondary.kind != secHelp || !a.secondary.open() {
		t.Fatalf("Help with an update: pane %d", a.secondary.kind)
	}
	if title, desc := a.updateCardText(); title != "Clack v0.2.0" || desc != "Click to update to a new version of the app." {
		t.Errorf("card %q / %q", title, desc)
	}
	for i := 0; i < 5; i++ {
		helpFrame(a)
	}
	a.updateFound(UpdateInfo{Tag: "v0.2.0"}, false)
	helpFrame(a)
	if n := f.installs.Load(); n != 0 || a.update.phase != updReady || a.update.dialog.open() {
		t.Fatalf("frames went by: %d installs, phase %d, dialog %v", n, a.update.phase, a.update.dialog.open())
	}

	// the card opens "Restart and update Clack?", once
	a.update.card.Click()
	helpFrame(a)
	d := a.update.dialog
	if !d.open() || d.notNow.Label != "Not Now" || d.restart.Label != "Restart Clack" {
		t.Fatalf("dialog: %+v", d)
	}
	a.update.card.Click()
	helpFrame(a)
	if a.update.dialog != d || len(a.overlays) != 1 {
		t.Errorf("a second click stacked a dialog (%d overlays)", len(a.overlays))
	}
	// Not Now: closed, still pending, nothing installed; no snooze either
	press(d, d.notNow)
	helpFrame(a)
	if d.open() || !a.updatePending() || f.installs.Load() != 0 {
		t.Errorf("Not Now: open %v, pending %v, %d installs", d.open(), a.updatePending(), f.installs.Load())
	}

	// Restart Clack: installs once, then quits
	a.update.card.Click()
	helpFrame(a)
	press(a.update.dialog, a.update.dialog.restart)
	deadline := time.Now().Add(5 * time.Second)
	for f.quits == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		a.drain()
	}
	if f.installs.Load() != 1 || f.quits != 1 {
		t.Errorf("after Restart: %d installs, %d quits", f.installs.Load(), f.quits)
	}

	// Help toggles its pane; the pane's × closes it
	a.helpClick()
	if a.secondary.kind == secHelp {
		t.Error("Help again didn't close the pane")
	}
	a.helpClick()
	a.update.close.Click()
	helpFrame(a)
	if a.secondary.kind == secHelp {
		t.Error("× didn't close the Help pane")
	}

	// checks off: nothing pending, no dot
	a.toggleUpdateChecks()
	if a.updatePending() || menu() != "Check for Updates…" {
		t.Errorf("checks off: pending %v, menu %q", a.updatePending(), menu())
	}
}

// What a restart that doesn't install looks like, and the fallbacks: a
// failed download goes back to "available" (no dot), an unverified
// download never installs.
func TestUpdateErrorsPresentation(t *testing.T) {
	f := &fakeUpdater{release: make(chan error, 2)}
	a := updateApp(t, f)
	a.updateFound(UpdateInfo{Tag: "v0.2.0"}, false)
	f.release <- errors.New("update: the download doesn't match the release's SHA256SUMS")
	waitPhase(t, a, updAvailable)
	if a.updatePending() || a.update.dlErr == "" {
		t.Errorf("failed download: pending %v, dlErr %q", a.updatePending(), a.update.dlErr)
	}
	if title, _ := a.updateMenuItem(); title != "An Update Is Available" {
		t.Errorf("failed download: menu %q", title)
	}
	a.installUpdate()
	a.openAppUpdateDialog()
	if f.installs.Load() != 0 || a.update.dialog.open() {
		t.Error("an unverified update could be installed")
	}

	// the system installer opened instead: a toast, the card says so
	a.startUpdateDownload(true)
	f.release <- nil
	waitPhase(t, a, updReady)
	f.installErr = ErrUpdateManual
	a.openAppUpdateDialog()
	press(a.update.dialog, a.update.dialog.restart)
	waitPhase(t, a, updManual)
	if !strings.HasPrefix(a.toast.text, "Finish installing Clack v0.2.0") || f.quits != 0 || !a.updatePending() {
		t.Errorf("manual: toast %q, %d quits, pending %v", a.toast.text, f.quits, a.updatePending())
	}
	if _, desc := a.updateCardText(); !strings.HasPrefix(desc, "Finish installing") {
		t.Errorf("manual card %q", desc)
	}
	a.openAppUpdateDialog() // nothing to restart into
	if a.update.dialog.open() {
		t.Error("the dialog opened while the installer runs")
	}

	// the install failed: Slack's "Couldn’t update" box, the card says why,
	// the dialog can try again
	f.installErr = errors.New("update: moving the old copy aside: permission denied")
	a.update.phase = updReady
	a.openAppUpdateDialog()
	press(a.update.dialog, a.update.dialog.restart)
	waitPhase(t, a, updFailed)
	if got := a.updateBoxShown(); got != "Couldn’t update Clack" || f.quits != 0 {
		t.Errorf("failed: box %q, %d quits", got, f.quits)
	}
	if _, desc := a.updateCardText(); !strings.Contains(desc, "permission denied") || !a.updatePending() {
		t.Errorf("failed card %q", desc)
	}
	if title, ok := a.updateMenuItem(); title != "Check for Updates…" || !ok {
		t.Errorf("failed: menu %q", title)
	}
	f.installErr = nil
	a.openAppUpdateDialog()
	press(a.update.dialog, a.update.dialog.restart)
	deadline := time.Now().Add(5 * time.Second)
	for f.quits == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		a.drain()
	}
	if f.quits != 1 {
		t.Error("retry after a failed install didn't restart")
	}

	// downloads off: found, nothing fetched; on starts it, off cancels it
	f2 := &fakeUpdater{release: make(chan error, 1)}
	a2 := updateApp(t, f2)
	a2.toggleUpdateDownloads()
	a2.updateFound(UpdateInfo{Tag: "v0.3.0"}, false)
	if a2.update.phase != updAvailable || f2.downloads.Load() != 0 {
		t.Errorf("downloads off: phase %d, %d downloads", a2.update.phase, f2.downloads.Load())
	}
	a2.toggleUpdateDownloads()
	if a2.update.phase != updDownloading {
		t.Errorf("downloads on: phase %d", a2.update.phase)
	}
	a2.toggleUpdateDownloads()
	if a2.update.phase != updAvailable {
		t.Errorf("downloads off again: phase %d", a2.update.phase)
	}
	if b := New(Options{ConfigDir: a2.opts.ConfigDir}, nil); b.updateDownloadsOn() {
		t.Error("download choice not saved")
	}
}

// The new presentation drawn in each phase, light and dark: the top nav's
// Help button (no dot while available or downloading, the dot once
// downloaded), the Help flexpane's Take action card (ready, installing,
// installer open, failed), the update dialog (with the huddle warning
// too) and the manual check's message boxes. With UPDATE_SHOTS_DIR set
// (the updater workflow's prefs-shots job) each is saved as a PNG.
func TestUpdateUIShots(t *testing.T) {
	f := &fakeUpdater{release: make(chan error, 1)}
	a := updateApp(t, f)
	shots := os.Getenv("UPDATE_SHOTS_DIR")
	big := image.Pt(760, 420)
	r := &ttRenderer{t: t, a: a, sz: big, dir: shots}
	if shots != "" {
		w, err := pixeltest.NewWindow(big.X, big.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		r.win = w
		defer w.Release()
	}
	u := &a.update
	info := UpdateInfo{Tag: "v0.1.0-alpha.15", URL: "https://github.com/some-du6e/clack/releases/tag/v0.1.0-alpha.15"}
	set := func(p updatePhase, err string) {
		u.info, u.phase, u.err = info, p, err
	}
	// the top nav's right end with the Help button (chrome.go's layout)
	nav := func(gtx layout.Context) {
		c := &a.th.C
		theme.Fill(gtx, image.Rect(0, 0, 240, 44), c.ChromeB)
		b := gtx.Dp(26)
		defer op.Offset(image.Pt(240-gtx.Dp(8)-b, (44-b)/2)).Push(gtx.Ops).Pop()
		navHelp{a: a, b: b}.layout(gtx, nil)
	}
	pane := func(gtx layout.Context) {
		atExact(gtx, image.Point{}, image.Pt(380, 260), (&helpPane{a: a}).Layout)
	}
	overlays := func(gtx layout.Context) {
		for _, o := range a.overlays {
			if !o.Dismissed() {
				o.Layout(gtx)
			}
		}
	}
	closeAll := func() {
		for _, o := range a.overlays {
			if d, ok := o.(dismisser); ok {
				d.Dismiss()
			}
		}
		a.overlays = nil
	}
	shot := func(name string, w func(gtx layout.Context), h int) {
		t.Helper()
		r.frame(w) // first frame: text shaping, slackc styles settle
		r.save(name, r.frame(w), h)
	}
	for _, dark := range []bool{false, true} {
		a.setDark(dark)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, s := range []struct {
			name  string
			phase updatePhase
			err   string
			dot   bool
			card  string
		}{
			{"available", updAvailable, "", false, ""},
			{"downloading", updDownloading, "", false, ""},
			{"ready", updReady, "", true, "Click to update to a new version of the app."},
			{"installing", updInstalling, "", true, "Installing Clack v0.1.0-alpha.15…"},
			{"installer-open", updManual, "", true, "Finish installing in the window that opened, then restart Clack."},
			{"install-failed", updFailed, "update: moving the old copy aside: permission denied", true, "Couldn’t install: update: moving the old copy aside: permission denied"},
		} {
			set(s.phase, s.err)
			if a.updatePending() != s.dot {
				t.Errorf("%s: dot %v", s.name, a.updatePending())
			}
			shot("nav-"+s.name+"-"+mode, nav, 44)
			if s.dot {
				if _, desc := a.updateCardText(); desc != s.card {
					t.Errorf("%s: card %q, want %q", s.name, desc, s.card)
				}
				shot("help-pane-"+s.name+"-"+mode, pane, 260)
			}
		}
		set(updReady, "")
		a.openAppUpdateDialog()
		shot("dialog-"+mode, overlays, big.Y)
		on := true
		huddleOverride = &on
		shot("dialog-in-huddle-"+mode, overlays, big.Y)
		huddleOverride = nil
		closeAll()
		a.updateMessageBox("You’re all good", "You’ve got the latest version of Clack; thanks for staying on the ball.")
		shot("box-up-to-date-"+mode, overlays, big.Y)
		closeAll()
		a.updateMessageBox("Couldn’t update Clack", updateErrText(offline, time.Now()))
		shot("box-check-failed-"+mode, overlays, big.Y)
		closeAll()
	}
}

// The whole window against the fake Slack (internal/fakeslack), light and
// dark: an update downloading (no dot), downloaded (the Help dot), Help's
// Take action card, and the restart dialog. UPDATE_SHOTS_DIR only (the
// updater workflow's prefs-shots job, under xvfb).
func TestUpdateAppShots(t *testing.T) {
	dir := os.Getenv("UPDATE_SHOTS_DIR")
	if dir == "" {
		t.Skip("UPDATE_SHOTS_DIR not set")
	}
	t.Setenv("CLACK_BENCH_GPU", "1")
	e, _ := sectionEnv(t)
	a := e.a
	if s := sectionOfType(a, "channels"); s != nil && len(s.Rows) > 0 {
		a.Navigate("c/" + s.Rows[0])
	}
	e.settle(800 * time.Millisecond)
	u := &a.update
	u.info = UpdateInfo{Tag: "v0.1.0-alpha.15", URL: "https://github.com/some-du6e/clack/releases/tag/v0.1.0-alpha.15"}
	closeAll := func() {
		for _, o := range a.overlays {
			if d, ok := o.(dismisser); ok {
				d.Dismiss()
			}
		}
		a.overlays = nil
		a.CloseSecondary()
		e.frame()
	}
	for _, dark := range []bool{false, true} {
		a.th.SetDark(dark)
		a.Invalidate()
		e.settle(6 * time.Second) // llvmpipe frames are slow; tokens restyle over several
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		shot := func(name string) {
			e.frame()
			e.frame()
			e.frame()
			e.shotTo(t, dir, "app-"+name+"-"+mode)
		}
		u.phase = updDownloading
		shot("1-downloading")
		u.phase = updReady
		shot("2-ready-help-dot")
		a.helpClick()
		if a.secondary.kind != secHelp {
			t.Fatal("Help didn't open its pane")
		}
		shot("3-help-take-action")
		a.openAppUpdateDialog()
		shot("4-restart-dialog")
		closeAll()
	}
}
