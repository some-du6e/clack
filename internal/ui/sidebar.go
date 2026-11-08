package ui

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	section string
	gap     int               // dp, for sbGap
	itemKey string            // Slack's data-item-key ("" = untagged spacer)
	btn     *widget.Clickable // resolved once per rebuild (no per-frame key strings)
}

type sidebarState struct {
	width     int // dp
	hidden    bool
	list      widget.List
	rows      []sbRow
	builtFor  int // model version
	collapsed map[string]bool
	rowCount  int
	drag      gesture.Drag
	dragX     float32
	dragW     float32
	hover     bool // pointer over the sidebar (.p-channel_sidebar:hover)

	// ChannelSidebar (slick_shell.go)
	gen       int   // bumped by every rebuild
	propRows  []any // rows as Slack-shaped props, for gen propsGen
	propsGen  int
	shown     []int // rows a plugin left (nil: all), from shownFrom
	shownFrom []any
	shownGen  int
}

var sbLinks = []sbRow{
	{kind: sbLink, key: "threads", label: "Threads", icon: "threads"},
	{kind: sbLink, key: "huddles", label: "Huddles", icon: "huddle"},
	{kind: sbLink, key: "drafts", label: "Drafts & sent", icon: "drafts"},
	{kind: sbLink, key: "saved", label: "Saved", icon: "saved"},
	{kind: sbLink, key: "directories", label: "Directories", icon: "directories"},
}

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
	}
	return "list-view"
}

// rebuild flattens the model into sidebar rows (only when the model changes).
func (s *sidebarState) rebuild(m *model) {
	if s.collapsed == nil {
		s.collapsed = map[string]bool{}
	}
	rows := append(s.rows[:0], sbLinks...)
	rows = append(rows, sbRow{kind: sbDivider, gap: 24})
	for i, sec := range m.Sections {
		if i > 0 {
			rows = append(rows, sbRow{kind: sbGap, gap: 10})
		}
		rows = append(rows, sbRow{kind: sbHeading, key: sec.ID, label: sec.Name, icon: sectionIcon(sec.Type), section: sec.ID})
		col := s.collapsed[sec.ID]
		for _, id := range sec.Rows {
			c := m.conv(id)
			if c == nil {
				continue
			}
			// collapsed sections still show unread and the selected conv
			if col && !c.Unread && c.Mentions == 0 {
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
			rows = append(rows, sbRow{kind: sbAction, key: "add-channels", label: "Add channels", icon: "channel-plus"})
		case "direct_messages":
			rows = append(rows, sbRow{kind: sbAction, key: "browse-people", label: "Browse all people", icon: "contact"})
		case "recent_apps", "apps":
			rows = append(rows, sbRow{kind: sbAction, key: "add-apps", label: "Add apps", icon: "plus-small"})
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
	if a.sidebar.rows == nil || a.sidebar.builtFor != m.version {
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
					return a.th.Label(gtx, 13, theme.Regular, c.SidebarTextUnread, "Find a conversation…")
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

func (a *App) layoutSidebarRow(gtx layout.Context, r *sbRow) layout.Dimensions {
	c := &a.sbPal
	w := gtx.Constraints.Max.X
	rowH := gtx.Dp(theme.SidebarRowH)
	pad := gtx.Dp(8)
	switch r.kind {
	case sbGap:
		return layout.Dimensions{Size: image.Pt(w, gtx.Dp(unit.Dp(r.gap)))}
	case sbDivider:
		h := gtx.Dp(unit.Dp(r.gap))
		theme.Fill(gtx, image.Rect(pad, h/2, w-pad, h/2+gtx.Dp(1)), c.SidebarDivider)
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
	clickPrefix := "sbr."
	switch r.kind {
	case sbLink:
		sel = string(route) == r.key
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
		if conv.Unread && !conv.Muted {
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
		if conv.Unread {
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
				rs := sbRowState{sel: sel, label: label, icon: icon, weight: weight, col: col, mentions: mentions, indent: indent, conv: conv, suffix: suffix, rowH: rowH}
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
	rowH := gtx.Dp(theme.SidebarRowH)
	pad := gtx.Dp(8)
	sel, conv := rs.sel, rs.conv
	return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
		switch {
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
		if rs.mentions > 0 {
			m := op.Record(gtx.Ops)
			bd := a.badge(gtx, rs.mentions, c.Badge, c.BadgeText)
			call := m.Stop()
			t := op.Offset(image.Pt(right-bd.Size.X, (rowH-bd.Size.Y)/2)).Push(gtx.Ops)
			call.Add(gtx.Ops)
			t.Pop()
			right -= bd.Size.X + gtx.Dp(6)
		}
		// unsent drafts: "✎ N" on Drafts & sent (Slack's p-outbox_link__badge,
		// 13px pencil + 400 13px count, 8px from the edge) and a 13px pencil
		// on conversations with a draft.
		if d := a.data; d != nil {
			pencil := func(x int) {
				at(gtx, image.Pt(x, (rowH-gtx.Dp(13))/2), image.Pt(gtx.Dp(13), gtx.Dp(13)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("edit")(gtx, 13, fg)
				})
			}
			switch {
			case r.kind == sbLink && r.key == "drafts" && d.draftN > 0:
				num := strconv.Itoa(d.draftN)
				nw := a.th.Measure(gtx, 13, theme.Regular, num)
				right = rw - gtx.Dp(8) - nw
				at(gtx, image.Pt(right, 0), image.Pt(nw+2, rowH), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Regular, fg, num)
					})
				})
				right -= gtx.Dp(3) + gtx.Dp(13)
				pencil(right)
				right -= gtx.Dp(6)
			case conv != nil && rs.mentions == 0 && d.draftChans[conv.ID]:
				right = rw - gtx.Dp(8) - gtx.Dp(13)
				pencil(right)
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
		if icon != "" {
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
