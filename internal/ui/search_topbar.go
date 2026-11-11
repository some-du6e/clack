package ui

// The top bar's search box is Slack's BaseSearch (gantry render zuQH, CDN
// build 4d0c78085232; client-boot-styles p-top_nav__search):
//
//   - getSearchText: "Search {teamName}", or "Search: {plainTextQuery}"
//     once the switcher holds a query;
//   - with a query, a 26px p-top_nav__search__clear button (a 16px close,
//     tooltip "Clear search" after 300 ms, below) right of the text runs
//     clearSearchInput;
//   - onMouseDown opens the search: a primary-button press without Ctrl
//     (onKeyPress: Enter or Space, which the button's Clickable reports);
//   - on Windows and Linux (p-top_nav__search--windows-linux) the search
//     icon hides while there is a query (--with-query), the text starting
//     where the icon was (padding 12 + the text's 8).
//
// Opening shows the switcher with the query it holds, as Slack's
// omniswitcher keeps its display query.

import (
	"image"
	"runtime"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// searchBoxState is the box's input state.
type searchBoxState struct {
	down     struct{} // the press handler's tag (onMouseDown)
	opened   bool     // a press opened the search: its click is spent
	clearTip popups.TipState
}

// searchBoxText is BaseSearch.getSearchText.
func (a *App) searchBoxText() string {
	if q := a.searchQuery; q != "" {
		return i18n.T("classic_nav", "Search: {plainTextQuery}", "plainTextQuery", q)
	}
	return i18n.T("channel_sidebar", "Search {teamName}", "teamName", a.teamName())
}

// searchBoxIcon reports whether the search icon shows: always on macOS,
// on Windows and Linux only without a query.
func (a *App) searchBoxIcon() bool {
	return a.searchQuery == "" || runtime.GOOS == "darwin"
}

// searchBoxTextX is where the text starts: after the icon (34), or at the
// box's padding + the text's when the icon hides (20).
func (a *App) searchBoxTextX(gtx layout.Context) int {
	if a.searchBoxIcon() {
		return gtx.Dp(34)
	}
	return gtx.Dp(20)
}

// searchBoxPress is onMouseDown over the box (sz at the current origin):
// a primary press without Ctrl opens the search at once. The handler
// passes events on, so the box's Clickable still hovers and takes keys.
func (a *App) searchBoxPress(gtx layout.Context, sz image.Point) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &a.searchBox.down, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || e.Kind != pointer.Press || e.Buttons != pointer.ButtonPrimary {
			continue
		}
		// the press is the box's whole answer: its click is spent, and
		// with Ctrl nothing opens (BaseSearch has no onClick)
		a.searchBox.opened = true
		if e.Modifiers&key.ModCtrl == 0 {
			a.openSearch(a.searchQuery)
		}
	}
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &a.searchBox.down)
	pass.Pop()
	st.Pop()
}

// searchBoxClicked handles the box's Clickable: a click that follows a
// pointer press is spent (the press already acted); any other, Enter or
// Space, opens the search.
func (a *App) searchBoxClicked() {
	if a.searchBox.opened {
		a.searchBox.opened = false
		return
	}
	a.openSearch(a.searchQuery)
}

// searchBoxClear draws the clear button at the right of a box sw×sh wide
// at the current origin, with a query only, and empties the query when
// clicked. It returns the width it took (0 without a query).
func (a *App) searchBoxClear(gtx layout.Context, sw, sh int) int {
	if a.searchQuery == "" {
		return 0
	}
	c := &a.th.C
	bw := gtx.Dp(26)
	btn := a.clicks.get("nav.search.clear")
	at(gtx, image.Pt(sw-bw, (sh-bw)/2), image.Pt(bw, bw), func(gtx layout.Context) layout.Dimensions {
		d := clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
			is := gtx.Dp(16)
			at(gtx, image.Pt((bw-is)/2, (bw-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("close")(gtx, 16, c.SearchText)
			})
			return layout.Dimensions{Size: image.Pt(bw, bw)}
		})
		a.searchBox.clearTip.Delay = 300 * time.Millisecond
		popups.Tooltip(gtx, a.th, &a.searchBox.clearTip, btn.Hovered(), d.Size, popups.Below,
			i18n.T("teams", "Clear search"), "")
		return d
	})
	if clicked(btn) { // clearSearchInput
		a.searchQuery = ""
		a.Invalidate()
	}
	return bw
}
