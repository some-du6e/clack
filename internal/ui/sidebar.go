package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

type sbKind uint8

const (
	sbLink sbKind = iota
	sbHeading
	sbConv
	sbAction
	sbGap
	sbNote
	sbDivider
)

type sbRow struct {
	kind    sbKind
	key     string // click key / route / conv id
	label   string
	icon    string
	emoji   string // section headings: the section emoji's image URL, drawn instead of icon
	section string
	gap     int               // dp, for sbGap
	itemKey string            // Slack's data-item-key ("" = untagged spacer)
	tr      i18n.NS           // label is Slack's string in this namespace ("" = data, drawn as is)
	btn     *widget.Clickable // resolved once per rebuild (no per-frame key strings)
}

type sidebarState struct {
	width     int // dp
	hidden    bool
	list      widget.List
	rows      []sbRow
	builtFor  int    // model version
	viewsFor  string // sbViewsKey the rows were built with
	collapsed map[string]bool
	rowCount  int
	drag      gesture.Drag
	dragX     float32
	dragW     float32
	hover     bool            // pointer over the sidebar (.p-channel_sidebar:hover)
	headTags  map[string]*int // section headings' right-click tags (create_channel.go)

	// Drafts & sent's badge list: hovering it shows Slack's tooltip
	// ("N Drafts" / "N Scheduled", to the right).
	outboxHover gesture.Hover
	outboxTip   popups.TipState

	// ChannelSidebar (slick_shell.go)
	gen       int   // bumped by every rebuild
	propRows  []any // rows as Slack-shaped props, for gen propsGen
	propsGen  int
	shown     []int // rows a plugin left (nil: all), from shownFrom
	shownFrom []any
	shownGen  int
}

// sbLinks are the page links in Slack's order (client-channel-list-view
// bG: Unreads, Threads, Huddles, Drafts & sent, Saved). Unreads is
// UnreadsSidebarIcon's mark-as-unread, shown only while
// enable_unread_view is true (shouldShowAllUnreadsLink).
var sbLinks = []sbRow{
	{kind: sbLink, key: "unreads", label: "Unreads", icon: "mark-unread", tr: "all_unreads"},
	{kind: sbLink, key: "threads", label: "Threads", icon: "threads", tr: "channel_sidebar"},
	{kind: sbLink, key: "huddles", label: "Huddles", icon: "huddle", tr: "huddles"},
	{kind: sbLink, key: "drafts", label: "Drafts & sent", icon: "paper-plane-alt", tr: "outbox"}, // Slack's OutboxPageIcon
	{kind: sbLink, key: "saved", label: "Saved", icon: "saved", tr: "saved_for_later"},
	{kind: sbLink, key: "directories", label: "Directories", icon: "directories", tr: "channel_sidebar"},
}

// sbViewPref is each link's Preferences > Home pref (off: hidden).
var sbViewPref = map[string]string{"unreads": "enable_unread_view", "threads": "enable_threads_view", "huddles": "enable_huddles_home_view",
	"drafts": "enable_sent_view", "saved": "enable_saved_view", "directories": "enable_unified_directory_view"}

// outboxCount is the drafts count Drafts & sent shows: Slack's
// OutboxLinkContent renders maxCount 99 and over as "99+".
func outboxCount(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// outboxTip is the badge list's tooltip: one line per badge, Slack's
// "{numDrafts, plural, =1 {# Draft} other {# Drafts}}" and
// "{numScheduled} Scheduled".
func outboxTip(drafts, sched int) string {
	var lines []string
	switch {
	case drafts == 1:
		lines = append(lines, "1 Draft")
	case drafts > 1:
		lines = append(lines, strconv.Itoa(drafts)+" Drafts")
	}
	if sched > 0 {
		lines = append(lines, strconv.Itoa(sched)+" Scheduled")
	}
	return strings.Join(lines, "\n")
}

// sbViewsKey is the links turned off, for rebuilding when they change.
func sbViewsKey() string { return strings.Join(uipref.Get().HiddenViews, ",") }

func sectionIcon(typ string) string {
	switch typ {
	case "stars":
		return "star"
	case "channels":
		return "channels-section"
	case "direct_messages":
		return "dms"
	case "recent_apps", "apps":
		return "apps"
	case "coding_channels": // Slack's Code channels heading carries the code glyph
		return "code"
	case "priority": // VIP unreads: getIconPrefix's vip glyph
		return "vip"
	}
	return "list-view"
}

// sbEmoji is the emoji image a heading row draws instead of icon: only
// while icon is the section's own (collapsed and hover carets win).
func sbEmoji(r *sbRow, icon string) string {
	if r.kind != sbHeading || icon != r.icon {
		return ""
	}
	return r.emoji
}

// rebuild flattens the model into sidebar rows (only when the model changes).
func (s *sidebarState) rebuild(m *model) {
	if s.collapsed == nil {
		s.collapsed = map[string]bool{}
	}
	rows := s.rows[:0]
	s.viewsFor = sbViewsKey()
	for _, l := range sbLinks { // Home > "Always show in the sidebar"
		if !strings.Contains(","+s.viewsFor+",", ","+sbViewPref[l.key]+",") {
			rows = append(rows, l)
		}
	}
	rows = append(rows, sbRow{kind: sbDivider, gap: 24})
	for i, sec := range m.Sections {
		if i > 0 {
			rows = append(rows, sbRow{kind: sbGap, gap: 10})
		}
		hr := sbRow{kind: sbHeading, key: sec.ID, label: sec.Name, icon: sectionIcon(sec.Type), emoji: sec.EmojiURL, section: sec.ID}
		if sec.DefaultName {
			hr.tr = "channel_sidebar"
		}
		rows = append(rows, hr)
		col, seen := s.collapsed[sec.ID]
		if !seen { // first sight: Slack's saved state (channel_sections "c")
			col = sec.Collapsed
			s.collapsed[sec.ID] = col
		}
		ids := sec.Rows
		if col { // collapsed: Slack's unread rows (core's CollapsedChannels)
			ids = sec.CollapsedRows
		}
		for _, id := range ids {
			if m.conv(id) == nil {
				continue
			}
			rows = append(rows, sbRow{kind: sbConv, key: id, section: sec.ID})
		}
		if col {
			continue
		}
		switch sec.Type {
		case "stars":
			if len(sec.Rows) == 0 {
				rows = append(rows, sbRow{kind: sbNote, label: "Drag and drop important stuff here"})
			}
		case "channels":
			rows = append(rows, sbRow{kind: sbAction, key: "add-channels", label: "Add channels", icon: "channel-plus", tr: "channel_sidebar"})
		case "direct_messages":
			rows = append(rows, sbRow{kind: sbAction, key: "browse-people", label: "Browse all people", icon: "contact", tr: "channel_sidebar"})
		case "recent_apps", "apps":
			rows = append(rows, sbRow{kind: sbAction, key: "add-apps", label: "Add apps", icon: "plus-small", tr: "channel_sidebar"})
		}
	}
	rows = append(rows, sbRow{kind: sbGap, gap: 16})
	for i := range rows {
		r := &rows[i]
		switch r.kind {
		case sbHeading:
			r.itemKey = "sectionHeading-" + r.key
		case sbLink, sbConv:
			r.itemKey = r.key
		case sbAction:
			r.itemKey = "sectionFooter-" + r.section + "-" + r.key
		}
	}
	s.rows = rows
	s.gen++
	s.builtFor = m.version
	s.rowCount = len(rows)
}

func (a *App) model() *model {
	if a.data == nil {
		return &emptyModel
	}
	return &a.data.m
}

var emptyModel = model{TeamName: AppName, Convs: map[string]*convInfo{}}

// layoutSidebar draws the sidebar through Slack's ChannelSidebar.
func (a *App) layoutSidebar(gtx layout.Context) layout.Dimensions {
	return slackc.Component(gtx, "ChannelSidebar", a, (*App).sidebarProps, (*App).layoutSidebarNative)
}

// layoutSidebarNative draws the sidebar header, jump box and scrolling
// list. Under a patch it draws only the rows p's rows still list.
func (a *App) layoutSidebarNative(gtx layout.Context, p slackc.Props) layout.Dimensions {
	sz := gtx.Constraints.Max
	sbst := clsSidebar.Enter(gtx, hoverState(a.sidebar.hover), 0)
	defer sbst.Leave()
	if sbst.Hidden() {
		return layout.Dimensions{Size: sz}
	}
	c := a.sidebarPalette()
	m := a.model()
	if a.sidebar.rows == nil || a.sidebar.builtFor != m.version || a.sidebar.viewsFor != sbViewsKey() {
		a.sidebar.rebuild(m)
		for i := range a.sidebar.rows {
			r := &a.sidebar.rows[i]
			switch r.kind {
			case sbHeading:
				r.btn = a.clicks.get("sbh." + r.key)
			case sbLink, sbConv, sbAction:
				r.btn = a.clicks.get("sbr." + r.key)
			}
		}
	}

	// header: team name 18px/900 + chevron; settings + compose on the right.
	hh := gtx.Dp(49)
	nameW := a.th.Measure(gtx, 18, theme.Black, m.TeamName)
	if max := sz.X - gtx.Dp(110); nameW > max {
		nameW = max
	}
	at(gtx, image.Pt(gtx.Dp(8), gtx.Dp(9)), image.Pt(nameW+gtx.Dp(34), gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("sb.team"), func(gtx layout.Context, sz image.Point, hovered bool) {
			if hovered {
				theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(6), c.SidebarHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			h := gtx.Dp(32)
			at(gtx, image.Pt(gtx.Dp(8), 0), image.Pt(nameW, h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 18, theme.Black, c.SidebarTextUnread, m.TeamName)
				})
			})
			at(gtx, image.Pt(gtx.Dp(10)+nameW, (h-gtx.Dp(16))/2+gtx.Dp(1)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("chevron-down")(gtx, 16, c.SidebarTextUnread)
			})
			return layout.Dimensions{Size: image.Pt(nameW+gtx.Dp(34), h)}
		})
	})
	if clicked(a.clicks.get("sb.team")) {
		a.openTeamMenu(image.Pt(gtx.Dp(theme.RailW)-gtx.Dp(12), gtx.Dp(theme.TopNavH)+gtx.Dp(51))) // Slack: menu at (58, 91)
	}
	for i, b := range []string{"compose", "settings"} {
		x := sz.X - gtx.Dp(8) - gtx.Dp(28)*(i+1) - gtx.Dp(6)*i
		at(gtx, image.Pt(x, gtx.Dp(11)), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, a.clicks.get("sb."+b), b, 28, 20, c.SidebarTextUnread, c.SidebarHover, 6)
		})
	}
	if clicked(a.clicks.get("sb.compose")) {
		a.compose()
	}
	if clicked(a.clicks.get("sb.settings")) {
		a.openPrefs()
	}

	// "Find a conversation…" box.
	fy := hh
	fh := gtx.Dp(28)
	at(gtx, image.Pt(gtx.Dp(8), fy), image.Pt(sz.X-gtx.Dp(16), fh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("sb.find"), func(gtx layout.Context, s image.Point, hovered bool) {
			// Slack: rgba(249,237,255,.11) fill, 1px rgba(249,237,255,.22), radius 8.
			lilac := color.NRGBA{R: 0xf9, G: 0xed, B: 0xff} // Slack's tint in both modes
			bg := theme.Over(theme.WithAlpha(lilac, 0x1c), a.sbBase)
			if a.th.Dark {
				bg = a.sbBase // dark: transparent box, outline only
			}
			if hovered {
				bg = theme.Over(theme.WithAlpha(lilac, 0x2a), a.sbBase)
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), theme.Over(theme.WithAlpha(lilac, 0x38), a.sbBase))
		}, func(gtx layout.Context) layout.Dimensions {
			w := gtx.Constraints.Max.X
			at(gtx, image.Pt(gtx.Dp(8), (fh-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("find")(gtx, 14, c.SidebarTextUnread)
			})
			at(gtx, image.Pt(gtx.Dp(26), 0), image.Pt(w-gtx.Dp(30), fh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, fh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.SidebarTextUnread, i18n.T("channel_sidebar", "Find a conversation…"))
				})
			})
			return layout.Dimensions{Size: image.Pt(w, fh)}
		})
	})
	if clicked(a.clicks.get("sb.find")) {
		a.openSwitcher()
	}

	// scrolling list
	top := fy + fh + gtx.Dp(10)
	barH := a.huddles().BarHeight(gtx) // the huddle bar sits under the list
	if barH > 0 {
		at(gtx, image.Pt(0, sz.Y-barH), image.Pt(sz.X, barH), func(gtx layout.Context) layout.Dimensions {
			return a.huddles().LayoutBar(gtx, a.th)
		})
	}
	at(gtx, image.Pt(0, top), image.Pt(sz.X, sz.Y-top-barH), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = gtx.Constraints.Max
		ls := clsSbList.Enter(gtx, 0, 0)
		defer ls.Leave()
		a.sidebar.list.Axis = layout.Vertical
		if shown := a.sidebar.sidebarShown(p); shown != nil {
			return a.sidebar.list.List.Layout(gtx, len(shown), func(gtx layout.Context, i int) layout.Dimensions {
				return a.layoutSidebarRow(gtx, &a.sidebar.rows[shown[i]])
			})
		}
		return a.sidebar.list.List.Layout(gtx, len(a.sidebar.rows), func(gtx layout.Context, i int) layout.Dimensions {
			return a.layoutSidebarRow(gtx, &a.sidebar.rows[i])
		})
	})
	return layout.Dimensions{Size: sz}
}

// sbRowH is --sidebar_item_height (X4Y6 getSidebarItemHeight):
// space.175 (28px), space.275 (44px) in Simplified layout mode.
func (a *App) sbRowH(gtx layout.Context) int {
	if a.bbmOn() {
		return gtx.Dp(44)
	}
	return gtx.Dp(theme.SidebarRowH)
}

// sbBBMBox: in the simplified layout conversation and page rows are boxes
// (.p-channel_sidebar__channel--bbm, …__link--page--bbm) with a 12px
// spacer after each (xnuR pushBBMChannelSpacer, BBMPagesLinkSpacer).
func (a *App) sbBBMBox(r *sbRow) bool {
	return a.bbmOn() && (r.kind == sbConv || r.kind == sbLink)
}

func (a *App) layoutSidebarRow(gtx layout.Context, r *sbRow) layout.Dimensions {
	c := &a.sbPal
	w := gtx.Constraints.Max.X
	rowH := a.sbRowH(gtx)
	pad := gtx.Dp(8)
	switch r.kind {
	case sbGap:
		return layout.Dimensions{Size: image.Pt(w, gtx.Dp(unit.Dp(r.gap)))}
	case sbDivider:
		// Slack's pagesDivider under the links (divider__QhKlG): margin
		// 12px, border-bottom 1px rgba(var(--sk_foreground_low), .13), the
		// var being 232,232,232 under .sk-client-theme--dark and
		// --light-inverted-sidebar alike.
		h := gtx.Dp(unit.Dp(r.gap))
		m := gtx.Dp(12)
		theme.Fill(gtx, image.Rect(m, m, w-m, m+gtx.Dp(1)), theme.Over(color.NRGBA{R: 232, G: 232, B: 232, A: 0x21}, a.sbBase))
		return layout.Dimensions{Size: image.Pt(w, h)}
	case sbNote:
		// Slack: 13px note under the empty Starred heading; the row is
		// 44px with the text in its first 26px.
		lh := gtx.Dp(26)
		at(gtx, image.Pt(pad+gtx.Dp(24), 0), image.Pt(w-pad*2-gtx.Dp(24), lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Regular, c.SidebarText, r.label)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, gtx.Dp(44))}
	}

	route := a.router.Current()
	var (
		sel      bool
		label    = r.label
		icon     = r.icon
		weight   = theme.Regular
		col      = c.SidebarText
		mentions int
		indent   = 0
		conv     *convInfo
		suffix   string
	)
	if r.tr != "" {
		label = r.tr.T(label)
	}
	clickPrefix := "sbr."
	switch r.kind {
	case sbLink:
		sel = string(route) == r.key
		// the Unreads page's sidebar.hasUnreads (hasUnreadsPageUnreads:
		// totalUnsuppressedUnreadsInChannels > 0) makes the link bold
		if r.key == "unreads" && a.model().unsuppressedUnreads() {
			weight = theme.Black
			col = c.SidebarTextUnread
		}
	case sbHeading:
		clickPrefix = "sbh."
		if a.sidebar.collapsed[r.key] {
			icon = "caret-right"
		}
	case sbConv:
		conv = a.model().conv(r.key)
		if conv == nil {
			return layout.Dimensions{}
		}
		indent = gtx.Dp(16)
		label = conv.displayName()
		sel = route.Channel() == conv.ID
		if conv.Unread && !conv.Muted && !a.focusQuiet() { // focus mode: no bolding (focus_mode.go)
			weight = theme.Black
			col = c.SidebarTextUnread
		}
		if conv.Muted {
			col = theme.Over(theme.WithAlpha(a.th.C.SidebarText, 0x80), a.sbBase)
		}
		mentions = conv.Mentions
		if conv.Self {
			suffix = "you"
		}
	case sbAction:
		indent = gtx.Dp(16)
	}
	if a.sbBBMBox(r) {
		sel = false // "--selected" is only added outside the mode (Jbto: n&&!w)
	}
	btn := r.btn
	if btn == nil {
		btn = a.clicks.get(clickPrefix + r.key)
	}
	hov := hoverState(btn.Hovered())
	item := clsSbItem.EnterAttr(gtx, hov, 0, "data-item-key", r.itemKey)
	defer item.Leave()
	if item.Hidden() {
		return layout.Dimensions{}
	}
	var inner slackc.Styled
	switch {
	case conv != nil:
		var mods uint64
		if conv.Unread && !a.focusQuiet() {
			mods |= 1
		}
		if conv.Muted {
			mods |= 2
		}
		s := 0
		if sel {
			s = 1
		}
		inner = clsSbChannel[sbChanType(conv.Kind)][s].EnterAttr(gtx, hov, mods, "data-qa-channel-sidebar-channel-id", conv.ID)
	case r.kind == sbLink:
		inner = clsSbLink.Enter(gtx, hov, 0)
	case r.kind == sbHeading:
		inner = clsSbHeading.Enter(gtx, hov, 0)
	}
	defer inner.Leave()
	if inner.Hidden() {
		return layout.Dimensions{}
	}
	d := item.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		return at(gtx, image.Pt(pad, 0), image.Pt(w-pad*2, rowH), func(gtx layout.Context) layout.Dimensions {
			return inner.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
				rs := sbRowState{sel: sel, label: label, icon: icon, emoji: sbEmoji(r, icon), weight: weight, col: col, mentions: mentions, indent: indent, conv: conv, suffix: suffix, rowH: rowH}
				if conv != nil {
					// ConnectedChannel > Channel > ChannelContextMenu > trigger
					rs.fg = col
					if sel {
						rs.fg = c.SidebarActiveText
					}
					return slackc.Component(gtx, "ConnectedChannel", sbConvRow{a: a, r: r, btn: btn, rs: rs}, sbConvRow.connectedProps, sbConvRow.layoutConnected)
				}
				return a.sidebarRowBody(gtx, r, btn, rs, nil)
			})
		})
	})
	if conv != nil {
		a.sbRowInput(gtx, conv.ID, btn, image.Pt(w, d.Size.Y))
	}
	if r.kind == sbHeading {
		a.sbHeadingControls(gtx, r, btn.Hovered(), image.Pt(w, d.Size.Y))
	}
	if clicked(btn) {
		switch r.kind {
		case sbLink:
			a.Navigate(r.key)
		case sbHeading:
			a.sidebar.collapsed[r.key] = !a.sidebar.collapsed[r.key]
			a.sidebar.builtFor = -1
		case sbConv:
			a.Navigate("c/" + r.key)
		case sbAction:
			a.sidebarAction(r.key)
		}
	}
	if a.sbBBMBox(r) {
		return layout.Dimensions{Size: image.Pt(w, d.Size.Y+gtx.Dp(12))} // SpacerRow (height 12)
	}
	return layout.Dimensions{Size: image.Pt(w, d.Size.Y)}
}

// presenceDot draws Slack's 12px presence badge: filled green when active,
// hollow ring when away; ringed with the row background.
func (a *App) presenceDot(gtx layout.Context, center image.Point, active, sel bool) {
	c := &a.sbPal
	ring := a.sbBase
	if sel {
		ring = c.SidebarActive
	}
	theme.Circle(gtx, center, gtx.Dp(10), ring)
	if active {
		theme.Circle(gtx, center, gtx.Dp(7), c.Presence)
		return
	}
	fg := c.SidebarText
	if sel {
		fg = c.SidebarActiveText
	}
	theme.Circle(gtx, center, gtx.Dp(7), fg)
	theme.Circle(gtx, center, gtx.Dp(4), ring)
}

// layoutSidebarResize is the 6px drag handle on the sidebar's right edge.
func (a *App) layoutSidebarResize(gtx layout.Context, x, y, h int) {
	s := &a.sidebar
	base := float32(s.width) // all events this frame share last frame's origin
	for {
		e, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch e.Kind {
		case pointer.Press:
			// the handle moves with the edge, so positions are relative to
			// the current edge: keep the press offset and accumulate.
			s.dragX = e.Position.X
			s.dragW = float32(s.width)
		case pointer.Drag:
			s.dragW = base + (e.Position.X-s.dragX)/gtx.Metric.PxPerDp
			if s.dragW < theme.SidebarMinW {
				s.dragW = theme.SidebarMinW
			}
			if s.dragW > theme.SidebarMaxW {
				s.dragW = theme.SidebarMaxW
			}
			s.width = int(s.dragW + .5)
		case pointer.Release, pointer.Cancel:
			a.savePrefs()
		}
	}
	r := image.Rect(x-gtx.Dp(3), y, x+gtx.Dp(3), y+h)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	sz := r.Size()
	g := gtx
	g.Constraints = layout.Exact(sz)
	area := image.Rectangle{Max: sz}
	pointerArea(g, &s.drag, area, pointer.CursorColResize)
}

var _ color.NRGBA

// sbRowState is what layoutSidebarRow resolved for a row.
type sbRowState struct {
	sel      bool
	label    string
	icon     string
	emoji    string // image URL drawn in place of icon (section emoji)
	weight   font.Weight
	col      color.NRGBA
	mentions int
	indent   int
	conv     *convInfo
	suffix   string
	rowH     int         // px
	fg       color.NRGBA // conversation rows: the name colour
}

// sidebarRowBody draws a row's tile, icon, name and badge. Conversation
// rows draw their name through Slack's ListChannelEntity and their suffix
// through BaseChannelSuffix; p is their trigger's props under a patch.
func (a *App) sidebarRowBody(gtx layout.Context, r *sbRow, btn *widget.Clickable, rs sbRowState, p slackc.Props) layout.Dimensions {
	c := &a.sbPal
	rw := gtx.Constraints.Max.X
	rowH := a.sbRowH(gtx)
	pad := gtx.Dp(8)
	sel, conv := rs.sel, rs.conv
	box := a.sbBBMBox(r)
	return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
		switch {
		case box:
			// border 1px theme-otl-inv-pry, radius 4, the
			// theme-surf-ter-pressed fill (drawn as the sidebar's lilac at
			// .22, its dark-ramp value), theme-surf-ter on hover (drawn as
			// the sidebar hover over it)
			rr := image.Rectangle{Max: s}
			bg := theme.Over(theme.WithAlpha(a.th.C.ChromeHover, 0x38), a.sbBase)
			if hovered {
				bg = theme.Over(a.th.C.SidebarHover, bg)
			}
			theme.RRect(gtx, rr, gtx.Dp(4), bg)
			theme.Border(gtx, rr, gtx.Dp(4), max(1, gtx.Dp(1)), c.SidebarDivider)
		case sel:
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.SidebarActive)
		case hovered && r.kind != sbHeading:
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.SidebarHover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		fg := rs.col
		if sel {
			fg = c.SidebarActiveText
		}
		icon := rs.icon
		if r.kind == sbHeading && btn.Hovered() && !a.sidebar.collapsed[r.key] {
			icon = "caret-down"
		}
		ix := pad + rs.indent
		iy := (rowH - gtx.Dp(16)) / 2
		right := rw - gtx.Dp(6)
		// BaseChannelSuffix's draft mark (showDraft: a draft here, the
		// channel not open, and the viewer can post in it) goes just
		// before the badge
		draft := conv != nil && !sel && !conv.Archived && !conv.Preview && a.data != nil && a.data.draftChans[conv.ID] &&
			a.Core() != nil && a.Core().PrefBool("has_drafted_message", true)
		if rs.mentions > 0 {
			m := op.Record(gtx.Ops)
			bd := a.badge(gtx, rs.mentions, c.Badge, c.BadgeText)
			call := m.Stop()
			if a.focusQuiet() && !btn.Hovered() {
				// focus mode: c-badge--dot, the pill clipped to a 16px circle
				// at scale .4445 (the count shows again on hover)
				ctr := image.Pt(right-bd.Size.X/2, rowH/2)
				theme.Circle(gtx, ctr, gtx.Dp(16)*4445/10000, c.Badge)
			} else {
				t := op.Offset(image.Pt(right-bd.Size.X, (rowH-bd.Size.Y)/2)).Push(gtx.Ops)
				call.Add(gtx.Ops)
				t.Pop()
			}
			right -= bd.Size.X
			if !draft {
				right -= gtx.Dp(6)
			}
		}
		// Drafts & sent's badges (Slack's OutboxLinkContent): "✎ N" for
		// unsent drafts, capped at maxCount 99 ("99+"), then "schedule-send
		// N" for scheduled messages (uncapped). Each p-outbox_link__badge
		// is 13px text 8px after the previous, its 13px icon 2px before
		// the number; the list ends at the row's 8px padding.
		if d := a.data; d != nil {
			switch {
			case r.kind == sbLink && r.key == "drafts" && (d.draftN > 0 || d.schedN > 0):
				right = rw - gtx.Dp(8)
				badge := func(icon, num string) {
					nw := a.th.Measure(gtx, 13, theme.Regular, num)
					right -= nw
					at(gtx, image.Pt(right, 0), image.Pt(nw+2, rowH), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
							return a.th.Label(gtx, 13, theme.Regular, fg, num)
						})
					})
					right -= gtx.Dp(2) + gtx.Dp(13)
					at(gtx, image.Pt(right, (rowH-gtx.Dp(13))/2), image.Pt(gtx.Dp(13), gtx.Dp(13)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(icon)(gtx, 13, fg)
					})
					right -= gtx.Dp(8)
				}
				// laid out right to left
				if d.schedN > 0 {
					badge("schedule-send", strconv.Itoa(d.schedN))
				}
				if d.draftN > 0 {
					badge("edit", outboxCount(d.draftN))
				}
				// the p-outbox_link__badge_list span (its first badge's
				// 8px padding included) carries the tooltip
				lr := image.Rect(right, 0, rw-gtx.Dp(8), rowH)
				t := op.Offset(lr.Min).Push(gtx.Ops)
				pass := pointer.PassOp{}.Push(gtx.Ops)
				area := clip.Rect(image.Rectangle{Max: lr.Size()}).Push(gtx.Ops)
				a.sidebar.outboxHover.Add(gtx.Ops)
				area.Pop()
				pass.Pop()
				popups.Tooltip(gtx, a.th, &a.sidebar.outboxTip, a.sidebar.outboxHover.Update(gtx.Source), lr.Size(), popups.Right, outboxTip(d.draftN, d.schedN), "")
				t.Pop()
			case draft:
				// BaseChannelSuffixDraftIcon: the icon font's pencil
				// (c-icon--pencil, inline, size inherit) in a 20px
				// p-channel_sidebar__suffix_generic at 14px, the row's
				// colour (a muted channel's suffix at .64); the suffix ends
				// at the row's 8px padding (6px with a badge, which it
				// abuts)
				if rs.mentions == 0 {
					right = rw - gtx.Dp(8)
				}
				slot, gs := gtx.Dp(20), gtx.Dp(14)
				right -= slot
				pc := fg
				if conv.Muted {
					pc = theme.WithAlpha(pc, uint8(float32(pc.A)*0.64+0.5))
				}
				at(gtx, image.Pt(right+(slot-gs)/2, (rowH-gs)/2), image.Pt(gs, gs), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("pencil-glyph")(gtx, 14, pc)
				})
				right -= gtx.Dp(6)
			}
		}
		if conv != nil {
			// a huddle is on here: Slack's headphones + head count
			if n := a.huddles().LiveCount(conv.ID); n > 0 {
				num := strconv.Itoa(n)
				nw := a.th.Measure(gtx, 13, theme.Bold, num)
				right -= nw
				at(gtx, image.Pt(right, 0), image.Pt(nw, rowH), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, fg, num)
					})
				})
				right -= gtx.Dp(17)
				at(gtx, image.Pt(right, (rowH-gtx.Dp(15))/2), image.Pt(gtx.Dp(15), gtx.Dp(15)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("headphones")(gtx, 15, fg)
				})
				right -= gtx.Dp(6)
			}
			rs.fg = fg
			a.layoutConvBody(gtx, sbConvRow{a: a, r: r, btn: btn, rs: rs}, p, ix, right)
			return layout.Dimensions{Size: image.Pt(rw, rowH)}
		}
		switch {
		case rs.emoji != "" && icon == rs.icon:
			// a custom section's emoji (Slack: 16px, where the icon goes);
			// the hover caret still replaces it
			es := gtx.Dp(16)
			at(gtx, image.Pt(ix, iy), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
				if img, ok := a.images.Get(rs.emoji, es*2); ok {
					return theme.Image(gtx, img, image.Pt(es, es), 0)
				}
				return layout.Dimensions{Size: image.Pt(es, es)}
			})
		case icon != "":
			at(gtx, image.Pt(ix, iy), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 16, fg)
			})
		}
		tx := ix + gtx.Dp(24)
		tw := right - tx
		ns := clsSbName.Enter(gtx, 0, 0)
		defer ns.Leave()
		if ns.Hidden() {
			return layout.Dimensions{Size: image.Pt(rw, rowH)}
		}
		size := ns.FontSize(15)
		weight := cssWeight(ns, rs.weight)
		nfg := ns.Color(fg)
		nw := a.th.Measure(gtx, size, weight, rs.label)
		if nw > tw {
			nw = tw
		}
		at(gtx, image.Pt(tx, gtx.Dp(1)), image.Pt(nw, rowH) /* Slack: text 1px below center */, func(gtx layout.Context) layout.Dimensions {
			return ns.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, size, weight, nfg, rs.label)
				})
			})
		})
		return layout.Dimensions{Size: image.Pt(rw, rowH)}
	})
}
