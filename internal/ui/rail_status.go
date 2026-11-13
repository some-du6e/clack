package ui

// The status tab over your avatar at the bottom of the control strip
// (boot-render jC7T User with showEmojiStatusIcon; web client build
// fa938a086569): while you have a status emoji (core.SelfStatusEmoji:
// T6Tm's currentStatusEmoji, the telescope while focus mode's automatic
// status is on) the user button shows .p-ia__nav__user__status_icon
// above the avatar. client-boot-styles:
//
//	.p-control_strip .p-ia__nav__user__status_icon {
//	  background-color: var(--dt_color-theme-surf-inv-pry);
//	  border-radius: 8px 8px 0 0; width: 36px; height: 32px;
//	  margin-top: -8px; padding: 4px 0 12px; position: relative;
//	  bottom: -8px; overflow: hidden; display: flex (column, centred);
//	  animation: .4s p-ia__nav__user__status_icon }
//	.--exiting { animation: .4s forwards p-ia__nav__user__status_icon--exit }
//	@keyframes p-ia__nav__user__status_icon {
//	  0% { height: 0; margin-top: 0; padding-top: 0; padding-bottom: 0;
//	       transform: translateY(24px) }
//	  to { height: 32px; margin-top: -8px; padding-top: 4px;
//	       padding-bottom: 12px; transform: translate(0) } }
//
// and the exit keyframes run the other way. So at progress e (the
// animation's ease timing, CSS's default) the tab is 32e tall, sits 24e
// higher in the strip (height − margin), and is drawn 8 + 24(1 − e) lower
// than that: it rises out from behind the avatar (.c-avatar is positioned
// and comes later, so it paints over the tab's bottom 8px) while the +
// and focus mode buttons above move up 24px with it. The emoji is a
// "small" status emoji (16px) 4e below the tab's top, clipped to it.
// useExitAnimation keeps the tab (with the emoji it last had) for the
// 400ms exit; the status coming back meanwhile mounts a new tab, which
// enters from the start. A change of emoji while it shows just swaps.
// Slack's CSS has no reduced-motion rule here; Clack shows and hides the
// tab without the slide when motion is reduced.

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// railStatusDur is the tab's animation (enter and exit).
const railStatusDur = 400 * time.Millisecond

// railStatusTab is the tab's state (UI goroutine).
type railStatusTab struct {
	name, url string    // the emoji drawn (kept through the exit)
	on        bool      // the status is set: shouldRender, not exiting
	exiting   bool      // the exit animation runs
	at        time.Time // the running animation's start (zero: start on the next frame)
}

var railStatusTabs = map[*App]*railStatusTab{} // UI goroutine only

func (a *App) railStatus() *railStatusTab {
	t := railStatusTabs[a]
	if t == nil {
		t = &railStatusTab{}
		railStatusTabs[a] = t
	}
	return t
}

// railStatusFrame updates the tab for this frame and returns its progress
// e (0 hidden … 1 fully in) and whether it's drawn at all.
func (a *App) railStatusFrame(gtx layout.Context) (e float64, shown bool) {
	t := a.railStatus()
	name, url := "", ""
	if c := a.Core(); c != nil {
		name, url = c.SelfStatusEmoji(gtx.Now)
	}
	want := name != ""
	switch {
	case want && !t.on: // mounts: enters from the start (an exit in progress included)
		t.on, t.exiting, t.at = true, false, gtx.Now
	case !want && t.on:
		t.on, t.exiting, t.at = false, true, gtx.Now
	}
	if want {
		t.name, t.url = name, url // O.current
	}
	if !t.on && !t.exiting {
		return 0, false
	}
	reduce := a.reduceMotion()
	x := float64(gtx.Now.Sub(t.at)) / float64(railStatusDur)
	if reduce || x >= 1 {
		if t.exiting {
			t.exiting = false
			return 0, false
		}
		return 1, true
	}
	gtx.Execute(op.InvalidateCmd{}) // the animation's frames, then none
	e = cssEase(max(0, x))
	if t.exiting {
		e = 1 - e
	}
	return e, true
}

// railStatusTabDraw draws the tab for an avatar whose top-left is av (window
// coordinates), cs wide, at progress e.
func (a *App) railStatusTabDraw(gtx layout.Context, av image.Point, cs int, e float64) {
	t := a.railStatus()
	c := &a.th.C
	h := float64(gtx.Dp(32)) * e
	top := float64(av.Y) - h + float64(gtx.Dp(8)) + float64(gtx.Dp(24))*(1-e)
	r := image.Rect(av.X, int(top+.5), av.X+cs, int(top+h+.5))
	if r.Dy() <= 0 {
		return
	}
	rad := gtx.Dp(8)
	cl := clip.RRect{Rect: r, NW: rad, NE: rad}.Op(gtx.Ops)
	a.grad.draw(gtx, a.winSize, image.Point{}, cl, c.ChromeA, c.ChromeB, c.SidebarButton) // surf-inv-pry, as the strip's buttons
	defer clip.Rect(r).Push(gtx.Ops).Pop()                                                // overflow: hidden
	es := gtx.Dp(16)
	ey := int(top + float64(gtx.Dp(4))*e + .5)
	url := t.url
	if url == "" {
		if c := a.Core(); c != nil {
			url, _, _ = c.Emoji(t.name)
		}
	}
	if url == "" {
		return
	}
	if img, ok := a.images.Get(url, es*2); ok {
		at(gtx, image.Pt(av.X+(cs-es)/2, ey), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
			return theme.Image(gtx, img, image.Pt(es, es), 0)
		})
	}
}
