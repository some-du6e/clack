package views

import (
	"image"
	"math/rand/v2"
	"strings"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// KeyboardShortcuts is Slack's Keyboard Shortcuts pane
// (gantry-v2-async-client-keyboard-shortcuts-view u1zV,
// KeyboardShortcutsFlexpane; build 4d0c78085232), in the secondary view:
//
//   - the flexpane header: "Keyboard Shortcuts" (18px/24px 900) and the
//     close button; with keyboard_shortcuts_search its header content is
//     KeyboardShortcutsSearch, a search field ("Search keyboard
//     shortcuts", .p-flexpane_header__secondary margin-bottom 12px) that
//     filters the rows by title (filterSections). Without the experiment
//     there is no field and the list is always whole.
//   - the list (KeyboardShortcutsList, .p-shortcuts_flexpane__list:
//     16px above, 32px below): "⌘/ to toggle this panel" with slim key
//     chips (.p-keyboard_shortcuts_list__subtitle: 70% text, 12px in),
//     then per section an h3 (.p-shortcuts_flexpane__shortcut_heading:
//     18px 900, a 1px #ddd line under it, margin 32px 16px 0, 24px above
//     the first, 4px under the text) and its rows
//     (.p-shortcuts_flexpane__shortcut: margin 0 16px, a 1px #ddd line,
//     the title on a 22px line with 12px above and below, the keys at the
//     right in 13px). A row with an id or a tab is a button that runs the
//     shortcut (executeKeyboardShortcut); one with an info message has a
//     20px help icon after the title with it as a tooltip.
//   - a search with no rows: KeyboardShortcutsEmptyState, one of three
//     picked at random when the list is made (🔍 No matches / Try a
//     different search term., 🤔 Nothing here / That shortcut might not
//     exist…yet., 🏝️ No shortcuts found / Try searching for something
//     else.).
//   - KeyboardKey chips: 28px tall, at least 28 wide, 8px inside, 4px
//     around, radius 4, the base-ter fill with a 1px otl-ter bottom
//     border; Cmd and the arrows are icons (command, caret-*-filled).
//     Slim chips are 20px with 4px inside and 13px text.
type KeyboardShortcuts struct {
	base
	opts   func() shortcut.ListOpts
	search *search
	list   widget.List
	close  widget.Clickable
	rows   []ksRow
	click  []widget.Clickable
	tips   []popups.TipState
	empty  int // the empty state, picked once
	query  string
	opened bool // the first frame focused the search field
}

// ShortcutRunner is implemented by the Host: executeKeyboardShortcut for
// a row with an id ("new_message", "open_preferences", …) or a rail tab.
type ShortcutRunner interface {
	RunShortcut(id, tab string)
}

type ksKind uint8

const (
	ksSubtitle ksKind = iota
	ksHeader
	ksShortcut
	ksEmpty
)

type ksRow struct {
	kind  ksKind
	title string
	first bool
	sc    shortcut.Shortcut
}

// NewKeyboardShortcuts makes the pane; opts gives the list's inputs each
// time it is built.
func NewKeyboardShortcuts(h Host, opts func() shortcut.ListOpts) *KeyboardShortcuts {
	return &KeyboardShortcuts{base: newBase(h), opts: opts, search: newSearch("Search keyboard shortcuts"), empty: rand.IntN(3)}
}

// ksEmptyStates are KeyboardShortcutsEmptyState's emoji (w) and texts by
// index.
var ksEmptyStates = [3]struct{ emoji, title, body string }{
	{"mag", "No matches", "Try a different search term."},
	{"thinking_face", "Nothing here", "That shortcut might not exist…yet."},
	{"desert_island", "No shortcuts found", "Try searching for something else."},
}

func (v *KeyboardShortcuts) searchOn() bool {
	c := v.h.Core()
	return c != nil && c.KeyboardShortcutsSearch()
}

// buildRows is createRows over the sections (filtered by the query with
// the search box).
func (v *KeyboardShortcuts) buildRows() {
	secs := shortcut.Sections(v.opts())
	q := ""
	if v.searchOn() {
		q = v.search.text()
	}
	v.query = q
	if q != "" {
		secs = shortcut.FilterSections(secs, q)
	}
	v.rows = v.rows[:0]
	if q != "" && len(secs) == 0 {
		v.rows = append(v.rows, ksRow{kind: ksEmpty})
		return
	}
	v.rows = append(v.rows, ksRow{kind: ksSubtitle})
	for i, s := range secs {
		v.rows = append(v.rows, ksRow{kind: ksHeader, title: s.Title, first: i == 0})
		for _, sc := range s.Shortcuts {
			v.rows = append(v.rows, ksRow{kind: ksShortcut, sc: sc})
		}
	}
}

// Rows returns the list's rows as text (tests).
func (v *KeyboardShortcuts) Rows() []string {
	var out []string
	for _, r := range v.rows {
		switch r.kind {
		case ksSubtitle:
			out = append(out, "(subtitle)")
		case ksHeader:
			out = append(out, "## "+r.title)
		case ksEmpty:
			out = append(out, "(empty "+ksEmptyStates[v.empty].title+")")
		default:
			out = append(out, r.sc.Title+" = "+strings.Join(r.sc.Keys, " "))
		}
	}
	return out
}

// SetQuery types q into the search field (tests).
func (v *KeyboardShortcuts) SetQuery(q string) { v.search.ed.SetText(q) }

// Layout draws the pane.
func (v *KeyboardShortcuts) Layout(gtx C) D {
	v.frame()
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	if v.close.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	if !v.opened {
		v.opened = true
		// with the search, opening the view forces the focus into its
		// field (odUS shouldForceFocus; KeyboardShortcutsSearch's
		// VIEW_KEYBOARD_SHORTCUTS focus handler)
		if v.searchOn() {
			gtx.Execute(key.FocusCmd{Tag: &v.search.ed})
		}
	}
	v.search.update(gtx)
	v.buildRows()
	if len(v.click) < len(v.rows) {
		v.click = make([]widget.Clickable, len(v.rows))
		v.tips = make([]popups.TipState, len(v.rows))
	}
	for i := range v.rows {
		r := v.rows[i]
		if r.kind == ksShortcut && (r.sc.ID != "" || r.sc.Tab != "") && v.click[i].Clicked(gtx) {
			if sr, ok := v.h.(ShortcutRunner); ok {
				sr.RunShortcut(r.sc.ID, r.sc.Tab)
			}
		}
	}
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.header),
		layout.Flexed(1, v.body),
	)
	return D{Size: size}
}

// header is the flexpane header: the 49px title row (16px in, controls
// from 12px at the right), then the search field with 12px under it, a
// 1px line at the bottom.
func (v *KeyboardShortcuts) header(gtx C) D {
	w := gtx.Constraints.Max.X
	hh := gtx.Dp(49)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, hh))
	layout.Inset{Left: 16, Right: 12}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.W.Layout(gtx, func(gtx C) D {
					return v.para(gtx, 18, 24, wBlack, v.p.Text, 1, "Keyboard Shortcuts")
				})
			}),
			layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &v.close, "close", 28, 18, v.p.Sec) }),
		)
	})
	h := hh
	if v.searchOn() {
		func() {
			defer opOffset(gtx, image.Pt(0, hh)).Pop()
			sd := layout.Inset{Bottom: 12, Left: 16, Right: 12}.Layout(gtx, func(gtx C) D {
				return v.searchBox(gtx, v.search, 36)
			})
			h += sd.Size.Y
		}()
	}
	hl := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, h-hl, w, h), v.p.Border)
	return D{Size: image.Pt(w, h)}
}

func (v *KeyboardShortcuts) body(gtx C) D {
	if len(v.rows) == 1 && v.rows[0].kind == ksEmpty {
		return v.emptyRow(gtx)
	}
	n := len(v.rows)
	return v.scrollList(gtx, &v.list, n+2, func(gtx C, i int) D {
		switch {
		case i == 0: // the scroll container's 16px top margin
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(16))}
		case i == n+1: // and its 32px bottom one
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(32))}
		}
		return v.rowAt(gtx, i-1)
	})
}

func (v *KeyboardShortcuts) rowAt(gtx C, i int) D {
	r := v.rows[i]
	w := gtx.Constraints.Max.X
	switch r.kind {
	case ksSubtitle:
		return v.subtitle(gtx)
	case ksHeader:
		top := unit.Dp(32)
		if r.first {
			top = 24
		}
		d := layout.Inset{Top: top, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			td := layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return v.para(gtx, 18, 24, wBlack, v.p.Text, 1, r.title) })
			hl := max(1, gtx.Dp(1))
			fill(gtx, image.Rect(0, td.Size.Y, gtx.Constraints.Max.X, td.Size.Y+hl), v.p.Border)
			return D{Size: image.Pt(gtx.Constraints.Max.X, td.Size.Y+hl)}
		})
		return D{Size: image.Pt(w, d.Size.Y)}
	}
	return v.shortcutRow(gtx, i, r.sc)
}

// subtitle is "{⌘ or Ctrl}{/} to toggle this panel": the slim chips are
// shortcut.Chips' (⌘ on macOS, Ctrl elsewhere, as Slack's isMac picks).
func (v *KeyboardShortcuts) subtitle(gtx C) D {
	muted := theme.WithAlpha(v.p.Text, 0xb3) // rgba(sk_foreground_max, .7)
	var ch []layout.FlexChild
	for _, k := range shortcut.Chips("Mod+/") {
		k := k
		ch = append(ch, layout.Rigid(func(gtx C) D { return v.keyChip(gtx, k, true) }))
	}
	ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, muted, " to toggle this panel") }))
	return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D { return row(gtx, layout.Middle, ch...) })
}

// shortcutRow is Shortcut: the title (a button when the row runs a
// shortcut, the search term in bold while searching) and its keys.
func (v *KeyboardShortcuts) shortcutRow(gtx C, i int, sc shortcut.Shortcut) D {
	w := gtx.Constraints.Max.X
	in := gtx.Dp(16)
	cw := w - 2*in
	m := opRecordKeys(gtx, v, sc.Keys)
	keysW := m.size.X
	tw := max(0, cw-keysW-gtx.Dp(8))
	// the title on its 22px line, 12px above and below
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Constraints.Max.Y)}
	title := func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.titleText(gtx, sc.Title) }),
			layout.Rigid(func(gtx C) D {
				if sc.Info == "" {
					return D{}
				}
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
					d := v.icon(gtx, "help", 20, v.p.Meta)
					hov := v.click[i].Hovered()
					popups.Tooltip(gtx, v.th, &v.tips[i], hov, d.Size, popups.Above, sc.Info, "")
					return d
				})
			}),
		)
	}
	var td D
	rec := op.Record(gtx.Ops)
	if sc.ID != "" || sc.Tab != "" {
		td = v.click[i].Layout(tg, func(gtx C) D {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, title)
		})
	} else if sc.Info != "" {
		td = v.click[i].Layout(tg, func(gtx C) D { return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, title) })
	} else {
		td = layout.Inset{Top: 12, Bottom: 12}.Layout(tg, title)
	}
	tcall := rec.Stop()
	h := max(td.Size.Y, m.size.Y)
	func() {
		defer opOffset(gtx, image.Pt(in, 0)).Pop()
		tcall.Add(gtx.Ops)
	}()
	func() {
		defer opOffset(gtx, image.Pt(in+cw-keysW, (h-m.size.Y)/2)).Pop()
		m.call.Add(gtx.Ops)
	}()
	hl := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(in, h, in+cw, h+hl), v.p.Border)
	return D{Size: image.Pt(w, h+hl)}
}

// titleText draws a row's title, the search term in bold (d8TK H) while
// searching.
func (v *KeyboardShortcuts) titleText(gtx C, title string) D {
	q := v.query
	at := -1
	if q != "" {
		at = strings.Index(strings.ToLower(title), strings.ToLower(q))
	}
	if at < 0 {
		return v.para(gtx, 15, 22, wReg, v.p.Text, 0, title)
	}
	parts := []struct {
		s    string
		bold bool
	}{{title[:at], false}, {title[at : at+len(q)], true}, {title[at+len(q):], false}}
	var ch []layout.FlexChild
	for _, p := range parts {
		if p.s == "" {
			continue
		}
		wt := wReg
		if p.bold {
			wt = wBold
		}
		s := p.s
		ch = append(ch, layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wt, v.p.Text, 1, s) }))
	}
	return row(gtx, layout.Baseline, ch...)
}

// recordedKeys is a row's keys laid out off screen.
type recordedKeys struct {
	call op.CallOp
	size image.Point
}

// opRecordKeys lays out keys (chips and the text between them, 13px) and
// returns them recorded.
func opRecordKeys(gtx C, v *KeyboardShortcuts, keys []string) recordedKeys {
	rec := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, gtx.Constraints.Max.Y)}
	var ch []layout.FlexChild
	for _, k := range keys {
		k := k
		if name, ok := strings.CutPrefix(k, "key_"); ok {
			ch = append(ch, layout.Rigid(func(gtx C) D { return v.keyChip(gtx, name, false) }))
			continue
		}
		ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Text, k) }))
	}
	d := row(g, layout.Middle, ch...)
	return recordedKeys{call: rec.Stop(), size: d.Size}
}

// keyChip is KeyboardKey: Cmd and the arrows as icons, else the label.
func (v *KeyboardShortcuts) keyChip(gtx C, label string, slim bool) D {
	icon := map[string]string{"Cmd": "command", "↑": "caret-up-filled", "↓": "caret-down-filled",
		"←": "caret-left-filled", "→": "caret-right-filled"}[label]
	h, minW, padX := unit.Dp(28), unit.Dp(28), unit.Dp(8)
	size := unit.Sp(15)
	mt, mx, mb := unit.Dp(4), unit.Dp(4), unit.Dp(4)
	isize := unit.Dp(20)
	if slim {
		h, minW, padX, size = 20, 20, 4, 13
		mt, mx, mb = 0, 4, 4
		isize = 13
	}
	if icon != "" && !slim {
		padX = 4 // isArrow: paddingInline 1 (arrows; Cmd keeps the icon's own size)
	}
	return layout.Inset{Top: mt, Bottom: mb, Left: mx, Right: mx}.Layout(gtx, func(gtx C) D {
		rec := op.Record(gtx.Ops)
		var cd D
		if icon != "" {
			cd = v.icon(gtx, icon, isize, v.p.Text)
		} else {
			cd = v.label(gtx, size, wReg, v.p.Text, label)
		}
		call := rec.Stop()
		bw := max(gtx.Dp(minW), cd.Size.X+2*gtx.Dp(padX))
		bh := gtx.Dp(h)
		r := image.Rect(0, 0, bw, bh)
		rad := gtx.Dp(4)
		rrect(gtx, r, rad, v.p.Track)
		hl := max(1, gtx.Dp(1))
		// the 1px bottom border (borderBlockEnd), inside the rounded box
		func() {
			defer clip.RRect{Rect: r, NW: rad, NE: rad, SW: rad, SE: rad}.Push(gtx.Ops).Pop()
			fill(gtx, image.Rect(0, bh-hl, bw, bh), v.p.Border)
		}()
		func() {
			defer opOffset(gtx, image.Pt((bw-cd.Size.X)/2, (bh-cd.Size.Y)/2)).Pop()
			call.Add(gtx.Ops)
		}()
		return D{Size: image.Pt(bw, bh)}
	})
}

// emptyRow is KeyboardShortcutsEmptyState: "{emoji} {title}" over the
// description, centred.
func (v *KeyboardShortcuts) emptyRow(gtx C) D {
	e := ksEmptyStates[v.empty]
	return layout.Inset{Top: 48, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if c := v.h.Core(); c != nil {
							if u, _, ok := c.Emoji(e.emoji); ok && u != "" {
								return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, u, 32) })
							}
						}
						return D{}
					}),
					layout.Rigid(func(gtx C) D { return v.label(gtx, 18, wBlack, v.p.Text, e.title) }),
				)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Muted, Align: text.Middle}, e.body)
				})
			}),
		)
	})
}
