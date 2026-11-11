package views

// Shared drawing kit for every view: palette, text, avatars, rows, tabs,
// toggles, search boxes, buttons, async loading and time formatting.
// Sizes and colors come from the real Slack client (measured in the
// browser with getComputedStyle); see the per-view files for specifics.

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

type (
	C = layout.Context
	D = layout.Dimensions
	W = layout.Widget
)

// ---- palette ----

// pal is the set of colors views use. It is derived from the theme's
// light/dark flag; values are Slack's own (measured from the web client).
type pal struct {
	Bg          color.NRGBA // view background
	Text        color.NRGBA // primary text
	Muted       color.NRGBA // secondary text (rgba(29,28,29,.7))
	Faint       color.NRGBA // tertiary text, placeholders
	Border      color.NRGBA // hairlines (rgba(29,28,29,.13))
	BorderStr   color.NRGBA // input borders (rgba(29,28,29,.3))
	Hover       color.NRGBA // row hover
	SelBg       color.NRGBA // selected row (Slack blue)
	SelText     color.NRGBA
	SelMuted    color.NRGBA
	Link        color.NRGBA
	Green       color.NRGBA // primary button
	GreenHover  color.NRGBA
	Red         color.NRGBA // danger text / mention badge
	Badge       color.NRGBA
	Presence    color.NRGBA
	Skel        color.NRGBA
	Focus       color.NRGBA // focus ring
	Chip        color.NRGBA // filter chip background
	ChipSel     color.NRGBA
	ChipSelText color.NRGBA
	Surface     color.NRGBA // cards, dialogs
	Overlay     color.NRGBA // modal scrim
	Mention     color.NRGBA // @mention highlight bg
	Yellow      color.NRGBA
	TabLine     color.NRGBA
	Sec         color.NRGBA // secondary text, unselected tabs, icon buttons (#454447)
	TitleText   color.NRGBA // view titles (theme content, #39063a)
	Accent      color.NRGBA // theme accent: tab underline, unread dots/badges (#83388a)
	AccentText  color.NRGBA // text on Accent (#f9edff)
	ReadFill    color.NRGBA // read rows (#f8f8f8)
	Track       color.NRGBA // segmented-control track, placeholders (#eaeaea)
	PillOnBg    color.NRGBA // pressed quick filter (#e3f8ff)
	PillOnFg    color.NRGBA // (#1264a3)
	TabHover    color.NRGBA // #611f69@.07
	Pressed     color.NRGBA // pressed overlay #454447@.13
	CardLine    color.NRGBA // card/list frames (#1d1c1d@.1)
	SegLine     color.NRGBA // segment button borders (#1d1c1d@.3)
	Meta        color.NRGBA // metadata text (#616061)
	Ring        color.NRGBA // popover/tile rings (#1d1c1d@.13)
	Page        color.NRGBA // gray page background (#f8f8f8)

	// Activity's pressed quick filter (Unreads):
	// .p-activity_ia4_page__filter_bar--active, light-dark(...) and its
	// :hover.
	QuickBg, QuickBorder, QuickFg                color.NRGBA
	QuickHoverBg, QuickHoverBorder, QuickHoverFg color.NRGBA
}

// Light values are Slack's design tokens (--dt_color-*), measured in the
// web client with the default Aubergine theme.
var lightPal = pal{
	Bg:          rgb(0xffffff),
	Text:        rgb(0x1d1c1d),
	Muted:       rgb(0x5e5d60),
	Faint:       rgb(0x7c7a7f),
	Border:      argb(0x215e5d60),
	BorderStr:   argb(0x735e5d60),
	Hover:       argb(0x0f454447),
	SelBg:       rgb(0xfffae0),
	SelText:     rgb(0x1d1c1d),
	SelMuted:    rgb(0x5e5d60),
	Link:        rgb(0x1264a3),
	Green:       rgb(0x007a5a),
	GreenHover:  rgb(0x148567),
	Red:         rgb(0xc01343),
	Badge:       rgb(0x83388a),
	Presence:    rgb(0x2bac76),
	Skel:        rgb(0xeaeaea),
	Focus:       argb(0x4d1d9bd1),
	Chip:        argb(0x0f454447),
	ChipSel:     rgb(0xe3f8ff),
	ChipSelText: rgb(0x1264a3),
	Surface:     rgb(0xffffff),
	Overlay:     argb(0x99000000),
	Mention:     argb(0x331d9bd1),
	Yellow:      rgb(0xfffae0),
	TabLine:     rgb(0x83388a),
	Sec:         rgb(0x454447),
	TitleText:   rgb(0x39063a),
	Accent:      rgb(0x83388a),
	AccentText:  rgb(0xf9edff),
	ReadFill:    rgb(0xf8f8f8),
	Track:       rgb(0xeaeaea),
	PillOnBg:    rgb(0xe3f8ff),
	PillOnFg:    rgb(0x1264a3),
	TabHover:    argb(0x12611f69),
	Pressed:     argb(0x21454447),
	CardLine:    argb(0x1a1d1c1d),
	SegLine:     argb(0x4d1d1c1d),
	Meta:        rgb(0x616061),
	Ring:        argb(0x211d1c1d),
	Page:        rgb(0xf8f8f8),

	QuickBg:          rgb(0xe3f8ff),    // base-hgl-1
	QuickBorder:      argb(0x330e9dd3), // otl-hgl-1-sec
	QuickFg:          rgb(0x1264a3),    // content-hgl-1
	QuickHoverBg:     rgb(0xe3f8ff),    // base-hgl-1
	QuickHoverBorder: rgb(0x1264a3),    // content-hgl-1
	QuickHoverFg:     rgb(0x1264a3),
}

// Dark values follow Slack's dark mode tokens.
var darkPal = pal{
	// Values are Slack's dark-theme design tokens (.sk-client-theme--dark
	// --dt_color-*), read from the web client's stylesheets.
	Bg:          rgb(0x1a1d21), // base-pry
	Text:        rgb(0xf8f8f8), // content-pry
	Muted:       rgb(0x9a9b9e), // content-ter
	Faint:       rgb(0x9a9b9e),
	Border:      argb(0x4d797c81), // otl-ter
	BorderStr:   argb(0x80797c81), // otl-sec
	Hover:       argb(0x14f8f8f8), // base-pry-hover
	SelBg:       rgb(0x2f1e00),    // base-hgl-3
	SelText:     rgb(0xf8f8f8),
	SelMuted:    rgb(0x9a9b9e),
	Link:        rgb(0x2ba5ce), // content-hgl-1
	Green:       rgb(0x177e56), // base-inv-hgl-2
	GreenHover:  rgb(0x1f8f62),
	Red:         rgb(0xde678a), // content-imp
	Badge:       rgb(0xcfa1de), // theme-base-imp (aubergine-70)
	Presence:    rgb(0x2bac76),
	Skel:        rgb(0x212428), // base-ter
	Focus:       argb(0x4d2ba5ce),
	Chip:        argb(0x14f8f8f8),
	ChipSel:     rgb(0x001a2d), // base-hgl-1
	ChipSelText: rgb(0x2ba5ce),
	Surface:     rgb(0x212428),    // ctr-pry
	Overlay:     argb(0x99101214), // base-modal
	Mention:     argb(0x40036696), // surf-hgl-1
	Yellow:      rgb(0x2f1e00),
	TabLine:     rgb(0xe3ceeb), // theme-base-pry (aubergine-90)
	Sec:         rgb(0xb9babd), // content-sec
	TitleText:   rgb(0xefe1f5), // theme-content-pry (aubergine-100)
	Accent:      rgb(0xcfa1de),
	AccentText:  rgb(0x200221), // aubergine-0
	ReadFill:    rgb(0x1a1d21), // base-sec
	Track:       rgb(0x212428), // base-ter
	PillOnBg:    rgb(0x001a2d), // base-hgl-1
	PillOnFg:    rgb(0x2ba5ce), // content-hgl-1
	TabHover:    argb(0x14efe1f5),
	Pressed:     argb(0x2ef8f8f8), // base-pry-pressed
	CardLine:    argb(0x4d797c81),
	SegLine:     argb(0x80797c81),
	Meta:        rgb(0x9a9b9e),
	Ring:        argb(0x4d797c81),
	Page:        rgb(0x1a1d21), // base-sec

	QuickBg:          rgb(0x05241b), // base-hgl-2
	QuickBorder:      rgb(0x083f2e), // plt-jade-10
	QuickFg:          rgb(0x8dd2b5), // plt-jade-80
	QuickHoverBg:     rgb(0x001a2d), // base-hgl-1
	QuickHoverBorder: rgb(0x2ba5ce), // content-hgl-1
	QuickHoverFg:     rgb(0x2ba5ce),
}

// Gio composites in linear RGB while browsers blend in sRGB, so a
// translucent token (Slack's #5e5d60@.13 hairlines, #454447@.06 hovers)
// would come out lighter/stronger than in Slack. palOf therefore returns
// the palette with those tokens pre-flattened over Bg in sRGB. Overlay
// (the modal scrim) and Mention stay translucent.
var (
	flatOnce            sync.Once
	flatLight, flatDark pal
)

func palOf(th *theme.Theme) *pal {
	flatOnce.Do(func() {
		flatLight, flatDark = flatPal(lightPal), flatPal(darkPal)
	})
	if p := themedPal(th); p != nil {
		return accountPal(th, p)
	}
	if th != nil && th.Dark {
		return accountPal(th, &flatDark)
	}
	return accountPal(th, &flatLight)
}

var (
	acctMu  sync.Mutex
	acctTh  *theme.Theme
	acctGen uint64
	acctIn  *pal
	acctOut pal
)

// accountPal lays the account's Slack theme (ia_theme, applied to th.C by
// the ui root) over p where it changes Slack's --dt_color-theme-* tokens:
// theme-base-pry (the selected tab's underline), theme-base-imp and
// theme-content-imp (unread dots and count badges). With Slack's default
// theme p is returned as is.
func accountPal(th *theme.Theme, p *pal) *pal {
	if th == nil {
		return p
	}
	def := theme.LightPalette
	if th.Dark {
		def = theme.DarkPalette
	}
	tab := th.C.Accent != def.Accent
	imp := th.C.ThemeImp.A != 0
	if !tab && !imp {
		return p
	}
	acctMu.Lock()
	defer acctMu.Unlock()
	if acctTh != th || acctGen != th.Gen || acctIn != p {
		o := *p
		if tab {
			o.TabLine = th.C.Accent
		}
		if imp {
			o.Accent, o.Badge = th.C.ThemeImp, th.C.ThemeImp
			if th.C.ThemeImpText.A != 0 {
				o.AccentText = th.C.ThemeImpText
			}
		}
		acctTh, acctGen, acctIn, acctOut = th, th.Gen, p, o
	}
	cp := acctOut
	return &cp
}

func flatPal(p pal) pal {
	for _, c := range []*color.NRGBA{&p.Border, &p.BorderStr, &p.Hover, &p.SelMuted, &p.Focus, &p.Chip,
		&p.TabHover, &p.Pressed, &p.CardLine, &p.SegLine, &p.Ring} {
		*c = flatten(*c, p.Bg)
	}
	p.QuickBorder = flatten(p.QuickBorder, p.QuickBg)
	return p
}

// lt picks the light or dark value (for literals that have a Slack token
// in each theme).
func (b *base) lt(light, dark color.NRGBA) color.NRGBA {
	if b.th != nil && b.th.Dark {
		return dark
	}
	return light
}

// flatten composites c over the opaque base in sRGB (like a browser) and
// returns an opaque color. Use it for any translucent literal drawn over a
// known background.
func flatten(c, base color.NRGBA) color.NRGBA {
	if c.A == 0xff {
		return c
	}
	o := c
	o.A = 0xff
	base.A = 0xff
	return theme.Mix(base, o, float32(c.A)/255)
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func argb(v uint32) color.NRGBA {
	return color.NRGBA{A: uint8(v >> 24), R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
}

// ---- base ----

// base is embedded in every view: host access plus per-frame shortcuts.
type base struct {
	h    Host
	th   *theme.Theme
	p    *pal
	rich richCache
	// tt is the Host's Taut plugin system (nil without plugins); tgen/tmemo
	// cache its patched member/channel/message views for one frame.
	tt    *taut.Taut
	tgen  uint32
	tmemo map[string]*memoEntry
	// addBtns are the messages' add-reaction buttons (reactions_slack.go)
	addBtns map[string]*widget.Clickable
}

func newBase(h Host) base { return base{h: h, rich: richCache{}} }

// frame refreshes per-frame state; call at the top of Layout.
func (b *base) frame() {
	b.th = b.h.Theme()
	b.p = palOf(b.th)
	b.tt = tautOf(b.h)
	b.tgen++
}

// ---- text ----

const (
	wReg   = theme.Regular
	wBold  = theme.Bold
	wBlack = theme.Black
	wLight = theme.Light
)

// label draws one ellipsized line.
// Text helpers drop the minimum height: Flex passes its cross-axis minimum
// to children, and a stretched label would draw top-aligned instead of
// being centered by the parent's alignment.
func (b *base) label(gtx C, size unit.Sp, w font.Weight, col color.NRGBA, s string) D {
	gtx.Constraints.Min.Y = 0
	return b.th.Label(gtx, size, w, col, s)
}

// para draws wrapping text, at most maxLines lines (0 = unlimited).
func (b *base) para(gtx C, size, lh unit.Sp, w font.Weight, col color.NRGBA, maxLines int, s string) D {
	gtx.Constraints.Min.Y = 0
	return b.th.Layout(gtx, theme.TextStyle{Size: size, Weight: w, Color: col, MaxLines: maxLines, LineHeight: lh}, s)
}

// centered draws one line centered in the available width.
func (b *base) centered(gtx C, size unit.Sp, w font.Weight, col color.NRGBA, s string) D {
	gtx.Constraints.Min.Y = 0
	return b.th.Layout(gtx, theme.TextStyle{Size: size, Weight: w, Color: col, Align: text.Middle}, s)
}

// ---- geometry helpers ----

func px(gtx C, v unit.Dp) int { return gtx.Dp(v) }

func fill(gtx C, r image.Rectangle, c color.NRGBA) { theme.Fill(gtx, r, c) }

func rrect(gtx C, r image.Rectangle, radius int, c color.NRGBA) { theme.RRect(gtx, r, radius, c) }

func border(gtx C, r image.Rectangle, radius, w int, c color.NRGBA) {
	theme.Border(gtx, r, radius, w, c)
}

// hline draws a full-width hairline.
func hline(gtx C, c color.NRGBA) D {
	h := max(1, gtx.Dp(1)/1)
	fill(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), c)
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// vline draws a full-height hairline.
func vline(gtx C, c color.NRGBA) D {
	w := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, 0, w, gtx.Constraints.Max.Y), c)
	return D{Size: image.Pt(w, gtx.Constraints.Max.Y)}
}

// background draws bg behind w, sized to w.
func background(gtx C, radius unit.Dp, bg color.NRGBA, w W) D {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	if bg.A > 0 {
		rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(radius), bg)
	}
	call.Add(gtx.Ops)
	return d
}

// fixed constrains w to exactly size.
func fixed(gtx C, size image.Point, w W) D {
	gtx.Constraints = layout.Exact(size)
	w(gtx)
	return D{Size: size}
}

// spacer returns a rigid gap.
func hgap(v unit.Dp) layout.FlexChild {
	return layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(v), 0)} })
}

func vgap(v unit.Dp) layout.FlexChild {
	return layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(v))} })
}

func gapW(v unit.Dp) W { return func(gtx C) D { return D{Size: image.Pt(gtx.Dp(v), gtx.Dp(v))} } }

func row(gtx C, align layout.Alignment, ch ...layout.FlexChild) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: align}.Layout(gtx, ch...)
}

func col(gtx C, ch ...layout.FlexChild) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

func inset(t, r, bm, l unit.Dp, w W) W {
	return func(gtx C) D { return layout.Inset{Top: t, Right: r, Bottom: bm, Left: l}.Layout(gtx, w) }
}

func pointerCursor(gtx C) { pointer.CursorPointer.Add(gtx.Ops) }

// ---- interactive bits ----

// hoverable lays w inside a clickable and paints bg (radius r) on hover or
// when sel is set. It returns the dimensions of w.
func (b *base) hoverable(gtx C, c *widget.Clickable, sel bool, r unit.Dp, w W) D {
	return c.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		d := w(gtx)
		call := m.Stop()
		switch {
		case sel:
			rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(r), b.p.SelBg)
		case c.Hovered():
			rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(r), b.p.Hover)
		}
		pointerCursor(gtx)
		call.Add(gtx.Ops)
		return d
	})
}

type btnKind uint8

const (
	btnOutline btnKind = iota
	btnPrimary
	btnDanger
	btnGhost
)

// button draws a Slack button: 28px (small) or 36px tall, radius 8,
// bold 13/15px label. Primary is green, outline is white with a border.
func (b *base) button(gtx C, c *widget.Clickable, kind btnKind, small bool, label string) D {
	hgt, size, padX := unit.Dp(36), unit.Sp(15), unit.Dp(16)
	if small {
		hgt, size, padX = 28, 13, 8
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := b.th.Measure(gtx, size, wBold, label)
		sz := image.Pt(tw+2*gtx.Dp(padX), gtx.Dp(hgt))
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(8)
		fg := b.p.Text
		switch kind {
		case btnPrimary:
			bg := b.p.Green
			if c.Hovered() {
				bg = b.p.GreenHover
			}
			rrect(gtx, r, rad, bg)
			fg = rgb(0xffffff)
		case btnDanger:
			bg := rgb(0xe01e5a)
			if c.Hovered() {
				bg = rgb(0xc91b52)
			}
			rrect(gtx, r, rad, bg)
			fg = rgb(0xffffff)
		case btnGhost:
			if c.Hovered() {
				rrect(gtx, r, rad, b.p.Chip)
			}
		default:
			rrect(gtx, r, rad, b.p.Bg)
			if c.Hovered() {
				rrect(gtx, r, rad, b.p.Hover)
			}
			border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.BorderStr)
		}
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, size, wBold, fg, label) })
		return D{Size: sz}
	})
}

// iconButton draws a square hover-highlighted icon button.
func (b *base) iconButton(gtx C, c *widget.Clickable, name string, size, iconSize unit.Dp, col color.NRGBA) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		s := gtx.Dp(size)
		if c.Hovered() {
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), b.p.Hover)
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return b.icon(gtx, name, iconSize, col) })
		return D{Size: image.Pt(s, s)}
	})
}

// ---- tabs ----

// tabs is Slack's underline tab strip (Activity, Later, Directories, channel
// details): 13px bold labels, 2px underline under the selected tab.
type tabs struct {
	Labels []string
	Sel    int
	clicks []widget.Clickable
	// Tr gives a label's text in the active locale (nil: drawn as is).
	// Labels keep the English sources (Slack's keys); a view's own data
	// (a custom view's name) passes through unchanged.
	Tr func(i int, s string) string
}

// label is tab i's text as drawn.
func (t *tabs) label(i int) string {
	if t.Tr != nil {
		return t.Tr(i, t.Labels[i])
	}
	return t.Labels[i]
}

// trIn is a tabs.Tr for labels that are all Slack strings of ns.
func trIn(ns i18n.NS) func(int, string) string {
	return func(_ int, s string) string { return ns.T(s) }
}

func newTabs(labels ...string) *tabs {
	return &tabs{Labels: labels, clicks: make([]widget.Clickable, len(labels))}
}

// update reports whether the selection changed this frame.
func (t *tabs) update(gtx C) bool {
	changed := false
	for i := range t.clicks {
		if t.clicks[i].Clicked(gtx) && t.Sel != i {
			t.Sel, changed = i, true
		}
	}
	return changed
}

// tabStrip draws tabs: padding 9px 8px, 13px/16px bold, no gap,
// unselected #454447, selected #1d1c1d with a 2px accent underline, hover
// fill #611f69@.07 with top corners rounded 8px. badges[i] > 0 draws an
// accent unread dot after the label (dots only, like Slack); icons[i]
// names an optional 16px icon before the label.
func (b *base) tabStrip(gtx C, t *tabs, badges []int) D {
	return b.tabStripIcons(gtx, t, nil, badges)
}

func (b *base) tabStripIcons(gtx C, t *tabs, icons []string, badges []int) D {
	ch := make([]layout.FlexChild, 0, len(t.Labels))
	for i := range t.Labels {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return t.clicks[i].Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				sel := i == t.Sel
				fg := b.p.Sec
				if sel {
					fg = b.p.Text
				}
				m := op.Record(gtx.Ops)
				d := layout.Inset{Top: 9, Bottom: 8, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							if i >= len(icons) || icons[i] == "" {
								return D{}
							}
							return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icons[i], 16, fg) })
						}),
						layout.Rigid(func(gtx C) D { return b.label(gtx, 13, wBold, fg, t.label(i)) }),
						layout.Rigid(func(gtx C) D {
							if i >= len(badges) || badges[i] <= 0 {
								return D{}
							}
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return b.dot(gtx, 8, b.p.Accent) })
						}),
					)
				})
				call := m.Stop()
				if t.clicks[i].Hovered() {
					r := gtx.Dp(8)
					rr := clip.RRect{Rect: image.Rectangle{Max: d.Size}, NE: r, NW: r}
					paint.FillShape(gtx.Ops, b.p.TabHover, rr.Op(gtx.Ops))
				}
				call.Add(gtx.Ops)
				if sel {
					h := gtx.Dp(2)
					fill(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), b.p.TabLine)
				}
				return d
			})
		}))
	}
	return row(gtx, layout.End, ch...)
}

// pills is a row of filter chips (Files type filters, Directories filters).
type pills struct {
	tabs
}

func newPills(labels ...string) *pills {
	return &pills{tabs{Labels: labels, clicks: make([]widget.Clickable, len(labels))}}
}

func (b *base) pillRow(gtx C, p *pills) D {
	ch := make([]layout.FlexChild, 0, len(p.Labels)*2)
	for i := range p.Labels {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return p.clicks[i].Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				sel := i == p.Sel
				fg := b.p.Text
				tw := b.th.Measure(gtx, 13, wBold, p.label(i))
				sz := image.Pt(tw+gtx.Dp(16), gtx.Dp(32))
				r := image.Rectangle{Max: sz}
				rad := gtx.Dp(8)
				if sel {
					fg = b.p.PillOnFg
					rrect(gtx, r, rad, b.p.PillOnBg)
					border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.PillOnFg)
				} else {
					rrect(gtx, r, rad, b.p.Bg)
					if p.clicks[i].Hovered() {
						rrect(gtx, r, rad, b.p.Hover)
					}
					border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Border)
				}
				g := gtx
				g.Constraints = layout.Exact(sz)
				layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, 13, wBold, fg, p.label(i)) })
				return D{Size: sz}
			})
		}))
		ch = append(ch, hgap(8))
	}
	return row(gtx, layout.Middle, ch...)
}

// toggle is Slack's small switch (Activity "Unreads").
type toggle struct {
	widget.Bool
}

func (b *base) toggleSwitch(gtx C, t *toggle, label string) D {
	return t.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return b.label(gtx, 13, wReg, b.p.Text, label) }),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				w, h := gtx.Dp(28), gtx.Dp(16)
				r := image.Rect(0, 0, w, h)
				if t.Value {
					rrect(gtx, r, h/2, b.p.Green)
				} else {
					rrect(gtx, r, h/2, b.p.Faint)
				}
				k := h - gtx.Dp(4)
				x := gtx.Dp(2)
				if t.Value {
					x = w - k - gtx.Dp(2)
				}
				theme.Circle(gtx, image.Pt(x+k/2, h/2), k, rgb(0xffffff))
				return D{Size: image.Pt(w, h)}
			}),
		)
	})
}

// ---- search box ----

// search is a single-line filter input with a magnifier icon and a
// placeholder ("Find a DM", "Search for people").
type search struct {
	ed   widget.Editor
	last string
	ph   string
	ns   i18n.NS // ph is Slack's string in ns ("": drawn as is)
}

func newSearch(placeholder string) *search {
	return &search{ed: widget.Editor{SingleLine: true, Submit: true}, ph: placeholder}
}

// newSearchIn is newSearch with a placeholder that is Slack's string in
// ns (drawn in the active locale).
func newSearchIn(ns i18n.NS, placeholder string) *search {
	s := newSearch(placeholder)
	s.ns = ns
	return s
}

// placeholder is the placeholder as drawn.
func (s *search) placeholder() string {
	if s.ns != "" {
		return s.ns.T(s.ph)
	}
	return s.ph
}

// update returns (text, changed, submitted).
func (s *search) update(gtx C) (string, bool, bool) {
	submitted := false
	for {
		ev, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			submitted = true
		}
	}
	t := s.ed.Text()
	ch := t != s.last
	s.last = t
	return strings.TrimSpace(t), ch, submitted
}

func (s *search) text() string { return strings.TrimSpace(s.ed.Text()) }

// searchBox draws the input: height 36, radius 8, 1px border that turns
// blue with a focus ring when focused.
func (b *base) searchBox(gtx C, s *search, height unit.Dp) D {
	hgt := gtx.Dp(height)
	w := gtx.Constraints.Max.X
	r := image.Rect(0, 0, w, hgt)
	rad := gtx.Dp(8)
	focused := gtx.Focused(&s.ed)
	rrect(gtx, r, rad, b.p.Bg)
	if focused {
		border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(3), b.p.Focus)
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Link)
	} else {
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.BorderStr)
	}
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, hgt))
	layout.Inset{Left: 10, Right: 10}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return b.icon(gtx, "search", 18, b.p.Muted) }),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Stack{Alignment: layout.W}.Layout(gtx,
					layout.Stacked(func(gtx C) D {
						if s.ed.Len() > 0 {
							return D{}
						}
						return b.label(gtx, 15, wReg, b.p.Faint, s.placeholder())
					}),
					layout.Stacked(func(gtx C) D {
						return b.th.Editor(gtx, &s.ed, 15, b.p.Text, b.p.Mention)
					}),
				)
			}),
		)
	})
	return D{Size: image.Pt(w, hgt)}
}

// ---- avatars / images ----

// avatar draws a rounded-square avatar (Slack: radius ≈ 22% of size, 4px
// at 20px, 8px at 36px, 12px at 72px). Falls back to a skeleton tile.
func (b *base) avatar(gtx C, url string, size unit.Dp) D {
	s := gtx.Dp(size)
	rad := avatarRadius(gtx, size)
	sz := image.Pt(s, s)
	if url != "" && b.h.Images() != nil {
		if img, ok := b.h.Images().Get(url, s); ok {
			return theme.Image(gtx, img, sz, rad)
		}
	}
	return b.th.Placeholder(gtx, sz, rad)
}

func avatarRadius(gtx C, size unit.Dp) int {
	r := float32(size) * 0.22222
	if r < 4 {
		r = 4
	}
	if r > 12 {
		r = 12
	}
	return gtx.Dp(unit.Dp(r))
}

// avatarPresence draws an avatar with a presence dot on its bottom-right
// corner (green filled when active, hollow when away).
func (b *base) avatarPresence(gtx C, url string, size unit.Dp, presence string, ring color.NRGBA) D {
	d := b.avatar(gtx, url, size)
	if presence == "" {
		return d
	}
	dot := gtx.Dp(size / 3.6)
	if dot < gtx.Dp(9) {
		dot = gtx.Dp(9)
	}
	ringW := gtx.Dp(2)
	c := image.Pt(d.Size.X-dot/2+ringW/2, d.Size.Y-dot/2+ringW/2)
	theme.Circle(gtx, c, dot+2*ringW, ring)
	if presence == "active" {
		theme.Circle(gtx, c, dot, b.p.Presence)
	} else {
		theme.Circle(gtx, c, dot, b.p.Faint)
		theme.Circle(gtx, c, dot-2*gtx.Dp(1.5), ring)
	}
	return d
}

// emojiImg draws a custom/standard emoji image at size, or nothing.
func (b *base) emojiImg(gtx C, url string, size unit.Dp) D {
	s := gtx.Dp(size)
	if url != "" && b.h.Images() != nil {
		if img, ok := b.h.Images().Get(url, s); ok {
			return theme.Image(gtx, img, image.Pt(s, s), 0)
		}
	}
	return D{Size: image.Pt(s, s)}
}

// groupAvatar draws the stacked two-avatar tile Slack uses for group DMs.
func (b *base) groupAvatar(gtx C, a, c string, size unit.Dp) D {
	s := gtx.Dp(size)
	sub := size * 0.72
	subPx := gtx.Dp(sub)
	func() {
		defer op.Offset(image.Pt(0, 0)).Push(gtx.Ops).Pop()
		b.avatar(gtx, a, sub)
	}()
	func() {
		off := s - subPx
		defer op.Offset(image.Pt(off, off)).Push(gtx.Ops).Pop()
		ring := gtx.Dp(2)
		rrect(gtx, image.Rect(-ring, -ring, subPx+ring, subPx+ring), avatarRadius(gtx, sub)+ring, b.p.Bg)
		b.avatar(gtx, c, sub)
	}()
	return D{Size: image.Pt(s, s)}
}

// countBadge draws Slack's count pill: h 20, padding 0 8px, 10px bold,
// theme accent background.
func (b *base) countBadge(gtx C, n int) D {
	s := strconv.Itoa(n)
	if n > 99 {
		s = "99+"
	}
	tw := b.th.Measure(gtx, 10, wBold, s)
	h := gtx.Dp(20)
	w := max(gtx.Dp(22), tw+gtx.Dp(16))
	rrect(gtx, image.Rect(0, 0, w, h), h/2, b.p.Badge)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, 10, wBold, b.p.AccentText, s) })
	return D{Size: image.Pt(w, h)}
}

// dot draws the small unread dot.
func (b *base) dot(gtx C, d unit.Dp, c color.NRGBA) D {
	s := gtx.Dp(d)
	theme.Circle(gtx, image.Pt(s/2, s/2), s, c)
	return D{Size: image.Pt(s, s)}
}

// ---- view chrome ----

// header draws a view title row: 16px side insets, the title in 18px/24px
// black in the theme title color starting 16px from the top, trailing
// widgets centered on the title line. Height 53 (Slack's Activity header
// above its tab row); put tabs and a hairline below it.
func (b *base) header(gtx C, title string, trailing ...layout.FlexChild) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(53)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(28)))
	func() {
		defer op.Offset(image.Pt(0, gtx.Dp(14))).Push(gtx.Ops).Pop()
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			ch := []layout.FlexChild{
				layout.Flexed(1, func(gtx C) D { return b.label(gtx, 18, wBlack, b.p.TitleText, title) }),
			}
			ch = append(ch, trailing...)
			return row(gtx, layout.Middle, ch...)
		})
	}()
	return D{Size: image.Pt(w, h)}
}

// emptyState draws Slack's centered empty state: a 32px icon, a 16px/22px
// bold title, a 15px/22px description (max 372 wide) and an optional link
// button (15px #1264a3).
func (b *base) emptyState(gtx C, icon, title, body string, btn *widget.Clickable, btnLabel string) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(372))
		gtx.Constraints.Min.X = 0
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if icon == "" {
					return D{}
				}
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icon, 32, b.p.Muted) })
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return b.centered(gtx, 16, wBold, b.p.Text, title) })
			}),
			layout.Rigid(func(gtx C) D {
				return b.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: b.p.Text, Align: text.Middle}, body)
			}),
			layout.Rigid(func(gtx C) D {
				if btn == nil {
					return D{}
				}
				return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D { return b.linkButton(gtx, btn, 15, btnLabel) })
			}),
		)
	})
}

// linkButton is a text button in link blue (underlined on hover).
func (b *base) linkButton(gtx C, c *widget.Clickable, size unit.Sp, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		d := b.label(gtx, size, wReg, b.p.Link, label)
		if c.Hovered() {
			fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), b.p.Link)
		}
		return d
	})
}

// filterButton is the outline quick-filter toggle ("Unreads"): h 32,
// padding 0 8px, radius 8, 13px bold with a 16px icon; pressed, Slack's
// p-activity_ia4_page__filter_bar--active: light base-hgl-1 with an
// otl-hgl-1-sec border and content-hgl-1 text, dark base-hgl-2 with jade
// border and text (plt-jade-10 / -80); hovered, base-hgl-1 with a
// content-hgl-1 border and text in both.
func (b *base) filterButton(gtx C, t *toggle, icon, label string) D {
	return t.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		on, hov := t.Value, t.Hovered()
		fg := b.p.Text
		if on {
			fg = b.p.QuickFg
			if hov {
				fg = b.p.QuickHoverFg
			}
		}
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(32)
			return layout.W.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if icon == "" {
							return D{}
						}
						return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icon, 16, fg) })
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wBold, fg, label) })
					}),
				)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(8)
		switch {
		case on && hov:
			rrect(gtx, r, rad, b.p.QuickHoverBg)
			border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.QuickHoverBorder)
		case on:
			rrect(gtx, r, rad, b.p.QuickBg)
			border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.QuickBorder)
		default:
			rrect(gtx, r, rad, b.p.Bg)
			if hov {
				rrect(gtx, r, rad, b.p.Hover)
			}
			border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Border)
		}
		call.Add(gtx.Ops)
		return d
	})
}

// outlineIcon is an outline icon button (filter bar): h 32, radius 8,
// 1px #5e5d60@.13 border, 20px icon; width = 38 or wider with a caret.
func (b *base) outlineIcon(gtx C, c *widget.Clickable, icon string, caret bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w := gtx.Dp(38)
		if caret {
			w = gtx.Dp(58)
		}
		sz := image.Pt(w, gtx.Dp(32))
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, b.p.Bg)
		if c.Hovered() {
			rrect(gtx, r, rad, b.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Border)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return b.icon(gtx, icon, 20, b.p.Sec) }),
				layout.Rigid(func(gtx C) D {
					if !caret {
						return D{}
					}
					return b.icon(gtx, "caret-down", 20, b.p.Sec)
				}),
			)
		})
		return D{Size: sz}
	})
}

// segmented draws Slack's two-icon segmented toggle (Detailed/Dense,
// list/grid): track #eaeaea radius 8, 30x28 segments, the selected one a
// white thumb with a 1px #5e5d60@.45 border. Returns the clicked index or -1.
func (b *base) segmented(gtx C, clicks []widget.Clickable, icons []string, sel int) (D, int) {
	n := len(icons)
	sw, sh := gtx.Dp(30), gtx.Dp(28)
	size := image.Pt(sw*n, sh)
	rrect(gtx, image.Rectangle{Max: size}, gtx.Dp(8), b.p.Track)
	clicked := -1
	for i := 0; i < n && i < len(clicks); i++ {
		if clicks[i].Clicked(gtx) {
			clicked = i
		}
		func() {
			defer op.Offset(image.Pt(i*sw, 0)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(sw, sh))
			clicks[i].Layout(g, func(gtx C) D {
				pointerCursor(gtx)
				r := image.Rect(0, 0, sw, sh)
				if i == sel {
					rrect(gtx, r, gtx.Dp(8), b.p.Bg)
					border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), b.p.BorderStr)
				}
				return layout.Center.Layout(gtx, func(gtx C) D { return b.icon(gtx, icons[i], 18, b.p.Muted) })
			})
		}()
	}
	return D{Size: size}, clicked
}

// loading draws a few skeleton rows.
func (b *base) skeleton(gtx C, rows int, avatar unit.Dp) D {
	ch := make([]layout.FlexChild, 0, rows)
	for i := 0; i < rows; i++ {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Start,
					layout.Rigid(func(gtx C) D {
						return b.th.Placeholder(gtx, image.Pt(gtx.Dp(avatar), gtx.Dp(avatar)), avatarRadius(gtx, avatar))
					}),
					hgap(12),
					layout.Flexed(1, func(gtx C) D {
						w := gtx.Constraints.Max.X
						w1 := w * (40 + (i*17)%30) / 100
						w2 := w * (60 + (i*23)%35) / 100
						rrect(gtx, image.Rect(0, gtx.Dp(2), w1, gtx.Dp(14)), gtx.Dp(4), b.p.Skel)
						rrect(gtx, image.Rect(0, gtx.Dp(22), w2, gtx.Dp(34)), gtx.Dp(4), b.p.Skel)
						return D{Size: image.Pt(w, gtx.Dp(avatar))}
					}),
				)
			})
		}))
	}
	return col(gtx, ch...)
}

// errorLine draws a muted error with a retry button.
func (b *base) errorState(gtx C, err error, retry *widget.Clickable) D {
	// the views' shared error state: the files namespace's words, which
	// Slack's own views use for theirs
	return b.emptyState(gtx, "", i18n.T("files", "Something went wrong"), friendlyErr(err), retry, i18n.T("files", "Try again"))
}

func friendlyErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// ---- async loading ----

// async runs a fetch off the UI goroutine and invalidates the window when
// it finishes. Generation counters drop stale results.
type async[T any] struct {
	mu      sync.Mutex
	val     T
	err     error
	loaded  bool
	loading bool
	gen     int
	cancel  context.CancelFunc
}

// load starts f (cancelling any in-flight run). keep retains the previous
// value while loading (pagination, refresh).
func (a *async[T]) load(h Host, keep bool, f func(ctx context.Context) (T, error)) {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.gen++
	gen := a.gen
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	a.cancel = cancel
	a.loading = true
	if !keep {
		var zero T
		a.val, a.loaded, a.err = zero, false, nil
	}
	a.mu.Unlock()
	go func() {
		defer cancel()
		v, err := f(ctx)
		a.mu.Lock()
		if gen == a.gen {
			a.loading = false
			if err == nil || !keep {
				a.val = v
			}
			a.err = err
			a.loaded = true
		}
		a.mu.Unlock()
		h.Invalidate()
	}()
}

// get returns the current value, whether a value has arrived, and the
// last error.
func (a *async[T]) get() (T, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.val, a.loaded, a.err
}

func (a *async[T]) busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loading
}

func (a *async[T]) started() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gen > 0
}

// mutate edits the stored value in place (optimistic updates).
func (a *async[T]) mutate(f func(v *T)) {
	a.mu.Lock()
	f(&a.val)
	a.mu.Unlock()
}

// ---- debounced text ----

// debounce fires after the input has been idle for d.
type debounce struct {
	at   time.Time
	q    string
	sent string
}

func (d *debounce) set(q string, now time.Time) { d.q, d.at = q, now }

// due returns the query once it has been idle for wait; it schedules a
// repaint until then.
func (d *debounce) due(gtx C, wait time.Duration) (string, bool) {
	if d.q == d.sent {
		return "", false
	}
	left := wait - gtx.Now.Sub(d.at)
	if left > 0 {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(left)})
		return "", false
	}
	d.sent = d.q
	return d.q, true
}

// ---- time ----

// tsTime parses a Slack ts ("1730900000.123456").
func tsTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var ns int64
	if frac != "" {
		for len(frac) < 9 {
			frac += "0"
		}
		ns, _ = strconv.ParseInt(frac[:9], 10, 64)
	}
	return time.Unix(s, ns)
}

func unixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// clock formats "3:04 PM" ("15:04" with time24).
func clock(t time.Time) string { return uipref.Clock(t) }

// listTime is the compact time Slack shows on list rows: "3:04 PM" today,
// "Yesterday", the weekday within a week, "Oct 12" this year, else
// "Oct 12, 2024".
func listTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if l := i18n.Locale(); l != i18n.EnUS {
		// the same steps in the locale's dayjs formats
		switch {
		case sameDay(t, now):
			return clock(t)
		case sameDay(t, now.AddDate(0, 0, -1)):
			return i18n.RelativeDay(l, t, now, false)
		case now.Sub(t) < 6*24*time.Hour && t.Before(now):
			return i18n.Weekday(l, t, false)
		}
		return i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: true, ShortenDay: true, ExcludeYear: t.Year() == now.Year()})
	}
	switch {
	case sameDay(t, now):
		return clock(t)
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case now.Sub(t) < 6*24*time.Hour && t.Before(now):
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	default:
		return t.Format("Jan 2, 2006")
	}
}

// longDate formats "Monday, October 12th" (Slack day dividers, created on).
func longDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	s := t.Format("January 2") + ordinal(t.Day())
	if t.Year() != time.Now().Year() {
		s += ", " + strconv.Itoa(t.Year())
	}
	return s
}

func ordinal(d int) string {
	if d >= 11 && d <= 13 {
		return "th"
	}
	switch d % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

// dueText formats a reminder due time ("Due today at 3:00 PM", "Overdue
// by 2 days").
func dueText(t time.Time) (string, bool) {
	if t.IsZero() {
		return "", false
	}
	now := time.Now()
	if l := i18n.Locale(); l != i18n.EnUS {
		// getDateDuePillString's toTimeAgo in Slack's "Overdue by
		// {dateString}" / "Due {dateString}" (saved_for_later)
		if t.Before(now) {
			return nsLater.T("Overdue by {dateString}", "dateString", i18n.TimeAgo(l, t, now, false, true)), true
		}
		return nsLater.T("Due {dateString}", "dateString", i18n.TimeAgo(l, t, now, false, false)), false
	}
	if t.Before(now) {
		d := now.Sub(t)
		switch {
		case d < time.Hour:
			return "Overdue by " + plural(int(d.Minutes())+1, "minute"), true
		case d < 24*time.Hour:
			return "Overdue by " + plural(int(d.Hours()), "hour"), true
		default:
			return "Overdue by " + plural(int(d.Hours()/24), "day"), true
		}
	}
	switch {
	case sameDay(t, now):
		return "Due today at " + clock(t), false
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "Due tomorrow at " + clock(t), false
	case t.Sub(now) < 6*24*time.Hour:
		return "Due " + t.Format("Monday") + " at " + clock(t), false
	default:
		return "Due " + t.Format("Jan 2") + " at " + clock(t), false
	}
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return strconv.Itoa(n) + " " + w + "s"
}

// humanCount formats 1234 as "1,234".
func humanCount(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// oneLine collapses whitespace for previews.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clipRect pushes a rectangular clip of the constraints' max size.
func clipMax(gtx C) clip.Stack {
	return clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops)
}

// themeImage draws img covering size with rounded corners.
func themeImage(gtx C, img paint.ImageOp, size image.Point, radius int) D {
	return theme.Image(gtx, img, size, radius)
}

// opOffset pushes a translation; Pop it when done.
func opOffset(gtx C, pt image.Point) op.TransformStack { return op.Offset(pt).Push(gtx.Ops) }

// scrollList lays out a vertical virtualized list with Slack's slim
// overlay scrollbar (8px wide, draggable, shown when content overflows).
func (b *base) scrollList(gtx C, l *widget.List, n int, el layout.ListElement) D {
	l.Axis = layout.Vertical
	d := l.List.Layout(gtx, n, el)
	if n == 0 {
		return d
	}
	pos := l.Position
	if pos.Length <= 0 {
		return d
	}
	est := float32(pos.Length) / float32(n)
	start := clamp01((float32(pos.First)*est + float32(pos.Offset)) / float32(pos.Length))
	end := clamp01((float32(pos.First+pos.Count)*est + float32(pos.OffsetLast)) / float32(pos.Length))
	if pos.First == 0 && pos.Offset <= 0 && !l.Position.BeforeEnd {
		return d
	}
	l.Scrollbar.Update(gtx, layout.Vertical, start, end)
	if delta := l.Scrollbar.ScrollDistance(); delta != 0 {
		l.List.ScrollBy(delta * float32(n))
	}
	h := d.Size.Y
	w := gtx.Dp(8)
	x := d.Size.X - w - gtx.Dp(2)
	track := image.Rect(x, 0, x+w, h)
	func() {
		defer clip.Rect(track).Push(gtx.Ops).Pop()
		l.Scrollbar.AddTrack(gtx.Ops)
	}()
	y0, y1 := int(start*float32(h)), int(end*float32(h))
	if y1-y0 < gtx.Dp(24) {
		y1 = min(h, y0+gtx.Dp(24))
	}
	thumb := image.Rect(x, y0, x+w, y1)
	func() {
		defer clip.Rect(thumb).Push(gtx.Ops).Pop()
		l.Scrollbar.AddIndicator(gtx.Ops)
		l.Scrollbar.AddDrag(gtx.Ops)
	}()
	c := b.p.BorderStr
	if l.Scrollbar.IndicatorHovered() || l.Scrollbar.Dragging() {
		c = b.p.Muted
	}
	rrect(gtx, thumb, w/2, c)
	return d
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---- dropdown menus ----

// dropdown draws a popover at `at` (relative to the caller's current
// origin) on top of everything via op.Defer, with an invisible scrim that
// sets *open = false on an outside click. Popover style per Slack: white,
// radius 8, padding 8px 0, shadow 0 0 0 1px #1d1c1d21 + 0 4px 12px .12.
func (b *base) dropdown(gtx C, open *bool, scrim *widget.Clickable, at image.Point, width unit.Dp, content W) {
	if !*open {
		return
	}
	if scrim.Clicked(gtx) {
		*open = false
		return
	}
	m := op.Record(gtx.Ops)
	func() {
		const far = 1 << 15
		defer op.Offset(image.Pt(-far, -far)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(2*far, 2*far))
		scrim.Layout(g, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}()
	func() {
		defer op.Offset(at).Push(gtx.Ops).Pop()
		pw := gtx.Dp(width)
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(pw, 0), Max: image.Pt(pw, gtx.Dp(600))}
		rec := op.Record(gtx.Ops)
		d := layout.Inset{Top: 8, Bottom: 8}.Layout(g, content)
		call := rec.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(8)
		theme.Shadow(gtx, r, rad, gtx.Dp(12), argb(0x1f000000))
		rrect(gtx, r, rad, b.p.Surface)
		// a handler on the popover body keeps its clicks off the scrim
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, open)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: open, Kinds: pointer.Press | pointer.Release | pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
				break
			}
		}
		call.Add(gtx.Ops)
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Ring)
	}()
	op.Defer(gtx.Ops, m.Stop())
}

// menuItem is a 28px popover row: optional check (✓ in link blue when
// on), optional 20px icon, 15px label.
func (b *base) menuItem(gtx C, c *widget.Clickable, icon, label string, checkable, on bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(32)
		if c.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), b.p.Hover)
		}
		fg := b.p.Text
		if checkable && on {
			fg = b.p.Link
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if !checkable {
							return D{}
						}
						s := gtx.Dp(20)
						if on {
							b.icon(gtx, "check", 20, b.p.Link)
						}
						return D{Size: image.Pt(s+gtx.Dp(8), s)}
					}),
					layout.Rigid(func(gtx C) D {
						if icon == "" {
							return D{}
						}
						return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icon, 20, fg) })
					}),
					layout.Rigid(func(gtx C) D { return b.label(gtx, 15, wReg, fg, label) }),
				)
			})
		})
		return D{Size: image.Pt(w, h)}
	})
}
