package popups

// The suggestion toolbar: a picked row's secondary actions (rs63
// getSuggestionSecondaryActions and SearchAutocomplete's
// renderSuggestionToolbar, build 4d0c78085232).
//
// While a row is selected (selectedIndex not null) and it has actions,
// .c-search_autocomplete__suggestion_toolbar sits over its right end:
// 28px buttons 8px apart, 16px from the edge, each a 20px icon in
// content-inv-pry with its label as the tooltip (surf-inv behind a hovered
// or focused one); the row's left part stops short of it (max-width
// calc(100% - 44px / 80px / 116px)). Tab in the box moves the focus to
// the first button (the footer reads "Tab Actions"); Tab and Shift+Tab
// step through them, Enter or Space runs one, ↑ / ↓ go back to the box and
// move the selection, ←, →, Backspace, Home and End go back to the box and
// clear it. A copy action shows its activeLabel and a check for 3 s.
//
// The actions, by row:
//
//   - a person (user-navigation): View profile (View app details for an
//     app), then Open in… (Open in split view);
//   - a channel (channel-navigation): Search in channel, then Open in…; a
//     group DM (mpim-navigation): Search in conversation, then Open in…;
//   - a recent search (query-history): Copy search query, Delete;
//   - a file (file-navigation): Copy link.
//
// Slack also offers Open in new window (a conversation, file or view in a
// window of its own); Clack has one window, so those rows are left out,
// as are the Open in… menus that would hold nothing else.

import (
	"context"
	"image"
	"slices"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srAction is one secondary action.
type srAction struct {
	label, desc, icon string
	kind              string // actionType
	activeLabel       string // a copy's "Link copied!"
	do                func(gtx layout.Context)
	surface           []MenuItem // renderSurfaceMenu: the Open in… menu
}

// srTBState is the toolbar's state between frames.
type srTBState struct {
	key    string // the row the toolbar was built for
	acts   []srAction
	btns   [3]widget.Clickable
	tips   [3]TipState
	tags   [3]srTBTag
	focus  int // 1 + the button with the keyboard focus
	active int // 1 + the button showing its activeLabel
	until  time.Time
	rects  [3]image.Rectangle // in the row
	run    int                // 1 + a button clicked during the list's layout, run after it
	gone   map[string]bool    // deleted recent searches (onDelete: the rows go at once)
}

type srTBTag struct{ i int }

// srActionsOf is getSuggestionSecondaryActions for row r.
func (p *SearchPopover) srActionsOf(r *srRow) []srAction {
	h := p.h
	switch r.kind {
	case srRUser:
		id, name := r.id, r.title
		app := false
		if u, ok := lookupUser(h, id); ok {
			app = u.IsBot || u.AppUser
		}
		a := srAction{label: "View profile", desc: "View the profile of " + name, icon: "user-directory", kind: "view_profile"}
		if app {
			a = srAction{label: "View app details", desc: "View the details of the " + name + " app", icon: "app-directory", kind: "open_details"}
		}
		a.do = func(layout.Context) {
			pfOpenFull(h, id)
			p.Dismiss()
		}
		acts := []srAction{a}
		if it, ok := p.srSplitItem(r); ok {
			acts = append(acts, srOpenIn(it))
		}
		return acts
	case srRConv, srRChan:
		id := r.id
		a := srAction{label: "Search in channel", desc: "Search in the " + r.title + " channel", icon: "quick-switch", kind: "search_in_channel"}
		if r.mpim {
			a.label, a.desc = "Search in conversation", "Search in the group message with you, "+srMpimMembers(h, r.members)
		}
		a.do = func(layout.Context) { p.searchInConv(id) }
		acts := []srAction{a}
		if it, ok := p.srSplitItem(r); ok {
			acts = append(acts, srOpenIn(it))
		}
		return acts
	case srRQuery:
		q := r.id
		shown := SearchDisplayText(h, q)
		return []srAction{
			{label: "Copy search query", desc: "Copy the search query " + shown, icon: "posts", kind: "copy_to_clipboard",
				activeLabel: "Query copied!", do: func(gtx layout.Context) { copyText(gtx, shown) }},
			{label: "Delete", desc: "Delete the search query " + shown + " from your query history", icon: "close",
				kind: "delete_query_history", do: func(layout.Context) { p.deleteHistory(q) }},
		}
	case srRFile:
		f := p.fileByID[r.id]
		if f.Permalink == "" {
			return nil
		}
		kind := strings.ToLower(f.PrettyType) // fileTypeDescription
		if kind == "" {
			kind = "file"
		}
		link := f.Permalink
		return []srAction{{label: "Copy link", desc: "Copy the link to the " + r.title + " " + kind, icon: "link",
			kind: "copy_to_clipboard", activeLabel: "Link copied!", do: func(gtx layout.Context) { copyText(gtx, link) }}}
	}
	return nil
}

// srOpenIn is the "Open in…" button (open_in_surface_menu) holding it.
func srOpenIn(it MenuItem) srAction {
	return srAction{label: "Open in…", desc: "Open in…", icon: "surface", kind: "open_in_surface_menu", surface: []MenuItem{it}}
}

// srSplitItem is the surface menu's "Open in split view" for a row
// (onOpenInSplitView: the conversation in the split view; for a person,
// their DM), false where Clack has no split view or Slack offers none.
func (p *SearchPopover) srSplitItem(r *srRow) (MenuItem, bool) {
	route := ""
	switch r.kind {
	case srRUser:
		if r.conv == "" {
			return MenuItem{}, false
		}
		route = "c/" + r.conv
	default:
		if !srLooksLikeChannelID(r.id) {
			return MenuItem{}, false
		}
		route = "c/" + r.id
	}
	it, ok := SplitViewItem(p.h, route, true)
	if !ok {
		return MenuItem{}, false
	}
	do := it.Do
	it.Icon = "move-to-split-view"
	it.Do = func() {
		do()
		p.Dismiss()
	}
	return it, true
}

// srMpimMembers is the group DM's members but you, "A, B and C".
func srMpimMembers(h Host, ids []string) string {
	self := ""
	if c := h.Core(); c != nil {
		self = c.Self().ID
	}
	var names []string
	for _, id := range ids {
		if id == self {
			continue
		}
		if u, ok := lookupUser(h, id); ok {
			names = append(names, u.RealName)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// searchInConv is Search in channel / conversation: the conversation
// opens when it isn't the one on screen, then its search in channel pane.
func (p *SearchPopover) searchInConv(id string) {
	h := p.h
	p.Dismiss()
	if srCurrentPlace() != id {
		h.Navigate("c/" + id)
	}
	if s, ok := h.(interface{ OpenSearchInChannel(ch string) }); ok {
		s.OpenSearchInChannel(id)
	}
}

// deleteHistory is Delete: the row goes (onDelete), the query is
// forgotten by the session's recents, and search.delete takes it off
// Slack's history.
func (p *SearchPopover) deleteHistory(q string) {
	c := p.h.Core()
	if c == nil {
		return
	}
	it := core.QueryHistoryItem{Query: q}
	if hs, ok := c.CachedRecentSearches(100); ok {
		for _, x := range hs {
			if x.Query == q {
				it = x
				break
			}
		}
	}
	if p.tb.gone == nil {
		p.tb.gone = map[string]bool{}
	}
	p.tb.gone[q] = true
	c.ForgetRecentSearch(q)
	srForgetQuery(q)
	run(p.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.DeleteRecentSearch(ctx, it) },
		func(struct{}, error) {
			if !p.done {
				p.rebuild(time.Now(), false)
			}
		})
	p.rebuild(time.Now(), false)
	p.refocus = true
}

// tbFor is the picked row's actions (none while nothing is picked).
func (p *SearchPopover) tbFor() []srAction {
	tb := &p.tb
	if !p.picked || p.sel < 0 || p.sel >= len(p.rows) {
		tb.key, tb.acts, tb.focus, tb.active = "", nil, 0, 0
		return nil
	}
	k := p.rows[p.sel].key()
	if k != tb.key {
		// a new selection: the active label goes (componentDidUpdate)
		tb.key, tb.focus, tb.active = k, 0, 0
		tb.acts = p.srActionsOf(&p.rows[p.sel])
		if len(tb.acts) > len(tb.btns) {
			tb.acts = tb.acts[:len(tb.btns)]
		}
	}
	return tb.acts
}

// tbRight is where the picked row's left part stops for its toolbar.
func (p *SearchPopover) tbRight(gtx layout.Context, w int) int {
	n := len(p.tbFor())
	if n == 0 {
		return w - dp(gtx, 16)
	}
	return w - dp(gtx, 16) - n*dp(gtx, 28) - (n-1)*dp(gtx, 8)
}

// tbLayout draws the toolbar at the right of a row sz big; org is the
// row's window position.
func (p *SearchPopover) tbLayout(gtx layout.Context, th *theme.Theme, sz image.Point) {
	tb := &p.tb
	acts := p.tbFor()
	if len(acts) == 0 {
		return
	}
	if tb.active > 0 && gtx.Now.After(tb.until) {
		tb.active = 0
	}
	bs, gap := dp(gtx, 28), dp(gtx, 8)
	x := sz.X - dp(gtx, 16) - len(acts)*bs - (len(acts)-1)*gap
	y := (sz.Y - bs) / 2
	for i := range acts {
		a := acts[i]
		if tb.btns[i].Clicked(gtx) {
			tb.run = i + 1 // the rows may change: after the list
		}
		label, ic := a.label, a.icon
		if tb.active == i+1 && a.activeLabel != "" {
			label, ic = a.activeLabel, "check"
		}
		bx := x + i*(bs+gap)
		at(gtx, image.Pt(bx, y), func(gtx layout.Context) layout.Dimensions {
			d := tb.btns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				box := image.Rectangle{Max: image.Pt(bs, bs)}
				// content-inv-pry; hovered, the icon container is
				// constants-white on surf-inv
				fg := tokContentInv.of(th)
				hov := tb.btns[i].Hovered()
				switch {
				case tb.focus == i+1:
					// :focus-visible: a 1px content-inv-pry ring, its 30%
					// halo 5px out, on surf-inv
					rrect(gtx, box.Inset(-dp(gtx, 5)), dp(gtx, 13), theme.WithAlpha(tokContentInv.of(th), 0x4d))
					rrect(gtx, box.Inset(-1), dp(gtx, 9), tokContentInv.of(th))
					rrect(gtx, box, dp(gtx, 8), theme.Over(tokSurfInv.of(th), colorsOf(th).Highlight))
				case hov:
					rrect(gtx, box, dp(gtx, 8), theme.Over(tokSurfInv.of(th), colorsOf(th).Highlight))
				}
				if hov {
					fg = tokWhite.of(th)
				}
				is := dp(gtx, 20)
				at(gtx, image.Pt((bs-is)/2, (bs-is)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, ic, 20, fg)
				})
				pointerCursor(gtx, box.Max)
				return layout.Dimensions{Size: box.Max}
			})
			Tooltip(gtx, th, &tb.tips[i], tb.btns[i].Hovered(), d.Size, Above, label, "")
			// focusable (Tab moves here)
			keyTarget(gtx, &tb.tags[i], image.Rectangle{Max: d.Size})
			return d
		})
		tb.rects[i] = image.Rectangle{Min: image.Pt(bx, y), Max: image.Pt(bx+bs, y+bs)}
	}
}

// tbRunPending runs the button clicked while the list was laid out.
func (p *SearchPopover) tbRunPending(gtx layout.Context) {
	if i := p.tb.run - 1; i >= 0 {
		p.tb.run = 0
		p.tbRun(gtx, i)
	}
}

// tbRun runs button i: a copy shows its activeLabel; Open in… opens its
// menu (bottom-right, offsetX 180).
func (p *SearchPopover) tbRun(gtx layout.Context, i int) {
	tb := &p.tb
	if i < 0 || i >= len(tb.acts) {
		return
	}
	a := tb.acts[i]
	if len(a.surface) > 0 {
		r := tb.rects[i].Add(p.listOrg).Add(image.Pt(0, p.rowTop(gtx, p.sel)))
		p.h.ShowOverlay(NewMenuOpts(p.h, a.surface, MenuOpts{Anchor: r, Popover: "bottom-right", OffsetX: 180,
			OnClose: func() { p.refocus = true }}))
		return
	}
	if a.do != nil {
		a.do(gtx)
	}
	if a.activeLabel != "" {
		tb.active, tb.until = i+1, gtx.Now.Add(3*time.Second)
		gtx.Execute(op.InvalidateCmd{At: tb.until})
	}
}

// tbKeys handles the keys while a toolbar button has the focus.
func (p *SearchPopover) tbKeys(gtx layout.Context) {
	tb := &p.tb
	for i := range tb.tags {
		tag := &tb.tags[i]
		for {
			e, ok := gtx.Event(
				key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift},
				key.Filter{Focus: tag, Name: key.NameReturn},
				key.Filter{Focus: tag, Name: key.NameEnter},
				key.Filter{Focus: tag, Name: key.NameSpace},
				key.Filter{Focus: tag, Name: key.NameUpArrow},
				key.Filter{Focus: tag, Name: key.NameDownArrow},
				key.Filter{Focus: tag, Name: key.NameLeftArrow},
				key.Filter{Focus: tag, Name: key.NameRightArrow},
				key.Filter{Focus: tag, Name: key.NameDeleteBackward},
				key.Filter{Focus: tag, Name: key.NameHome},
				key.Filter{Focus: tag, Name: key.NameEnd},
				key.Filter{Focus: tag, Name: key.NameEscape},
				key.FocusFilter{Target: tag},
			)
			if !ok {
				break
			}
			switch ev := e.(type) {
			case key.FocusEvent:
				if ev.Focus {
					tb.focus = i + 1
				} else if tb.focus == i+1 {
					tb.focus = 0
				}
			case key.Event:
				if ev.State != key.Press {
					continue
				}
				switch ev.Name {
				case key.NameTab:
					next := i + 1
					if ev.Modifiers.Contain(key.ModShift) {
						next = i - 1
					}
					switch {
					case next < 0:
						p.refocus = true
					case next < len(tb.acts):
						gtx.Execute(key.FocusCmd{Tag: &tb.tags[next]})
					}
				case key.NameReturn, key.NameEnter, key.NameSpace:
					p.tbRun(gtx, i)
				case key.NameUpArrow:
					p.refocus = true
					p.move(-1)
				case key.NameDownArrow:
					p.refocus = true
					p.move(1)
				case key.NameEscape:
					if isTop(p) {
						p.Dismiss()
					}
				default:
					p.refocus = true
					p.unpick()
				}
				p.h.Invalidate()
				if p.done {
					return
				}
			}
		}
	}
}

// tbFocus is Tab in the box: the first button takes the focus.
func (p *SearchPopover) tbFocus(gtx layout.Context) bool {
	if len(p.tbFor()) == 0 {
		return false
	}
	gtx.Execute(key.FocusCmd{Tag: &p.tb.tags[0]})
	return true
}

// srForgetQuery drops q from the session's own query list.
func srForgetQuery(q string) {
	srMu.Lock()
	srRecentQs = slices.DeleteFunc(srRecentQs, func(x string) bool { return x == q })
	srMu.Unlock()
}

// rowTop is row i's top in the list's viewport (the rows above it less
// the scroll).
func (p *SearchPopover) rowTop(gtx layout.Context, i int) int {
	pos := p.list.Position
	y := -pos.Offset
	for j := pos.First; j < i && j < len(p.rows); j++ {
		y += dp(gtx, p.rows[j].heightDp())
	}
	return y
}
