package ui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// gradient renders Slack's window background:
//
//	radial-gradient(circle, A 20%, transparent 80%),
//	conic-gradient(from 45deg, B 0%, A 25%, B 50%, A 75%, B 100%)
//
// into a small image (1/8 scale) that the GPU upsamples with linear
// filtering. Re-rendered only when the window size or palette changes.
type gradient struct {
	size image.Point
	pal  [2]color.NRGBA
	img  paint.ImageOp
	ok   bool
}

const gradScale = 8

func (g *gradient) layout(gtx layout.Context, a, b color.NRGBA) {
	sz := gtx.Constraints.Max
	if !g.ok || g.size != sz || g.pal != [2]color.NRGBA{a, b} {
		g.size, g.pal, g.ok = sz, [2]color.NRGBA{a, b}, true
		g.img = paint.NewImageOp(renderGradient(sz, a, b))
		g.img.Filter = paint.FilterLinear
	}
	is := g.img.Size()
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	sx := float32(sz.X) / float32(is.X)
	sy := float32(sz.Y) / float32(is.Y)
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx, sy))).Push(gtx.Ops).Pop()
	g.img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func renderGradient(sz image.Point, a, b color.NRGBA) *image.RGBA {
	w, h := sz.X/gradScale+2, sz.Y/gradScale+2
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	far := math.Hypot(cx, cy)
	lerp := func(x, y uint8, t float64) float64 { return float64(x) + (float64(y)-float64(x))*t }
	i := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)+.5-cx, float64(y)+.5-cy
			// conic: CSS angle 0 = up, clockwise; "from 45deg".
			ang := math.Atan2(dx, -dy)*180/math.Pi - 45
			for ang < 0 {
				ang += 360
			}
			q := math.Mod(ang, 180) / 90 // 0..2: B→A→B
			t := q
			if t > 1 {
				t = 2 - t
			}
			r := lerp(b.R, a.R, t)
			g := lerp(b.G, a.G, t)
			bb := lerp(b.B, a.B, t)
			// radial overlay: A at alpha 1 until 20%, fading to 0 at 80%.
			d := math.Hypot(dx, dy) / far
			al := 1.0
			if d > .2 {
				al = 1 - (d-.2)/.6
				if al < 0 {
					al = 0
				}
			}
			r = r + (float64(a.R)-r)*al
			g = g + (float64(a.G)-g)*al
			bb = bb + (float64(a.B)-bb)*al
			img.Pix[i+0] = uint8(r + .5)
			img.Pix[i+1] = uint8(g + .5)
			img.Pix[i+2] = uint8(bb + .5)
			img.Pix[i+3] = 0xff
			i += 4
		}
	}
	return img
}

// ---- top nav (40px) ----

func (a *App) layoutTopNav(gtx layout.Context, primaryX, width int) {
	c := &a.th.C
	h := gtx.Dp(theme.TopNavH)
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
		at(gtx, image.Pt(x, y), image.Pt(b, b), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, btn, name, 26, 18, col, c.ChromeHover, 4)
		})
		x -= b + gap
	}
	if a.clicks.get("nav.back").Clicked(gtx) {
		a.Back()
	}
	if a.clicks.get("nav.forward").Clicked(gtx) {
		a.Forward()
	}
	if a.clicks.get("nav.history").Clicked(gtx) {
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
	at(gtx, image.Pt(primaryX, (h-sh)/2), image.Pt(sw, sh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("nav.search"), func(gtx layout.Context, sz image.Point, hovered bool) {
			bg := c.SearchBg
			if hovered {
				bg = c.SearchHover
			}
			theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), bg)
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
	if a.clicks.get("nav.search").Clicked(gtx) {
		a.openSearch("")
	}

	// help button at the right edge.
	at(gtx, image.Pt(width-gtx.Dp(34), y), image.Pt(b, b), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, a.clicks.get("nav.help"), "help", 26, 20, c.ChromeText, c.ChromeHover, 4)
	})
	if a.clicks.get("nav.help").Clicked(gtx) {
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
	{"later", "Later", "later"},
	{"more", "More", "more"},
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
	if a.clicks.get("rail.team").Clicked(gtx) {
		a.Navigate("home")
	}

	y := top + gtx.Dp(61)
	active := a.activeRail()
	for _, t := range railTabs {
		t := t
		btn := a.clicks.get("rail." + t.key)
		th := gtx.Dp(68)
		at(gtx, image.Pt(gtx.Dp(9), y), image.Pt(gtx.Dp(52), th), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				sel := active == t.key
				tile := image.Rect(gtx.Dp(8), gtx.Dp(8), gtx.Dp(44), gtx.Dp(44))
				if sel {
					theme.RRect(gtx, tile, gtx.Dp(8), c.ChromeActive)
				} else if btn.Hovered() {
					theme.RRect(gtx, tile, gtx.Dp(8), c.ChromeHover)
				}
				name := t.icon
				if sel && icons.Has(name+"-filled") {
					name += "-filled"
				}
				at(gtx, image.Pt(tile.Min.X+gtx.Dp(8), tile.Min.Y+gtx.Dp(8)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(name)(gtx, 20, c.ChromeText)
				})
				// label 700 11px/12px, centered under the tile.
				lw := a.th.Measure(gtx, 11, theme.Bold, t.label)
				at(gtx, image.Pt((gtx.Dp(52)-lw)/2, gtx.Dp(48)), image.Pt(lw+2, gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 11, theme.Bold, c.ChromeText, t.label)
				})
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
		if btn.Clicked(gtx) {
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
		at(gtx, image.Pt(x0, by), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
			btn := a.clicks.get(k)
			return clickArea(gtx, btn, func(gtx layout.Context, sz image.Point, hovered bool) {
				bg := c.SidebarButton
				if hovered {
					bg = c.SearchHover
				}
				theme.Circle(gtx, image.Pt(sz.X/2, sz.Y/2), sz.X, bg)
			}, func(gtx layout.Context) layout.Dimensions {
				off := (cs - gtx.Dp(20)) / 2
				at(gtx, image.Pt(off, off), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 20, c.ChromeTextDim)
				})
				return layout.Dimensions{Size: image.Pt(cs, cs)}
			})
		})
		by += gtx.Dp(52)
	}
	if a.clicks.get("rail.new").Clicked(gtx) {
		a.compose()
	}
	if a.clicks.get("rail.theme").Clicked(gtx) {
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
	if a.clicks.get("rail.me").Clicked(gtx) {
		a.openSelfMenu(image.Pt(x0+cs, by))
	}
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

var _ = unit.Dp(0)
