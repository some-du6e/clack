package composer

import (
	"image"
	"strings"
	"time"
	"weak"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The hint bar's formatting tip and offline notice: NotificationBar
// (boot-shared V6lB) under the box.
//
//   - The right section (.p-notification_bar__section--right) holds the
//     formatting tip while the left one is empty: "Shift + Enter to add a
//     new line" (renderFormattingTips, hintSource). It is there from the
//     start at opacity 0 and turns visible (--is-visible: opacity 1,
//     pointer-events auto) once the message is past two characters as
//     Slack counts them (MessagePaneInput's hasMoreThanTwoCharacters:
//     getMessageLengthForDelta > 2, slackLength) and Slack is recently
//     online; opacity moves over .2s ease-in-out both ways. It's a
//     ButtonLink (c-link--button, underlined on hover) in an "Input
//     options" tooltip (top), and a click opens Preferences on Advanced
//     at its input options (openPrefsModal {activeSection: "advanced",
//     activeSubsection: "input-options"}).
//   - Not recently online (Vdrr TP wasRecentlyOnline: online, or the
//     client's connection changed under 10s ago), the left section says
//     "Slack is trying to connect." ("Slack is trying to connect, so
//     messages can’t be sent yet." past two characters), a space and the
//     cloud-offline-filled SvgIcon inline (1em, vertical-align -.125em),
//     and the formatting tip isn't there.
//
// Not ported: the stash hint ("Message stashed — Ctrl+S to restore", the
// message_stash experiment the account isn't in), the announcer's
// "Slack is trying to connect." every 30s, and the media rules hiding the
// tip in a narrow window (width <= 545px, or <= 920px with a flexpane).

// recentlyOnline is wasRecentlyOnline for the hint bar: until is when a
// lost connection stops counting as recent (zero while online). Tests
// replace it.
var recentlyOnline = func(h Host) (on bool, until time.Time) {
	if h == nil {
		return true, time.Time{}
	}
	a := h.Core()
	if a == nil {
		return true, time.Time{}
	}
	return a.RecentlyOnline()
}

// crHint is the formatting tip's state: its opacity transition, the
// ButtonLink and its tooltip.
type crHint struct {
	mounted bool
	on      bool      // the transition's target: --is-visible
	from    float32   // the opacity it started at
	start   time.Time // when it started
	dur     time.Duration
	btn     widget.Clickable
	tip     popups.TipState
	rect    image.Rectangle // the ButtonLink's, composer-local ("" while it takes no pointer)
	// graceFor is the end of the grace a frame is due at, graceTimer the
	// frame's timer (layoutFormattingTip)
	graceFor   time.Time
	graceTimer *time.Timer
}

// hintFadeDur is .p-notification_bar__section--right's transition.
const hintFadeDur = 200 * time.Millisecond

// unmount drops the right section: the left one has something (a new one
// starts at its target, untransitioned).
func (h *crHint) unmount() { h.mounted = false }

// opacity moves the transition to on at now and returns the opacity.
// Reversing one midway runs from where it got to, for that part of the
// duration (CSS's reversing shortening factor).
func (h *crHint) opacity(now time.Time, on bool) (a float32, moving bool) {
	if !h.mounted {
		h.mounted, h.on, h.dur = true, on, 0
	}
	if on != h.on {
		cur, _ := h.at(now)
		h.on, h.from, h.start = on, cur, now
		to := float32(0)
		if on {
			to = 1
		}
		d := to - cur
		if d < 0 {
			d = -d
		}
		h.dur = time.Duration(float32(hintFadeDur) * d)
	}
	return h.at(now)
}

func (h *crHint) at(now time.Time) (float32, bool) {
	to := float32(0)
	if h.on {
		to = 1
	}
	if h.dur <= 0 || !now.Before(h.start.Add(h.dur)) {
		return to, false
	}
	x := float64(now.Sub(h.start)) / float64(h.dur)
	return h.from + (to-h.from)*float32(attBezier(.42, 0, .58, 1, max(x, 0))), true
}

// layoutFormattingTip draws the right section: the tip right-aligned 8px
// inside the box, at ty.
func (c *chrome) layoutFormattingTip(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, box image.Rectangle, ty, maxW int) {
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	vis := st.Online && st.MoreThanTwo
	// Frames come from the host, never an InvalidateCmd mid-layout (it
	// requeues the frame's unread events: a press after a click got lost
	// that way, gio-midframe-invalidate-loses-clicks)
	if st.Online && !st.OnlineUntil.IsZero() && !st.OnlineUntil.Equal(c.hint.graceFor) {
		c.hint.graceFor = st.OnlineUntil // refreshRecentlyOnline: a frame when the grace ends
		if c.hint.graceTimer != nil {
			c.hint.graceTimer.Stop()
		}
		// stopped at Close; weak, so a composer dropped without one isn't
		// kept (nor its session) until the grace ends
		wc := weak.Make(c)
		c.hint.graceTimer = time.AfterFunc(time.Until(st.OnlineUntil), func() {
			if c := wc.Value(); c != nil {
				c.f.h.Invalidate()
			}
		})
	}
	alpha, moving := c.hint.opacity(now, vis)
	if moving {
		c.f.h.Invalidate() // the fade's next frame
	}
	if vis && c.hint.btn.Clicked(gtx) && popups.OpenPreferencesSection != nil {
		popups.OpenPreferencesSection("advanced")
	}
	c.hint.rect = image.Rectangle{}
	if alpha <= 0 {
		return
	}
	// rt("<b>Shift + Enter</b> to add a new line") (SyrM, the
	// notification_bar I18n): the keys bold, wherever the locale has them
	pref := uipref.Get()
	src := hintSource(shortcut.GOOS, pref.SendOnModEnter, pref.EnterIsSpecial, st.HintInPre)
	type seg struct {
		call op.CallOp
		w    int
		gap  bool // a space before it
	}
	var segs []seg
	total, space, base := 0, false, 0
	col := crFade(p.hint, p.bg, alpha) // the section's opacity over the pane
	for _, pt := range notificationBarNS.RT(src) {
		txt := strings.TrimSpace(pt.Text)
		gap := space || strings.HasPrefix(pt.Text, " ")
		space = strings.HasSuffix(pt.Text, " ")
		if txt == "" {
			space = space || gap
			continue
		}
		s := textStyle{Size: 10.5, LineHeight: 16, Color: col, MaxLines: 1}
		if pt.Tag == "b" {
			s.Weight = theme.Bold
		}
		call, d := crRec(gtx, th.Shaper, maxW, s, txt)
		gap = gap && len(segs) > 0
		segs = append(segs, seg{call, d.Size.X, gap})
		total += d.Size.X
		if gap {
			total += c.space(gtx, th.Shaper, 2)
		}
		base = max(base, d.Size.Y-d.Baseline)
	}
	x0 := box.Max.X - gtx.Dp(8) - total
	x := x0
	for _, sg := range segs {
		if sg.gap {
			x += c.space(gtx, th.Shaper, 2)
		}
		t := op.Offset(image.Pt(x, ty)).Push(gtx.Ops)
		sg.call.Add(gtx.Ops)
		t.Pop()
		x += sg.w
	}
	if !vis {
		return // pointer-events: none
	}
	// the ButtonLink: underlined under the pointer (text-decoration, the
	// richview underline's offset and thickness at this size)
	r := image.Rect(x0, ty, x0+total, ty+gtx.Dp(16))
	c.hint.rect = r
	hovered := c.hint.btn.Hovered()
	if hovered {
		em := float32(gtx.Sp(10.5))
		uy := ty + base + max(1, int(em/8))
		fillRect(gtx, image.Rect(x0, uy, x0+total, uy+max(1, int(em/14))), col)
	}
	t := op.Offset(r.Min).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(r.Size())
	c.hint.btn.Layout(g, crPointerArea)
	popups.Tooltip(gtx, th, &c.hint.tip, hovered, r.Size(), popups.Above, notificationBarNS.T("Input options"), "")
	t.Pop()
}

// layoutOffline draws the left section while Slack isn't recently online,
// its text from at.
func (c *chrome) layoutOffline(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, at image.Point, maxW int) {
	txt := notificationBarNS.T("Slack is trying to connect.")
	if st.MoreThanTwo {
		txt = notificationBarNS.T("Slack is trying to connect, so messages can’t be sent yet.")
	}
	em := gtx.Sp(10.5)
	sp := c.space(gtx, th.Shaper, 2)
	call, d := crRec(gtx, th.Shaper, max(maxW-sp-em, 1), textStyle{Size: 10.5, LineHeight: 16, Color: p.typing, MaxLines: 1}, txt)
	t := op.Offset(at).Push(gtx.Ops)
	call.Add(gtx.Ops)
	base := d.Size.Y - d.Baseline
	it := op.Offset(image.Pt(d.Size.X+sp, base+em/8-em)).Push(gtx.Ops)
	icons.Get("cloud-offline-filled")(gtx, unit.Dp(10.5), p.typing)
	it.Pop()
	t.Pop()
}
