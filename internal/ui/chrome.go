package ui

import (
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"unicode"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	tn := clsTopNav.Enter(gtx, 0, a.scopeMod())
	defer tn.Leave()
	if tn.Hidden() {
		return
	}
	left := clsTopNavLeft.Enter(gtx, 0, 0)
	a.appMenuButton(gtx, h) // appmenu.go: Linux and Windows
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
	if clicked(a.clicks.get("nav.history")) { // the rightmost of the three
		hx := primaryX - gtx.Dp(9) - b
		a.openHistoryMenu(image.Rect(hx, y, hx+b, y+b))
	}

	// search pill: starts at the primary panel's left edge. Slack's middle
	// container is flex 2 1 0 (max 1000px) beside a right container of
	// flex 1 0 auto (~88px of buttons): ⅔ of what's left after the 4px
	// right padding, the sidebar edge and those 88px.
	sw := (width - gtx.Dp(4) - primaryX - gtx.Dp(88)) * 2 / 3
	if max := gtx.Dp(1000); sw > max {
		sw = max
	}
	if sw < gtx.Dp(200) {
		sw = gtx.Dp(200)
	}
	sh := gtx.Dp(28)
	sorg := image.Pt(primaryX, (h-sh)/2)
	a.searchPill = image.Rectangle{Min: sorg, Max: sorg.Add(image.Pt(sw, sh))}
	at(gtx, sorg, image.Pt(sw, sh), func(gtx layout.Context) layout.Dimensions {
		d := clickArea(gtx, a.clicks.get("nav.search"), func(gtx layout.Context, sz image.Point, hovered bool) {
			bg := c.SearchBg
			if hovered {
				bg = c.SearchHover
			}
			// Slack: rgba(249,237,255,.25) plus a rgba(131,56,138,.08) tint.
			a.chrome(gtx, sorg, image.Rectangle{Max: sz}, gtx.Dp(4), bg, theme.SearchTint)
		}, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(sw, sh)
			if a.searchBoxIcon() {
				at(gtx, image.Pt(gtx.Dp(10), (sh-gtx.Dp(16))/2), sz, func(gtx layout.Context) layout.Dimensions {
					return icons.Search(gtx, 16, c.SearchText)
				})
			}
			tx, tw := a.searchBoxTextX(gtx), sw-gtx.Dp(44)
			if a.searchQuery != "" {
				tw = sw - gtx.Dp(26) - tx // the clear button's 26px
			}
			at(gtx, image.Pt(tx, 0), image.Pt(tw, sh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.SearchText, a.searchBoxText())
				})
			})
			return layout.Dimensions{Size: sz}
		})
		a.searchBoxPress(gtx, d.Size) // BaseSearch onMouseDown
		a.searchBoxClear(gtx, sw, sh)
		return d
	})
	if clicked(a.clicks.get("nav.search")) {
		a.searchBoxClicked()
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
		a.helpClick() // update_ui.go: the Help flexpane while an update waits
	}
}

// ---- tab rail (70px) ----

type railTab struct {
	key, label, icon, sub string
	id                    string // Slack's tab id (core.Tab*)
}

// railKey maps Slack's tab ids to Clack's rail keys (routes).
func railKey(id string) string {
	switch id {
	case core.TabActivity, core.TabActivityInbox, core.TabActivityProto:
		return "activity"
	case core.TabFiles:
		return "files"
	}
	return id // home, dms, later, platform, browse
}

// railTabFor is a tab's label and icon (q0zJ icons, mBDz labels).
func (a *App) railTabFor(id string) railTab {
	t := a.model().Rail.Tabs[id]
	rt := railTab{key: railKey(id), id: id, label: t.Label, sub: t.Subtitle}
	switch id {
	case core.TabHome:
		rt.icon = "home"
	case core.TabDMs:
		rt.icon = "dms"
	case core.TabActivity, core.TabActivityInbox, core.TabActivityProto:
		rt.icon = "activity"
	case core.TabFiles:
		rt.icon = "files"
	case core.TabLater:
		rt.icon = "bookmark"
		if t.Label == "To-dos" {
			rt.icon = "todos"
		}
	case core.TabPlatform, core.TabAgents:
		rt.icon = "agents"
	case core.TabBrowse:
		rt.icon = "more"
	}
	if rt.label == "" { // before the first snapshot
		rt.label = map[string]string{core.TabHome: "Home", core.TabDMs: "DMs", core.TabActivity: "Activity",
			core.TabActivityInbox: "Activity", core.TabFiles: "Files", core.TabLater: "Later",
			core.TabPlatform: "Tools", core.TabBrowse: "More"}[id]
	}
	rt.label = railNS.T(rt.label)
	return rt
}

// railNS: the rail's tab names are the client's ("Home", "DMs",
// "Activity", "Files"), More and Later the view's.
var railNS = i18n.NewChain("client", "view", "saved_for_later")

// railTall is useTabRailOrganizer's (4v2a) per-locale table of the tabs
// whose label wraps onto two lines: each costs 80 of the rail's budget
// instead of 68.
var railTall = map[string][]string{
	"de-DE": {core.TabPlatform},
	"es-ES": {core.TabDMs, core.TabPlatform},
	"es-LA": {core.TabDMs, core.TabPlatform},
	"fr-FR": {core.TabDMs, core.TabPlatform},
	"it-IT": {core.TabPlatform},
	"pt-BR": {core.TabPlatform},
	"ja-JP": {core.TabActivity, core.TabActivityInbox},
	"zh-CN": {core.TabLater},
}

// railTabH is a tab's height (dp) with n label lines: 8px padding, the
// 36px tile, a 4px gap, the 12px lines and 8px padding
// (.p-tab_rail__button .c-tabs__tab_content).
func railTabH(n int) int { return 56 + 12*max(n, 1) }

// railLabelLines is a tab's label as Slack's rail wraps it: the tab
// content is 52px of 11px bold text with white-space normal
// (.p-tab_rail__button .c-tabs__tab_content), so a label breaks at its
// spaces and between CJK characters (i18n.LineChunks): "Mensajes
// directos" (es-ES DMs) and アクティビティ (ja-JP Activity) take two lines.
// Agents & tools is balanced (.p-tab_rail__button__label--agents-tools:
// text-wrap balance). The label is hyphens:auto too, which Blink applies
// only with a hyphenation dictionary for the locale; Clack has none, so a
// word wider than the tab keeps its line and overflows (layoutRail). Only
// en-US and en-GB's Agents & tools label ellipsizes
// (.p-tab_rail__button__label--en), and its words fit.
func (a *App) railLabelLines(gtx layout.Context, t railTab) []string {
	w := gtx.Dp(52)
	lines := a.railWrap(gtx, t.label, w)
	if t.id == core.TabPlatform && len(lines) > 1 {
		lo, hi := 1, w // the narrowest width that keeps the line count
		for lo < hi {
			if mid := (lo + hi) / 2; len(a.railWrap(gtx, t.label, mid)) <= len(lines) {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		lines = a.railWrap(gtx, t.label, hi)
	}
	return lines
}

// railWrap breaks label into lines of at most w px, as many chunks to a
// line as fit (a chunk wider than w gets a line of its own).
func (a *App) railWrap(gtx layout.Context, label string, w int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(label) {
		for i, c := range i18n.LineChunks(word) {
			next := cur + c
			if i == 0 && cur != "" {
				next = cur + " " + c
			}
			if cur != "" && a.th.Measure(gtx, 11, theme.Bold, next) > w {
				lines = append(lines, cur)
				next = c
			}
			cur = next
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// railFit runs Slack's tab rail organizer for the space between the first
// tab and the control strip (px), returning the rail and More's tabs.
func (a *App) railFit(gtx layout.Context, space int) (show, overflow []string) {
	r := a.model().Rail
	if len(r.Rail) == 0 { // no snapshot yet: Slack's defaults
		r.Rail = []string{core.TabHome, core.TabDMs, core.TabActivityInbox}
		r.Available = r.Rail
	}
	h := func(id string) int {
		b := core.RailTabHeight(r.Compact)
		if !r.Compact && slices.Contains(railTall[i18n.Locale()], id) {
			b = 80
		}
		if n := len(a.railLabelLines(gtx, a.railTabFor(id))); n > 1 {
			// the lines Clack draws (Slack's table keeps 68 for en-US's
			// two-line Agents & tools; Clack budgets what it draws)
			b = max(b, railTabH(n))
		}
		return b
	}
	return core.OrganizeRail(r, int(float32(space)/gtx.Metric.PxPerDp), h)
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
	// the status tab over the avatar (rail_status.go) makes the user
	// button 24px taller as it comes in: the strip above it moves up
	te, tshow := a.railStatusFrame(gtx)
	lift := 0
	if tshow {
		lift = int(float64(gtx.Dp(24))*te + .5)
	}
	show, overflow := a.railFit(gtx, height-gtx.Dp(61)-gtx.Dp(164)-lift)
	a.railOverflow = overflow
	active := a.activeRail()
	for _, k := range overflow { // the active tab is under More: More is active (Zxh6)
		if railKey(k) == active {
			active = "browse"
		}
	}
	for _, id := range show {
		t := a.railTabFor(id)
		btn := a.clicks.get("rail." + t.key)
		lines := a.railLabelLines(gtx, t)
		th := gtx.Dp(unit.Dp(railTabH(len(lines))))
		org := image.Pt(gtx.Dp(9), y)
		if a.railRects != nil { // tests: each button's box, window px
			a.railRects[t.key] = image.Rectangle{Min: org, Max: org.Add(image.Pt(gtx.Dp(52), th))}
		}
		// More is a PeekTrigger: hovering opens its menu (rail_peek.go),
		// and it keeps the hover tile while the menu shows
		// (.p-peek_trigger--is-peek-visible)
		peek := false
		if t.key == "browse" {
			a.railMoreFrame(gtx, image.Rectangle{Min: org, Max: org.Add(image.Pt(gtx.Dp(52), th))}, btn.Hovered())
			peek = a.railPeekOpen()
		} else if a.hasTabPeek(t.key) { // a TabPeekTrigger (rail_tab_peek.go)
			a.tabPeekFrame(gtx, t.key, image.Rectangle{Min: org, Max: org.Add(image.Pt(gtx.Dp(52), th))}, btn.Hovered(), active == t.key)
			peek = a.tabPeekOpen(t.key)
		}
		at(gtx, org, image.Pt(gtx.Dp(52), th), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				sel := active == t.key
				tile := image.Rect(gtx.Dp(8), gtx.Dp(8), gtx.Dp(44), gtx.Dp(44))
				// .p-tab_rail__button__icon: hover, :active, --active and a
				// visible peek all fill it theme-surf-inv-pry
				if sel || btn.Hovered() || peek {
					a.chrome(gtx, org, tile, gtx.Dp(8), c.ChromeActive)
				}
				name := t.icon
				if sel && icons.Has(name+"-filled") {
					name += "-filled"
				}
				// .p-tab_rail__button:hover (and a visible peek) scales
				// __icon_inner to 1.15 about its centre: the 20px icon
				// draws at 23
				isz := unit.Dp(20)
				if btn.Hovered() || peek {
					isz = 23
				}
				io := (tile.Dx() - gtx.Dp(isz) + 1) / 2
				at(gtx, tile.Min.Add(image.Pt(io, io)), image.Pt(gtx.Dp(isz), gtx.Dp(isz)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(name)(gtx, isz, c.ChromeText)
				})
				// unread badge / dot on the tile's top-right corner. The
				// badge's right edge is fixed (.p-tab_rail__button .c-badge
				// {top:4px; right:2px}, absolute in the 52px tab), so 10
				// and 99+ grow left, inside the tab, not past its clip.
				if n, dot := a.railBadge(t.key); n > 0 {
					// .p-tab_rail__button .c-badge: top 4px, right 2px
					bw := a.railCountWidth(gtx, n)
					at(gtx, image.Pt(gtx.Dp(52)-gtx.Dp(2)-bw, gtx.Dp(4)), image.Pt(bw, gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
						return a.railCount(gtx, n)
					})
				} else if dot {
					// .p-unread_dot: 6px, theme-base-imp; in the rail button
					// top 12px, right 12px (centre 37, 15 of the 52px box)
					theme.Circle(gtx, image.Pt(gtx.Dp(37), gtx.Dp(15)), gtx.Dp(6), c.RailBadge) // the rail's theme scope
				}
				return layout.Dimensions{Size: image.Pt(gtx.Dp(52), th)}
			})
		})
		// the label, 700 11px/12px under the tile, each line centered
		// (.p-tab_rail__tab_menu .c-tabs__tab: text-align center). A line
		// wider than the tab starts at its left edge and runs on to the
		// rail's (CSS start-aligns an overflowing line; .p-tab_rail is
		// overflow hidden), past the button's clip.
		lc := clip.Rect{Min: image.Pt(0, org.Y), Max: image.Pt(gtx.Dp(70), org.Y+th)}.Push(gtx.Ops)
		for li, ln := range lines {
			lw := a.th.Measure(gtx, 11, theme.Bold, ln)
			lo := org.Add(image.Pt(max((gtx.Dp(52)-lw)/2, 0), gtx.Dp(48)+li*gtx.Dp(12)))
			at(gtx, lo, image.Pt(lw+2, gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 11, theme.Bold, c.ChromeText, ln)
			})
		}
		lc.Pop()
		if clicked(btn) {
			if t.key == "browse" {
				a.railMoreClick() // forceOpenOnClick
			} else {
				a.railClick(t.key, org)
			}
		}
		y += th
	}
	a.tabPeekEndFrame()

	// control strip: + (new), Slack's focus mode button when the account
	// has it (SilentSidebarTrigger, focus_mode.go), avatar with presence.
	cs := gtx.Dp(36)
	by := top + height - gtx.Dp(164) + gtx.Dp(4)
	if !a.focusModeOn() {
		by += gtx.Dp(52) // the strip keeps its bottom
	}
	newY := by - lift
	for _, k := range []string{"rail.new", "rail.focus"} {
		k := k
		if k == "rail.focus" {
			if a.focusModeOn() {
				a.focusButton(gtx, image.Pt(x0, by-lift), cs)
				by += gtx.Dp(52)
			}
			continue
		}
		icon := "plus"
		org := image.Pt(x0, by-lift)
		at(gtx, org, image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
			btn := a.clicks.get(k)
			bg := c.ChromeActive // theme-surf-inv-pry in the rail's scope
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
		org := image.Pt(x0, newY) // the + button (first in the strip)
		a.openCreateMenu(image.Rectangle{Min: org, Max: org.Add(image.Pt(cs, cs))})
	}
	if tshow { // under the avatar (.c-avatar is positioned and comes later)
		a.railStatusTabDraw(gtx, image.Pt(x0, by), cs, te)
	}
	// the user button: the tab's part above the avatar, then the avatar
	at(gtx, image.Pt(x0, by-lift), image.Pt(cs+gtx.Dp(8), cs+gtx.Dp(8)+lift), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("rail.me"), nil, func(gtx layout.Context) layout.Dimensions {
			defer op.Offset(image.Pt(0, lift)).Push(gtx.Ops).Pop()
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
			return layout.Dimensions{Size: image.Pt(cs, cs+lift)}
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

// railCount draws a rail tab's count: Slack's Badge (client-boot, Ex) as
// useGetMentions renders it on the rail, <span class="c-badge c-badge--circle
// c-badge--themed">, maxCount 99 ("99+", c-badge--max). .c-badge--circle is
// 16px tall, 10px bold text (line-height 1) with 5px side padding,
// --max 7px; .c-badge--themed fills it theme-base-imp under
// theme-content-imp. Nothing rings it: no border, outline or shadow (the
// 2px rail-coloured ring Clack drew read as a border over the hover and
// selected tiles). The caller places it (.p-tab_rail__button .c-badge:
// top 4px, right 2px of the 52px button).
func (a *App) railCount(gtx layout.Context, n int) layout.Dimensions {
	c := &a.th.C
	txt, pad := itoa(n), gtx.Dp(5)
	if n > 99 {
		txt, pad = "99+", gtx.Dp(7)
	}
	h := gtx.Dp(16)
	tw := a.th.Measure(gtx, 10, theme.Bold, txt)
	w := tw + 2*pad
	theme.RRect(gtx, image.Rect(0, 0, w, h), h/2, c.RailBadge) // the rail's theme scope
	at(gtx, image.Pt(pad, 0), image.Pt(tw+2, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 10, theme.Bold, c.RailBadgeText, txt)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// railCountWidth is railCount's width for n (px).
func (a *App) railCountWidth(gtx layout.Context, n int) int {
	txt, pad := itoa(n), gtx.Dp(5)
	if n > 99 {
		txt, pad = "99+", gtx.Dp(7)
	}
	return a.th.Measure(gtx, 10, theme.Bold, txt) + 2*pad
}

// teamIcon draws the workspace icon (image, or initials on aubergine).
func (a *App) teamIcon(gtx layout.Context, s int) layout.Dimensions {
	r := s * 2 / 9
	if url := a.teamIconURL(); url != "" {
		if img, ok := a.images.Get(url, s*2); ok {
			a.teamIconShown = url
			return theme.Image(gtx, img, image.Pt(s, s), r)
		}
		// a rotated icon (team_icon_change) loads under a new URL: keep the
		// previous one until it is ready
		if prev := a.teamIconShown; prev != "" && prev != url {
			if img, ok := a.images.Get(prev, s*2); ok {
				return theme.Image(gtx, img, image.Pt(s, s), r)
			}
		}
	}
	theme.RRect(gtx, image.Rect(0, 0, s, s), r, theme.WithAlpha(a.th.C.ChromeText, 0x33))
	ini := teamInitials(a.teamName())
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

// teamInitials is mAu1.getTeamInitials(name, {limit: 2}): the first letter
// of each word (split on spaces, "|" and "-"), uppercased.
func teamInitials(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == ' ' || r == '-' || r == '|' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	var out []rune
	for _, w := range strings.FieldsFunc(b.String(), func(r rune) bool { return r == ' ' || r == '|' || r == '-' }) {
		if len(out) == 2 {
			break
		}
		out = append(out, []rune(w)[0])
	}
	if len(out) == 0 {
		rs := []rune(name)
		if len(rs) > 2 {
			rs = rs[:2]
		}
		return string(rs)
	}
	return strings.ToUpper(string(out))
}

// railCountText is the tabs' Badge text (InjD useGetMentions: S1+c Ex
// with maxCount 99): the count, or "99+" past it.
func railCountText(n int) string {
	if n > 99 {
		return "99+"
	}
	return itoa(n)
}
