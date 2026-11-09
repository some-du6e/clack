package ui

import (
	"context"
	"errors"
	"image"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
		DownloadUpdate: func(ctx context.Context, u UpdateInfo) error {
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

// bannerFrame lays the banner out (and handles its clicks) like App.frame.
func bannerFrame(a *App) {
	a.drain()
	gtx := ttContext(new(op.Ops), image.Pt(1200, 60))
	if a.updateBannerShown() {
		a.layoutUpdateBanner(gtx, image.Rect(0, 0, 1200, 40))
	}
	a.drain()
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

func TestUpdateBannerStates(t *testing.T) {
	f := &fakeUpdater{release: make(chan error, 1)}
	a := updateApp(t, f)
	a.updateFound(UpdateInfo{Tag: "v0.2.0", URL: "https://github.com/x"}, false)
	// downloading in the background: no banner yet
	if a.update.phase != updDownloading || a.updateBannerShown() {
		t.Fatalf("while downloading: phase %d, shown %v", a.update.phase, a.updateBannerShown())
	}
	// the same release found again doesn't download twice
	a.updateFound(UpdateInfo{Tag: "v0.2.0"}, false)
	f.release <- nil
	waitPhase(t, a, updReady)
	if n := f.downloads.Load(); n != 1 {
		t.Errorf("%d downloads", n)
	}
	msg, buttons := a.updateBannerText()
	if msg != "Clack v0.2.0 is ready to install" || strings.Join(buttons, "|") != "Install and restart|Later" || !a.updateBannerShown() {
		t.Errorf("ready banner %q %v", msg, buttons)
	}
	// frames go by, the release is found again: nothing installs without the click
	for i := 0; i < 5; i++ {
		bannerFrame(a)
	}
	a.updateFound(UpdateInfo{Tag: "v0.2.0"}, false)
	bannerFrame(a)
	if n := f.installs.Load(); n != 0 || a.update.phase != updReady {
		t.Fatalf("installed %d times without the click (phase %d)", n, a.update.phase)
	}
	// Later: hidden for this session, still not installed
	a.update.delay.Click()
	bannerFrame(a)
	if a.updateBannerShown() || f.installs.Load() != 0 {
		t.Errorf("after Later: shown %v, installs %d", a.updateBannerShown(), f.installs.Load())
	}
	// next launch: offered again
	a.update.later = false
	// Install and restart: installs once, then quits
	a.update.install.Click()
	bannerFrame(a)
	deadline := time.Now().Add(5 * time.Second)
	for f.quits == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		a.drain()
	}
	if f.installs.Load() != 1 || f.quits != 1 {
		t.Errorf("after the click: %d installs, %d quits", f.installs.Load(), f.quits)
	}
}

func TestUpdateBannerFallbacks(t *testing.T) {
	// a failed (or refused: bad checksum, no package) download shows the release page
	f := &fakeUpdater{release: make(chan error, 1)}
	a := updateApp(t, f)
	a.updateFound(UpdateInfo{Tag: "v0.2.0"}, false)
	f.release <- errors.New("update: the download doesn't match the release's SHA256SUMS")
	waitPhase(t, a, updAvailable)
	if msg, b := a.updateBannerText(); msg != "A new version of Clack is available: v0.2.0" || strings.Join(b, "|") != "View release" || !a.updateBannerShown() {
		t.Errorf("after a failed download: %q %v", msg, b)
	}
	// clicking "Install" is impossible here (no button) and does nothing if forced
	a.installUpdate()
	if f.installs.Load() != 0 {
		t.Error("installed an unverified update")
	}
	// × hides it until a newer release
	a.update.dismiss.Click()
	bannerFrame(a)
	if a.updateBannerShown() || a.prefs.UpdateDismissed != "v0.2.0" {
		t.Error("× didn't dismiss")
	}

	// downloads off: the release page only, nothing fetched
	f2 := &fakeUpdater{release: make(chan error, 1)}
	a2 := updateApp(t, f2)
	a2.toggleUpdateDownloads()
	if a2.updateDownloadsOn() {
		t.Fatal("toggle")
	}
	a2.updateFound(UpdateInfo{Tag: "v0.3.0"}, false)
	if a2.update.phase != updAvailable || f2.downloads.Load() != 0 {
		t.Errorf("downloads off: phase %d, %d downloads", a2.update.phase, f2.downloads.Load())
	}
	// turning it on starts the download; off again cancels it
	a2.toggleUpdateDownloads()
	if a2.update.phase != updDownloading {
		t.Errorf("downloads on: phase %d", a2.update.phase)
	}
	a2.toggleUpdateDownloads()
	if a2.update.phase != updAvailable {
		t.Errorf("downloads off again: phase %d", a2.update.phase)
	}
	// checks off: no banner at all
	a2.toggleUpdateChecks()
	if a2.updateBannerShown() {
		t.Error("banner with checks off")
	}

	// the system installer opened instead, or the install failed
	f3 := &fakeUpdater{release: make(chan error, 2), installErr: ErrUpdateManual}
	a3 := updateApp(t, f3)
	a3.updateFound(UpdateInfo{Tag: "v0.4.0"}, false)
	f3.release <- nil
	waitPhase(t, a3, updReady)
	a3.update.install.Click()
	bannerFrame(a3)
	waitPhase(t, a3, updManual)
	if msg, _ := a3.updateBannerText(); !strings.HasPrefix(msg, "Finish installing Clack v0.4.0") || f3.quits != 0 {
		t.Errorf("manual: %q, %d quits", msg, f3.quits)
	}
	f3.installErr = errors.New("update: moving the old copy aside: permission denied")
	a3.update.phase = updReady
	a3.update.install.Click()
	bannerFrame(a3)
	waitPhase(t, a3, updFailed)
	if msg, b := a3.updateBannerText(); !strings.Contains(msg, "permission denied") || strings.Join(b, "|") != "View release" || f3.quits != 0 {
		t.Errorf("failed: %q %v", msg, b)
	}
	// saved prefs keep the download choice
	if b := New(Options{ConfigDir: a2.opts.ConfigDir}, nil); b.updateDownloadsOn() {
		t.Error("download choice not saved")
	}
}
