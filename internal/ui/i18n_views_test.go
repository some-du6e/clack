package ui

import (
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
)

// The rail's list views in the "no English left" checks and the locale
// screenshots (docs/I18N.md): each view drawn in en-US, then in Slack's
// es-ES and ja-JP bundles, against the fake's views fixtures.

// i18nView is one view, by route.
type i18nView struct{ name, route string }

var i18nViews = []i18nView{
	{"threads", "threads"},
	{"activity", "activity"},
	{"unreads", "unreads"},
	{"dms", "dms"},
	{"files", "files"},
	{"todos", "later"},
	{"saved", "saved"},
	{"drafts", "drafts"},
	{"drafts-scheduled", "drafts?tab=scheduled"},
	{"directories-people", "directories"},
	{"directories-channels", "directories?tab=channels"},
	{"directories-groups", "directories?tab=groups"},
	{"huddles", "huddles"},
}

// i18nViewsSize is Small with the views' fixtures: followed threads,
// drafts and scheduled messages, the activity feed, unread channels and
// the files gallery.
func i18nViewsSize() fakeslack.Size {
	sz := fakeslack.Small
	sz.Views, sz.Unreads, sz.Files = true, true, true
	return sz
}

// viewsUnkeyed are strings the views draw unchanged in es-ES and ja-JP
// that Slack draws in English too: its component's namespace lacks the
// key (another namespace has it), or Slack writes the string out in
// English. docs/I18N.md lists them.
var viewsUnkeyed = map[string]bool{
	"Search for channels": true, // BrowseChannelsView (search): no search key in the bundles
}

func unkeyedWith(lists ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, l := range lists {
		for k, v := range l {
			out[k] = v
		}
	}
	return out
}

// viewsNoEnglishLeft is TestMainNoEnglishLeft's views part: every view's
// strings in en-US against es-ES and ja-JP. As in the main check, only
// strings Slack translates fail (the account's own text is drawn alike).
func viewsNoEnglishLeft(t *testing.T, cats map[string]*i18n.Catalog) {
	fs, err := fakeslack.Start(i18nViewsSize())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	allow := unkeyedWith(mainUnkeyed, viewsUnkeyed)
	for _, v := range i18nViews {
		i18n.Use(i18n.EnUS, nil)
		e.a.Navigate(v.route)
		e.settle(1500 * time.Millisecond) // the view's loads
		en := drawnStrings(e, 300*time.Millisecond)
		for _, loc := range []string{"es-ES", "ja-JP"} {
			i18n.Use(loc, cats[loc])
			got := drawnStrings(e, 300*time.Millisecond)
			if u, _ := englishLeft(cats[loc], en, got, allow); len(u) > 0 {
				reportLeft(t, loc+" "+v.name, u, nil)
			}
		}
	}
	i18n.Use(i18n.EnUS, nil)
}

// viewsLocaleShots is TestLocaleShots' views part: one screenshot of each
// view in loc (i18n-<loc>-view-<name>.png).
func viewsLocaleShots(t *testing.T, dir, loc string, cat *i18n.Catalog) {
	fs, err := fakeslack.Start(i18nViewsSize())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(map[string]any{"locale": loc})
	i18n.Use(loc, cat)
	e := startEnv(t, fs, t.TempDir())
	w, err := headless.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	e.gpu = w
	e.waitReady()
	for _, v := range i18nViews {
		e.a.Navigate(v.route)
		e.settle(1500 * time.Millisecond)
		e.shotTo(t, dir, "i18n-"+loc+"-view-"+v.name)
	}
}
