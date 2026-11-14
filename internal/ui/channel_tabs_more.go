package ui

// The channel tab bar's overflow (boot-shared E/Yw DraggableDroppableTabs,
// build fa938a086569): the tabs that don't fit go into the More tab's
// menu (DroppableOverflowMenuTab, useTabsOverflowMenuRenderer).
//
//   - measureVisibleTabs: when every tab fits the bar, all show. Otherwise
//     the ones whose right edge is at least 80px inside the bar's right
//     edge do (at least the first), and the More tab follows the last of
//     them, then the add-tab "+".
//   - The More tab (c-tabs__tab_overflow): "More" (tabbed-channel) and the
//     caret-down icon; selected when the open tab is one of the hidden
//     ones. A click opens its menu at "bottom-left".
//   - The menu (c-channel_tabs_overflow_menu, 200px): each hidden tab as a
//     radio row with its 18px icon, checked when it's the open tab; a
//     click opens it. With tab editing (useCanEditTabs) a separator and
//     "Edit tabs" (edit icon), which opens the channel details on Tabs.
//   - channel_tab_overflow_right_click (treatment, mwX5 yG): a right-click
//     on a row opens that tab's context menu (its contextMenuTemplate,
//     getTabContextMenu) at the pointer, the row staying highlighted.
//     Otherwise the right-click does nothing there.
//
// Dragging tabs onto More, into its menu and out of it is tab_drag.go's.

import (
	"image"
	"image/color"
	"sync"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tabsOverflowReserve is measureVisibleTabs' 80px kept clear for the More
// tab once tabs overflow.
const tabsOverflowReserve = 80

// tabsLastVisible is measureVisibleTabs: the index of the last tab shown
// for tabs of widths ws laid out from x0 with gap between them in a bar
// whose usable right edge is right.
func tabsLastVisible(ws []int, x0, gap, right, reserve int) int {
	n := len(ws)
	if n == 0 {
		return -1
	}
	fit, x := 0, x0
	for _, w := range ws {
		if x+w <= right {
			fit++
		}
		x += w + gap
	}
	if fit >= n {
		return n - 1
	}
	c, x := 0, x0
	for _, w := range ws {
		if x+w <= right-reserve {
			c++
		}
		x += w + gap
	}
	return max(c-1, 0)
}

// tabsMore is the More tab's state for one channel's bar.
type tabsMore struct {
	tag    int          // its press, for the window position
	off    image.Point  // window − local, from the last press
	hidden []int        // the hidden tabs' indexes (tests)
	menu   *popups.Menu // the menu last opened
}

// openMenu is the More menu while it is open.
func (st *tabsMore) openMenu() *popups.Menu {
	if st.menu == nil || st.menu.Dismissed() {
		return nil
	}
	return st.menu
}

var tabsMoreState sync.Map // *App -> map[string]*tabsMore (UI goroutine only)

func (a *App) tabsMoreFor(ch string) *tabsMore {
	m, _ := tabsMoreState.Load(a)
	mm, _ := m.(map[string]*tabsMore)
	if mm == nil {
		mm = map[string]*tabsMore{}
		tabsMoreState.Store(a, mm)
	}
	st := mm[ch]
	if st == nil {
		st = &tabsMore{}
		mm[ch] = st
	}
	return st
}

// TabsHidden are the indexes of ch's tabs in the More menu (tests).
func (a *App) TabsHidden(ch string) []int { return a.tabsMoreFor(ch).hidden }

// tabsMoreWidth is the More tab's width: 8px, "More", 4px, the 16px
// caret, 8px.
func (a *App) tabsMoreWidth(gtx layout.Context) int {
	return gtx.Dp(8) + a.th.Measure(gtx, 13, theme.Bold, i18n.T("tabbed-channel", "More")) + gtx.Dp(4) + gtx.Dp(16) + gtx.Dp(8)
}

// layoutTabsMore draws the More tab at x (bar coordinates, the bar sz
// tall) for all's hidden tabs (those after last); cur is the open tab.
// It returns the tab's width.
func (a *App) layoutTabsMore(gtx layout.Context, ch string, all []chTab, last, cur int, x int, sz image.Point) int {
	c := &a.th.C
	st := a.tabsMoreFor(ch)
	st.hidden = st.hidden[:0]
	for i := last + 1; i < len(all); i++ {
		st.hidden = append(st.hidden, i)
	}
	label := i18n.T("tabbed-channel", "More")
	w := a.tabsMoreWidth(gtx)
	sel := cur > last // ej: the open tab is in the menu
	btn := a.clicks.get("tab." + ch + ".more")
	r := image.Rect(x, 0, x+w, sz.Y)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &st.tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			st.off = a.ptrWin.Sub(image.Pt(int(e.Position.X+.5), int(e.Position.Y+.5)))
		}
	}
	at(gtx, image.Pt(x, 0), image.Pt(w, sz.Y), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
			if hov && !sel {
				theme.RRect(gtx, image.Rect(0, gtx.Dp(5), s.X, s.Y-gtx.Dp(5)), gtx.Dp(6), c.BgHover)
			}
			if sel {
				theme.Fill(gtx, image.Rect(0, s.Y-gtx.Dp(2), s.X, s.Y), c.Accent)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			col := c.FgMuted
			if sel {
				col = c.Fg
				if a.th.Dark {
					col = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}
				}
			}
			lw := a.th.Measure(gtx, 13, theme.Bold, label)
			at(gtx, image.Pt(gtx.Dp(8), 0), image.Pt(lw+2, sz.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Bold, col, label)
				})
			})
			at(gtx, image.Pt(gtx.Dp(8)+lw+gtx.Dp(4), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("chevron-down")(gtx, 16, col) // Slack's caret-down glyph
			})
			return layout.Dimensions{Size: image.Pt(w, sz.Y)}
		})
	})
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &st.tag)
	}()
	if clicked(btn) {
		a.openTabsMore(ch, all, last, cur, r.Add(st.off))
	}
	return w
}

// openTabsMore opens the More tab's menu below anchor (window
// coordinates, Popover "bottom-left"); Edit tabs reads useCanEditTabs
// (core.CanEditTabs). It returns the menu (tab_drag.go drags on it).
func (a *App) openTabsMore(ch string, all []chTab, last, cur int, anchor image.Rectangle) *popups.Menu {
	edit := a.Core() != nil && a.Core().CanEditTabs(ch)
	m := popups.NewMenuOpts(a, a.tabsMoreItems(ch, all, last, cur, edit), popups.MenuOpts{Anchor: anchor, Width: 200,
		Popover: "bottom-left", MenuClass: "c-channel_tabs_overflow_menu"})
	a.tabsMoreFor(ch).menu = m
	a.ShowOverlay(m)
	return m
}

// tabsMoreItems is useTabsOverflowMenuRenderer's template: the hidden
// tabs, then (edit: isTabEditingEnabled) a separator and Edit tabs.
func (a *App) tabsMoreItems(ch string, all []chTab, last, cur int, edit bool) []popups.MenuItem {
	var items []popups.MenuItem
	right := false
	if c := a.Core(); c != nil {
		right = c.TabOverflowRightClick()
	}
	for i := last + 1; i < len(all); i++ {
		i, t := i, all[i]
		it := popups.MenuItem{Key: "channel_tab_" + t.key, Label: t.label, Icon: t.icon, Radio: true, Checked: i == cur,
			Do: func() { a.selectChannelTab(ch, i, t) }}
		if right {
			it.Context = func(p image.Point) { a.openTabMenu(ch, t, p) }
		}
		items = append(items, it)
	}
	if edit {
		items = append(items, popups.Sep(), popups.MenuItem{Key: "edit_tabs", Label: i18n.T("tabbed-channel", "Edit tabs"), Icon: "edit",
			Do: func() { a.OpenDetailsTab(ch, "Tabs") }})
	}
	return items
}
