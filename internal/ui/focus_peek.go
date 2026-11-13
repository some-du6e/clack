package ui

// The control strip's focus mode button and its popover, as Slack draws
// and drives them (web client build fa938a086569, boot-render
// SilentSidebarTrigger / SilentSidebarTriggerButton, boot-shared 20Sq
// PeekTrigger, client-boot-styles):
//
//   - Off, the button is wrapped in a Tooltip ("Turn on focus mode / Pause
//     notifications and reduce badging and bolding.", position right). On,
//     it's wrapped in a PeekTrigger instead (renderContent
//     FocusModePopoverContent, position right-top, offsetX 12, offsetY 15,
//     triggerDelay 300, forceOpenOnClick). The two branches are different
//     trees, so turning focus mode on or off remounts the trigger: the
//     pointer already resting on it counts as coming onto it once the
//     page under it changes.
//   - PeekTrigger: coming onto the trigger starts the 300 ms delay
//     (peeksEnabledOnMouseEnter, off in the simplified layout); leaving
//     before it's up cancels it. The hover peek has no scrim and doesn't
//     take the keyboard focus. While it's open a document mousemove,
//     throttled to one per 30 ms (lodash throttle, leading and trailing),
//     closes it once the pointer is outside the safe area: the content
//     (and what it opened: the Duration select's menu, the date picker,
//     the time select are React children of the card, so their portals
//     count as inside it), the trigger, then the union of trigger and card
//     until there is a previous position, after that the triangle from the
//     previous position to the card's outermost corners (railPeekSafe).
//     Escape closes it; while the pointer stays on the button it opens
//     again only after the pointer moves on it twice (ep / eh). A
//     non-primary or Ctrl press on the trigger closes it.
//   - A click on the button runs toggleFocusMode: it turns focus mode off
//     (the peek goes with the PeekTrigger branch) or on.
//   - The button: .p-control_strip__circle_button (36px, radius 18,
//     base-pry with content-ter while --active) whose background, colour
//     and transform transition for .125s cubic-bezier(.17,.67,.55,1.09)
//     (this rule comes after circleButton__cMiUK's, so its timing wins);
//     :hover scales it to 1.1. Around it buttonContainer__BXOiH tilts it
//     -15° (transform .2s ease-in-out) unless idle__FcMfs: idle unless
//     hovered (its onMouseEnter / onMouseLeave) or the focus mode toggle's
//     view transition is running from this button (focus_fade.go). The
//     two ai-sparkle-alt-filled stars (20px boxes at top 1 right 4 and top
//     6 right 1, scale .2 and .3) fade in from opacity 0 and scale .1 /
//     .2 with it (.2s ease-in-out).
//   - Not ported: the education coachmark and its 12 s glow (glowPulse),
//     the calendar sync (calendar_focus_sync) force-open, the narrow
//     client's 24px buttons.

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	focusPeekDelay    = 300 * time.Millisecond // SilentSidebarTrigger's triggerDelay
	focusPeekThrottle = 30 * time.Millisecond  // PeekTrigger's throttled document mousemove
)

// stripEase is .p-control_strip__circle_button's transition timing,
// cubic-bezier(.17,.67,.55,1.09).
func stripEase(x float64) float64 { return cubicBezierY(.17, .67, .55, 1.09, x) }

// cssEaseInOut is CSS's ease-in-out, cubic-bezier(.42,0,.58,1).
func cssEaseInOut(x float64) float64 { return cubicBezierY(.42, 0, .58, 1, x) }

// cssTransition is one CSS transition of a number: a new target starts
// it from the value it has now; heading back to where it started runs
// for the share already covered (CSS Transitions' reversing shortening).
type cssTransition struct {
	from, to float64
	at       time.Time
	dur      time.Duration
	ok       bool
}

// value is the property at now with target as its computed value; snap
// jumps there (no transition).
func (t *cssTransition) value(now time.Time, target float64, dur time.Duration, ease func(float64) float64, snap bool) float64 {
	if !t.ok || snap {
		*t = cssTransition{from: target, to: target, at: now, ok: true}
		return target
	}
	if target != t.to {
		cur := t.sample(now, ease)
		d := dur
		if target == t.from && t.to != t.from {
			d = time.Duration(float64(dur) * math.Min(1, math.Abs(cur-t.from)/math.Abs(t.to-t.from)))
		}
		t.from, t.to, t.at, t.dur = cur, target, now, d
	}
	return t.sample(now, ease)
}

func (t *cssTransition) sample(now time.Time, ease func(float64) float64) float64 {
	if t.dur <= 0 {
		return t.to
	}
	x := float64(now.Sub(t.at)) / float64(t.dur)
	if x >= 1 {
		return t.to
	}
	return t.from + (t.to-t.from)*ease(math.Max(0, x))
}

// running reports whether the transition still moves at now.
func (t *cssTransition) running(now time.Time) bool {
	return t.ok && t.dur > 0 && now.Before(t.at.Add(t.dur))
}

// colorTransition is a cssTransition of an opaque colour (sRGB, channels
// clamped when the timing overshoots).
type colorTransition struct {
	from, to color.NRGBA
	p        cssTransition // 0 at from, 1 at to
}

func (t *colorTransition) value(now time.Time, target color.NRGBA, dur time.Duration, ease func(float64) float64, snap bool) color.NRGBA {
	if !t.p.ok || snap {
		t.from, t.to = target, target
		t.p.value(now, 1, dur, ease, true)
		return target
	}
	if target != t.to {
		cur := t.sample(now, ease)
		t.from, t.to = cur, target
		t.p = cssTransition{from: 0, to: 0, at: now, ok: true}
		t.p.value(now, 1, dur, ease, false)
	}
	return t.sample(now, ease)
}

func (t *colorTransition) sample(now time.Time, ease func(float64) float64) color.NRGBA {
	k := t.p.sample(now, ease)
	l := func(a, b uint8) uint8 {
		v := float64(a) + (float64(b)-float64(a))*k
		return uint8(math.Max(0, math.Min(255, math.Round(v))))
	}
	return color.NRGBA{R: l(t.from.R, t.to.R), G: l(t.from.G, t.to.G), B: l(t.from.B, t.to.B), A: l(t.from.A, t.to.A)}
}

func (t *colorTransition) running(now time.Time) bool { return t.p.running(now) }

// focusPeekState is the PeekTrigger's state (in focusMode).
type focusPeekState struct {
	mounted   bool // the PeekTrigger branch (focus mode on) was drawn last frame
	hov       bool // the pointer was on the trigger last frame
	enterAt   time.Time
	prev      image.Point // the last checked pointer position (safe triangle)
	hasPrev   bool
	dismissed bool // ep: closed by Escape with the pointer on the button
	rearm     bool // eh: the first move after that; the next one starts the delay
	lastPtr   image.Point

	checkedAt  time.Time   // the throttle's last call
	pending    image.Point // a move inside the throttle window, checked at its end
	hasPending bool

	// the button's transitions
	scale, tilt, stars cssTransition
	bg, fg             colorTransition
}

// focusPeekOpen reports whether the popover is showing.
func (a *App) focusPeekOpen() bool {
	f := a.fm()
	return f.pop != nil && !f.pop.Dismissed()
}

// focusChildOpen reports whether an overlay the card opened is showing.
func (a *App) focusChildOpen() bool {
	f := a.fm()
	return f.child != nil && !f.child.Dismissed()
}

// focusButton draws the control strip's focus mode button at org (window
// coordinates; the strip's own) cs wide, with its tooltip or popover.
// Clicks toggle focus mode.
func (a *App) focusButton(gtx layout.Context, org image.Point, cs int) {
	f := a.fm()
	if f.wasActive != a.focusModeActive() || f.wasActive && f.wasInv != a.focusInverted() {
		a.focusChanged()
	}
	active := a.focusModeActive()
	if exp := a.focusExp(); active && exp != focusForever {
		gtx.Execute(op.InvalidateCmd{At: time.Unix(exp, 0)}) // it ends by itself
	}
	f.btnOrigin = org
	trig := image.Rectangle{Min: org, Max: org.Add(image.Pt(cs, cs))}
	f.trig = trig
	c := &a.th.C
	now := gtx.Now
	fading := a.focusFading(now)
	reduce := a.reduceMotion()
	p := &f.peek
	if active != p.mounted { // Tooltip ⇄ PeekTrigger: a new trigger under the pointer
		p.mounted, p.hov, p.enterAt = active, false, time.Time{}
		p.dismissed, p.rearm, p.hasPrev, p.hasPending = false, false, false, false
	}
	at(gtx, org, image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
		// :hover and buttonContainer's onMouseEnter; while the toggle's
		// view transition runs its pseudo-elements take the pointer
		hov := f.btn.Hovered() && !fading
		nonIdle := hov || fading // idle__FcMfs off
		base := a.chromeBase(org.Add(image.Pt(cs/2, cs/2)))
		var bgT, fgT color.NRGBA
		switch {
		case active: // --active: base-pry, content-ter
			bgT, fgT = c.Bg, c.FgSubtle
		case hov: // :hover: content-inv-pry
			bgT, fgT = theme.Over(c.SearchHover, base), c.ChromeText
		default: // surf-inv-pry, content-inv-sec
			bgT = theme.Over(c.ChromeActive, base) // the rail scope's surf-inv-pry
			fgT = theme.Over(c.ChromeTextDim, bgT)
		}
		bg := p.bg.value(now, bgT, 125*time.Millisecond, stripEase, false)
		fg := p.fg.value(now, fgT, 125*time.Millisecond, stripEase, false)
		scaleT := 1.0
		if hov {
			scaleT = 1.1
		}
		tiltT := 0.0
		if nonIdle {
			tiltT = 1
		}
		scale := p.scale.value(now, scaleT, 125*time.Millisecond, stripEase, reduce)
		tilt := p.tilt.value(now, tiltT, 200*time.Millisecond, cssEaseInOut, reduce)
		stars := p.stars.value(now, tiltT, 200*time.Millisecond, cssEaseInOut, false)
		if p.bg.running(now) || p.fg.running(now) || p.scale.running(now) || p.tilt.running(now) || p.stars.running(now) {
			gtx.Execute(op.InvalidateCmd{}) // the transitions' frames, then none
		}
		ctr := f32.Pt(float32(cs)/2, float32(cs)/2)
		tr := f32.Affine2D{}.Rotate(ctr, float32(-15*tilt*math.Pi/180)).Mul(f32.Affine2D{}.Scale(ctr, f32.Pt(float32(scale), float32(scale))))
		ts := op.Affine(tr).Push(gtx.Ops) // the button only: its tooltip stays upright
		d := clickArea(gtx, &f.btn, func(gtx layout.Context, sz image.Point, _ bool) {
			if !active && bg == bgT && !hov { // settled: the strip's own chrome composite
				a.chrome(gtx, org, image.Rectangle{Max: sz}, sz.X/2, c.ChromeActive)
				return
			}
			theme.RRect(gtx, image.Rectangle{Max: sz}, sz.X/2, bg)
		}, func(gtx layout.Context) layout.Dimensions {
			icon := "moon"
			if active {
				icon = "moon-filled"
			}
			off := (cs - gtx.Dp(20)) / 2
			at(gtx, image.Pt(off, off), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 20, fg)
			})
			if stars > 0.001 {
				// starOne: a 20px box at top 1 right 4, scale .1 → .2;
				// starTwo at top 6 right 1, scale .2 → .3 (about each
				// box's centre)
				star := func(top, right unit.Dp, s0 float64) {
					h := float32(gtx.Dp(10))
					cx := float32(cs-gtx.Dp(right)) - h
					cy := float32(gtx.Dp(top)) + h
					k := float32(s0 + .1*stars)
					st := op.Affine(f32.Affine2D{}.Scale(f32.Pt(cx, cy), f32.Pt(k, k)).Mul(f32.Affine2D{}.Offset(f32.Pt(cx-h, cy-h)))).Push(gtx.Ops)
					icons.Get("ai-sparkle-alt-filled")(gtx, 20, fg)
					st.Pop()
				}
				ost := paint.PushOpacity(gtx.Ops, float32(stars))
				star(1, 4, .1)
				star(6, 1, .2)
				ost.Pop()
			}
			return layout.Dimensions{Size: image.Pt(cs, cs)}
		})
		ts.Pop()
		if !active {
			popups.Tooltip(gtx, a.th, &f.tip, hov, d.Size, popups.Right,
				"Turn on focus mode\nPause notifications and reduce badging and bolding.", "")
			return d
		}
		popups.Tooltip(gtx, a.th, &f.tip, false, d.Size, popups.Right, "", "")
		a.focusPeekFrame(gtx, hov)
		return d
	})
	if clicked(&f.btn) {
		a.toggleFocusMode()
	}
}

// focusPeekFrame runs the PeekTrigger for this frame: hov is whether the
// pointer is on the trigger.
func (a *App) focusPeekFrame(gtx layout.Context, hov bool) {
	f := a.fm()
	p := &f.peek
	open := a.focusPeekOpen()
	switch {
	case hov && !p.hov: // onMouseEnter
		if !p.dismissed && !open && a.peeksOnHover() {
			p.enterAt = gtx.Now
		}
	case !hov && p.hov: // onMouseLeave: a pending open is cancelled
		p.dismissed, p.rearm = false, false
		if !open {
			p.enterAt = time.Time{}
		}
	}
	p.hov = hov
	// the throttle's trailing call
	if open && p.hasPending {
		if due := p.checkedAt.Add(focusPeekThrottle); gtx.Now.Before(due) {
			gtx.Execute(op.InvalidateCmd{At: due})
		} else {
			p.hasPending = false
			p.checkedAt = gtx.Now
			a.focusPeekCheck(p.pending)
		}
	}
	if open || p.enterAt.IsZero() {
		return
	}
	if due := p.enterAt.Add(focusPeekDelay); gtx.Now.Before(due) {
		gtx.Execute(op.InvalidateCmd{At: due})
		return
	}
	p.enterAt = time.Time{}
	a.openFocusPopover(f.trig)
}

// focusPeekPointer is the PeekTrigger's document mousemove and the
// trigger's onMouseMove / onMouseDown (pe in window coordinates).
func (a *App) focusPeekPointer(pe pointer.Event) {
	f, ok := focusModes[a]
	if !ok || !f.peek.mounted {
		return
	}
	p := &f.peek
	pt := image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
	open := a.focusPeekOpen()
	if pe.Kind == pointer.Press && pt.In(f.trig) && open &&
		(pe.Buttons != pointer.ButtonPrimary || pe.Modifiers.Contain(key.ModCtrl)) {
		f.pop.Dismiss() // onMouseDown: forceOpenOnClick keeps only a plain click
		a.Invalidate()
		return
	}
	if pe.Kind != pointer.Move && pe.Kind != pointer.Drag {
		return
	}
	moved := pt != p.lastPtr
	p.lastPtr = pt
	if moved && pt.In(f.trig) && !open { // eD after an Escape
		switch {
		case p.dismissed:
			p.dismissed, p.rearm = false, true
		case p.rearm:
			p.rearm = false
			if a.peeksOnHover() {
				p.enterAt = time.Now()
				a.Invalidate()
			}
		}
	}
	if !open {
		return
	}
	now := time.Now()
	if !p.checkedAt.IsZero() && now.Sub(p.checkedAt) < focusPeekThrottle {
		p.pending, p.hasPending = pt, true
		a.Invalidate() // focusPeekFrame waits for the window's end
		return
	}
	p.checkedAt, p.hasPending = now, false
	a.focusPeekCheck(pt)
}

// focusPeekCheck is the throttled handler: close the hover peek when pt
// is outside the safe area, and remember pt as the previous position.
func (a *App) focusPeekCheck(pt image.Point) {
	f := a.fm()
	p := &f.peek
	if !a.focusPeekOpen() {
		return
	}
	safe := a.focusChildOpen() || f.pop.InsideCard(pt) || railPeekSafe(pt, f.trig, f.pop.Rect(), p.prev, p.hasPrev)
	if !safe {
		f.pop.Dismiss()
		a.Invalidate()
		return
	}
	p.prev, p.hasPrev = pt, true
}

// openFocusPopover opens "You’re in focus mode" by the button: a hover
// peek at right-top, offsetX 12, offsetY 15 (Popover's allowances 16 /
// 10, no tryOppositePosition), 320 wide (popover__oOIzS), radius 12
// (p-peek_card--as-card), animating in (p-peek_card--animate-in).
func (a *App) openFocusPopover(btn image.Rectangle) {
	f := a.fm()
	if !f.opened {
		f.subtitle = focusSubtitles[0] // getInitialSubtitle on the first open
	} else {
		f.subtitle = focusSubtitles[rand.Intn(len(focusSubtitles))] // getRandomSubtitle
	}
	f.opened = true
	a.closeOtherPeeks("focus") // one activePeekId
	var pop *popups.Popover
	pop = popups.NewPopover(a, a.focusCard, popups.PopoverOpts{
		Anchor: btn, Popover: "right-top", OffsetX: 12, OffsetY: 15, Width: 320, Radius: 12,
		Peek: true, AnimateIn: true, NoMotion: a.reduceMotion(),
		OnClose: func() {
			if f.pop != pop {
				return
			}
			p := &f.peek
			p.hasPrev, p.hasPending = false, false
			if p.hov { // eI: Escape with the pointer still on the button
				p.dismissed = true
			}
			if d, ok := f.child.(dismisser); ok && !f.child.Dismissed() {
				d.Dismiss() // the select's menu goes with the card
			}
			a.Invalidate()
		},
	})
	f.pop = pop
	f.peek.hasPrev, f.peek.hasPending, f.peek.checkedAt = false, false, time.Time{}
	a.ShowOverlay(pop)
}

// showFocusChild opens o from the card and keeps it as the card's child
// (inside the peek's safe area while it shows).
func (a *App) showFocusChild(o Overlay) {
	f := a.fm()
	f.child = o
	f.peek.hasPrev = false // after it closes, the union until a move
	a.ShowOverlay(o)
}
