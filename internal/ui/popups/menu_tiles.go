package popups

// Menu rows plugin CSS lays out inline. Slack's MenuItem is
// li.c-menu_item__li > button.c-menu_item__button.<className> >
// (.c-menu_item__icon, .c-menu_item__label, .c-menu_item__shortcut), and
// its CSS (client-boot-styles) makes the li a block (line-height 28px) and
// the button a row flex (align-items center, min-height 28px, padding
// 0 24px, 15px/28px text). A row whose li CSS turns into an inline box
// with a width (display inline-flex / inline-block, width calc(100% / 3))
// sits beside its inline neighbours, wrapping at the card's edge, each
// line as tall as its tallest (vertical-align top); a button that is a
// column flex stacks its icon over its label, centred, with the gap and
// padding the CSS gives it. That is Better Message Menu's big buttons:
//
//	.p-message_actions_menu .c-menu_item__li:has(.slick-bmm-bigaction) { display: inline-flex; width: calc(100% / 3) }
//	.p-message_actions_menu .c-menu_item__button.slick-bmm-bigaction { flex-direction: column; align-items: center; gap: 4px; padding: 10px 6px }
//	.p-message_actions_menu .slick-bmm-bigaction .c-menu_item__shortcut { display: none }
//
// Only rows with classes (MenuItem.Class) are looked at, and only while
// some CSS is installed.

import (
	"image"
	"image/color"
	"math"
	"sync"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	menuButtonMods    = []string{"c-menu_item__button--highlighted", "c-menu_item__button--danger", "c-menu_item__button--disabled"}
	menuIconClass     = slackc.Class("c-menu_item__icon")
	menuLabelClass    = slackc.Class("c-menu_item__label")
	menuShortcutClass = slackc.Class("c-menu_item__shortcut")

	rowDefsMu sync.Mutex
	rowDefs   = map[string]*menuRowDefs{}
)

// menuRowDefs are a classed row's li (declaring its button, so :has()
// sees the button's classes) and button.
type menuRowDefs struct{ li, btn *slackc.ClassDef }

func menuRowDefsFor(class string) *menuRowDefs {
	rowDefsMu.Lock()
	defer rowDefsMu.Unlock()
	if d := rowDefs[class]; d != nil {
		return d
	}
	if len(rowDefs) > 512 { // classes made up per render: don't keep them all
		rowDefs = map[string]*menuRowDefs{}
	}
	btn := slackc.Class("c-menu_item__button "+class, "type", "button", "role", "menuitem").Tag("button").
		Mods(menuButtonMods...).Kids(menuIconClass, menuLabelClass, menuShortcutClass)
	d := &menuRowDefs{li: menuItemClass.WithKids(btn), btn: btn}
	rowDefs[class] = d
	return d
}

// menuTile is how rowBoxes laid out an inline row (px).
type menuTile struct {
	on                     bool
	column                 bool // the button is a column flex: icon over label
	padT, padR, padB, padL int
	gap, line              int
	hint                   bool // the shortcut shows
}

// rowBoxes are the rows' boxes inside the card's padding (y from 0) and
// their height: each row the card's width below the last, except inline
// rows (menuTile), which line up left to right and wrap.
func (m *Menu) rowBoxes(gtx layout.Context, th *theme.Theme, w int) ([]image.Rectangle, int) {
	n := len(m.items)
	if cap(m.boxes) < n {
		m.boxes = make([]image.Rectangle, n)
	}
	boxes := m.boxes[:n]
	if cap(m.tiles) < n {
		m.tiles = make([]menuTile, n)
	}
	m.tiles = m.tiles[:n]
	y, x, lineH, inLine := 0, 0, 0, false
	endLine := func() {
		if inLine {
			y += lineH
			x, lineH, inLine = 0, 0, false
		}
	}
	for i := range m.items {
		t, tw, tht := m.tileBox(gtx, i, w)
		m.tiles[i] = t
		if !t.on {
			endLine()
			h := m.rowHeight(gtx, th, i, w)
			boxes[i] = image.Rect(0, y, w, y+h)
			y += h
			continue
		}
		if inLine && x+tw > w+1 {
			endLine()
		}
		boxes[i] = image.Rect(x, y, x+tw, y+tht)
		x += tw
		lineH = max(lineH, tht)
		inLine = true
	}
	endLine()
	return boxes, y
}

// tileBox is row i's inline box (width, height), when CSS makes it one.
func (m *Menu) tileBox(gtx layout.Context, i, w int) (menuTile, int, int) {
	it := &m.items[i]
	if it.Class == "" || it.Separator || it.Header != "" || it.Note != "" || it.Widget != nil || slackc.Styles.Empty() {
		return menuTile{}, 0, 0
	}
	defs := menuRowDefsFor(it.Class)
	st := defs.li.Enter(gtx, 0, 0)
	defer st.Leave()
	li := st.Style()
	if li == nil {
		return menuTile{}, 0, 0
	}
	switch li.Display {
	case css.DisplayInlineFlex, css.DisplayInlineBlock, css.DisplayInlineGrid:
	default:
		return menuTile{}, 0, 0
	}
	if li.Width.Kind != css.LenFixed {
		return menuTile{}, 0, 0 // shrink-to-fit inline rows aren't laid out inline
	}
	scale := gtx.Metric.PxPerDp
	if scale <= 0 {
		scale = 1
	}
	cw := float32(w) / scale // the list's width in CSS px
	px := func(l css.Length, def float32) int {
		return int(math.Round(float64(l.Resolve(cw, 0, 0, def) * scale)))
	}
	tw := px(li.Width, cw)
	if tw <= 0 || tw > w {
		tw = w
	}
	b := st.Child(defs.btn, 0).Style()
	t := menuTile{on: true, line: dp(gtx, 28), padL: dp(gtx, 24), padR: dp(gtx, 24)}
	if b != nil {
		t.padT, t.padR, t.padB, t.padL = px(b.Padding[0], 0), px(b.Padding[1], 0), px(b.Padding[2], 0), px(b.Padding[3], 0)
		t.column = b.FlexDirection == css.Column || b.FlexDirection == css.ColumnReverse
		t.gap = px(b.RowGap, 0)
		if b.LineHeight.Kind == css.LenFixed && b.LineHeight.Px > 0 && b.LineHeight.Pct == 0 {
			t.line = px(b.LineHeight, 28)
		}
	}
	sc := st.Child(defs.btn, 0).Child(menuShortcutClass, 0)
	t.hint = it.Hint != "" && !sc.Hidden()
	if !t.column {
		return t, tw, max(dp(gtx, 28), t.padT+t.line+t.padB)
	}
	h := t.padT + t.line + t.padB // the label
	if it.Icon != "" {
		h += t.line + t.gap
	}
	if t.hint {
		h += t.gap + t.line
	}
	return t, tw, max(dp(gtx, 28), h)
}

// tileRow draws inline row i (st its li's style) in its box: a row like
// any other in the box, or with a column button the icon over the label,
// each in a line box, centred.
func (m *Menu) tileRow(gtx layout.Context, th *theme.Theme, cl *Colors, st slackc.Styled, i int, sz image.Point, hl bool) layout.Dimensions {
	t := m.tiles[i]
	if !t.column {
		return m.itemRow(gtx, th, cl, st, i, sz, hl)
	}
	it := &m.items[i]
	fg, hint := cl.Fg, cl.Fg3
	if it.Danger {
		fg = cl.Danger
	}
	if it.Disabled {
		fg, hint = cl.Subtle, cl.Subtle
	}
	return m.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		if hl {
			bg := cl.Highlight
			if it.Danger || it.DangerHighlight {
				bg = cl.DangerBg
			}
			fill(gtx, image.Rectangle{Max: sz}, st.Background(bg))
			fg, hint = cl.HighText, cl.HighText
		} else if st.Has("background-color") {
			fill(gtx, image.Rectangle{Max: sz}, st.Background(cl.Bg))
		}
		fg = st.Color(fg)
		cw := max(1, sz.X-t.padL-t.padR)
		y := t.padT
		if it.Icon != "" {
			is := dp(gtx, 18)
			at(gtx, image.Pt(t.padL+(cw-is)/2, y+(t.line-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, it.Icon, 18, fg)
			})
			y += t.line + t.gap
		}
		centred := func(s string, size float32, c color.NRGBA) {
			lw := min(cw, textW(gtx, th, size, theme.Regular, s))
			at(gtx, image.Pt(t.padL+(cw-lw)/2, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(lw, t.line)}
				return vcenter(gtx, t.line, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, size, theme.Regular, c, s)
				})
			})
		}
		centred(it.Label, 15, fg)
		y += t.line
		if t.hint {
			y += t.gap
			centred(it.Hint, 13, hint)
		}
		if !it.Disabled {
			pointerCursor(gtx, sz)
		}
		return layout.Dimensions{Size: sz}
	})
}
