package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The plugin Library refreshes itself (Karim 2026-11-23: "should auto
// refresh idk"). The Plugins pane shows the index Clack has at once; when
// that copy is past libraryTTL the pane fetches it again and shows what's
// new with no click, and a fresh copy sends nothing. The window coming to
// the front refreshes a stale copy in the background, and an open pane
// shows what a refresh it didn't start brought. A failed refresh keeps
// the copy on screen, sends no request per frame (it backs off), and
// says nothing unless Refresh asked.
func TestLibraryRefreshesStale(t *testing.T) {
	// the published release has LibSlick; the copy on disk is the one
	// before it, age old
	setup := func(t *testing.T, age time.Duration) (*libServer, *App) {
		t.Helper()
		ls := newLibServer(t)
		ls.publish(libTestPlugin("one"))
		var ix pluginlib.Index
		ls.mu.Lock()
		err := json.Unmarshal(ls.files["index.json"], &ix)
		ls.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		var before []pluginlib.Entry
		for _, e := range ix.Plugins {
			if e.ID != "LibSlick" {
				before = append(before, e)
			}
		}
		ix.Plugins = before
		a, _ := ttApp(t, t.TempDir())
		b, _ := json.Marshal(ix)
		dir := filepath.Join(a.opts.ConfigDir, "cache", "plugin-library")
		p := filepath.Join(dir, "index.json")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return ls, a
	}
	// pane opens Preferences > Plugins (its Installed list); draw draws a
	// frame of it and returns its text
	pane := func(t *testing.T, a *App) (*prefsModal, func() string) {
		t.Helper()
		pm := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
		pm.selectTab(pluginsTabID)
		t.Cleanup(pm.close)
		r := &ttRenderer{t: t, a: a, sz: image.Pt(800, 1000)}
		return pm, func() string {
			var b strings.Builder
			theme.DrawHook = func(s string) { b.WriteString(s + "\n") }
			defer func() { theme.DrawHook = nil }()
			r.frame(func(gtx layout.Context) { pm.layout(gtx) })
			return b.String()
		}
	}
	has := func(ix *pluginlib.Index, id string) bool {
		if ix != nil {
			for _, e := range ix.Plugins {
				if e.ID == id {
					return true
				}
			}
		}
		return false
	}
	until := func(t *testing.T, what string, draw func() string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			draw()
			time.Sleep(10 * time.Millisecond)
		}
	}

	t.Run("stale copy, pane opens", func(t *testing.T) {
		ls, a := setup(t, time.Hour)
		pm, draw := pane(t, a)
		draw()
		lp := pm.plugins.lib
		if pm.plugins.mode != "installed" {
			t.Fatalf("the pane opened on %q, not its Installed list", pm.plugins.mode)
		}
		if lp.ix == nil || has(lp.ix, "LibSlick") {
			t.Fatalf("the pane didn't show the copy on disk at once (%d entries)", len(libraryEntries(lp.ix)))
		}
		if !lp.loading {
			t.Fatal("a copy an hour old opened without a request")
		}
		until(t, "the fresh index", draw, func() bool { return !lp.loading && has(lp.ix, "LibSlick") })
		if n := ls.asked("index.json"); n != 1 {
			t.Errorf("the index was asked for %d times, want 1", n)
		}
		// Browse lists the new plugin with no click and no second request
		pm.plugins.setMode("browse")
		var text string
		for range 5 {
			text = draw()
		}
		if !strings.Contains(text, "Lib Slick") {
			t.Errorf("Browse doesn't list the new plugin:\n%s", text)
		}
		if n := ls.asked("index.json"); n != 1 {
			t.Errorf("after Browse the index was asked for %d times, want 1", n)
		}
	})

	t.Run("fresh copy", func(t *testing.T) {
		ls, a := setup(t, time.Minute)
		pm, draw := pane(t, a)
		for range 5 {
			draw()
		}
		pm.plugins.setMode("browse")
		var text string
		for range 5 {
			text = draw()
		}
		lp := pm.plugins.lib
		if n := ls.asked("index.json"); n != 0 || lp.loading {
			t.Fatalf("a minute-old copy sent %d requests (loading %v)", n, lp.loading)
		}
		if lp.ix == nil || has(lp.ix, "LibSlick") || strings.Contains(text, "Lib Slick") || !strings.Contains(text, "Another Example") {
			t.Fatalf("Browse doesn't show the copy on disk:\n%s", text)
		}
		// a refresh the pane didn't start (the hourly check's): the open
		// pane shows what it brought
		lib := a.pluginLib()
		if !lib.Refresh(libraryTTL, true, func(*pluginlib.Index, error) { a.Invalidate() }) {
			t.Fatal("a forced refresh didn't start")
		}
		until(t, "the background refresh on screen", draw, func() bool { return !lib.Refreshing() && has(lp.ix, "LibSlick") })
		if lp.loading {
			t.Error("the pane counts a refresh it didn't start as its own")
		}
		until(t, "Browse's new row", draw, func() bool { return strings.Contains(draw(), "Lib Slick") })
		// Refresh still asks, whatever the copy's age
		lp.load(true)
		if !lp.loading {
			t.Fatal("Refresh sent no request for a fresh copy")
		}
		until(t, "Refresh's answer", draw, func() bool { return !lp.loading })
		if n := ls.asked("index.json"); n != 2 || lp.loadErr != "" {
			t.Errorf("after Refresh: %d requests (want 2), error %q", n, lp.loadErr)
		}
	})

	t.Run("window focus", func(t *testing.T) {
		ls, a := setup(t, time.Hour)
		lib := a.pluginLib()
		a.setFocused(false)
		a.setFocused(true) // the window comes to the front: a stale copy is fetched again
		deadline := time.Now().Add(30 * time.Second)
		for ls.asked("index.json") == 0 || lib.Refreshing() {
			if time.Now().After(deadline) {
				t.Fatal("focusing the window didn't refresh an hour-old copy")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !has(lib.Cached(), "LibSlick") {
			t.Fatal("the background refresh didn't keep the new index")
		}
		// fresh now: the next focus, or the hourly check, sends nothing
		a.refreshLibrary()
		if lib.Refreshing() || ls.asked("index.json") != 1 {
			t.Errorf("a fresh copy was fetched again (%d requests)", ls.asked("index.json"))
		}
		// and the pane opens on it, no request
		pm, draw := pane(t, a)
		draw()
		if lp := pm.plugins.lib; !has(lp.ix, "LibSlick") || lp.loading {
			t.Errorf("the pane didn't open on the refreshed copy (loading %v)", lp.loading)
		}
		if n := ls.asked("index.json"); n != 1 {
			t.Errorf("opening the pane after the focus's refresh asked %d times, want 1", n)
		}
	})

	t.Run("failure backs off", func(t *testing.T) {
		ls, a := setup(t, time.Hour)
		ls.mu.Lock()
		delete(ls.files, "index.json") // the release answers 404
		ls.mu.Unlock()
		pm, draw := pane(t, a)
		draw()
		lp := pm.plugins.lib
		until(t, "the failed refresh", draw, func() bool { return !lp.loading })
		pm.plugins.setMode("browse")
		var text string
		for range 20 {
			text = draw()
		}
		if n := ls.asked("index.json"); n != 1 {
			t.Errorf("a failing release was asked %d times in 20 frames, want 1 (backoff)", n)
		}
		if lp.loadErr != "" || !strings.Contains(text, "Another Example") {
			t.Errorf("a failed background refresh: error %q, or the copy isn't shown:\n%s", lp.loadErr, text)
		}
		// Refresh isn't held back, and says why it failed
		lp.load(true)
		until(t, "Refresh's failure", draw, func() bool { return !lp.loading })
		if n := ls.asked("index.json"); n != 2 || !strings.Contains(lp.loadErr, "404") {
			t.Errorf("Refresh on a failing release: %d requests (want 2), error %q", n, lp.loadErr)
		}
		if !strings.Contains(draw(), "404") {
			t.Error("Browse doesn't show Refresh's error")
		}
	})
}
