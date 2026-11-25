package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
