package huddleui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The huddle window follows your theme, as Slack's does. Slack's huddle
// window is a client window of its own (gantry-v2-async-open-huddle-
// window-mva, openHuddleWindowMva: a multi-view window whose primary view
// is the huddle), so it takes the client's theme like any other:
//
//   - light or dark: the client's body gets sk-client-theme--dark only in
//     dark mode (render /+fI, Client: `"dark"===o ? body.classList.add(RD)
//     : …remove(RD)`), so its menus (Slack Kit c-menus on
//     sk_primary_background, the mic caret's HuddleAVDevicesMenu among
//     them) are light in light mode;
//   - the background: the huddle view is transparent over the window's
//     .p-theme_background (`.p-window_layout_wrapper .p-view_contents--
//     huddle {background-color: transparent}`, `.p-huddle_window__mva_
//     wrapper .p-huddle_window {background-color: #0000}`), the account
//     theme's window colours (zoFo Theme: `.p-theme_background
//     {background: …}`), Slack's aubergine only with Slack's default theme;
//   - the toolbar and the title: HuddleToolbar's .button paints
//     theme-surf-inv-pry with theme-content-inv-pry (hover
//     theme-surf-inv-ter, toggled theme-base-pry with theme-content-ter,
//     the segment's line theme-otl-inv-pry) in the chrome's scope (shared
//     +2Ig: useInvertedTheme({view: "chrome"}); vYdp: the inverted class
//     only in light mode when the theme's brightness inverts the chrome):
//     ChromeText, ChromeActive, ChromeSurfInvTer, ChromeBasePry,
//     ChromeContentTer and ChromeOtlInvPry, never the sidebar's fields.
//
// What Slack keeps dark whatever the theme stays dark here: the screen
// share's mini panel (/+fI HuddleMiniPanelMounter:
// body.classList.add(RD), background #1a1d21), the captions' boxes and the
// tiles' own colours.

// winTheme is the main window's theme as the huddle window copies it.
type winTheme struct {
	pal  theme.Palette
	tok  theme.Tokens
	dark bool
	gen  uint64
	set  bool
}

// noteMainTheme keeps a copy of the main window's theme for the huddle
// window, and redraws the window when it changed. The main window's
// goroutine: Start, and every frame of the main window (DockHeight, with
// or without a bar). The palette is a copy; the stylesheet tokens are the
// main window's own (css.RootTokens are safe for concurrent use).
func (c *Controller) noteMainTheme(th *theme.Theme) {
	if th == nil {
		return
	}
	c.mu.Lock()
	if c.wt.set && c.wt.gen == th.Gen && c.wt.dark == th.Dark {
		c.mu.Unlock()
		return
	}
	c.wt = winTheme{pal: th.C, tok: th.Tokens, dark: th.Dark, gen: th.Gen, set: true}
	w := c.win
	c.mu.Unlock()
	if w != nil {
		w.invalidate()
	}
}

func (c *Controller) mainTheme() winTheme {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wt
}

// syncTheme gives the window's theme the main window's mode, palette (the
// account theme's colours) and stylesheet tokens. The window's goroutine,
// before each frame. The screen share popout keeps its own dark look.
func (w *window) syncTheme() {
	if w.popout != "" {
		return
	}
	wt := w.c.mainTheme()
	if !wt.set || (w.themed && w.themeGen == wt.gen) {
		return
	}
	w.th.SetDark(wt.dark)
	w.th.C, w.th.Tokens = wt.pal, wt.tok
	w.th.Gen++ // palettes derived from the theme rebuild (theme.Theme.Gen)
	w.themeGen, w.themed = wt.gen, true
}

// winInk is the window's chrome-scope tokens.
type winInk struct {
	bgA, bgB      color.NRGBA // .p-theme_background's two colours
	title         color.NRGBA // the title, at .p-multi_view_window_title's .8
	btn, btnHov   color.NRGBA // theme-surf-inv-pry, theme-surf-inv-ter
	btnOn, onFg   color.NRGBA // theme-base-pry, theme-content-ter
	icon, divider color.NRGBA // theme-content-inv-pry, theme-otl-inv-pry
}

// windowInks is the window's colours: .p-theme_background's (the
// background mode's, ChromeA and ChromeB), and every ink of the title and
// the toolbar from the chrome's scope, never the sidebar's. The two differ
// in a light theme at a dark brightness with Darker sidebars off (Ochin,
// Clementine, Jade, Aubergine with Darker sidebars unticked…): the chrome
// is inverted, white over the theme's dark window, while the sidebar's
// scope stays light, its dark ink nearly invisible there.
func windowInks(th *theme.Theme) winInk {
	p := th.C
	return winInk{bgA: p.ChromeA, bgB: p.ChromeB, title: p.ChromeText,
		btn: p.ChromeActive, btnHov: p.ChromeSurfInvTer, btnOn: p.ChromeBasePry, onFg: p.ChromeContentTer,
		icon: p.ChromeText, divider: p.ChromeOtlInvPry}
}

// caption is the Windows caption for the window's colours (caption.go):
// the window's colour, with the buttons' glyphs and the title's ink both
// chosen by its lightness. The title keeps the chrome's ink when that
// reads on the caption; an inverted chrome over a pale window (white ink:
// Slack's own, over Karim's imported peach) gets Slack's dark text
// instead, as a light caption's glyphs are dark. (Slack's huddle
// titlebar is the legacy theme's top_nav_bg with top_nav_text,
// useWindowThemedTitlebar; the window's colours stand in for them.)
func (k winInk) caption() captionLook {
	dark := luma(k.bgA) < 0.5
	text := k.title
	if (luma(text) < 0.5) == dark { // it would vanish into the caption
		text = captionTextLight
		if dark {
			text = captionTextDark
		}
	}
	return captionLook{dark: dark, bg: k.bgA, text: text}
}

var (
	captionTextLight = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff} // sk_primary_foreground
	captionTextDark  = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

func luma(c color.NRGBA) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// themeBg paints Slack's .p-theme_background, as the main window's
// (ui/chrome.go gradient: radial-gradient(circle, A 20%, transparent 80%)
// over conic-gradient(from 45deg, B 0%, A 25%, B 50%, A 75%, B 100%); a
// theme without window gradients has A = B), rendered at 1/8 scale and
// upsampled. Re-rendered when the size or the colours change.
type themeBg struct {
	size image.Point
	a, b color.NRGBA
	img  paint.ImageOp
	ok   bool
}

const themeBgScale = 8

func (g *themeBg) paint(gtx C, sz image.Point, a, b color.NRGBA) {
	if a == b {
		theme.Fill(gtx, image.Rectangle{Max: sz}, a)
		return
	}
	if !g.ok || g.size != sz || g.a != a || g.b != b {
		g.size, g.a, g.b, g.ok = sz, a, b, true
		g.img = paint.NewImageOp(renderThemeBg(sz, a, b))
		g.img.Filter = paint.FilterLinear
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(themeBgScale, themeBgScale))).Push(gtx.Ops).Pop()
	g.img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func renderThemeBg(sz image.Point, a, b color.NRGBA) *image.RGBA {
	w, h := sz.X/themeBgScale+1, sz.Y/themeBgScale+1
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	far := math.Hypot(cx, cy)
	i := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := themeBgPixel(float64(x*themeBgScale)+themeBgScale/2-cx, float64(y*themeBgScale)+themeBgScale/2-cy, far, a, b)
			img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 0xff
			i += 4
		}
	}
	return img
}

// themeBgPixel is ui/chrome.go's gradPixel.
func themeBgPixel(dx, dy, far float64, a, b color.NRGBA) color.NRGBA {
	lerp := func(x, y uint8, t float64) float64 { return float64(x) + (float64(y)-float64(x))*t }
	ang := math.Atan2(dx, -dy)*180/math.Pi - 45 // CSS angle 0 = up, clockwise; "from 45deg"
	for ang < 0 {
		ang += 360
	}
	t := math.Mod(ang, 180) / 90 // 0..2: B→A→B
	if t > 1 {
		t = 2 - t
	}
	r, g, bb := lerp(b.R, a.R, t), lerp(b.G, a.G, t), lerp(b.B, a.B, t)
	d := math.Hypot(dx, dy) / far // radial: A at alpha 1 until 20%, 0 at 80%
	al := 1.0
	if d > .2 {
		al = max(0, 1-(d-.2)/.6)
	}
	r += (float64(a.R) - r) * al
	g += (float64(a.G) - g) * al
	bb += (float64(a.B) - bb) * al
	return color.NRGBA{R: uint8(r + .5), G: uint8(g + .5), B: uint8(bb + .5), A: 0xff}
}
