package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui"
)

// α.82 was listed before its files were up: the check must not call a
// newer release available (and the UI download it) until this install's
// package and SHA256SUMS are uploaded. Until then it's ui.ErrUpdatePending,
// which the UI checks again in minutes.
func TestCheckWaitsForAssets(t *testing.T) {
	var reply atomic.Pointer[desktop.Release]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]desktop.Release{*reply.Load()})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CLACK_UPDATE_URL", srv.URL)
	t.Setenv("CLACK_UPDATE_AS", "v0.1.0-alpha.81")
	now := time.Date(2026, 11, 15, 3, 27, 30, 0, time.UTC)
	u := &updates{inst: desktop.Install{Kind: desktop.KindMacApp, GOOS: "darwin", GOARCH: "arm64"}}
	const tag = "v0.1.0-alpha.82"
	dmg := desktop.Asset{Name: "Clack-0.1.0-alpha.82-macos-universal.dmg", URL: srv.URL + "/dmg", Size: 71816075}
	sums := desktop.Asset{Name: "SHA256SUMS", URL: srv.URL + "/sums", Size: 1133}
	check := func(r desktop.Release) (ui.UpdateInfo, bool, error) {
		t.Helper()
		reply.Store(&r)
		return u.checkAt(context.Background(), now)
	}
	at := func(a desktop.Asset, state string) desktop.Asset { a.State = state; return a }
	published := now.Add(-20 * time.Second)

	for _, c := range []struct {
		what   string
		assets []desktop.Asset
	}{
		{"tagged, nothing uploaded", nil},
		{"the dmg still uploading", []desktop.Asset{at(dmg, "open"), at(sums, "uploaded")}},
		{"no SHA256SUMS yet", []desktop.Asset{at(dmg, "uploaded")}},
		{"SHA256SUMS still uploading", []desktop.Asset{at(dmg, "uploaded"), at(sums, "open")}},
	} {
		info, ok, err := check(desktop.Release{Tag: tag, Prerelease: true, PublishedAt: published, Assets: c.assets})
		if ok || !errors.Is(err, ui.ErrUpdatePending) || info.Tag != tag {
			t.Errorf("%s: ok %v, err %v, tag %q; want pending", c.what, ok, err, info.Tag)
		}
	}
	// uploaded: available, with the release for the download
	info, ok, err := check(desktop.Release{Tag: tag, Prerelease: true, PublishedAt: published, Assets: []desktop.Asset{at(dmg, "uploaded"), at(sums, "uploaded")}})
	if !ok || err != nil || info.Tag != tag {
		t.Fatalf("uploaded: ok %v, err %v, %+v", ok, err, info)
	}
	if r, _ := info.Release.(desktop.Release); len(r.Assets) != 2 {
		t.Errorf("the release handed to the download: %+v", info.Release)
	}
	// not newer: up to date, whatever its files
	if _, ok, err := check(desktop.Release{Tag: "v0.1.0-alpha.81", Prerelease: true}); ok || err != nil {
		t.Errorf("same version: ok %v, err %v", ok, err)
	}
	// no mac package an hour after publishing: missing for good, offered
	// as before (the release page; the download says why)
	if _, ok, err := check(desktop.Release{Tag: tag, Prerelease: true, PublishedAt: now.Add(-2 * time.Hour)}); !ok || err != nil {
		t.Errorf("long missing: ok %v, err %v", ok, err)
	}
	// an install with no package at all (a bare binary): offered as before
	u.inst = desktop.Install{Kind: desktop.KindUnknown, GOOS: "darwin"}
	if _, ok, err := check(desktop.Release{Tag: tag, Prerelease: true, PublishedAt: published}); !ok || err != nil {
		t.Errorf("unknown install: ok %v, err %v", ok, err)
	}
}

// Quitting installs only what stageForQuit staged, and only a quiet kind
// of install; anything else waits for Install and restart, and says why
// (the quit log claimed an install when nothing ran).
func TestInstallOnQuitNeedsAStage(t *testing.T) {
	info := ui.UpdateInfo{Tag: "v9.9.9"}
	u := &updates{inst: desktop.Install{Kind: desktop.KindDeb}, pkg: map[string]desktop.Package{}, plans: map[string]desktop.Plan{}}
	if err := u.installOnQuit(info); !errors.Is(err, errNotOnQuit) {
		t.Errorf("a .deb install: %v, want errNotOnQuit", err)
	}
	u.inst.Kind = desktop.KindMacApp
	u.pkg["v9.9.9"] = desktop.Package{Tag: "v9.9.9"}
	if err := u.installOnQuit(info); !errors.Is(err, errNotOnQuit) || !strings.Contains(err.Error(), "nothing staged") {
		t.Errorf("downloaded, not staged: %v, want errNotOnQuit (nothing staged)", err)
	}
}

// Windows' silent installer, the portable exe's swap and the AppImage's
// install on quit again, but never on a quit Windows' WM_ENDSESSION
// started (sign-out, shut down, restart, Restart Manager): Windows ends
// the process and the session's helper with it, and an installer killed
// halfway leaves Clack broken.
func TestNoInstallOnQuitAsTheSessionEnds(t *testing.T) {
	for _, why := range []string{
		"Windows is signing the user out " + desktop.SessionEndMark,
		"Windows is shutting down or restarting, forced " + desktop.SessionEndMark,
		"an installer or update is replacing Clack's files (Windows Restart Manager closed it) " + desktop.SessionEndMark + " (render: device lost)",
	} {
		if err := quitInstallBlocked(why, false); !errors.Is(err, errNotOnQuit) {
			t.Errorf("%q: %v, want errNotOnQuit", why, err)
		}
	}
	// the user's quits: Quit, the window closed, Ctrl+C
	for _, why := range []string{"Quit", "the window closed", "signal: interrupt"} {
		if err := quitInstallBlocked(why, false); err != nil {
			t.Errorf("%q: %v, want it to install", why, err)
		}
	}
	// a SIGTERM outside macOS is the session's end (signalQuit)
	if err := quitInstallBlocked("signal: terminated", true); !errors.Is(err, errNotOnQuit) {
		t.Errorf("a SIGTERM as the session ends: %v, want errNotOnQuit", err)
	}
	// the kinds that swap in silently go on quit once staged
	for _, k := range []desktop.InstallKind{desktop.KindMacApp, desktop.KindWinInstaller, desktop.KindWinPortable, desktop.KindAppImage} {
		u := &updates{inst: desktop.Install{Kind: k}, pkg: map[string]desktop.Package{}, plans: map[string]desktop.Plan{}}
		if err := u.installOnQuit(ui.UpdateInfo{Tag: "v9.9.9"}); !errors.Is(err, errNotOnQuit) || !strings.Contains(err.Error(), "nothing staged") {
			t.Errorf("%s, nothing staged: %v, want it to wait only for its stage", k, err)
		}
	}
}

// freshSession starts a test with the session not ending.
func freshSession(t *testing.T) {
	sessionEnding.Store(false)
	t.Cleanup(func() { sessionEnding.Store(false) })
}

// A session end that comes while a user's quit is already closing still
// installs nothing: Windows' WM_ENDSESSION (the tray's OnSessionEnd runs
// sessionEnds) during a Quit whose exit reason stays the user's, "Quit".
func TestNoInstallWhenTheSessionEndsDuringAQuit(t *testing.T) {
	freshSession(t)
	if err := quitInstall("Quit", nil); err != nil {
		t.Fatalf("a plain Quit: %v, want it to install", err)
	}
	var started []string
	sessionEnds("Windows is signing the user out "+desktop.SessionEndMark, func(why string) { started = append(started, why) })
	if len(started) != 1 {
		t.Errorf("the session's end started %d quits, want 1 (Clack's quit records the first reason)", len(started))
	}
	if err := quitInstall("Quit", nil); !errors.Is(err, errNotOnQuit) {
		t.Errorf("a session ending during a Quit: %v, want errNotOnQuit", err)
	}
}

// A signal's quit: SIGTERM and SIGHUP end a Linux session (and are Go's for
// a Windows console's sign-out and shut down), so nothing installs; on
// macOS they're a plain kill, which installs as it did before the
// Windows and AppImage kinds; Ctrl+C installs everywhere.
func TestSignalQuitMarksTheSessionsEnd(t *testing.T) {
	for _, c := range []struct {
		goos    string
		sig     os.Signal
		blocked bool
	}{
		{"linux", syscall.SIGTERM, true},
		{"linux", syscall.SIGHUP, true},
		{"linux", os.Interrupt, false},
		{"windows", syscall.SIGTERM, true},
		{"darwin", syscall.SIGTERM, false},
		{"darwin", syscall.SIGHUP, false},
		{"darwin", os.Interrupt, false},
	} {
		freshSession(t)
		var why string
		signalQuit(c.sig, c.goos, func(w string) { why = w })
		if want := "signal: " + c.sig.String(); why != want {
			t.Errorf("%s %v: the quit's reason %q, want %q", c.goos, c.sig, why, want)
		}
		if err := quitInstall(why, nil); errors.Is(err, errNotOnQuit) != c.blocked {
			t.Errorf("%s %v: %v, want blocked %v", c.goos, c.sig, err, c.blocked)
		}
	}
}

// macOS's log out, restart and shut down reach Clack as the quit Apple
// Event (desktop.QuitEndsSession reads its reason): nothing installs; the
// Dock's Quit installs.
func TestAppleQuitMarksTheSessionsEnd(t *testing.T) {
	for _, ends := range []bool{false, true} {
		freshSession(t)
		var why string
		appleQuit(ends, func(w string) { why = w })
		if !strings.HasPrefix(why, "macOS asked Clack to quit") {
			t.Errorf("ends %v: the quit's reason %q", ends, why)
		}
		if err := quitInstall(why, nil); errors.Is(err, errNotOnQuit) != ends {
			t.Errorf("a quit Apple Event that ends the session %v: %v", ends, err)
		}
	}
}

// A window that ended with an error installs nothing: on Wayland that is
// the compositor going as the session ends ("display file descriptor
// gone"), before any SIGTERM.
func TestWindowErrorInstallsNothing(t *testing.T) {
	freshSession(t)
	if err := quitInstall("the window closed (wayland: display file descriptor gone)", errors.New("wayland: display file descriptor gone")); !errors.Is(err, errNotOnQuit) {
		t.Errorf("the display went: %v, want errNotOnQuit", err)
	}
}
