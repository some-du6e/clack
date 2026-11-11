package ui

import (
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// drawnStrings runs frames for d and returns every string theme.Text drew.
func drawnStrings(e *perfEnv, d time.Duration) []string {
	var mu sync.Mutex
	set := map[string]bool{}
	theme.DrawHook = func(s string) { mu.Lock(); set[s] = true; mu.Unlock() }
	e.settle(d)
	theme.DrawHook = nil
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// mainUnkeyed are strings the main surfaces draw unchanged that are the
// fake account's own data (its custom section names happen to be Slack
// keys too).
var mainUnkeyed = map[string]bool{"Projects": true, "Events": true}

// TestMainNoEnglishLeft: the sidebar, the rail, the top bar and a
// channel's message rows in es-ES and ja-JP. Only strings Slack
// translates fail here: the account's own text (names, messages) is
// drawn alike in every locale.
func TestMainNoEnglishLeft(t *testing.T) {
	cats := map[string]*i18n.Catalog{"es-ES": slackBundle(t, "es-ES"), "ja-JP": slackBundle(t, "ja-JP")}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
	// no locale pref: the app follows the account's (applyLocalePref, on
	// every snapshot), which would switch back to en-US mid-draw
	fs.SetPrefs(map[string]any{"locale": ""})
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fs.ChannelIDs()[0]
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	i18n.Use(i18n.EnUS, nil)
	en := drawnStrings(e, 300*time.Millisecond)
	for _, loc := range []string{"es-ES", "ja-JP"} {
		i18n.Use(loc, cats[loc])
		got := drawnStrings(e, 300*time.Millisecond)
		u, _ := englishLeft(cats[loc], en, got, mainUnkeyed)
		if len(u) > 0 {
			reportLeft(t, loc, u, nil)
		}
	}
	checkSurfaces(t, e, cats, ch) // the menus, dialogs and search (i18n_surfaces_test.go)
	viewsNoEnglishLeft(t, cats)   // the views, huddles, canvases and lists (i18n_views_test.go)
}

// TestLocaleShots: the client and Preferences > Language & region in
// es-ES and ja-JP (I18N_SHOTS_DIR; ja-JP checks the CJK fallback font).
func TestLocaleShots(t *testing.T) {
	dir := os.Getenv("I18N_SHOTS_DIR")
	if dir == "" {
		t.Skip("I18N_SHOTS_DIR not set")
	}
	for _, loc := range []string{"es-ES", "ja-JP"} {
		cat := slackBundle(t, loc)
		t.Run(loc, func(t *testing.T) {
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
			fs.SetPrefs(map[string]any{"locale": loc})
			i18n.Use(loc, cat)
			e := startEnv(t, fs, t.TempDir())
			w, err := headless.NewWindow(e.size.X, e.size.Y)
			if err != nil {
				t.Fatalf("headless window: %v", err)
			}
			e.gpu = w
			e.waitReady()
			ch := fs.ChannelIDs()[0]
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			e.settle(500 * time.Millisecond)
			e.shotTo(t, dir, "i18n-"+loc+"-channel")
			e.a.showPrefs("language_region")
			e.settle(500 * time.Millisecond)
			e.shotTo(t, dir, "i18n-"+loc+"-prefs-language")
			for e.a.dismissTop() {
			}
			e.a.showPrefs("notifications")
			e.settle(500 * time.Millisecond)
			e.shotTo(t, dir, "i18n-"+loc+"-prefs-notifications")
			shotSurfaces(t, e, dir, loc, ch) // the menus, dialogs and search (i18n_surfaces_test.go)
			for e.a.dismissTop() {
			}
			viewsLocaleShots(t, dir, loc, cat) // i18n_views_test.go
		})
	}
}
