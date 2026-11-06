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
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
}

var lightPal = pal{
	Bg:          rgb(0xffffff),
	Text:        rgb(0x1d1c1d),
	Muted:       rgb(0x616061),
	Faint:       rgb(0x868686),
	Border:      argb(0x211d1c1d),
	BorderStr:   argb(0x4d1d1c1d),
	Hover:       rgb(0xf8f8f8),
	SelBg:       rgb(0x1164a3),
	SelText:     rgb(0xffffff),
	SelMuted:    argb(0xccffffff),
	Link:        rgb(0x1264a3),
	Green:       rgb(0x007a5a),
	GreenHover:  rgb(0x148567),
	Red:         rgb(0xe01e5a),
	Badge:       rgb(0xcd2553),
	Presence:    rgb(0x2bac76),
	Skel:        rgb(0xeeeeee),
	Focus:       argb(0x4d1d9bd1),
	Chip:        argb(0x0f1d1c1d),
	ChipSel:     rgb(0x1d1c1d),
	ChipSelText: rgb(0xffffff),
	Surface:     rgb(0xffffff),
	Overlay:     argb(0x99000000),
	Mention:     argb(0x331d9bd1),
	Yellow:      rgb(0xfef7da),
	TabLine:     rgb(0x1d1c1d),
}

var darkPal = pal{
	Bg:          rgb(0x1a1d21),
	Text:        rgb(0xd1d2d3),
	Muted:       rgb(0xababad),
	Faint:       rgb(0x9a9b9e),
	Border:      argb(0x21e8e8e8),
	BorderStr:   argb(0x4de8e8e8),
	Hover:       rgb(0x222529),
	SelBg:       rgb(0x1164a3),
	SelText:     rgb(0xffffff),
	SelMuted:    argb(0xccffffff),
	Link:        rgb(0x1d9bd1),
	Green:       rgb(0x007a5a),
	GreenHover:  rgb(0x148567),
	Red:         rgb(0xe8677f),
	Badge:       rgb(0xcd2553),
	Presence:    rgb(0x2bac76),
	Skel:        rgb(0x2c2d30),
	Focus:       argb(0x4d1d9bd1),
	Chip:        argb(0x14e8e8e8),
	ChipSel:     rgb(0xd1d2d3),
	ChipSelText: rgb(0x1a1d21),
	Surface:     rgb(0x222529),
	Overlay:     argb(0x99000000),
	Mention:     argb(0x331d9bd1),
	Yellow:      argb(0x26f2c744),
	TabLine:     rgb(0xd1d2d3),
}

func palOf(th *theme.Theme) *pal {
	if th != nil && th.Dark {
		return &darkPal
	}
	return &lightPal
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
	h  Host
	th *theme.Theme
	p  *pal
}

func newBase(h Host) base { return base{h: h} }

// frame refreshes per-frame state; call at the top of Layout.
func (b *base) frame() {
	b.th = b.h.Theme()
	b.p = palOf(b.th)
}

// ---- text ----

const (
	wReg   = theme.Regular
	wBold  = theme.Bold
	wBlack = theme.Black
	wLight = theme.Light
)

// label draws one ellipsized line.
func (b *base) label(gtx C, size unit.Sp, w font.Weight, col color.NRGBA, s string) D {
	return b.th.Label(gtx, size, w, col, s)
}

// para draws wrapping text, at most maxLines lines (0 = unlimited).
func (b *base) para(gtx C, size, lh unit.Sp, w font.Weight, col color.NRGBA, maxLines int, s string) D {
	return b.th.Layout(gtx, theme.TextStyle{Size: size, Weight: w, Color: col, MaxLines: maxLines, LineHeight: lh}, s)
}

// centered draws one line centered in the available width.
func (b *base) centered(gtx C, size unit.Sp, w font.Weight, col color.NRGBA, s string) D {
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
		hgt, size, padX = 28, 13, 12
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
			bg := b.p.Bg
			if c.Hovered() {
				bg = b.p.Hover
			}
			rrect(gtx, r, rad, bg)
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
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(6), b.p.Chip)
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

func (b *base) tabStrip(gtx C, t *tabs, badges []int) D {
	ch := make([]layout.FlexChild, 0, len(t.Labels)*2)
	for i := range t.Labels {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return t.clicks[i].Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				sel := i == t.Sel
				fg := b.p.Muted
				if sel || t.clicks[i].Hovered() {
					fg = b.p.Text
				}
				d := layout.Inset{Left: 2, Right: 2}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							g := gtx
							g.Constraints.Min.Y = gtx.Dp(36)
							return layout.W.Layout(g, func(gtx C) D {
								return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
									return b.label(gtx, 13, wBold, fg, t.Labels[i])
								})
							})
						}),
						layout.Rigid(func(gtx C) D {
							if i >= len(badges) || badges[i] <= 0 {
								return D{}
							}
							return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return b.countBadge(gtx, badges[i]) })
						}),
					)
				})
				if sel {
					h := gtx.Dp(2)
					rrect(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), h/2, b.p.TabLine)
				}
				return d
			})
		}))
		if i < len(t.Labels)-1 {
			ch = append(ch, hgap(20))
		}
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
				bg, fg := b.p.Bg, b.p.Text
				if sel {
					bg, fg = b.p.ChipSel, b.p.ChipSelText
				} else if p.clicks[i].Hovered() {
					bg = b.p.Hover
				}
				tw := b.th.Measure(gtx, 13, wBold, p.Labels[i])
				sz := image.Pt(tw+gtx.Dp(24), gtx.Dp(28))
				rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(14), bg)
				if !sel {
					border(gtx, image.Rectangle{Max: sz}, gtx.Dp(14), max(1, gtx.Dp(1)), b.p.BorderStr)
				}
				g := gtx
				g.Constraints = layout.Exact(sz)
				layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, 13, wBold, fg, p.Labels[i]) })
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
}

func newSearch(placeholder string) *search {
	return &search{ed: widget.Editor{SingleLine: true, Submit: true}, ph: placeholder}
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
		border(gtx, r, rad, max(1, gtx.Dp(1)), rgb(0x1264a3))
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
						return b.label(gtx, 15, wReg, b.p.Faint, s.ph)
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
	switch {
	case size <= 20:
		return gtx.Dp(4)
	case size <= 24:
		return gtx.Dp(5)
	case size <= 36:
		return gtx.Dp(8)
	case size <= 48:
		return gtx.Dp(10)
	default:
		return gtx.Dp(12)
	}
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

// countBadge draws a red pill with a number (mention counts).
func (b *base) countBadge(gtx C, n int) D {
	s := strconv.Itoa(n)
	if n > 99 {
		s = "99+"
	}
	tw := b.th.Measure(gtx, 12, wBold, s)
	h := gtx.Dp(18)
	w := max(h, tw+gtx.Dp(12))
	rrect(gtx, image.Rect(0, 0, w, h), h/2, b.p.Badge)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, 12, wBold, rgb(0xffffff), s) })
	return D{Size: image.Pt(w, h)}
}

// dot draws the small unread dot.
func (b *base) dot(gtx C, d unit.Dp, c color.NRGBA) D {
	s := gtx.Dp(d)
	theme.Circle(gtx, image.Pt(s/2, s/2), s, c)
	return D{Size: image.Pt(s, s)}
}

// ---- view chrome ----

// header draws the view title bar: 49px tall (incl. 1px bottom border),
// 18px black title, right-aligned trailing widgets.
func (b *base) header(gtx C, title string, trailing ...layout.FlexChild) D {
	h := gtx.Dp(49)
	w := gtx.Constraints.Max.X
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 20, Right: 16}.Layout(g, func(gtx C) D {
		ch := []layout.FlexChild{
			layout.Flexed(1, func(gtx C) D { return b.label(gtx, 18, wBlack, b.p.Text, title) }),
		}
		ch = append(ch, trailing...)
		return row(gtx, layout.Middle, ch...)
	})
	return D{Size: image.Pt(w, h)}
}

// emptyState draws a centered title + body + optional button.
func (b *base) emptyState(gtx C, icon, title, body string, btn *widget.Clickable, btnLabel string) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(420))
		gtx.Constraints.Min.X = 0
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if icon == "" {
					return D{}
				}
				return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icon, 48, b.p.Faint) })
			}),
			layout.Rigid(func(gtx C) D { return b.centered(gtx, 18, wBlack, b.p.Text, title) }),
			vgap(8),
			layout.Rigid(func(gtx C) D {
				return b.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: b.p.Muted, Align: text.Middle}, body)
			}),
			layout.Rigid(func(gtx C) D {
				if btn == nil {
					return D{}
				}
				return layout.Inset{Top: 20}.Layout(gtx, func(gtx C) D { return b.button(gtx, btn, btnOutline, false, btnLabel) })
			}),
		)
	})
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
	return b.emptyState(gtx, "", "Something went wrong", friendlyErr(err), retry, "Try again")
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

// clock formats "3:04 PM".
func clock(t time.Time) string { return t.Format("3:04 PM") }

// listTime is the compact time Slack shows on list rows: "3:04 PM" today,
// "Yesterday", the weekday within a week, "Oct 12" this year, else
// "Oct 12, 2024".
func listTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
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

var _ = paint.ColorOp{}
