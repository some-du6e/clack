package ui

import (
	"fmt"
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/applog"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The boot placeholder: Slack's client.
//
// Slack's page holds div.p-client_container_placeholder (a theme
// background, a sidebar block and a content panel; page.html's inline
// style) under div.p-client_container. client-boot's "Render Placeholder"
// hook (beforeDynamicImportsStart, renderBootPlaceholder) colours it from
// what the last session stored for the team ("Save channel sidebar and team
// switcher colors", renderComplete): the window gradient of the stored
// iaTheming (flat primary without window gradients), the sidebar block
// rgba(255,255,255,.9) (.11 for an inverted light sidebar,
// rgba(16,18,20,.6) dark) as wide as viewLayoutLastSidebarWidth, the
// content panel #fff (rgb(26,29,33) dark); with nothing stored it stays
// page.html's #bbbabb / #282828 (dark). React mounts the client over it in
// renderApp ("Render React"), after bootDataFinished ("Finish waiting for
// boot data"), or straight from the persisted store on a warm boot; inside
// the client the channel then shows its own loading state
// (c-channel_loading_state) until its history arrives.
//
// Clack draws that placeholder until its client would render the same way:
//
//   - core booted (users.conversations, counts, sections, prefs, stars…:
//     Slack's boot data), or a boot snapshot painted (a warm boot);
//   - the restored channel's header (tabs, bookmarks, member count), which
//     Slack's boot data carries in the channel object;
//   - the plugins enabled at start registered and their first renders
//     delivered: Taut patches Slack's components before its first render,
//     and those patches render synchronously with Slack's React; Clack's
//     plugins render on their own thread, so the placeholder waits until
//     their deliveries go quiet (pluginQuiet).
//
// Messages and images don't hold it (Slack shows its loading states).
// pluginCap bounds the plugin/header wait after boot. Only the real
// window (Run) draws it; tests drive App.frame directly.

const (
	pluginQuiet = 150 * time.Millisecond
	pluginCap   = 2 * time.Second
)

type revealState struct {
	inRun       bool
	done        bool
	bootAt      time.Time    // core booted (or the snapshot painted): pluginCap counts from here
	lastDeliver atomic.Int64 // unix ns of the last plugin tree delivery (taut_wire.go)
	deliveries  atomic.Int64 // plugin tree deliveries before the reveal
	roots       sync.Map     // root id -> deliveries (the reveal log line)
	wake        *time.Timer
	tag         int
	blocker     string // what the cover waited for last (the log line)
}

// revealBlocker names what the client still waits for ("" = render now).
func (a *App) revealBlocker(now time.Time) string {
	if a.login != nil || a.data == nil {
		return ""
	}
	if a.th.Shaper == nil {
		return "fonts"
	}
	b := a.data
	if b.core.SignedOut() {
		return ""
	}
	if b.booted.IsZero() && !b.warm {
		return "boot data"
	}
	if a.rv.bootAt.IsZero() {
		a.rv.bootAt = now
	}
	if now.Sub(a.rv.bootAt) > pluginCap {
		return ""
	}
	if ch := a.router.Current().Channel(); ch != "" && !b.tabsDone[ch] {
		return "channel header"
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

// revealFrame runs after Run laid out the frame: it renders the client
// when nothing blocks, else draws Slack's boot placeholder over it and
// looks again soon. UI goroutine.
func (a *App) revealFrame(gtx layout.Context) {
	if a.rv.done {
		return
	}
	now := time.Now()
	blk := a.revealBlocker(now)
	if blk == "" {
		a.rv.done = true
		why := "ready"
		if !a.rv.bootAt.IsZero() && now.Sub(a.rv.bootAt) > pluginCap {
			why = "cap, was waiting for " + a.rv.blocker
		}
		n, roots, busiest, most := a.rv.deliveries.Load(), 0, uint64(0), int64(0)
		a.rv.roots.Range(func(k, v any) bool {
			roots++
			if c := v.(*atomic.Int64).Load(); c > most {
				busiest, most = k.(uint64), c
			}
			return true
		})
		applog.Mark("reveal", fmt.Sprintf("%s; %d plugin deliveries from %d roots (most: root %d, %d)", why, n, roots, busiest, most))
		a.sw.lastChange = now
		return
	}
	if blk != a.rv.blocker {
		applog.Debugf("startup: boot placeholder, waiting for %s", blk)
		a.rv.blocker = blk
	}
	sz := gtx.Constraints.Max
	area := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	event.Op(gtx.Ops, &a.rv.tag) // the client under it takes no pointer input
	a.drawBootPlaceholder(gtx)
	area.Pop()
	for {
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

// drawBootPlaceholder is renderBootPlaceholder over page.html's
// .p-client_container_placeholder styles (see the comment at the top).
// Clack's window has no workspace switcher, so there's no tab-rail block:
// the sidebar starts at --tab-rail-width.
func (a *App) drawBootPlaceholder(gtx layout.Context) {
	sz := gtx.Constraints.Max
	dark := a.th.Dark
	stored := a.acct.set // ui.json's acct_theme: what the last session saved
	var side, content color.NRGBA
	if stored {
		a.grad.layout(gtx, a.th.C.ChromeA, a.th.C.ChromeB) // the stored iaTheming's window background
		switch {
		case dark:
			side = color.NRGBA{R: 16, G: 18, B: 20, A: 153} // rgba(16,18,20,0.6)
		case a.acct.t.SidebarInverted:
			side = color.NRGBA{R: 255, G: 255, B: 255, A: 28} // rgba(255,255,255,0.11)
		default:
			side = color.NRGBA{R: 255, G: 255, B: 255, A: 230} // rgba(255,255,255,0.9)
		}
		content = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		if dark {
			content = color.NRGBA{R: 26, G: 29, B: 33, A: 255} // rgb(26, 29, 33)
		}
	} else { // nothing stored: page.html's defaults (.sk-boot-theme--dark for dark)
		bg := color.NRGBA{R: 0xbb, G: 0xba, B: 0xbb, A: 255}
		side = color.NRGBA{R: 255, G: 255, B: 255, A: 230}
		content = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		if dark {
			bg = color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 255}
			side = color.NRGBA{R: 16, G: 18, B: 20, A: 153}
			content = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 255}
		}
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: sz}.Op())
	}
	rail, top, pad := gtx.Dp(theme.RailW), gtx.Dp(theme.TopNavH), gtx.Dp(theme.PanelInset)
	sw := gtx.Dp(unit.Dp(a.sidebar.width)) // viewLayoutLastSidebarWidth
	r6 := gtx.Dp(6)
	sideR := image.Rect(rail, top, rail+sw, sz.Y-pad)
	paint.FillShape(gtx.Ops, side, clip.RRect{Rect: sideR, NW: r6, SW: r6}.Op(gtx.Ops))
	contR := image.Rect(rail+sw, top, sz.X-pad, sz.Y-pad)
	if contR.Dx() > 0 && contR.Dy() > 0 {
		// box-shadow: 0 4px rgba(0,0,0,.05), 0 0 20px rgba(0,0,0,.1)
		theme.Shadow(gtx, contR, r6, gtx.Dp(20), color.NRGBA{A: 26})
		paint.FillShape(gtx.Ops, color.NRGBA{A: 13}, clip.RRect{Rect: contR.Add(image.Pt(0, gtx.Dp(4))), NE: r6, SE: r6}.Op(gtx.Ops))
		paint.FillShape(gtx.Ops, content, clip.RRect{Rect: contR, NE: r6, SE: r6}.Op(gtx.Ops))
	}
}

// revealed reports whether the window shows the real UI (always, outside Run).
func (a *App) revealed() bool { return !a.rv.inRun || a.rv.done }

// noteDelivery counts a plugin tree delivery (any goroutine).
func (a *App) noteDelivery(root uint64) {
	a.rv.lastDeliver.Store(time.Now().UnixNano())
	a.rv.deliveries.Add(1)
	v, _ := a.rv.roots.LoadOrStore(root, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}
