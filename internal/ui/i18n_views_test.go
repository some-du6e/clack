package ui

import (
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
)

// The rail's list views, a canvas and a list in the "no English left"
// checks and the locale screenshots (docs/I18N.md): each drawn in en-US,
// then in Slack's es-ES and ja-JP bundles, against the fake's views,
// canvas and list fixtures. The huddle UI has its own checks
// (internal/ui/huddleui TestHuddleNoEnglishLeft).

// i18nView is one view, by route, and the strings of the fake's data it
// draws that happen to be Slack keys (drawn as the account wrote them in
// every locale, as Slack does).
type i18nView struct {
	name, route string
	data        []string
}

var i18nViews = []i18nView{
	{"threads", "threads", nil},
	{"activity", "activity", nil},
	{"unreads", "unreads", nil},
	{"dms", "dms", nil},
	{"files", "files", nil},
	{"todos", "later", nil},
	{"saved", "saved", nil},
	{"drafts", "drafts", nil},
	{"drafts-scheduled", "drafts?tab=scheduled", nil},
	{"directories-people", "directories", nil},
	{"directories-channels", "directories?tab=channels", nil},
	{"directories-groups", "directories?tab=groups", nil},
	{"huddles", "huddles", nil},
	{"canvas", "docs/" + fakeslack.CanvasChannel, nil},
	{"list", "lists/" + fakeslack.ListKitchen, []string{ // the kitchen-sink list's columns, options and view
		"All items", "Blocked", "Channel", "Contact", "Created by", "Docs", "Due date", "Engineering", "Files",
		"In progress", "Item", "Last edited", "Last edited by", "Link", "Owner", "Phone", "Status", "Tags", "Thread",
	}},
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
	"Project tracker":     true, // the fake's list template (lists.templates data, drawn as the API names it)
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
	// no locale pref: every snapshot re-applies the account's prefs, and
	// an "en-US" one would switch the locale back between the passes
	// (applyLocalePref follows the pref when it differs from i18n.Wanted)
	fs.SetPrefs(map[string]any{"locale": ""})
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	for _, v := range i18nViews {
		data := map[string]bool{}
		for _, s := range v.data {
			data[s] = true
		}
		allow := unkeyedWith(mainUnkeyed, viewsUnkeyed, data)
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
