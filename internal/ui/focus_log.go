package ui

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// The missed-frame log line: field evidence for "I was getting deselected"
// and "I have to click like 5 times". A frame whose tree leaves the
// conversation out (a plugin re-rendering around ClientPrimaryView, like
// Slick's Chat Tabs) is covered by keepBody's stand-in and KeepFocus; the
// line says how often each one stepped in, whether a focused composer kept
// its focus, how often Gio dropped the focus anyway (and whether
// restoreDroppedFocus got it back), where (the route, without a search
// query) and which plugins were patching the view. At most one line per
// focusLogEvery, the first one right away. Ids, counts and plugin ids
// only: no message text.

// focusLogEvery is the most often the line is written.
const focusLogEvery = 10 * time.Second

type focusLog struct {
	standIns int   // keepBody draws since the last line
	kept     int   // of them, with the route's composer focused
	held     int64 // frames KeepFocus held an editor that wasn't drawn
	dropped  int64 // focus drops Gio made anyway (restoreDroppedFocus)
	restored int64 // of them, given back
	route    Route // where the last stand-in was
	since    time.Time
	at       time.Time // the last line (zero: none yet)
}

// standIn counts one keepBody draw (UI goroutine).
func (f *focusLog) standIn(r Route, focused bool) {
	f.standIns++
	if focused {
		f.kept++
	}
	f.route = r
}

func (f *focusLog) pending() bool { return f.standIns > 0 || f.held > 0 || f.dropped > 0 }

// routeComposerFocused reports whether r's composer holds the key focus.
func (a *App) routeComposerFocused(gtx layout.Context, r Route) bool {
	cv := a.convos[r.Channel()+"/"+r.Thread()]
	if cv == nil || cv.comp == nil {
		return false
	}
	ft, ok := cv.comp.(interface{ FocusTag() any })
	return ok && gtx.Focused(ft.FocusTag())
}

// logMissedFrames runs at the end of every frame, after KeepFocus.
func (a *App) logMissedFrames(gtx layout.Context) {
	f := &a.missedFrameLog
	held, dropped, restored := composer.TakeFocusStats()
	f.held += held
	f.dropped += dropped
	f.restored += restored
	if !f.pending() {
		return
	}
	if f.since.IsZero() {
		f.since = gtx.Now
	}
	if !f.at.IsZero() && gtx.Now.Sub(f.at) < focusLogEvery {
		return
	}
	r := f.route
	if r == "" {
		r = a.router.Current()
	}
	line := f.line(r, gtx.Now.Sub(f.since), slackc.Patched("ClientPrimaryView"), slackc.Patched("RenderedViewContents"))
	*f = focusLog{at: gtx.Now}
	t, s := a.Taut(), a.Slick()
	go func() { // the plugin systems' locks: never on the UI goroutine
		log.Printf("%s; plugins running: %s", line, runningPlugins(t, s))
	}()
}

func (f *focusLog) line(r Route, over time.Duration, cpv, rvc bool) string {
	return fmt.Sprintf("ui: missed frames over %v: body missing from %d frame(s), drawn in its place (a focused composer kept its focus in %d); "+
		"KeepFocus held an undrawn editor's focus in %d frame(s); Gio dropped a composer's focus %d time(s) anyway (restored %d); "+
		"route %s; patched ClientPrimaryView=%v RenderedViewContents=%v",
		over.Round(100*time.Millisecond), f.standIns, f.kept, f.held, f.dropped, f.restored, r.logString(), cpv, rvc)
}

// logString is the route for a log line: no search query.
func (r Route) logString() string {
	if r == "" {
		return "(none)"
	}
	return noQuery(string(r))
}

// noQuery is s without what follows "?" (a search, what a link opens):
// for log lines.
func noQuery(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i] + "?…"
	}
	return s
}

// runningPlugins is "slick: chat-tabs; taut: none". Any goroutine.
func runningPlugins(t *taut.Taut, s *slick.Slick) string {
	list := func(ids []string) string {
		if len(ids) == 0 {
			return "none"
		}
		return strings.Join(ids, ", ")
	}
	return "slick: " + list(s.Running()) + "; taut: " + list(t.Running())
}
