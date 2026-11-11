package popups

import (
	"image"
	"image/color"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	// Key is the row's MenuTemplateItem key ("sign-out"); "" derives one
	// from the label. Plugins find rows by it.
	Key   string
	Label string
	Icon  string // icons registry name ("" = no icon column)
	Hint  string // right-aligned shortcut label: shortcut.Format("Mod+Shift+S"), "L"
	// Keys are the item's key ids while the menu is open (Slack's
	// shortcut.key: "l", "mod+c"; see menu_keys.go). A press runs the
	// item and closes the menu.
	Keys     []string
	Subtitle string // a 13px second line under the label (Slack's More menu rows)
	Right    string // right-aligned value text before the chevron ("On")
	Danger   bool
	Disabled bool
	Checked  bool // draws a check at the right
	// Radio is Slack's type:"radio" item (ChannelNotificationPrefMenu): the
	// check sits in a left gutter, a checked row is link blue, the label
	// is regular and the Subtitle wraps to two lines.
	Radio    bool
	External bool // draws an external-link glyph at the right

	// Tag and BadgeCount are Slack Kit MenuItem's tag and badgeCount
	// (menu_badge.go): a micro Tag ({"Customized", "informative"} on
	// ChannelNotificationPrefMenu's customized rows) and a c-badge pill
	// (BadgeStyle: "red" by default, "blue", "white", "black",
	// "min_gray"), after the label and the shortcut, before the caret.
	// Slack hides the badge on the highlighted row unless
	// BadgeHighlighted (search's More filters menu).
	Tag              *MenuTag
	BadgeCount       int
	BadgeStyle       string
	BadgeHighlighted bool

	Separator bool   // a divider line instead of an item
	Header    string // a small non-interactive heading row
	Note      string // a wrapped 13px footnote row

	// Widget draws a custom row (user/team menu headers). It receives the
	// row width in its constraints. Rows with Do are clickable/highlightable.
	Widget func(gtx layout.Context, highlighted bool) layout.Dimensions

	Sub []MenuItem // submenu

	// LabelText, when set, draws the label (one line) in place of the
	// menu's 15px text: the font select's options in their own fonts.
	LabelText func(gtx layout.Context, c color.NRGBA) layout.Dimensions

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
	// Popover places the menu as Slack's Popover does at that position
	// ("right-bottom", "bottom-right", …; slack_place.go) with OffsetX /
	// OffsetY (dp) and TryOpposite (tryOppositePosition), instead of Side.
	Popover          string
	OffsetX, OffsetY float32
	TryOpposite      bool
	// AlignRight right-aligns the menu with the anchor's right edge
	// (header kebab menus).
	AlignRight bool
	OnClose    func()

	// Raw skips the MenuFromTemplate boundary: the items already came out
	// of a MenuFromTemplate a plugin rendered (slackc/kit's MenuTrigger).
	Raw bool
	// Within is the Slack component the menu belongs to, rendered around
	// its MenuFromTemplate so plugin context wraps reach the menu (the
	// profile ⋮ menu is RimetoMemberProfileOverflowMenu{memberId}).
	Within      string
	WithinProps slackc.Props
	// Wraps are more Slack components around Within, outermost first (the
	// channel menu is ChannelContextMenu{id} >
	// ContextMenuTriggerWithKeyboardFocus{getTemplate}).
	Wraps []MenuWrap
	// MenuClass is Slack's Menu menuClassNames.
	MenuClass string
}

// MenuWrap is a Slack component a menu is rendered inside.
type MenuWrap struct {
	Name  string
	Props slackc.Props
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

	typeBuf string    // type-ahead buffer (macOS)
	typeAt  time.Time // last type-ahead press

	// MenuFromTemplate (menu_template.go)
	orig    []MenuItem   // the native items
	tplFrom slackc.Props // props the items were last built from
	pending []MenuItem   // native actions JS clicked, run next frame
	key     string
	win     image.Point
	scale   float32

	// Slick boundaries (slick_menu.go)
	wraps   []MenuWrap     // Wraps + Within
	nExtra  int            // trailing rows a plugin added as Menu children
	ctxH    *taut.GoHandle // ContextMenuTriggerWithKeyboardFocus.getTemplate
	ctxDone bool           // asked a patched getTemplate
}

// NewMenu opens a menu with its top-left corner at anchor (flipping at the
// window edges).
func NewMenu(h Host, anchor image.Point, items []MenuItem) *Menu {
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor)})
}

// NewMenuOpts opens a menu with explicit options.
func NewMenuOpts(h Host, items []MenuItem, o MenuOpts) *Menu {
	m := &Menu{h: h, items: items, orig: items, opts: o, sel: -1, subIdx: -1, key: nextKey("menu")}
	m.clicks = make([]widget.Clickable, len(items))
	m.onClose = o.OnClose
	m.wraps = o.Wraps
	if o.Within != "" {
		m.wraps = append(m.wraps[:len(m.wraps):len(m.wraps)], MenuWrap{Name: o.Within, Props: o.WithinProps})
	}
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
	case it.Radio && it.Subtitle != "":
		// 3px padding, the 15/22 label, the 13/18 description (≤ 2 lines):
		// Slack's 46 / 63 px rows
		x := dp(gtx, 60)
		_, d := record(loose(gtx, image.Pt(max(1, w-x-dp(gtx, 24)-tagAndBadgeW(gtx, th, it)), 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Regular, colorsOf(th).Fg3, it.Subtitle, 2)
		})
		return dp(gtx, 6) + dp(gtx, 22) + d.Size.Y
	case it.Subtitle != "":
		return dp(gtx, 52)
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
	if m.ctxH != nil {
		m.ctxH.Release()
		m.ctxH = nil
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
	filters := m.keyFilters()
	for {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		a := m.active()
		nav := ke.Modifiers == 0
		switch name := ke.Name; {
		case !nav:
			// ⌘↑ isn't navigation, but may be an item shortcut
		case name == key.NameUpArrow:
			a.typeBuf = ""
			a.move(-1)
		case name == key.NameDownArrow:
			a.typeBuf = ""
			a.move(1)
		case name == key.NameHome:
			a.typeBuf = ""
			a.sel = -1
			a.move(1)
		case name == key.NameEnd:
			a.typeBuf = ""
			a.sel = -1
			a.move(-1)
		case name == key.NameRightArrow:
			if a.sel >= 0 && len(a.items[a.sel].Sub) > 0 {
				a.openSub(a.sel, true)
			}
		case name == key.NameLeftArrow:
			if a.parent != nil {
				a.parent.closeSub()
			}
		case name == key.NameReturn, name == key.NameEnter, name == key.NameSpace:
			a.activate(gtx, a.sel)
		case name == key.NameEscape:
			if a.parent != nil {
				a.parent.closeSub()
			} else {
				m.closeAll()
			}
		default:
			nav = false
		}
		if m.done {
			return
		}
		// Slack's onMenuKeyDown: an item shortcut runs after navigation
		// (a key can't be both: nav keys carry no shortcut ids here).
		if i := a.shortcutMatch(ke); i >= 0 {
			a.runShortcut(gtx, i)
			if m.done {
				return
			}
			continue
		}
		if MenuTypeAhead && !nav {
			a.typeAhead(gtx, ke)
			if m.done {
				return
			}
		}
	}
}

// Labels lists the menu's rows for tests and diagnostics: each item's
// label, "---" for a separator, "[header]" for a heading, "*" for a
// drawn (Widget) row.
func (m *Menu) Labels() []string {
	out := make([]string, 0, len(m.items))
	for _, it := range m.items {
		switch {
		case it.Separator:
			out = append(out, "---")
		case it.Header != "":
			out = append(out, "["+it.Header+"]")
		case it.Label == "" && it.Widget != nil:
			out = append(out, "*")
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

// Layout draws the menu (and any open submenu).
func (m *Menu) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if m.done {
		return layout.Dimensions{}
	}
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{} // robust to roots passing Exact constraints
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
		if len(m.pending) > 0 {
			m.runPending(gtx)
			if m.done {
				return layout.Dimensions{}
			}
		}
		m.layoutRoot(gtx, win)
	} else {
		m.layoutCard(gtx, win)
	}
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
		if m.opts.Popover != "" {
			pos = slackPlace(m.opts.Anchor, sz, win, m.opts.Popover, dp(gtx, m.opts.OffsetX), dp(gtx, m.opts.OffsetY), dp(gtx, 16), dp(gtx, 10), m.opts.TryOpposite)
		} else {
			pos = place(a, sz, win, dp(gtx, 8), m.opts.Side)
		}
	}
	m.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	if len(m.rows) != len(m.items) {
		m.rows = make([]image.Rectangle, len(m.items))
	}
	st := menuClass.Enter(gtx, 0, 0)
	if st.Hidden() {
		st.Leave()
		return
	}
	styledCard(gtx, st, cl, pos, sz, dp(gtx, 4), false, func(gtx layout.Context) {
		m.layoutBody(gtx, th, cl, pos, w, hs)
	})
	st.Leave()
	if m.sub != nil && !m.done {
		m.sub.layoutCard(gtx, win)
	}
}

// layoutBody draws the card at the origin (pos is its window position).
func (m *Menu) layoutBody(gtx layout.Context, th *theme.Theme, cl *Colors, pos image.Point, w int, hs []int) {
	local := image.Rectangle{Max: m.rect.Size()}
	rad := dp(gtx, 4)
	m.block.Layout(gtx, local)

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

	cs := clipRRect(gtx, local, rad)
	pad := dp(gtx, 12)
	y := pad
	for i := range m.items {
		r := image.Rect(0, y, w, y+hs[i])
		m.rows[i] = r.Add(pos)
		if y > local.Max.Y {
			y += hs[i]
			continue
		}
		at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
			return m.row(gtx, th, cl, i, image.Pt(w, hs[i]))
		})
		y += hs[i]
	}
	cs.Pop()
}

func (m *Menu) row(gtx layout.Context, th *theme.Theme, cl *Colors, i int, sz image.Point) layout.Dimensions {
	it := &m.items[i]
	switch {
	case it.Separator:
		st := menuSepClass.Enter(gtx, 0, 0)
		defer st.Leave()
		if st.Hidden() {
			return layout.Dimensions{Size: sz}
		}
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			y := sz.Y / 2
			fill(gtx, image.Rect(0, y, sz.X, y+1), st.Color(cl.Divider))
			return layout.Dimensions{Size: sz}
		})
	case it.Header != "":
		st := menuHdrClass.Enter(gtx, 0, 0)
		defer st.Leave()
		if st.Hidden() {
			return layout.Dimensions{Size: sz}
		}
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			return at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, st.Color(cl.Fg3), it.Header)
				})
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
	var state css.State
	var mods uint64
	if hl {
		state, mods = css.StateHover, 1
	}
	if it.Danger {
		mods |= 2
	}
	if it.Disabled {
		state, mods = state|css.StateDisabled, mods|4
	}
	st := menuItemClass.Enter(gtx, state, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{Size: sz}
	}
	if it.Widget != nil {
		// widgets get the row width as Max only (Min zero), so nested text
		// measures its natural size.
		gtx.Constraints = layout.Constraints{Max: sz}
		if !m.selectable(i) {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return it.Widget(gtx, false) })
		}
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &m.clicks[i], nil, func(gtx layout.Context) layout.Dimensions {
				return it.Widget(gtx, hl)
			})
		})
	}
	gtx.Constraints = layout.Exact(sz)
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		return m.itemRow(gtx, th, cl, st, i, sz, hl)
	})
}

// itemRow draws a plain item row (label, icon, hints, check, chevron).
func (m *Menu) itemRow(gtx layout.Context, th *theme.Theme, cl *Colors, st slackc.Styled, i int, sz image.Point, hl bool) layout.Dimensions {
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
			if it.Danger {
				bg = cl.DangerBg
			}
			fill(gtx, image.Rectangle{Max: sz}, st.Background(bg))
			fg, hint = cl.HighText, cl.HighText
		} else if st.Has("background-color") {
			fill(gtx, image.Rectangle{Max: sz}, st.Background(cl.Bg))
		}
		fg = st.Color(fg)
		x := dp(gtx, 24)
		if it.Radio {
			return m.radioRow(gtx, th, cl, it, sz, fg, hint, hl)
		}
		if it.Icon != "" && it.Subtitle != "" { // Slack's p-more_menu_icon: 20px
			at(gtx, image.Pt(x, (sz.Y-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, it.Icon, 20, fg)
			})
			x += dp(gtx, 36)
		} else if it.Icon != "" {
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
		right = m.tagAndBadge(gtx, th, it, hl, right, sz.Y)
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
		if it.Subtitle != "" {
			sub := hint
			at(gtx, image.Pt(x, dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(lw, dp(gtx, 20))}
				return text(gtx, th, 15, theme.Bold, fg, it.Label)
			})
			at(gtx, image.Pt(x, dp(gtx, 28)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(lw, dp(gtx, 18))}
				return text(gtx, th, 13, theme.Regular, sub, it.Subtitle)
			})
		} else {
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(lw, sz.Y)}
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					if it.LabelText != nil {
						return it.LabelText(gtx, fg)
					}
					return text(gtx, th, 15, theme.Regular, fg, it.Label)
				})
			})
		}
		if !it.Disabled {
			pointerCursor(gtx, sz)
		}
		return layout.Dimensions{Size: sz}
	})
}

// radioRow draws a Radio item: the 16px check at x 8, the 20px icon at
// x 24, the label at x 60 (one line) and the description under it; a
// checked row is link blue (Slack's c-menu_item--checked).
func (m *Menu) radioRow(gtx layout.Context, th *theme.Theme, cl *Colors, it *MenuItem, sz image.Point, fg, sub color.NRGBA, hl bool) layout.Dimensions {
	if it.Checked && !hl {
		fg, sub = cl.Link, cl.Link
	}
	top := dp(gtx, 3)
	if it.Subtitle == "" {
		top = (sz.Y - dp(gtx, 22)) / 2
	}
	if it.Checked {
		at(gtx, image.Pt(dp(gtx, 8), top+dp(gtx, 3)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "check", 16, fg)
		})
	}
	x := dp(gtx, 24)
	if it.Icon != "" {
		at(gtx, image.Pt(x, top+dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, it.Icon, 20, fg)
		})
		x += dp(gtx, 36)
	}
	right := m.tagAndBadge(gtx, th, it, hl, sz.X-dp(gtx, 24), sz.Y)
	lw := max(1, right-x)
	at(gtx, image.Pt(x, top), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(lw, dp(gtx, 22))}
		return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, fg, it.Label)
		})
	})
	if it.Subtitle != "" {
		at(gtx, image.Pt(x, top+dp(gtx, 22)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(lw, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, sub, it.Subtitle, 2)
		})
	}
	if !it.Disabled {
		pointerCursor(gtx, sz)
	}
	return layout.Dimensions{Size: sz}
}

// macroAt replays call at p.
func macroAt(gtx layout.Context, call op.CallOp, p image.Point) {
	t := op.Offset(p).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
}
