package popups

import (
	"image"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from app.slack.com (2026-11, c-menu): width 300, 12px vertical
// padding, 28px rows with 24px side padding, 15px labels, 13px #5e5d60
// shortcut hints, 20px icon column + 8px gap, separators 17px (8 + 1px line
// rgba(94,93,96,.13) + 8). Highlight: #1264a3 with white text; danger text
// #c01343, highlighted danger #e01e5a. Card radius 4px, ring 1px
// rgba(29,28,29,.13) and 0 4px 12px rgba(0,0,0,.12). Submenus open beside
// the parent item (first row level with it) and flip left at the screen
// edge, overlapping the parent by 2px.

// MenuItem is one menu row.
type MenuItem struct {
	Label    string
	Icon     string // icons registry name ("" = no icon column)
	Hint     string // right-aligned shortcut, e.g. "Ctrl+Shift+S"
	Right    string // right-aligned value text before the chevron ("On")
	Danger   bool
	Disabled bool
	Checked  bool // draws a check at the right
	External bool // draws an external-link glyph at the right

	Separator bool   // a divider line instead of an item
	Header    string // a small non-interactive heading row
	Note      string // a wrapped 13px footnote row

	// Widget draws a custom row (user/team menu headers). It receives the
	// row width in its constraints. Rows with Do are clickable/highlightable.
	Widget func(gtx layout.Context, highlighted bool) layout.Dimensions

	Sub []MenuItem // submenu

	Do       func()
	DoCtx    func(gtx layout.Context) // like Do, with the frame context (clipboard)
	KeepOpen bool                     // don't close the menu after Do
}

// Sep is a separator row.
func Sep() MenuItem { return MenuItem{Separator: true} }

// MenuOpts tune placement and size.
type MenuOpts struct {
	Width  float32         // dp; default 300
	Anchor image.Rectangle // the element the menu belongs to (flips around it)
	Side   Side            // Below (default), Above, Right, Left
	// AlignRight right-aligns the menu with the anchor's right edge
	// (header kebab menus).
	AlignRight bool
	OnClose    func()
}

// Menu is a Slack menu overlay with keyboard navigation and submenus.
type Menu struct {
	base
	h      Host
	items  []MenuItem
	opts   MenuOpts
	clicks []widget.Clickable
	scrim  scrim
	block  blocker
	sel    int // highlighted row, -1 none
	kbd    bool
	parent *Menu
	sub    *Menu
	subIdx int
	rows   []image.Rectangle // last frame's row rects (window coords)
	rect   image.Rectangle
	focus  bool
}

// NewMenu opens a menu with its top-left corner at anchor (flipping at the
// window edges).
func NewMenu(h Host, anchor image.Point, items []MenuItem) *Menu {
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor)})
}

// NewMenuOpts opens a menu with explicit options.
func NewMenuOpts(h Host, items []MenuItem, o MenuOpts) *Menu {
	m := &Menu{h: h, items: items, opts: o, sel: -1, subIdx: -1}
	m.clicks = make([]widget.Clickable, len(items))
	m.onClose = o.OnClose
	return m
}

func (m *Menu) root() *Menu {
	r := m
	for r.parent != nil {
		r = r.parent
	}
	return r
}

// active returns the deepest open submenu that has keyboard focus.
func (m *Menu) active() *Menu {
	a := m
	for a.sub != nil && a.sub.kbd {
		a = a.sub
	}
	return a
}

func (m *Menu) selectable(i int) bool {
	it := &m.items[i]
	if it.Separator || it.Header != "" || it.Note != "" || it.Disabled {
		return false
	}
	if it.Widget != nil && it.Do == nil && it.DoCtx == nil && len(it.Sub) == 0 {
		return false
	}
	return true
}

func (m *Menu) move(d int) {
	n := len(m.items)
	if n == 0 {
		return
	}
	i := m.sel
	for k := 0; k < n; k++ {
		if i < 0 {
			if d > 0 {
				i = 0
			} else {
				i = n - 1
			}
		} else {
			i = (i + d + n) % n
		}
		if m.selectable(i) {
			m.sel = i
			return
		}
	}
}

func (m *Menu) rowHeight(gtx layout.Context, th *theme.Theme, i, w int) int {
	it := &m.items[i]
	switch {
	case it.Separator:
		return dp(gtx, 17)
	case it.Note != "":
		_, d := record(loose(gtx, image.Pt(w-dp(gtx, 48), 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Regular, colorsOf(th).Fg3, it.Note, 0)
		})
		return d.Size.Y + dp(gtx, 8)
	case it.Widget != nil:
		_, d := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return it.Widget(gtx, false)
		})
		return d.Size.Y
	}
	return dp(gtx, 28)
}

func (m *Menu) activate(gtx layout.Context, i int) {
	if i < 0 || i >= len(m.items) || !m.selectable(i) {
		return
	}
	it := m.items[i]
	if len(it.Sub) > 0 {
		m.openSub(i, true)
		return
	}
	if !it.KeepOpen {
		m.root().closeAll()
	}
	if it.DoCtx != nil {
		it.DoCtx(gtx)
	}
	if it.Do != nil {
		it.Do()
	}
}

func (m *Menu) closeAll() {
	for s := m; s != nil; s = s.sub {
		s.done = true
	}
	m.Dismiss()
}

func (m *Menu) openSub(i int, kbd bool) {
	if m.sub != nil && m.subIdx == i {
		if kbd {
			m.sub.kbd = true
			if m.sub.sel < 0 {
				m.sub.move(1)
			}
		}
		return
	}
	it := m.items[i]
	s := &Menu{h: m.h, items: it.Sub, opts: MenuOpts{Width: m.opts.Width}, sel: -1, subIdx: -1, parent: m, kbd: kbd}
	s.clicks = make([]widget.Clickable, len(it.Sub))
	if kbd {
		s.move(1)
	}
	m.sub, m.subIdx = s, i
}

func (m *Menu) closeSub() {
	m.sub, m.subIdx = nil, -1
}

func (m *Menu) keys(gtx layout.Context) {
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: m, Name: key.NameUpArrow},
			key.Filter{Focus: m, Name: key.NameDownArrow},
			key.Filter{Focus: m, Name: key.NameLeftArrow},
			key.Filter{Focus: m, Name: key.NameRightArrow},
			key.Filter{Focus: m, Name: key.NameReturn},
			key.Filter{Focus: m, Name: key.NameEnter},
			key.Filter{Focus: m, Name: key.NameSpace},
			key.Filter{Focus: m, Name: key.NameEscape},
			key.Filter{Focus: m, Name: key.NameHome},
			key.Filter{Focus: m, Name: key.NameEnd},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		a := m.active()
		switch ke.Name {
		case key.NameUpArrow:
			a.move(-1)
		case key.NameDownArrow:
			a.move(1)
		case key.NameHome:
			a.sel = -1
			a.move(1)
		case key.NameEnd:
			a.sel = -1
			a.move(-1)
		case key.NameRightArrow:
			if a.sel >= 0 && len(a.items[a.sel].Sub) > 0 {
				a.openSub(a.sel, true)
			}
		case key.NameLeftArrow:
			if a.parent != nil {
				a.parent.closeSub()
			}
		case key.NameReturn, key.NameEnter, key.NameSpace:
			a.activate(gtx, a.sel)
		case key.NameEscape:
			if a.parent != nil {
				a.parent.closeSub()
			} else {
				m.closeAll()
			}
		}
	}
}

// Layout draws the menu (and any open submenu).
func (m *Menu) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if m.done {
		return layout.Dimensions{}
	}
	win := gtx.Constraints.Max
	if m.parent == nil {
		stacked(gtx, m)
		if !m.focus {
			gtx.Execute(key.FocusCmd{Tag: m})
			m.focus = true
		}
		m.keys(gtx)
		if m.done {
			return layout.Dimensions{}
		}
		if m.scrim.Layout(gtx) {
			m.closeAll()
			return layout.Dimensions{}
		}
	}
	m.layoutCard(gtx, win)
	if m.parent == nil && !m.done {
		keyTarget(gtx, m, m.rect)
	}
	return layout.Dimensions{Size: win}
}

func (m *Menu) layoutCard(gtx layout.Context, win image.Point) {
	th := m.h.Theme()
	cl := colorsOf(th)
	wdp := m.opts.Width
	if wdp == 0 {
		wdp = 300
	}
	w := dp(gtx, wdp)
	pad := dp(gtx, 12)
	hs := make([]int, len(m.items))
	h := 2 * pad
	for i := range m.items {
		hs[i] = m.rowHeight(gtx, th, i, w)
		h += hs[i]
	}
	maxH := win.Y - 2*dp(gtx, 8)
	if h > maxH {
		h = maxH
	}
	sz := image.Pt(w, h)
	var pos image.Point
	if m.parent != nil {
		pr := m.parent.rows[m.parent.subIdx]
		par := m.parent.rect
		anchor := image.Rect(par.Min.X+dp(gtx, 2), pr.Min.Y-pad, par.Max.X-dp(gtx, 2), pr.Min.Y-pad)
		pos = place(anchor, sz, win, dp(gtx, 4), Right)
	} else {
		a := m.opts.Anchor
		if m.opts.AlignRight {
			a.Min.X = a.Max.X - w
		}
		pos = place(a, sz, win, dp(gtx, 8), m.opts.Side)
	}
	m.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	card(gtx, cl, m.rect, dp(gtx, 4))
	m.block.Layout(gtx, m.rect)

	if len(m.rows) != len(m.items) {
		m.rows = make([]image.Rectangle, len(m.items))
	}
	// clicks & hover first (they reflect last frame's layout).
	for i := range m.items {
		if m.clicks[i].Clicked(gtx) {
			m.activate(gtx, i)
			if m.done {
				return
			}
		}
	}
	hovered := -1
	for i := range m.items {
		if m.clicks[i].Hovered() && m.selectable(i) {
			hovered = i
		}
	}
	if hovered >= 0 && (hovered != m.sel || m.kbd) {
		m.sel = hovered
		if m.parent != nil {
			m.kbd = false
			m.parent.sel = m.parent.subIdx
		}
		if len(m.items[hovered].Sub) > 0 {
			m.openSub(hovered, false)
		} else if m.sub != nil {
			m.closeSub()
		}
	}

	cs := clipRRect(gtx, m.rect, dp(gtx, 4))
	y := pos.Y + pad
	for i := range m.items {
		r := image.Rect(pos.X, y, pos.X+w, y+hs[i])
		m.rows[i] = r
		if y > m.rect.Max.Y {
			y += hs[i]
			continue
		}
		at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
			return m.row(gtx, th, cl, i, image.Pt(w, hs[i]))
		})
		y += hs[i]
	}
	cs.Pop()
	if m.sub != nil {
		m.sub.layoutCard(gtx, win)
	}
}

func (m *Menu) row(gtx layout.Context, th *theme.Theme, cl *Colors, i int, sz image.Point) layout.Dimensions {
	it := &m.items[i]
	switch {
	case it.Separator:
		y := sz.Y / 2
		fill(gtx, image.Rect(0, y, sz.X, y+1), cl.Divider)
		return layout.Dimensions{Size: sz}
	case it.Header != "":
		return at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Bold, cl.Fg3, it.Header)
			})
		})
	case it.Note != "":
		at(gtx, image.Pt(dp(gtx, 24), dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X-dp(gtx, 48), sz.Y)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, it.Note, 0)
		})
		return layout.Dimensions{Size: sz}
	}
	hl := m.sel == i && !it.Disabled
	gtx.Constraints = layout.Exact(sz)
	if it.Widget != nil {
		if !m.selectable(i) {
			return it.Widget(gtx, false)
		}
		return clickBox(gtx, &m.clicks[i], nil, func(gtx layout.Context) layout.Dimensions {
			return it.Widget(gtx, hl)
		})
	}
	fg, hint := cl.Fg, cl.Fg3
	if it.Danger {
		fg = cl.Danger
	}
	if it.Disabled {
		fg, hint = cl.Subtle, cl.Subtle
	}
	return m.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if hl {
			bg := cl.Highlight
			if it.Danger {
				bg = cl.DangerBg
			}
			fill(gtx, image.Rectangle{Max: sz}, bg)
			fg, hint = cl.HighText, cl.HighText
		}
		x := dp(gtx, 24)
		if it.Icon != "" {
			at(gtx, image.Pt(x, (sz.Y-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, it.Icon, 18, fg)
			})
			x += dp(gtx, 28)
		}
		right := sz.X - dp(gtx, 24)
		if len(it.Sub) > 0 {
			right -= dp(gtx, 15)
			at(gtx, image.Pt(right, (sz.Y-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
				c := cl.Fg2
				if hl {
					c = cl.HighText
				}
				return icon(gtx, "chevron-right", 15, c)
			})
			right -= dp(gtx, 8)
		}
		if it.External {
			right -= dp(gtx, 16)
			at(gtx, image.Pt(right, (sz.Y-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "external", 16, hint)
			})
			right -= dp(gtx, 8)
		}
		if it.Checked {
			right -= dp(gtx, 16)
			at(gtx, image.Pt(right, (sz.Y-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "check", 16, fg)
			})
			right -= dp(gtx, 8)
		}
		for _, s := range []struct {
			t    string
			size float32
		}{{it.Hint, 13}, {it.Right, 15}} {
			if s.t == "" {
				continue
			}
			tw := textW(gtx, th, s.size, theme.Regular, s.t)
			right -= tw
			at(gtx, image.Pt(right, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, s.size, theme.Regular, hint, s.t)
				})
			})
			right -= dp(gtx, 8)
		}
		lw := right - x
		if lw < 1 {
			lw = 1
		}
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(lw, sz.Y)}
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, it.Label)
			})
		})
		if !it.Disabled {
			pointerCursor(gtx, sz)
		}
		return layout.Dimensions{Size: sz}
	})
}

// macroAt replays call at p.
func macroAt(gtx layout.Context, call op.CallOp, p image.Point) {
	t := op.Offset(p).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
}
