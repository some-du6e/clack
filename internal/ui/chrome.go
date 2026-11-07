package ui

import (
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// gradient renders Slack's window background:
//
//	radial-gradient(circle, A 20%, transparent 80%),
//	conic-gradient(from 45deg, B 0%, A 25%, B 50%, A 75%, B 100%)
//
// into a small image (1/8 scale) that the GPU upsamples with linear
// filtering. Variants pre-composite translucent chrome overlays (sidebar
// tint, search pill, hover tiles) in sRGB, matching browser blending.
// Re-rendered only when the window size or palette changes.
type gradient struct {
	size image.Point
	pal  [2]color.NRGBA
	imgs map[[2]color.NRGBA]paint.ImageOp
}

const gradScale = 8

func (g *gradient) variant(sz image.Point, a, b color.NRGBA, over ...color.NRGBA) paint.ImageOp {
	if g.size != sz || g.pal != [2]color.NRGBA{a, b} || g.imgs == nil {
		g.size, g.pal = sz, [2]color.NRGBA{a, b}
		g.imgs = map[[2]color.NRGBA]paint.ImageOp{}
	}
	var k [2]color.NRGBA
	copy(k[:], over)
	if img, ok := g.imgs[k]; ok {
		return img
	}
	img := paint.NewImageOp(renderGradient(sz, a, b, k[0], k[1]))
	img.Filter = paint.FilterLinear
	g.imgs[k] = img
	return img
}

// draw paints the gradient (with overlays) clipped to clipR, both in window
// coordinates; origin is the current op offset in window coordinates.
func (g *gradient) draw(gtx layout.Context, win image.Point, origin image.Point, clipR clip.Op, a, b color.NRGBA, over ...color.NRGBA) {
	img := g.variant(win, a, b, over...)
	defer clipR.Push(gtx.Ops).Pop()
	// image pixel centers sit at the window positions they were sampled at
	aff := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(gradScale, gradScale)).
		Offset(f32.Pt(-float32(origin.X), -float32(origin.Y)))
	defer op.Affine(aff).Push(gtx.Ops).Pop()
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func (g *gradient) layout(gtx layout.Context, a, b color.NRGBA) {
	sz := gtx.Constraints.Max
	g.draw(gtx, sz, image.Point{}, clip.Rect{Max: sz}.Op(), a, b)
}

// gradientColor evaluates the background at window point (x, y).
func gradientColor(sz image.Point, x, y int, a, b color.NRGBA) color.NRGBA {
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	return gradPixel(float64(x)-cx, float64(y)-cy, math.Hypot(cx, cy), a, b)
}

func gradPixel(dx, dy, far float64, a, b color.NRGBA) color.NRGBA {
	lerp := func(x, y uint8, t float64) float64 { return float64(x) + (float64(y)-float64(x))*t }
	// conic: CSS angle 0 = up, clockwise; "from 45deg".
	ang := math.Atan2(dx, -dy)*180/math.Pi - 45
	for ang < 0 {
		ang += 360
	}
	t := math.Mod(ang, 180) / 90 // 0..2: B→A→B
	if t > 1 {
		t = 2 - t
	}
	r, g, bb := lerp(b.R, a.R, t), lerp(b.G, a.G, t), lerp(b.B, a.B, t)
	// radial overlay: A at alpha 1 until 20%, fading to 0 at 80%.
	d := math.Hypot(dx, dy) / far
	al := 1.0
	if d > .2 {
		al = 1 - (d-.2)/.6
		if al < 0 {
			al = 0
		}
	}
	r += (float64(a.R) - r) * al
	g += (float64(a.G) - g) * al
	bb += (float64(a.B) - bb) * al
	return color.NRGBA{R: uint8(r + .5), G: uint8(g + .5), B: uint8(bb + .5), A: 0xff}
}

func renderGradient(sz image.Point, a, b, o1, o2 color.NRGBA) *image.RGBA {
	w, h := sz.X/gradScale+1, sz.Y/gradScale+1
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(sz.X)/2, float64(sz.Y)/2
	far := math.Hypot(cx, cy)
	i := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := gradPixel(float64(x*gradScale)+gradScale/2-cx, float64(y*gradScale)+gradScale/2-cy, far, a, b)
			if o1.A > 0 {
				c = theme.Over(o1, c)
			}
			if o2.A > 0 {
				c = theme.Over(o2, c)
			}
			img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 0xff
			i += 4
		}
	}
	return img
}

// chrome paints a chrome surface with translucent overlays composited in
// sRGB. r is local (relative to origin, the current offset in window
// coordinates).
func (a *App) chrome(gtx layout.Context, origin image.Point, r image.Rectangle, radius int, over ...color.NRGBA) {
	cl := clip.UniformRRect(r, radius).Op(gtx.Ops)
	a.grad.draw(gtx, a.winSize, origin, cl, a.th.C.ChromeA, a.th.C.ChromeB, over...)
}

// sidebarBase is the sidebar's effective (opaque) color near its middle, for
// flattening translucent sidebar colors (text, hovers) in sRGB.
func (a *App) sidebarBase() color.NRGBA {
	g := gradientColor(a.winSize, a.px(70+a.sidebar.width/2), a.winSize.Y/2, a.th.C.ChromeA, a.th.C.ChromeB)
	return theme.Over(a.th.C.SidebarBg, g)
}

// sidebarPalette returns the palette with the translucent sidebar colors
// flattened over the sidebar's effective background (sRGB blending).
func (a *App) sidebarPalette() *theme.Palette {
	p := a.th.C
	base := a.sidebarBase()
	p.SidebarText = theme.Over(p.SidebarText, base)
	p.SidebarHover = theme.Over(p.SidebarHover, base)
	p.SidebarDivider = theme.Over(p.SidebarDivider, base)
	p.SidebarButton = theme.Over(p.SidebarButton, base)
	p.SidebarActive = theme.Over(p.SidebarActive, base)
	a.sbBase = base
	a.sbPal = p
	return &a.sbPal
}

// chromeBase is the effective chrome color at window point p.
func (a *App) chromeBase(p image.Point) color.NRGBA {
	return gradientColor(a.winSize, p.X, p.Y, a.th.C.ChromeA, a.th.C.ChromeB)
}

// ---- top nav (40px) ----

func (a *App) layoutTopNav(gtx layout.Context, primaryX, width int) {
	c := &a.th.C
	h := gtx.Dp(theme.TopNavH)
	tn := clsTopNav.Enter(gtx, 0, 0)
	defer tn.Leave()
	if tn.Hidden() {
		return
	}
	left := clsTopNavLeft.Enter(gtx, 0, 0)
	// history buttons: 26×26, 6px gaps, right-aligned 9px before the search.
	b := gtx.Dp(26)
	gap := gtx.Dp(6)
	y := (h - b) / 2
	x := primaryX - gtx.Dp(9) - b
	for i := 2; i >= 0; i-- {
		name := [...]string{"back", "forward", "history"}[i]
		btn := a.clicks.get("nav." + name)
		col := c.ChromeText
		if (i == 0 && !a.router.CanBack()) || (i == 1 && !a.router.CanForward()) {
			col = theme.WithAlpha(col, 0x70)
		}
		org := image.Pt(x, y)
		at(gtx, org, image.Pt(b, b), func(gtx layout.Context) layout.Dimensions {
			return a.chromeButton(gtx, org, btn, name, 26, 18, col, 4)
		})
		x -= b + gap
	}
	if clicked(a.clicks.get("nav.back")) {
		a.Back()
	}
	if clicked(a.clicks.get("nav.forward")) {
		a.Forward()
	}
	left.Leave()
	if clicked(a.clicks.get("nav.history")) {
		a.openHistoryMenu()
	}

	// search pill: starts at the primary panel's left edge.
	right := width - gtx.Dp(4)
	avail := right - primaryX - gtx.Dp(112)
	sw := avail
	if max := gtx.Dp(680); sw > max {
		sw = max
	}
	if sw < gtx.Dp(200) {
		sw = gtx.Dp(200)
	}
	sh := gtx.Dp(28)
	sorg := image.Pt(primaryX, (h-sh)/2)
	a.searchPill = image.Rectangle{Min: sorg, Max: sorg.Add(image.Pt(sw, sh))}
	at(gtx, sorg, image.Pt(sw, sh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("nav.search"), func(gtx layout.Context, sz image.Point, hovered bool) {
			bg := c.SearchBg
			if hovered {
				bg = c.SearchHover
			}
			// Slack: rgba(249,237,255,.25) plus a rgba(131,56,138,.08) tint.
			a.chrome(gtx, sorg, image.Rectangle{Max: sz}, gtx.Dp(4), bg, theme.SearchTint)
		}, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(sw, sh)
			at(gtx, image.Pt(gtx.Dp(10), (sh-gtx.Dp(16))/2), sz, func(gtx layout.Context) layout.Dimensions {
				return icons.Search(gtx, 16, c.SearchText)
			})
			label := "Search " + a.teamName()
			if q := a.searchQuery; q != "" {
				label = q
			}
			at(gtx, image.Pt(gtx.Dp(34), 0), image.Pt(sw-gtx.Dp(44), sh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.SearchText, label)
				})
			})
			return layout.Dimensions{Size: sz}
		})
	})
	if clicked(a.clicks.get("nav.search")) {
		a.openSearch("")
	}

	// help button at the right edge: Slack's HelpButton. Plugins add
	// buttons before it (StreamerMode, Taut's What's new), so lay it out
	// right-aligned in the space after the search pill.
	horg := image.Pt(width-gtx.Dp(34), y)
	rs := clsTopNavRight.Enter(gtx, 0, 0)
	call, d := measured(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(width-gtx.Dp(8)-(sorg.X+sw+gtx.Dp(8)), b), h)}
		return slackc.Component(gtx, "HelpButton", navHelp{a: a, org: horg, b: b}, nil, navHelp.layout)
	})
	rs.Leave()
	place(gtx, call, image.Pt(width-gtx.Dp(8)-d.Size.X, (h-d.Size.Y)/2))
	if clicked(a.clicks.get("nav.help")) {
		a.Toast("Keyboard shortcuts: Ctrl+K switch, Ctrl+Shift+A unreads, Alt+↑/↓ channels, Ctrl+. side pane")
	}
}

// ---- tab rail (70px) ----

type railTab struct {
	key, label, icon string
}

var railTabs = []railTab{
	{"home", "Home", "home"},
	{"dms", "DMs", "dms"},
	{"activity", "Activity", "activity"},
	{"files", "Files", "files"},
	{"later", "To-dos", "check-circle"},
	{"more", "Agents & tools", "agents"},
}

func (a *App) layoutRail(gtx layout.Context, top, height int) {
	c := &a.th.C
	// workspace icon 36×36 at (17, top+8), radius 8.
	ws := gtx.Dp(36)
	x0 := gtx.Dp(17)
	at(gtx, image.Pt(x0, top+gtx.Dp(8)), image.Pt(ws, ws), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("rail.team"), nil, func(gtx layout.Context) layout.Dimensions {
			return a.teamIcon(gtx, ws)
		})
	})
	if clicked(a.clicks.get("rail.team")) {
		a.Navigate("home")
	}

	y := top + gtx.Dp(61)
	active := a.activeRail()
	for _, t := range railTabs {
		t := t
		btn := a.clicks.get("rail." + t.key)
		th := gtx.Dp(68)
		lines := strings.SplitN(strings.Replace(t.label, " & ", "\n& ", 1), "\n", 2)
		if len(lines) == 2 {
			th = gtx.Dp(80) // Slack wraps "Agents & tools" onto two lines
		}
		org := image.Pt(gtx.Dp(9), y)
		at(gtx, org, image.Pt(gtx.Dp(52), th), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				sel := active == t.key
				tile := image.Rect(gtx.Dp(8), gtx.Dp(8), gtx.Dp(44), gtx.Dp(44))
				if sel {
					a.chrome(gtx, org, tile, gtx.Dp(8), c.ChromeActive)
				} else if btn.Hovered() {
					a.chrome(gtx, org, tile, gtx.Dp(8), c.ChromeHover)
				}
				name := t.icon
				if sel && icons.Has(name+"-filled") {
					name += "-filled"
				}
				at(gtx, image.Pt(tile.Min.X+gtx.Dp(8), tile.Min.Y+gtx.Dp(8)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(name)(gtx, 20, c.ChromeText)
				})
				// label 700 11px/12px, centered under the tile.
				for li, ln := range lines {
					ln := ln
					lw := a.th.Measure(gtx, 11, theme.Bold, ln)
					at(gtx, image.Pt((gtx.Dp(52)-lw)/2, gtx.Dp(48)+li*gtx.Dp(12)), image.Pt(lw+2, gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 11, theme.Bold, c.ChromeText, ln)
					})
				}
				// unread badge / dot on the tile's top-right corner.
				if n, dot := a.railBadge(t.key); n > 0 {
					at(gtx, image.Pt(tile.Max.X-gtx.Dp(10), tile.Min.Y-gtx.Dp(5)), image.Pt(gtx.Dp(40), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
						return a.railCount(gtx, n)
					})
				} else if dot {
					theme.Circle(gtx, image.Pt(tile.Max.X-gtx.Dp(2), tile.Min.Y+gtx.Dp(2)), gtx.Dp(8), c.ChromeText)
				}
				return layout.Dimensions{Size: image.Pt(gtx.Dp(52), th)}
			})
		})
		if clicked(btn) {
			a.railClick(t.key)
		}
		y += th
	}

	// control strip: + (new), theme toggle, avatar with presence.
	cs := gtx.Dp(36)
	by := top + height - gtx.Dp(164) + gtx.Dp(4)
	for _, k := range []string{"rail.new", "rail.theme"} {
		k := k
		icon := "plus"
		if k == "rail.theme" {
			icon = "moon"
			if a.th.Dark {
				icon = "sun"
			}
		}
		org := image.Pt(x0, by)
		at(gtx, org, image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
			btn := a.clicks.get(k)
			bg := c.SidebarButton
			if btn.Hovered() {
				bg = c.SearchHover
			}
			return clickArea(gtx, btn, func(gtx layout.Context, sz image.Point, hovered bool) {
				a.chrome(gtx, org, image.Rectangle{Max: sz}, sz.X/2, bg)
			}, func(gtx layout.Context) layout.Dimensions {
				off := (cs - gtx.Dp(20)) / 2
				fg := theme.Over(c.ChromeTextDim, theme.Over(bg, a.chromeBase(org.Add(image.Pt(cs/2, cs/2)))))
				at(gtx, image.Pt(off, off), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 20, fg)
				})
				return layout.Dimensions{Size: image.Pt(cs, cs)}
			})
		})
		by += gtx.Dp(52)
	}
	if clicked(a.clicks.get("rail.new")) {
		a.compose()
	}
	if clicked(a.clicks.get("rail.theme")) {
		a.ToggleDark()
	}
	at(gtx, image.Pt(x0, by), image.Pt(cs+gtx.Dp(8), cs+gtx.Dp(8)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("rail.me"), nil, func(gtx layout.Context) layout.Dimensions {
			a.avatar(gtx, a.selfAvatar(), a.selfName(), cs, gtx.Dp(8))
			// presence ring: 20px circle at the bottom-right, chrome-colored ring.
			pc := image.Pt(cs-gtx.Dp(4), cs-gtx.Dp(4))
			theme.Circle(gtx, pc, gtx.Dp(14), c.ChromeB)
			if a.selfActive() {
				theme.Circle(gtx, pc, gtx.Dp(10), c.Presence)
			} else {
				theme.Circle(gtx, pc, gtx.Dp(10), c.ChromeTextDim)
				theme.Circle(gtx, pc, gtx.Dp(5), c.ChromeB)
			}
			return layout.Dimensions{Size: image.Pt(cs, cs)}
		})
	})
	if clicked(a.clicks.get("rail.me")) {
		a.openSelfMenu(image.Pt(x0+cs, by))
	}
}

// chromeButton is an icon button on the chrome whose hover tile is
// composited in sRGB. org is the button's window position.
func (a *App) chromeButton(gtx layout.Context, org image.Point, c *widget.Clickable, name string, box, iconSz unit.Dp, col color.NRGBA, radius unit.Dp) layout.Dimensions {
	return clickArea(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
		if hovered {
			a.chrome(gtx, org, image.Rectangle{Max: sz}, gtx.Dp(radius), a.th.C.ChromeHover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		s := gtx.Dp(box)
		is := gtx.Dp(iconSz)
		off := (s - is) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		icons.Get(name)(gtx, iconSz, col)
		t.Pop()
		return layout.Dimensions{Size: image.Pt(s, s)}
	})
}

// railCount draws the small count bubble on a rail tile.
func (a *App) railCount(gtx layout.Context, n int) layout.Dimensions {
	c := &a.th.C
	txt := itoa(n)
	h := gtx.Dp(18)
	tw := a.th.Measure(gtx, 11, theme.Bold, txt)
	w := tw + gtx.Dp(10)
	if w < h {
		w = h
	}
	theme.RRect(gtx, image.Rect(0, 0, w, h).Inset(-gtx.Dp(2)), h/2+gtx.Dp(2), c.ChromeB)
	theme.RRect(gtx, image.Rect(0, 0, w, h), h/2, c.Badge)
	at(gtx, image.Pt((w-tw)/2, 0), image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 11, theme.Bold, c.BadgeText, txt)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// teamIcon draws the workspace icon (image, or initials on aubergine).
func (a *App) teamIcon(gtx layout.Context, s int) layout.Dimensions {
	r := s * 2 / 9
	if url := a.teamIconURL(); url != "" {
		if img, ok := a.images.Get(url, s*2); ok {
			return theme.Image(gtx, img, image.Pt(s, s), r)
		}
	}
	theme.RRect(gtx, image.Rect(0, 0, s, s), r, theme.WithAlpha(a.th.C.ChromeText, 0x33))
	name := a.teamName()
	ini := "Y"
	if name != "" {
		ini = string([]rune(name)[:1])
	}
	tw := a.th.Measure(gtx, 20, theme.Black, ini)
	at(gtx, image.Pt((s-tw)/2, 0), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, s, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 20, theme.Black, a.th.C.ChromeText, ini)
		})
	})
	return layout.Dimensions{Size: image.Pt(s, s)}
}

// avatar draws a user image (or a colored initial tile while loading).
func (a *App) avatar(gtx layout.Context, url, name string, s, radius int) layout.Dimensions {
	if url != "" {
		if img, ok := a.images.Get(url, s*2); ok {
			return theme.Image(gtx, img, image.Pt(s, s), radius)
		}
	}
	theme.RRect(gtx, image.Rect(0, 0, s, s), radius, a.th.C.Skeleton)
	return layout.Dimensions{Size: image.Pt(s, s)}
}
