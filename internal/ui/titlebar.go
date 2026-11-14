package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Windows' caption buttons over the top nav (gap 2, W34).
//
// Slack's main window is frameless on Windows (win32 main.bundle.cjs
// @1144516: frame false, titleBarStyle "hidden", titleBarOverlay {color
// "#00000000", symbolColor "#FFFFFF", height 33}; minWidth 668, minHeight
// 400 @1144029). Electron 45 (Slack 4.53's) draws Windows' minimize,
// maximize/restore and close buttons at the window's top right, over the
// page (shell/browser/ui/views/win_caption_button*.cc, win_frame_view.cc,
// win_icon_painter.cc on its 45-x-y branch), and the web client's top nav
// is the title bar: -webkit-app-region drag, with no-drag for its buttons,
// .p-ia4_top_nav__native_ui_spacer and the 138px
// .p-ia4_top_nav__windows_controls the right container ends with
// (client-boot-styles). Gio patch 22 (app.HiddenTitleBar) takes the
// caption away and tells Windows what each part of the nav is from the
// system.ActionInputOp areas laid out here; Run turns it on, on Windows
// (titlebar_windows.go). The code runs everywhere so the tests can drive
// it; a.wco.on is false outside Windows.
//
// Geometry, in window DIPs (unzoomed: the overlay isn't page content):
//
//   - WinCaptionButtonContainer at the right edge, 137 wide: minimize
//     (kWindowsCaptionButtonWidth 45), then maximize and close, each 45
//     plus kWindowsCaptionButtonVisualSpacing 1 on its left, whose
//     background starts after it (GetBetweenButtonSpacing). Restored, it
//     starts 1 below the top (WindowTopY) and runs to the overlay's
//     height; maximized, from the top.
//   - The overlay's height: the BrowserWindow's 33 until the client's
//     top nav mounts; then useWindowThemedTitlebar's
//     round(40 × zoom) - 1 (the nav's height less 1): 39.
//   - The symbol: round(10 × scale) square, centred in the button's
//     bounds (spacing included), stroked round(scale) wide.
//
// Colours (WinCaptionButton::OnPaintBackground, PaintSymbol): the
// background is transparent; hovered, the frame's contrasting colour
// (Chromium's kColorCaptionButtonForegroundActive: black over a light
// frame, white over a dark one; Slack's nativeTheme.themeSource follows
// its theme) at 0x1A, pressed 0x33; close #E81123 hovered, at 0x98
// pressed, its symbol white. The hover fades in and out over
// views::Button's 150 ms (ease-out SlideAnimation, a partial fade taking
// its part of it); a press drops it and a release over the button brings
// it back at once. The symbol is the overlay's symbolColor: #FFFFFF before
// the client sets it, then the sidebar theme's top_nav_text
// (titlebar_theme.go).

const (
	wcoButtonW     = 45  // WindowFrameUtil::kWindowsCaptionButtonWidth
	wcoSpacing     = 1   // kWindowsCaptionButtonVisualSpacing
	wcoWidth       = 137 // the container: 45 + 46 + 46
	wcoReserveW    = 138 // .p-ia4_top_nav__windows_controls: 138px / zoom
	wcoReserveH    = 32  // its height (CSS px)
	wcoBootHeight  = 33  // the BrowserWindow's titleBarOverlay height
	wcoSymbolSize  = 10  // PaintSymbol's symbol_size_pixels at 1x
	wcoHoverAlpha  = 0x1A
	wcoPressAlpha  = 0x33
	wcoClosePress  = 0x98
	wcoHoverFade   = 150 * time.Millisecond // views::Button hover_animation_
	wcoNarrowW     = 668                    // the top nav's narrow breakpoint (CSS px)
	wcoWideW       = 2000                   // and its wide one
	wcoNativeUIPad = 24                     // .p-ia4_top_nav__native_ui_spacer: 2 × 12px …
	wcoNativeUIW   = 52                     // … + 52px / zoom
)

var (
	wcoBootSymbol = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff} // lF
	wcoCloseRed   = color.NRGBA{R: 0xe8, G: 0x11, B: 0x23, A: 0xff}
)

// The Windows WM_SYSCOMMAND commands the buttons send, as Electron's
// NativeWindow::Minimize/Maximize/Restore (HWNDMessageHandler's
// ExecuteSystemMenuCommand) and Windows' own close button do.
const (
	scMinimize = 0xF020
	scMaximize = 0xF030
	scClose    = 0xF060
	scRestore  = 0xF120
)

// wcoKind is a caption button.
type wcoKind uint8

const (
	wcoMin wcoKind = iota
	wcoMax
	wcoClose
)

// wcoState is views::Button's state for a caption button.
type wcoState uint8

const (
	wcoNormal wcoState = iota
	wcoHovered
	wcoPressed
)

// wcoSlide is gfx::SlideAnimation as views::Button's hover_animation_
// runs it: 150 ms scaled by the distance left, Tween::EASE_OUT.
type wcoSlide struct {
	value, start, end float64
	dir               int8 // 1 showing, -1 hiding, 0 neither
	t0                time.Time
	dur               time.Duration
}

func (s *wcoSlide) reset(v float64) { s.value, s.dir = v, 0 }

func (s *wcoSlide) begin(dir int8, now time.Time) {
	if s.dir == dir {
		return
	}
	s.dir = dir
	s.start, s.end = s.value, 1
	progress := s.value
	if dir < 0 {
		s.end, progress = 0, 1-s.value
	}
	s.dur = time.Duration(float64(wcoHoverFade) * (1 - progress))
	s.t0 = now
}

// step advances to now; it reports whether the fade still runs.
func (s *wcoSlide) step(now time.Time) bool {
	if s.dir == 0 {
		return false
	}
	if s.value == s.end || s.dur <= 0 {
		s.value, s.dir = s.end, 0
		return false
	}
	p := float64(now.Sub(s.t0)) / float64(s.dur)
	if p >= 1 {
		s.value, s.dir = s.end, 0
		return false
	}
	p = 1 - (1-p)*(1-p) // EASE_OUT
	s.value = s.start + (s.end-s.start)*max(p, 0)
	return true
}

type wcoButton struct {
	click widget.Clickable
	state wcoState
	hover wcoSlide
}

// setState is views::Button::SetState's hover animation.
func (b *wcoButton) setState(st wcoState, now time.Time) {
	if st == b.state {
		return
	}
	switch {
	case b.state == wcoHovered && st == wcoNormal:
		b.hover.begin(-1, now)
	case st != wcoHovered:
		b.hover.reset(0)
	case b.state == wcoNormal:
		b.hover.begin(1, now)
	default: // pressed → hovered
		b.hover.reset(1)
	}
	b.state = st
}

// wcoBar is the caption buttons' state (App.wco).
type wcoBar struct {
	on    bool // the window has no OS title bar (Run on Windows; tests)
	win10 bool // Windows 10's square icons (WinIconPainter), not 11's
	btns  [3]wcoButton
	geo   wcoGeo // this frame's geometry (tests)
	scale float32
	noDrg int // the nav's no-drag areas' tag
	// the symbol colour's inputs and result (prefs read each frame,
	// parsed when they change)
	symKey [4]string
	symCol color.NRGBA
}

// wcoGeo is where the caption buttons are this frame, window px.
type wcoGeo struct {
	bounds [3]image.Rectangle // the hit areas
	bg     [3]image.Rectangle // the hover and pressed backgrounds
	sym    [3]image.Rectangle // the symbols' boxes
	stroke int
}

// wcoGeometry lays the buttons out in a window w px wide at s px per
// window DIP, under an overlay height DIPs tall.
func wcoGeometry(w int, s float32, height float32, maximized bool) wcoGeo {
	px := func(d float32) int { return int(math.Round(float64(d * s))) }
	top := float32(1) // WinFrameView::WindowTopY restored
	if maximized {
		top = 0
	}
	y0, y1 := px(top), px(height)
	var g wcoGeo
	// from the right edge: minimize's 45, maximize's and close's 46
	edge := [4]int{w - px(wcoWidth), w - px(wcoWidth-wcoButtonW), w - px(wcoButtonW+wcoSpacing), w}
	size := int(math.Round(float64(wcoSymbolSize * s)))
	g.stroke = max(int(math.Round(float64(s))), 1)
	for i := range 3 {
		b := image.Rect(edge[i], y0, edge[i+1], y1)
		g.bounds[i] = b
		bg := b
		if i > 0 {
			bg.Min.X = edge[i] + px(wcoSpacing)
		}
		g.bg[i] = bg
		x := b.Min.X + (b.Dx()-size)/2 // gfx::Rect::ClampToCenteredSize
		y := b.Min.Y + (b.Dy()-size)/2
		g.sym[i] = image.Rect(x, y, x+size, y+size)
	}
	return g
}

// wcoLook is the overlay as Slack has set it: before its client's top
// nav (the BrowserWindow's options) or after (useWindowThemedTitlebar).
type wcoLook struct {
	height float32     // window DIPs
	symbol color.NRGBA // symbolColor
	// flat is the opaque colour under the buttons, or zero over the
	// chrome gradient (the top nav)
	flat color.NRGBA
}

// wcoOn reports whether the caption buttons show: Windows' main window,
// except in full screen (TitlebarHeight 0, wcoVisible false).
func (a *App) wcoOn() bool { return a.wco.on && a.winMode != app.Fullscreen }

// wcoScale is px per window DIP: the overlay doesn't zoom with View >
// Zoom (138px / var(--zoom-factor) in Slack's CSS). gtx is zoomed, as
// newContext makes it.
func (a *App) wcoScale(gtx layout.Context) float32 {
	return gtx.Metric.PxPerDp / a.zoomFactor()
}

// wcoContext is gtx (app.NewContext's, unzoomed: Run's frames while the
// fonts load) zoomed as newContext zooms, for layoutWCO.
func (a *App) wcoContext(gtx layout.Context) layout.Context {
	if f := a.zoomFactor(); f != 1 {
		gtx.Metric.PxPerDp *= f
		gtx.Metric.PxPerSp *= f
	}
	return gtx
}

// wcoClientLook is the overlay once the top nav set it: height
// round(40 × zoom) - 1, the theme's symbol colour.
func (a *App) wcoClientLook() wcoLook {
	h := float32(math.Round(float64(float32(theme.TopNavH)*a.zoomFactor()))) - 1
	return wcoLook{height: h, symbol: a.wcoThemeSymbol()}
}

// wcoBootLook is the BrowserWindow's overlay over a flat background.
func wcoBootLook(flat color.NRGBA) wcoLook {
	return wcoLook{height: wcoBootHeight, symbol: wcoBootSymbol, flat: flat}
}

// wcoPlaceholderLook is the BrowserWindow's overlay over the boot
// placeholder (reveal.go): the stored theme's gradient, or page.html's
// #bbbabb (#282828 dark).
func (a *App) wcoPlaceholderLook() wcoLook {
	switch {
	case a.acct.set:
		return wcoBootLook(color.NRGBA{})
	case a.th.Dark:
		return wcoBootLook(color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xff})
	}
	return wcoBootLook(color.NRGBA{R: 0xbb, G: 0xba, B: 0xbb, A: 0xff})
}

// wcoThemeSymbol is the sidebar theme's top_nav_text.
func (a *App) wcoThemeSymbol() color.NRGBA {
	var key [4]string
	if a.th.Dark {
		key[0] = "dark"
	}
	if c := a.Core(); c != nil {
		key[1], key[2], key[3] = c.PrefString("sidebar_theme"), c.PrefString("sidebar_theme_custom_values"), c.PrefString("ia_top_nav_theme")
	}
	if key != a.wco.symKey || a.wco.symCol.A == 0 {
		a.wco.symKey = key
		a.wco.symCol = wcoSymbolColor(wcoThemeText(a.th.Dark, key[1], key[2], key[3]))
	}
	return a.wco.symCol
}

// wcoNavReserve is the top nav's .p-ia4_top_nav__windows_controls, px:
// 138 window DIPs at the right container's end while the buttons show.
func (a *App) wcoNavReserve(gtx layout.Context) int {
	if !a.wcoOn() {
		return 0
	}
	return int(math.Round(float64(wcoReserveW * a.wcoScale(gtx))))
}

// wcoNavAreas lays out the top nav's drag areas: first, so every widget
// the nav draws later is no-drag. The nav is -webkit-app-region drag; on
// Windows .p-ia4_top_nav__native_ui_spacer (2 × 12px + 52px / zoom, the
// app menu's) is no-drag (unless spacer is false: BBM hides it), and so is
// .p-ia4_top_nav__windows_controls (reserve px wide, 32px tall, at the end
// of the right container, 8px from the edge). Gio's ActionAt passes over
// areas without an action, so the no-drag ones carry an input handler.
func (a *App) wcoNavAreas(gtx layout.Context, width, h, reserve int, spacer bool) {
	if !a.wcoOn() {
		return
	}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &a.wco.noDrg, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	area := clip.Rect{Max: image.Pt(width, h)}.Push(gtx.Ops)
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	area.Pop()
	noDrag := func(r image.Rectangle) {
		if r.Empty() {
			return
		}
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &a.wco.noDrg)
	}
	if spacer {
		s := a.wcoScale(gtx)
		noDrag(image.Rect(0, 0, gtx.Dp(wcoNativeUIPad)+int(math.Round(float64(wcoNativeUIW*s))), h))
	}
	if reserve > 0 {
		x1 := width - gtx.Dp(8)
		y0 := (h - gtx.Dp(wcoReserveH)) / 2
		noDrag(image.Rect(x1-reserve, y0, x1, y0+gtx.Dp(wcoReserveH)))
	}
}

// wcoWelcomeDrag is the sign-in and introduction screens' caption: no
// Slack page draws them, so Clack makes the overlay's title bar area
// (navigator.windowControlsOverlay's titlebar rect: the overlay's height,
// left of the buttons) the drag area, as an Electron page with a
// titleBarOverlay marks it.
func (a *App) wcoWelcomeDrag(gtx layout.Context) {
	if !a.wcoOn() {
		return
	}
	s := a.wcoScale(gtx)
	w := gtx.Constraints.Max.X - int(math.Round(float64(wcoWidth*s)))
	h := int(math.Round(float64(wcoBootHeight * s)))
	defer clip.Rect{Max: image.Pt(max(w, 0), h)}.Push(gtx.Ops).Pop()
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
}

// wcoPost sends a WM_SYSCOMMAND to the window (tests record it).
var wcoPost = func(a *App, sc uint32) { a.postSysCommand(sc) }

// layoutWCO draws the caption buttons over everything (deferred, so above
// the deferred popups too: Electron's buttons are views over the page) and
// runs their clicks.
func (a *App) layoutWCO(gtx layout.Context, look wcoLook) {
	if !a.wcoOn() {
		return
	}
	b := &a.wco
	s := a.wcoScale(gtx)
	maximized := a.winMode == app.Maximized
	g := wcoGeometry(gtx.Constraints.Max.X, s, look.height, maximized)
	b.geo, b.scale = g, s
	for i := range b.btns {
		if b.btns[i].click.Clicked(gtx) {
			a.wcoCommand(wcoKind(i), maximized)
		}
	}
	m := op.Record(gtx.Ops)
	animating := false
	for i := range b.btns {
		btn := &b.btns[i]
		st := wcoNormal
		if h := btn.click.Hovered(); h && btn.click.Pressed() {
			st = wcoPressed
		} else if h {
			st = wcoHovered
		}
		btn.setState(st, gtx.Now)
		fading := btn.hover.step(gtx.Now)
		animating = animating || fading
		act := [3]system.Action{system.ActionMinimize, system.ActionMaximize, system.ActionClose}[i]
		if i == int(wcoMax) && maximized {
			act = system.ActionUnmaximize
		}
		r := g.bounds[i]
		func() {
			defer op.Offset(r.Min).Push(gtx.Ops).Pop()
			bg := gtx
			bg.Constraints = layout.Exact(r.Size())
			btn.click.Layout(bg, func(gtx layout.Context) layout.Dimensions {
				system.ActionInputOp(act).Add(gtx.Ops)
				return layout.Dimensions{Size: r.Size()}
			})
		}()
		a.wcoPaint(gtx, wcoKind(i), btn, g, look, maximized, fading)
	}
	op.Defer(gtx.Ops, m.Stop())
	if animating {
		gtx.Execute(op.InvalidateCmd{})
	}
}

// wcoCommand runs a button: Electron's NativeWindow::Minimize, Maximize,
// Restore and Close (WM_SYSCOMMAND, as Windows' own buttons send it).
// Posted: a close goes through the window's message loop, so Clack's
// ClosingEvent can keep the window in the tray (runfromtray.go).
func (a *App) wcoCommand(k wcoKind, maximized bool) {
	switch k {
	case wcoMin:
		wcoPost(a, scMinimize)
	case wcoMax:
		if maximized {
			wcoPost(a, scRestore)
		} else {
			wcoPost(a, scMaximize)
		}
	case wcoClose:
		wcoPost(a, scClose)
	}
}

// wcoPaint draws button k's background and symbol (window coordinates).
func (a *App) wcoPaint(gtx layout.Context, k wcoKind, btn *wcoButton, g wcoGeo, look wcoLook, maximized, fading bool) {
	base := color.NRGBA{A: 0xff} // black over a light frame
	if a.th.Dark {
		base = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	hovered, pressed := uint8(wcoHoverAlpha), uint8(wcoPressAlpha)
	if k == wcoClose {
		base, hovered, pressed = wcoCloseRed, 0xff, wcoClosePress
	}
	alpha := uint8(float64(hovered) * btn.hover.value) // Tween::IntValueBetween
	if btn.state == wcoPressed {
		alpha = pressed
	}
	if alpha > 0 {
		a.wcoFill(gtx, g.bg[k], theme.WithAlpha(base, alpha), look)
	}
	sym := look.symbol
	if k == wcoClose {
		switch {
		case fading:
			sym = wcoLerp(sym, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, btn.hover.value)
		case btn.state == wcoHovered || btn.state == wcoPressed:
			sym = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		}
	}
	wcoSymbol(gtx, k, maximized, g.sym[k], a.wco.scale, g.stroke, a.wco.win10, sym)
}

// wcoFill paints a translucent background the way Chromium blends it, in
// sRGB: with the alpha that gives that blend over what is under the button
// (the flat colour, or the chrome gradient's colour there: a few px of a
// slow gradient), so a fade doesn't render a gradient variant per frame.
func (a *App) wcoFill(gtx layout.Context, r image.Rectangle, c color.NRGBA, look wcoLook) {
	switch {
	case c.A == 0xff:
		theme.Fill(gtx, r, c)
	case look.flat.A != 0:
		theme.Fill(gtx, r, theme.LinearAlpha(c, look.flat))
	default:
		theme.Fill(gtx, r, theme.LinearAlpha(c, a.chromeBase(r.Min.Add(r.Max).Div(2))))
	}
}

// wcoLerp is Tween::ColorValueBetween for opaque colours.
func wcoLerp(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}

// wcoSymbol is WinIconPainter's (win10) or Win11IconPainter's symbol in
// box r at scale s, stroked sw px wide.
func wcoSymbol(gtx layout.Context, k wcoKind, maximized bool, r image.Rectangle, s float32, sw int, win10 bool, c color.NRGBA) {
	ops := gtx.Ops
	w := float32(sw)
	rad := float32(math.Floor(1.5 * float64(s))) // kWin11RoundedCornerRadius × image_scale
	switch {
	case k == wcoMin:
		// DrawLine from (x, centre) to (right, centre), not anti-aliased
		cy := r.Min.Y + r.Dy()/2
		y := cy - sw/2
		paint.FillShape(ops, c, clip.Rect{Min: image.Pt(r.Min.X, y), Max: image.Pt(r.Max.X, y+sw)}.Op())
	case k == wcoMax && !maximized && win10:
		wcoSquare(ops, r, sw, c)
	case k == wcoMax && !maximized:
		// a rounded rectangle inside the box, radius floor(1.5 × scale)
		rf := wcoInset(r, w/2)
		paint.FillShape(ops, c, clip.Stroke{Path: wcoRRect(ops, rf, rad, rad, rad, rad), Width: w}.Op())
	case k == wcoMax && win10:
		// two squares, separation floor(2 × scale); the back one only
		// outside the front one
		sep := int(math.Floor(2 * float64(s)))
		front := image.Rect(r.Min.X, r.Min.Y+sep, r.Max.X-sep, r.Max.Y)
		wcoSquare(ops, front, sw, c)
		back := front.Add(image.Pt(sep, -sep))
		ring := wcoRing(ops, back.Inset(-sw), f32Rect(front), 0)
		cl := clip.Outline{Path: ring}.Op().Push(ops)
		wcoSquare(ops, back, sw, c)
		cl.Pop()
	case k == wcoMax:
		// Win11IconPainter::PaintRestoreIcon
		sep := 2 * s
		front := wcoInset(r, w/2)
		front.Min.Y += sep
		front.Max.X -= sep
		paint.FillShape(ops, c, clip.Stroke{Path: wcoRRect(ops, front, rad, rad, rad, rad), Width: w}.Op())
		grow := sep - w/2
		hole := wcoRectF{Min: front.Min.Sub(f32.Pt(grow, grow)), Max: front.Max.Add(f32.Pt(grow, grow))}
		back := wcoRectF{Min: front.Min.Add(f32.Pt(sep, -sep)), Max: front.Max.Add(f32.Pt(sep, -sep))}
		outer := image.Rectangle{Min: r.Min.Sub(image.Pt(sw+2, sw+2)), Max: r.Max.Add(image.Pt(sw+2, sw+2))}
		cl := clip.Outline{Path: wcoRing(ops, outer, hole, rad+grow)}.Op().Push(ops)
		paint.FillShape(ops, c, clip.Stroke{Path: wcoRRect(ops, back, rad, 2*rad, rad, rad), Width: w}.Op())
		cl.Pop()
	default: // close: the X, a halo wider (DrawPath with Electron's flags)
		halo := float32(0.05)
		if c == (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
			halo = 0.1
		}
		defer clip.Rect(r).Push(ops).Pop()
		var p clip.Path
		p.Begin(ops)
		p.MoveTo(f32.Pt(float32(r.Min.X), float32(r.Min.Y)))
		p.LineTo(f32.Pt(float32(r.Max.X), float32(r.Max.Y)))
		p.MoveTo(f32.Pt(float32(r.Max.X), float32(r.Min.Y)))
		p.LineTo(f32.Pt(float32(r.Min.X), float32(r.Max.Y)))
		paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: w + w*halo}.Op())
	}
}

// wcoSquare is DrawRect's stroke inside r, not anti-aliased: four bars.
func wcoSquare(ops *op.Ops, r image.Rectangle, sw int, c color.NRGBA) {
	for _, b := range []image.Rectangle{
		{Min: r.Min, Max: image.Pt(r.Max.X, r.Min.Y+sw)},
		{Min: image.Pt(r.Min.X, r.Max.Y-sw), Max: r.Max},
		{Min: image.Pt(r.Min.X, r.Min.Y+sw), Max: image.Pt(r.Min.X+sw, r.Max.Y-sw)},
		{Min: image.Pt(r.Max.X-sw, r.Min.Y+sw), Max: image.Pt(r.Max.X, r.Max.Y-sw)},
	} {
		paint.FillShape(ops, c, clip.Rect(b).Op())
	}
}

// wcoRectF is a rectangle in float px (Gio 0.10 has no f32.Rectangle).
type wcoRectF struct{ Min, Max f32.Point }

func f32Rect(r image.Rectangle) wcoRectF {
	return wcoRectF{Min: f32.Pt(float32(r.Min.X), float32(r.Min.Y)), Max: f32.Pt(float32(r.Max.X), float32(r.Max.Y))}
}

func wcoInset(r image.Rectangle, d float32) wcoRectF {
	f := f32Rect(r)
	return wcoRectF{Min: f.Min.Add(f32.Pt(d, d)), Max: f.Max.Sub(f32.Pt(d, d))}
}

// wcoKappa is a quarter circle's cubic control distance.
const wcoKappa = 0.5522847

// wcoRRect is a rounded rectangle's outline, clockwise, with the corner
// radii nw, ne, se, sw.
func wcoRRect(ops *op.Ops, r wcoRectF, nw, ne, se, sw float32) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	wcoRRectTo(&p, r, nw, ne, se, sw, false)
	return p.End()
}

// wcoRRectTo adds a closed rounded rectangle to p, clockwise or, with
// ccw, counter-clockwise (a hole under the non-zero rule).
func wcoRRectTo(p *clip.Path, r wcoRectF, nw, ne, se, sw float32, ccw bool) {
	x0, y0, x1, y1 := r.Min.X, r.Min.Y, r.Max.X, r.Max.Y
	k := float32(wcoKappa)
	pt := f32.Pt
	if !ccw {
		p.MoveTo(pt(x0+nw, y0))
		p.LineTo(pt(x1-ne, y0))
		p.CubeTo(pt(x1-ne+ne*k, y0), pt(x1, y0+ne-ne*k), pt(x1, y0+ne))
		p.LineTo(pt(x1, y1-se))
		p.CubeTo(pt(x1, y1-se+se*k), pt(x1-se+se*k, y1), pt(x1-se, y1))
		p.LineTo(pt(x0+sw, y1))
		p.CubeTo(pt(x0+sw-sw*k, y1), pt(x0, y1-sw+sw*k), pt(x0, y1-sw))
		p.LineTo(pt(x0, y0+nw))
		p.CubeTo(pt(x0, y0+nw-nw*k), pt(x0+nw-nw*k, y0), pt(x0+nw, y0))
		p.Close()
		return
	}
	p.MoveTo(pt(x0+nw, y0))
	p.CubeTo(pt(x0+nw-nw*k, y0), pt(x0, y0+nw-nw*k), pt(x0, y0+nw))
	p.LineTo(pt(x0, y1-sw))
	p.CubeTo(pt(x0, y1-sw+sw*k), pt(x0+sw-sw*k, y1), pt(x0+sw, y1))
	p.LineTo(pt(x1-se, y1))
	p.CubeTo(pt(x1-se+se*k, y1), pt(x1, y1-se+se*k), pt(x1, y1-se))
	p.LineTo(pt(x1, y0+ne))
	p.CubeTo(pt(x1, y0+ne-ne*k), pt(x1-ne+ne*k, y0), pt(x1-ne, y0))
	p.Close()
}

// wcoRing is outer less hole (a rounded rectangle of radius rad): the
// difference clip Skia's clipRRect(kDifference) makes.
func wcoRing(ops *op.Ops, outer image.Rectangle, hole wcoRectF, rad float32) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	o := f32Rect(outer)
	p.MoveTo(o.Min)
	p.LineTo(f32.Pt(o.Max.X, o.Min.Y))
	p.LineTo(o.Max)
	p.LineTo(f32.Pt(o.Min.X, o.Max.Y))
	p.Close()
	wcoRRectTo(&p, hole, rad, rad, rad, rad, true)
	return p.End()
}
