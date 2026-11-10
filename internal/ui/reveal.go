package ui

import (
	"image"
	"sync/atomic"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/applog"
)

// The reveal: Slack's desktop client shows its loading screen until the
// client is booted, then the whole window at once. Clack does the same: from
// the first frame it lays out the real window (so images are requested,
// plugin roots mount and their React trees render, the channel's history
// and header load) under a cover in the account's chrome colours, and
// lifts the cover once nothing visible is still arriving:
//
//   - core booted (sidebar, counts, sections, stars) or a boot snapshot painted;
//   - the open channel's messages laid out and its header complete
//     (tabs, bookmarks, member count: opened());
//   - the plugins enabled at start started, and their renders quiet for
//     pluginQuiet;
//   - the visible images in, or revealImagesWait after the rest.
//
// revealCap bounds it: a slow network shows what's there instead of a
// cover. Only the real window (Run) covers; tests drive App.frame directly
// and never see one.

const (
	revealCap        = 3 * time.Second
	revealImagesWait = 400 * time.Millisecond
	pluginQuiet      = 150 * time.Millisecond
)

type revealState struct {
	inRun       bool
	done        bool
	readyAt     time.Time    // everything but images was ready
	lastDeliver atomic.Int64 // unix ns of the last plugin tree delivery (taut_wire.go)
	wake        *time.Timer
	tag         int
	blocker     string // what the cover waited for last (the log line)
}

// revealBlocker names what the window still waits for ("" = reveal now).
func (a *App) revealBlocker(now time.Time) string {
	if !a.opts.Started.IsZero() && now.Sub(a.opts.Started) > revealCap {
		return ""
	}
	if a.login != nil || a.data == nil || a.th.Shaper == nil {
		if a.th.Shaper == nil {
			return "fonts"
		}
		return ""
	}
	b := a.data
	if b.core.SignedOut() {
		return ""
	}
	if b.booted.IsZero() && !b.warm {
		return "core"
	}
	if ch := a.router.Current().Channel(); ch != "" {
		cv := a.convos[ch+"/"]
		if cv == nil {
			return "channel"
		}
		if l, ok := cv.list.(interface{ LayoutSig() (int, bool, uint64) }); ok {
			if _, loaded, _ := l.LayoutSig(); !loaded {
				return "messages"
			}
		}
		if l, ok := cv.list.(interface{ Loading() bool }); ok && l.Loading() {
			return "messages"
		}
		if !b.tabsDone[ch] {
			return "header"
		}
	}
	if b.tautStarting {
		return "plugins"
	}
	if b.taut != nil && !closed(b.taut.StartDone()) {
		return "plugins"
	}
	if b.slick != nil && !closed(b.slick.StartDone()) {
		return "plugins"
	}
	if last := a.rv.lastDeliver.Load(); last != 0 && now.Sub(time.Unix(0, last)) < pluginQuiet {
		return "plugin renders"
	}
	if a.rv.readyAt.IsZero() {
		a.rv.readyAt = now
	}
	if a.images.Pending() > 0 && now.Sub(a.rv.readyAt) < revealImagesWait {
		return "images"
	}
	return ""
}

func closed(c <-chan struct{}) bool {
	if c == nil {
		return false
	}
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// revealFrame runs after Run laid out the frame: it lifts the cover when
// nothing blocks, else draws it over everything and checks again soon.
// UI goroutine.
func (a *App) revealFrame(gtx layout.Context) {
	if a.rv.done {
		return
	}
	now := time.Now()
	blk := a.revealBlocker(now)
	if blk == "" {
		a.rv.done = true
		why := "ready"
		if !a.opts.Started.IsZero() && now.Sub(a.opts.Started) > revealCap {
			why = "cap, was waiting for " + a.rv.blocker
		}
		applog.Mark("reveal", why)
		a.sw.lastChange = now
		return
	}
	if blk != a.rv.blocker {
		applog.Debugf("startup: covered, waiting for %s", blk)
		a.rv.blocker = blk
	}
	// the cover: the window's chrome in the account's colours, taking
	// every pointer event (keys still reach whatever has focus)
	sz := gtx.Constraints.Max
	area := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	event.Op(gtx.Ops, &a.rv.tag)
	pointer.CursorDefault.Add(gtx.Ops)
	a.grad.layout(gtx, a.th.C.ChromeA, a.th.C.ChromeB)
	area.Pop()
	for { // drain the cover's pointer events
		if _, ok := gtx.Event(pointer.Filter{Target: &a.rv.tag, Kinds: pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Enter | pointer.Leave}); !ok {
			break
		}
	}
	if a.rv.wake == nil { // quiet periods end without an event: look again
		a.rv.wake = time.AfterFunc(40*time.Millisecond, func() {
			a.Post(func() { a.rv.wake = nil })
		})
	}
}

// revealed reports whether the window shows the real UI (always, outside Run).
func (a *App) revealed() bool { return !a.rv.inRun || a.rv.done }
