package ui

// Slack's focus mode, which its code calls the "silent sidebar" (web
// client build a84662d2d135):
//
//   - It exists while the silent_sidebar experiment is on (kp78 Pn); the
//     control strip then shows SilentSidebarTrigger, a moon circle button
//     between Create (+) and your avatar (boot-render: Create, then
//     SilentSidebarTrigger). Off, it shows the tooltip "Turn on focus mode /
//     Pause notifications and reduce badging and bolding."
//   - Clicking it toggles (toggleFocusMode, GTtr): activateFocusMode writes
//     users.prefs focus_mode_expiration_ts = now + 30 min / 1 h / 2 h, or
//     9999999999 for "Don’t turn off" (VgYv, xG++), the duration last picked
//     (silentSidebar.focusModeExpirationOption, "no_expiration" at first);
//     deactivateFocusMode writes 0. Focus mode is on while that pref is in
//     the future (isFocusModeActiveOnServer; maybeUpdateFocusModeSideEffect
//     follows it from other clients); the server pauses notifications and
//     sets the "In focus mode" status from it (Availability's
//     focus_mode_status), which Clack sees through dnd.info / dnd_updated and
//     the profile like any other DND or status.
//   - While on, hovering the button (300 ms) opens FocusModePopoverContent:
//     "You’re in focus mode" with the preferences gear (under
//     availability_prefs_section), the banner (a random subtitle, "Notifications
//     paused" / "…until {time}"), the Duration select (writes the pref at
//     once) and "Continue to get notifications from VIPs"
//     (priority_dnd_override).
//   - The sidebar goes quiet (shouldSuppressUnreadEmphasis): unread rows
//     aren't bold and mention badges shrink to dots (Link, BaseChannelSuffix
//     shape "dot"); and the theme becomes the monochrome Silent Sidebar Gray
//     theme (KQp3 getSilentSidebarThemeDefinition), its sidebarInverted the
//     Appearance pref focus_mode_force_dark_mode_enabled (default on).

import (
	"context"
	"image"
	"image/color"
	"math"
	"math/rand"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// focusForever is "Don’t turn off" (xG++ r: 0x2540be3ff).
const focusForever = 9999999999

// focusOptions are the popover's durations (1Zjz Z) and labels.
var focusOptions = [...]struct{ key, label string }{
	{"half_hour", "30 minutes"},
	{"one_hour", "1 hour"},
	{"two_hours", "2 hours"},
	{"no_expiration", "Don’t turn off"},
}

// focusExpiration is getFocusModeExpirationTimestamp.
func focusExpiration(option string, now time.Time) int64 {
	switch option {
	case "half_hour":
		return now.Unix() + 1800
	case "one_hour":
		return now.Unix() + 3600
	case "two_hours":
		return now.Unix() + 7200
	}
	return focusForever
}

// focusSubtitles are getRandomSubtitle's (the first open: the first).
var focusSubtitles = [...]string{"Enjoy the quiet time", "May you avoid all distractions", "Don’t forget to remember…",
	"You are one with the focus", "Snacks are in your future", "Gaze up to the sky"}

// focusMode is one App's focus-mode UI state (Slack's silentSidebar store).
type focusMode struct {
	option     string // focusModeExpirationOption
	pending    bool   // a write in flight: pendingExp until it lands
	pendingExp int64
	wasActive  bool // last frame's state (theme refresh on change)
	wasInv     bool // and focus_mode_force_dark_mode_enabled

	btn       widget.Clickable
	tip       popups.TipState
	hoverAt   time.Time
	pop       *popups.Popover
	opened    bool // the popover was opened since focus mode started
	subtitle  string
	durBtn    widget.Clickable
	durRect   image.Rectangle // card coords
	durList   popups.Overlay
	vipBtn    widget.Clickable
	gearBtn   widget.Clickable
	gearTip   popups.TipState
	btnOrigin image.Point
}

var focusModes = map[*App]*focusMode{} // UI goroutine only

func (a *App) fm() *focusMode {
	f := focusModes[a]
	if f == nil {
		f = &focusMode{option: "no_expiration"}
		focusModes[a] = f
	}
	return f
}

// focusModeOn reports whether this account has focus mode (silent_sidebar).
func (a *App) focusModeOn() bool {
	c := a.Core()
	return c != nil && c.Experiment("silent_sidebar") == "on"
}

// focusExp is focus_mode_expiration_ts (or the value being written).
func (a *App) focusExp() int64 {
	f := a.fm()
	if f.pending {
		return f.pendingExp
	}
	c := a.Core()
	if c == nil {
		return 0
	}
	return int64(c.PrefInt("focus_mode_expiration_ts", 0))
}

// focusModeActive is isSilentSidebarActive: on, and the pref in the future.
func (a *App) focusModeActive() bool {
	return a.focusModeOn() && a.focusExp() > time.Now().Unix()
}

// focusQuiet is shouldSuppressUnreadEmphasis for the sidebar's rows.
func (a *App) focusQuiet() bool { return a.focusModeActive() }

// setFocusExp writes focus_mode_expiration_ts, optimistically (Slack
// updates its store first and puts the old value back on failure).
func (a *App) setFocusExp(v int64) {
	f := a.fm()
	f.pending, f.pendingExp = true, v
	a.focusChanged()
	go func() {
		err := a.writePref(context.Background(), "focus_mode_expiration_ts", int(v))
		a.Post(func() {
			f.pending = false
			if err != nil {
				a.Toast("Couldn't do that: " + err.Error())
			}
			a.focusChanged()
		})
	}()
}

// toggleFocusMode is toggleFocusMode: deactivate, or activate with the
// last duration.
func (a *App) toggleFocusMode() {
	if !a.focusModeOn() {
		return
	}
	f := a.fm()
	if a.focusModeActive() {
		if f.pop != nil {
			f.pop.Dismiss()
		}
		a.setFocusExp(0)
		return
	}
	f.opened = false
	a.setFocusExp(focusExpiration(f.option, time.Now()))
}

// focusChanged rebuilds the palette when focus mode turns on or off (the
// silent sidebar theme) and redraws.
func (a *App) focusChanged() {
	f := a.fm()
	on, inv := a.focusModeActive(), a.focusInverted()
	if on != f.wasActive || on && inv != f.wasInv {
		f.wasActive, f.wasInv = on, inv
		if !on && f.pop != nil {
			f.pop.Dismiss()
		}
		a.th.Refresh()
	}
	a.Invalidate()
}

// focusInverted is focus_mode_force_dark_mode_enabled (default true).
func (a *App) focusInverted() bool {
	if c := a.Core(); c != nil {
		return c.PrefBool("focus_mode_force_dark_mode_enabled", true)
	}
	return true
}

// focusTheme is the silent sidebar theme while focus mode is on (KQp3:
// GRAY, the default selectedSilentSidebarTheme), with sidebarInverted from
// focus_mode_force_dark_mode_enabled (default true).
func (a *App) focusTheme() (theme.IATheme, bool) {
	if !a.focusModeActive() {
		return theme.IATheme{}, false
	}
	inv := a.focusInverted()
	return theme.IATheme{
		Primary: theme.IARole{Palette: "gray"}, Highlight1: theme.IARole{Palette: "gray"},
		Highlight2: theme.IARole{Palette: "mojito"}, Important: theme.IARole{Palette: "gray"},
		Brightness: 6, SidebarInverted: inv,
	}, true
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
	c := &a.th.C
	at(gtx, org, image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
		hovered := f.btn.Hovered()
		// p-control_strip__circle_button; --active: base-pry with the
		// tertiary text colour
		bg, fg := c.SidebarButton, c.ChromeText
		if active {
			bg, fg = c.Bg, c.FgSubtle
		} else if hovered {
			bg = c.SearchHover
		}
		d := clickArea(gtx, &f.btn, func(gtx layout.Context, sz image.Point, _ bool) {
			if active {
				theme.RRect(gtx, image.Rectangle{Max: sz}, sz.X/2, bg)
				return
			}
			a.chrome(gtx, org, image.Rectangle{Max: sz}, sz.X/2, bg)
		}, func(gtx layout.Context) layout.Dimensions {
			icon := "moon"
			if active {
				icon = "moon-filled"
			}
			off := (cs - gtx.Dp(20)) / 2
			// buttonContainer: tilted -15° and two sparkles while hovered
			// (idle: upright, no sparkles)
			if hovered || active {
				ctr := f32.Pt(float32(cs)/2, float32(cs)/2)
				defer op.Affine(f32.Affine2D{}.Rotate(ctr, -15*math.Pi/180)).Push(gtx.Ops).Pop()
			}
			at(gtx, image.Pt(off, off), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 20, fg)
			})
			if hovered || active {
				at(gtx, image.Pt(cs-gtx.Dp(4)-gtx.Dp(8), gtx.Dp(1)), image.Pt(gtx.Dp(8), gtx.Dp(8)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("sparkle")(gtx, 8, fg)
				})
				at(gtx, image.Pt(cs-gtx.Dp(1)-gtx.Dp(6), gtx.Dp(6)+gtx.Dp(4)), image.Pt(gtx.Dp(6), gtx.Dp(6)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("sparkle")(gtx, 6, fg)
				})
			}
			return layout.Dimensions{Size: image.Pt(cs, cs)}
		})
		if !active {
			popups.Tooltip(gtx, a.th, &f.tip, hovered, d.Size, popups.Right,
				"Turn on focus mode\nPause notifications and reduce badging and bolding.", "")
			f.hoverAt = time.Time{}
			return d
		}
		popups.Tooltip(gtx, a.th, &f.tip, false, d.Size, popups.Right, "", "")
		// the popover: on a 300 ms hover (triggerDelay) or a click
		if hovered && (f.pop == nil || f.pop.Dismissed()) {
			if f.hoverAt.IsZero() {
				f.hoverAt = gtx.Now
			}
			if open := f.hoverAt.Add(300 * time.Millisecond); gtx.Now.Before(open) {
				gtx.Execute(op.InvalidateCmd{At: open})
			} else {
				a.openFocusPopover(image.Rectangle{Min: org, Max: org.Add(image.Pt(cs, cs))})
			}
		} else if !hovered {
			f.hoverAt = time.Time{}
		}
		return d
	})
	if clicked(&f.btn) {
		a.toggleFocusMode()
	}
}

// openFocusPopover opens "You’re in focus mode" to the right of the button
// (RightTop, offset 12/15).
func (a *App) openFocusPopover(btn image.Rectangle) {
	f := a.fm()
	if !f.opened {
		f.subtitle = focusSubtitles[0] // getInitialSubtitle on the first open
	} else {
		f.subtitle = focusSubtitles[rand.Intn(len(focusSubtitles))]
	}
	f.opened = true
	// RightTop: the card's top at the button's, clamped into the window
	f.pop = popups.NewPopover(a, a.focusCard, popups.PopoverOpts{Anchor: btn, Side: popups.Right, Gap: 12, Width: 320, Radius: 12})
	a.ShowOverlay(f.pop)
}

// focusSubLabel is the banner's second line (QBEZ zv without the silent
// sidebar copy): "Notifications paused", or until a time.
func focusSubLabel(exp int64, now time.Time) string {
	if exp <= 0 || exp == focusForever {
		return "Notifications paused"
	}
	t := time.Unix(exp, 0).In(now.Location())
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	ty, tm, td := now.AddDate(0, 0, 1).Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "Notifications paused until " + uipref.Clock(t)
	case y1 == ty && m1 == tm && d1 == td:
		return "Notifications paused until tomorrow at " + uipref.Clock(t)
	}
	return "Notifications paused until " + t.Format("Jan 2") + " at " + uipref.Clock(t)
}

// focusCard draws the popover's content (320 wide).
func (a *App) focusCard(gtx layout.Context) layout.Dimensions {
	f := a.fm()
	th := a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	if !a.focusModeActive() {
		if f.pop != nil {
			f.pop.Dismiss()
		}
		return layout.Dimensions{Size: image.Pt(w, 1)}
	}
	if clicked(&f.durBtn) {
		a.openFocusDurations()
	}
	if clicked(&f.vipBtn) {
		if cr := a.Core(); cr != nil {
			v := !cr.PrefBool("priority_dnd_override", false)
			go func() { _ = a.writePref(context.Background(), "priority_dnd_override", v); a.Invalidate() }()
		}
	}
	if clicked(&f.gearBtn) {
		f.pop.Dismiss()
		a.openPrefsSection("availability")
	}
	y := 0
	// p-peek_card__header: 44 tall, 15/900, padding 0 8 0 16, a 1px shadow
	hh := gtx.Dp(44)
	at(gtx, image.Pt(gtx.Dp(16), 0), image.Pt(w, hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Black, c.Fg, "You’re in focus mode")
		})
	})
	if cr := a.Core(); cr != nil && cr.Experiment("availability_prefs_section") == "on" {
		bs := gtx.Dp(28)
		gp := image.Pt(w-gtx.Dp(8)-bs, (hh-bs)/2)
		at(gtx, gp, image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			d := iconButton(gtx, &f.gearBtn, "settings", 28, 18, c.FgMuted, c.BgHover, 4)
			popups.Tooltip(gtx, th, &f.gearTip, f.gearBtn.Hovered(), d.Size, popups.Above, "Focus mode preferences", "")
			return d
		})
	}
	theme.RRect(gtx, image.Rect(0, hh, w, hh+max(1, gtx.Dp(1))), 0, theme.WithAlpha(c.Fg, 0x21))
	y = hh
	// statusBanner: padding 12 16, gap 12, the 64px telescope and the text
	by := y + gtx.Dp(12)
	at(gtx, image.Pt(gtx.Dp(16), by), image.Pt(gtx.Dp(64), gtx.Dp(64)), func(gtx layout.Context) layout.Dimensions {
		focusTelescope(gtx, gtx.Dp(64))
		return layout.Dimensions{Size: image.Pt(gtx.Dp(64), gtx.Dp(64))}
	})
	tx := gtx.Dp(16 + 64 + 12)
	tw := w - tx - gtx.Dp(16)
	m := op.Record(gtx.Ops)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
	td := layout.Flex{Axis: layout.Vertical}.Layout(tg,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.Para(gtx, 15, 22, theme.Bold, c.Fg, f.subtitle)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.Para(gtx, 13, 18, theme.Regular, c.FgMuted, focusSubLabel(a.focusExp(), gtx.Now))
		}),
	)
	call := m.Stop()
	bannerH := max(gtx.Dp(64), td.Size.Y)
	func() {
		defer op.Offset(image.Pt(tx, by+(bannerH-td.Size.Y)/2)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	y = by + bannerH + gtx.Dp(12)
	// options: padding 16, gap 8 — "Duration", the select, the VIP box
	y += gtx.Dp(16)
	at(gtx, image.Pt(gtx.Dp(16), y), image.Pt(w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, c.Fg, "Duration")
	})
	y += gtx.Dp(22 + 8)
	sw := w - gtx.Dp(32)
	f.durRect = image.Rect(gtx.Dp(16), y, gtx.Dp(16)+sw, y+gtx.Dp(36))
	at(gtx, f.durRect.Min, f.durRect.Size(), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &f.durBtn, nil, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(sw, gtx.Dp(36))
			bc := c.BorderStrong
			if f.durBtn.Hovered() {
				bc = c.Fg
			}
			theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), bc)
			theme.RRect(gtx, image.Rectangle{Max: sz}.Inset(max(1, gtx.Dp(1))), gtx.Dp(8)-1, c.Bg)
			label := ""
			for _, o := range focusOptions {
				if o.key == f.option {
					label = o.label
				}
			}
			at(gtx, image.Pt(gtx.Dp(12), 0), sz, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Regular, c.Fg, label)
				})
			})
			at(gtx, image.Pt(sz.X-gtx.Dp(12+16), (sz.Y-gtx.Dp(16))/2), sz, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("chevron-down")(gtx, 16, c.Fg)
			})
			return layout.Dimensions{Size: sz}
		})
	})
	y += gtx.Dp(36)
	// vipCheckbox (13px, content-sec): Continue to get notifications from VIPs
	y += gtx.Dp(8)
	vip := false
	if cr := a.Core(); cr != nil {
		vip = cr.PrefBool("priority_dnd_override", false)
	}
	rh := gtx.Dp(20)
	at(gtx, image.Pt(gtx.Dp(16), y), image.Pt(sw, rh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &f.vipBtn, nil, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(0, (rh-gtx.Dp(theme.NativeCheckSize))/2), image.Pt(rh, rh), func(gtx layout.Context) layout.Dimensions {
				return theme.NativeCheck(gtx, th.Dark, theme.NativeCheckState{On: vip, Hover: f.vipBtn.Hovered()})
			})
			lbl := "Continue to get notifications from VIPs"
			at(gtx, image.Pt(gtx.Dp(24), 0), image.Pt(sw, rh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Regular, c.FgMuted, lbl)
				})
			})
			return layout.Dimensions{Size: image.Pt(gtx.Dp(24)+th.Measure(gtx, 13, theme.Regular, lbl), rh)}
		})
	})
	y += rh + gtx.Dp(16)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// openFocusDurations opens the Duration select's options; a pick writes
// focus_mode_expiration_ts at once (onSelectionChange).
func (a *App) openFocusDurations() {
	f := a.fm()
	if f.pop == nil {
		return
	}
	labels := make([]string, len(focusOptions))
	sel := 0
	for i, o := range focusOptions {
		labels[i] = o.label
		if o.key == f.option {
			sel = i
		}
	}
	r := f.durRect.Add(f.pop.Rect().Min)
	f.durList = popups.NewOptionList(a, r, labels, sel, func(i int) {
		if i < 0 || i >= len(focusOptions) || focusOptions[i].key == f.option {
			return
		}
		f.option = focusOptions[i].key
		a.setFocusExp(focusExpiration(f.option, time.Now()))
	})
	a.ShowOverlay(f.durList)
}

// focusTelescope draws Slack's focus illustration (focus-d8ca34d.svg, a
// 1000×1000 telescope) s px square at the origin.
func focusTelescope(gtx layout.Context, s int) {
	k := float32(s) / 1000
	light, dark := color.NRGBA{0xab, 0xb7, 0xbf, 0xff}, color.NRGBA{0x29, 0x2f, 0x33, 0xff}
	rect := func(x, y, w, h, rx, rot, cx, cy float32, col color.NRGBA) {
		st := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(k, k)).Mul(f32.Affine2D{}.Rotate(f32.Pt(cx, cy), rot*math.Pi/180))).Push(gtx.Ops)
		r := clip.RRect{Rect: image.Rect(int(x), int(y), int(x+w), int(y+h)), NE: int(rx), NW: int(rx), SE: int(rx), SW: int(rx)}
		paint.FillShape(gtx.Ops, col, r.Op(gtx.Ops))
		st.Pop()
	}
	rect(606.22, 338.431, 315.738, 132.942, 19.1356, 30.0533, 606.22, 338.431, light)
	rect(271.32, 97.6533, 509.007, 214.319, 19.1356, 30.0533, 271.32, 97.6533, light)
	{
		st := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(k, k))).Push(gtx.Ops)
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(261.737, 114.216))
		p.CubeTo(f32.Pt(267.03, 105.069), f32.Pt(278.736, 101.944), f32.Pt(287.883, 107.237))
		p.LineTo(f32.Pt(412.106, 179.111))
		p.LineTo(f32.Pt(304.774, 364.616))
		p.LineTo(f32.Pt(180.551, 292.742))
		p.CubeTo(f32.Pt(171.404, 287.45), f32.Pt(168.279, 275.744), f32.Pt(173.572, 266.596))
		p.Close()
		paint.FillShape(gtx.Ops, dark, clip.Outline{Path: p.End()}.Op())
		st.Pop()
	}
	rect(691.52, 387.785, 59.3204, 132.942, 0, 30.0533, 691.52, 387.785, dark)
	{
		st := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(k, k))).Push(gtx.Ops)
		paint.FillShape(gtx.Ops, light, clip.Ellipse{Min: image.Pt(491-56, 538-56), Max: image.Pt(491+56, 538+56)}.Op(gtx.Ops))
		st.Pop()
	}
	rect(525.326, 608.576, 277.486, 67.9314, 33.9657, 90, 525.326, 608.576, light)
	rect(474.799, 633.569, 304.155, 67.9314, 33.9657, 120, 474.799, 633.569, light)
	rect(564.873, 596.348, 307.914, 67.9314, 33.9657, 60, 564.873, 596.348, light)
	rect(366.501, 594.244, 249.72, 67.9314, 33.9657, 0, 366.501, 594.244, dark)
}
