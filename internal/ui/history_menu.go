package ui

import (
	"image"
	"strings"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The history menus, from Slack's web client (build 4d0c78085232,
// 2026-11-10):
//
//   - HistoryButtons (boot-render tLXx), the top bar's clock: a 360-wide
//     menu (p-ia4_history_menu, left -18px, top -8px on desktop) with a
//     "Recent" header, then the recents (qsEE Dx: one per place,
//     getUniqueEntryKey), each through getHistoryMenuItem (F9mM), at most
//     15, or 25 with desktop_history_menu (Fpm/ Ti). When that many show and
//     there are more, a separator and "Show more" (+gg6
//     openShowAllHistoryModal).
//   - ModalMoreHistory (+gg6): the "History" dialog with a "Clear history"
//     button (icon clear) after the title, the places grouped under
//     {date_pretty} headers with separators between days; a click opens
//     the place and closes the dialog, Clear history empties the recents.
//   - The desktop app's History menu (a6xy gJ, only with
//     desktop_history_menu): Back (⌘[ / Alt+Left), Forward (⌘] /
//     Alt+Right), Recently Closed (windows; Clack has one window), then up
//     to 15 places with their labels cut at 70 characters, then "Show full
//     history" (⌘Y / Ctrl+H) when 15 show and there are more. Without the
//     experiment the web client gives the History menu nothing
//     (getWorkspaceMenuItems still works out six recent views but no
//     longer returns them); macmenu.go keeps Back and Forward.
//
// desktop_history_recents_read (Fpm/ Q) merges visits.list into the
// recents (qsEE B7); Clack keeps its own recents only (docs/EXPERIMENTS.md).
//
// Clack's places are its routes: a conversation (any c/<id>… route is the
// channel's place; threads only count apart under agentic_ia,
// includeThreadsAndSurfaces), a search (search?q=), or a page. The Home
// view without a conversation is Slack's empty primary view (Pempty),
// which Dx skips, as are the simplified layout's own views.

const (
	historyMax     = 15  // tLXx maxMenuItems
	historyMaxExp  = 25  // with desktop_history_menu
	historyMenuW   = 360 // HistoryButtons width
	historyOSMax   = 15  // gJ: getRecentlyVisitedItemsForMenu(maxMenuItems 15)
	historyOSLabel = 70  // a6xy truncateOSMenuItemLabel
)

// historyPlaceKey is getUniqueEntryKey for a route ("" for views Dx skips).
func historyPlaceKey(r Route) string {
	switch p := r.Page(); {
	case p == "c":
		if ch := r.Channel(); ch != "" {
			return "c/" + ch
		}
		return ""
	case p == "search":
		return "search/" + strings.ToLower(strings.TrimSpace(r.Query()))
	case p == "" || p == "home" || isBBMRoute(r):
		return ""
	}
	return string(r)
}

// historyPlaces is Dx: the recents, one per place (its latest visit),
// at most max (0: all).
func (a *App) historyPlaces(max int) []visit {
	var out []visit
	seen := map[string]bool{}
	for _, v := range a.router.Recents() {
		if max > 0 && len(out) >= max {
			break
		}
		k := historyPlaceKey(v.Route)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}

// historyEntry is getHistoryMenuItem's label for a place.
type historyEntry struct {
	route Route
	label string // the text (the desktop menu's label)
	icon  string // page rows: Slack's page icon (va/N getSvgIconForPage)
	conv  *convInfo
	page  bool // p-ia4_history_menu__item--page: icon, bold label
}

// historyEntryFor returns the entry for a place, false when Slack's would
// have no label (a conversation Clack doesn't know).
func (a *App) historyEntryFor(r Route) (historyEntry, bool) {
	e := historyEntry{route: r}
	switch r.Page() {
	case "c":
		c := a.model().conv(r.Channel())
		if c == nil {
			return e, false
		}
		e.conv, e.label = c, c.displayName()
		if c.Kind == "channel" || c.Kind == "private" {
			e.label = "#" + e.label
		}
		return e, e.label != ""
	case "threads": // viewType Threads: "All Threads"
		e.page, e.icon, e.label = true, "threads", "All Threads"
		return e, true
	case "search": // the query's display string, icon search
		q := r.Query()
		e.page, e.icon, e.label = true, "search", q
		return e, q != ""
	}
	t := pageTitle(r)
	if t == "" {
		return e, false
	}
	e.page, e.label = true, t
	e.icon = historyPageIcon(r)
	return e, true
}

// historyPageIcon is getSvgIconForPage for Clack's pages; pages Slack's
// table leaves out keep the icon Clack draws for them elsewhere.
func historyPageIcon(r Route) string {
	switch r.Page() {
	case "activity":
		return "mentions" // Pactivity
	case "later", "saved":
		return "bookmark" // Plater, Psaved
	case "drafts":
		return "edit" // Pdrafts
	case "unreads":
		return "mark-as-unread" // Punreads
	case "huddles":
		return "headphones" // Phuddles
	case "directories":
		switch strings.TrimPrefix(string(r), "directories?tab=") {
		case "channels":
			return "channel" // Pbrowse-channels
		case "apps":
			return "apps" // Papps
		case "usergroups":
			return "user-groups"
		}
		return "user-directory" // Pbrowse-people
	}
	return routeIconFallback(r)
}

// routeIconFallback is Clack's own icon for a page with no Slack page icon.
func routeIconFallback(r Route) string {
	switch r.Page() {
	case "files":
		return "files"
	case "dms":
		return "dms"
	}
	return "history"
}

// historyMenuMax is O: 15, 25 with desktop_history_menu.
func (a *App) historyMenuMax() int {
	if c := a.Core(); c != nil && c.HistoryMenuExperiment() {
		return historyMaxExp
	}
	return historyMax
}

// historyItems is HistoryButtons' template (H).
func (a *App) historyItems() []popups.MenuItem {
	max := a.historyMenuMax()
	places := a.historyPlaces(max + 1) // F: Dx with maxItems O+1
	items := []popups.MenuItem{{Key: "history_recent", Header: "Recent"}}
	n := 0
	for _, v := range places { // U: v6, up to O entries
		if n >= max {
			break
		}
		e, ok := a.historyEntryFor(v.Route)
		if !ok {
			continue
		}
		n++
		items = append(items, a.historyMenuRow(e))
	}
	if n == max && len(places) > max {
		items = append(items, popups.Sep(), popups.MenuItem{Key: "history_item_show_more", Label: "Show more",
			Do: a.openShowAllHistory})
	}
	return items
}

// historyMenuRow is one entry: .p-ia4_history_menu_item (padding 0 16px)
// around the entity or page label.
func (a *App) historyMenuRow(e historyEntry) popups.MenuItem {
	r := e.route
	return popups.MenuItem{Key: "history_item", Label: e.label,
		Widget: func(gtx layout.Context, hl bool) layout.Dimensions { return a.historyEntryRow(gtx, e, 16, 16, hl) },
		Do:     func() { a.Navigate(string(r)) }}
}

// historyEntryRow draws an entry's label from x: a page row is
// p-ia4_history_menu__item--page (padding 6px, a 20px icon with 8px after
// it, a 700 label on a 20px line: 32px tall); a conversation is the small
// sidebar entity (c-base_entity--small: 20px icon or avatar, 8px, the
// name) on the menu's 28px row.
func (a *App) historyEntryRow(gtx layout.Context, e historyEntry, padL, padR unit.Dp, hl bool) layout.Dimensions {
	cl := popups.ColorsOf(a.th)
	w := gtx.Constraints.Max.X
	h := gtx.Dp(28)
	if e.page {
		h = gtx.Dp(32)
	}
	fg := cl.Fg
	if hl {
		theme.Fill(gtx, image.Rect(0, 0, w, h), cl.Highlight)
		fg = cl.HighText
	}
	x := gtx.Dp(padL)
	if e.page {
		x += gtx.Dp(6)
	}
	is := gtx.Dp(20)
	at(gtx, image.Pt(x, (h-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		switch {
		case e.page:
			icons.Get(e.icon)(gtx, unit.Dp(20), fg)
		case e.conv != nil && (e.conv.Kind == "im" || e.conv.Kind == "app") && e.conv.Avatar != "":
			a.avatar(gtx, e.conv.Avatar, e.conv.Name, is, gtx.Dp(4))
		default:
			icons.Get(historyConvIcon(e.conv))(gtx, unit.Dp(20), fg)
		}
		return layout.Dimensions{Size: image.Pt(is, is)}
	})
	x += is + gtx.Dp(8)
	wt := theme.Regular
	if e.page {
		wt = theme.Bold
	}
	right := gtx.Dp(padR)
	if e.page {
		right += gtx.Dp(6)
	}
	tw := max(0, w-x-right)
	at(gtx, image.Pt(x, 0), image.Pt(tw, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = tw
			return a.th.Label(gtx, 15, wt, fg, e.label)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// historyConvIcon is the small entity's icon for a conversation without
// an avatar image.
func historyConvIcon(c *convInfo) string {
	if c == nil {
		return "hash"
	}
	switch c.Kind {
	case "private":
		return "lock"
	case "im", "mpim", "app":
		return "dms"
	}
	return "hash"
}

// openHistoryMenu opens HistoryButtons' menu below the clock button btn
// (window coords).
func (a *App) openHistoryMenu(btn image.Rectangle) {
	// .p-ia4_history_menu{position:relative;left:-18px} and, in the
	// desktop app, --desktop{top:-8px}
	anchor := btn.Add(image.Pt(-a.px(18), -a.px(8)))
	a.ShowOverlay(popups.NewMenuOpts(a, a.historyItems(), popups.MenuOpts{Anchor: anchor, Width: historyMenuW,
		MenuClass: "p-ia4_history_menu p-ia4_history_menu--desktop"}))
}

// ---- the History dialog (ModalMoreHistory) ----

// historyDialogRow is one row of the dialog's list.
type historyDialogRow struct {
	header string // a {date_pretty} header
	sep    bool
	entry  *historyEntry
}

// historyDialogRows is ModalMoreHistory's rows for the places (newest
// first): a header for each day, a separator before every header but the
// first.
func (a *App) historyDialogRows(places []visit) []historyDialogRow {
	var rows []historyDialogRow
	var days []string
	for _, v := range places {
		e, ok := a.historyEntryFor(v.Route)
		if !ok {
			continue
		}
		d := richtext.FormatDate(v.At, "{date_pretty}")
		if !containsStr(days, d) {
			days = append(days, d)
			if len(rows) > 0 {
				rows = append(rows, historyDialogRow{sep: true})
			}
			rows = append(rows, historyDialogRow{header: d})
		}
		rows = append(rows, historyDialogRow{entry: &e})
	}
	return rows
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// openShowAllHistory is openShowAllHistoryModal: every place in the
// recents, in the History dialog.
func (a *App) openShowAllHistory() {
	d := &historyDialog{a: a, rows: a.historyDialogRows(a.historyPlaces(0))}
	d.m = popups.NewModal(a, popups.ModalOpts{Title: "History", Width: 520, Body: d.body, Over: d.over, Flush: true})
	a.ShowOverlay(d.m)
}

// historyDialog is ModalMoreHistory.
type historyDialog struct {
	a     *App
	m     *popups.Modal
	rows  []historyDialogRow
	list  layout.List
	click []widget.Clickable
	clear widget.Clickable
	tip   popups.TipState
}

// body is .p-modal_more_history_menu__content: 500px tall (the card is at
// most 640), the list padded 12px at the top. Headers are 13px/18px muted
// at 4px 24px; separators a 1px line with 8px around it; entries
// .p-history_menu__entry (padding 0 24px 0 28px), white on #1264a3 while
// hovered.
func (d *historyDialog) body(gtx layout.Context) layout.Dimensions {
	a := d.a
	cl := popups.ColorsOf(a.th)
	w := gtx.Constraints.Max.X
	h := gtx.Dp(500)
	if len(d.click) != len(d.rows) {
		d.click = make([]widget.Clickable, len(d.rows))
	}
	for i := range d.rows {
		if d.rows[i].entry != nil && d.click[i].Clicked(gtx) {
			r := d.rows[i].entry.route
			d.m.Dismiss()
			a.Navigate(string(r))
		}
	}
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	d.list.Axis = layout.Vertical
	layout.Inset{Top: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return d.list.Layout(gtx, len(d.rows), func(gtx layout.Context, i int) layout.Dimensions {
			row := d.rows[i]
			switch {
			case row.sep:
				lh := max(1, gtx.Dp(1))
				theme.Fill(gtx, image.Rect(0, gtx.Dp(8), w, gtx.Dp(8)+lh), cl.Divider)
				return layout.Dimensions{Size: image.Pt(w, gtx.Dp(16)+lh)}
			case row.header != "":
				return layout.Inset{Top: 4, Bottom: 4, Left: 24, Right: 24}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return a.th.Para(gtx, 13, 18, theme.Regular, cl.Fg3, row.header)
				})
			}
			e, c := *row.entry, &d.click[i]
			return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
				return a.historyEntryRow(gtx, e, 28, 24, c.Hovered())
			})
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// over draws the Clear history button after the title (ModalMoreHistory's
// header child, .p-modal_more_history_menu__clear: margin-left 12px), a
// medium icon button with the tooltip "Clear history".
func (d *historyDialog) over(gtx layout.Context) {
	a := d.a
	cl := popups.ColorsOf(a.th)
	tw := a.th.Measure(gtx, 22, theme.Black, "History")
	bs := gtx.Dp(36)
	x := gtx.Dp(28) + tw + gtx.Dp(12)
	y := (gtx.Dp(70) - bs) / 2
	if d.clear.Clicked(gtx) {
		a.router.ClearRecents() // b.TP: the recents, then the dialog closes
		d.m.Dismiss()
	}
	at(gtx, image.Pt(x, y), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return d.clear.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			hovered := d.clear.Hovered()
			if hovered {
				theme.RRect(gtx, image.Rect(0, 0, bs, bs), gtx.Dp(8), cl.BgHover)
			}
			is := gtx.Dp(20)
			at(gtx, image.Pt((bs-is)/2, (bs-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
				icons.Get("clear")(gtx, unit.Dp(20), cl.Fg2)
				return layout.Dimensions{Size: image.Pt(is, is)}
			})
			popups.Tooltip(gtx, a.th, &d.tip, hovered, image.Pt(bs, bs), popups.Above, "Clear history", "")
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
	})
}

// ---- the desktop app's History menu (gJ) ----

// historyOSLabel is truncateOSMenuItemLabel: past 70 characters, the
// first 70 (a lone high surrogate dropped) and "...".
func historyOSLabelText(s string) string {
	if utf8.RuneCountInString(s) <= historyOSLabel {
		return s
	}
	rs := []rune(s)
	return string(rs[:historyOSLabel]) + "..."
}

// historyOSRecents is gJ's recents (getRecentlyVisitedItemsForMenu with
// isOSMenu, from Dx with maxItems 16) and whether "Show full history"
// follows them (15 shown, more than 15 places).
func (a *App) historyOSRecents() (out []visit, labels []string, more bool) {
	places := a.historyPlaces(historyOSMax + 1)
	for _, v := range places {
		if len(out) >= historyOSMax {
			break
		}
		e, ok := a.historyEntryFor(v.Route)
		if !ok || e.label == "" {
			continue
		}
		out = append(out, v)
		labels = append(labels, historyOSLabelText(e.label))
	}
	return out, labels, len(out) == historyOSMax && len(places) > historyOSMax
}
