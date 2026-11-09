// Package uipref is the UI's snapshot of the account prefs that change how
// things render: Slack's users.prefs (store.Prefs) as last installed by the
// app. Render paths in every UI package read it without locking the store
// (time format, names, jumbomoji, link underlines, inline media…).
// Gen changes whenever a pref that affects text layout does, so cached
// layouts know to redo themselves.
package uipref

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

var (
	cur atomic.Pointer[store.Prefs]
	gen atomic.Uint32
)

// def is Slack's defaults, used until the app installs real prefs.
var def = store.Prefs{Jumbomoji: true, ConvertEmoticons: true, ShowTyping: true, ColorNames: true}

// Set installs p (the app calls it at start and on every "prefs" event).
func Set(p store.Prefs) {
	old := Get()
	cp := p
	cur.Store(&cp)
	if layoutKey(old) != layoutKey(&p) {
		gen.Add(1)
	}
}

// Reset goes back to Slack's defaults (tests).
func Reset() {
	cur.Store(nil)
	gen.Add(1)
}

// Get returns the current prefs.
func Get() *store.Prefs {
	if p := cur.Load(); p != nil {
		return p
	}
	return &def
}

// Gen is the layout generation: it changes when a pref that changes laid
// out text (jumbomoji, swatches, link underlines, inline media) does.
func Gen() uint32 { return gen.Load() }

type lkey struct {
	jumbo, underline, swatch, noInt, noImg, noText, real, t24 bool
	large, dense                                              bool // obey_inline_img_limit, messages_theme
}

func layoutKey(p *store.Prefs) lkey {
	return lkey{p.Jumbomoji, p.UnderlineLinks, p.HideHexSwatch, p.NoInternalImgs, p.NoInlineImgs, p.NoTextPreviews, p.DisplayRealNames, p.Time24,
		p.LargeImgs, p.Dense}
}

// Clock is a message time the way Slack shows it: "3:04 PM", or "15:04"
// with time24 (Messages & media > "Show times with 24-hour clock").
func Clock(t time.Time) string {
	if Get().Time24 {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// ShortClock is the hover time of a grouped row: "3:04", or "15:04".
func ShortClock(t time.Time) string {
	if Get().Time24 {
		return t.Format("15:04")
	}
	return t.Format("3:04")
}

// Layout is a time.Format layout with "3:04 PM" swapped for "15:04"
// under time24 (date + time strings like "Mon Jan 2 at 3:04 PM").
func Layout(l string) string {
	if !Get().Time24 {
		return l
	}
	for _, f := range [...]string{"3:04:05 PM", "3:04 PM", "3:04PM"} {
		if i := strings.Index(l, f); i >= 0 {
			r := "15:04"
			if f == "3:04:05 PM" {
				r = "15:04:05"
			}
			return l[:i] + r + l[i+len(f):]
		}
	}
	return l
}
