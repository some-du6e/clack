package ui

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Agents & tools peek: boot-render Zxh6 AutomationsPeekView in a
// PeekView (boot-shared 9eDG), web client build f132f0e78468, CSS from
// client-boot-styles:
//
//   - The card: .p-peek_card--as-card (ctr-pry, radius 12, 65vh tall, at
//     most 100vh - 8px) with .p-peek_card--automations (fit-content,
//     360…400 wide); the header (.p-peek_card__header: 44px, padding 0 8px
//     0 16px, 15px black) says the Platform tab's name, "Agents & tools"
//     (q0zJ C) or "Tools".
//   - With agent_tasks (HLQ9 rh) the view is "tabbed": no header buttons,
//     and the sub-header is AutomationsPeekViewTabs, a c-tabs__tab_menu
//     (inset 0 -1px otl-ter; .automationsPeekViewTabMenu padding 0 4px)
//     with "Agent conversations" and "Workflows" (.c-tabs__tab 13px bold,
//     content-sec, content-pry when hovered or active, the active one
//     underlined 2px in theme-base-pry; .automationsPeekViewTab margin 0
//     8px, padding 2px 4px 8px, no right margin but on the last). Each open
//     starts on Agent conversations (the view's mount effect).
//   - Without it the view is "workflows": the header says the name and
//     "{count} recently used" (.p-peek_card__header_subtitle, 13px,
//     foreground-max-solid, 8px after), and the card lists the workflows.
//   - Agent conversations: AgentSessionsPeekList (XsVf) over
//     fetchAgentSessions (core.AgentSessions): five skeleton rows until
//     the first answer, "No agent conversations yet" (centred, content-
//     sec) for none, the sessions in order, or with pinned ones a "Pinned
//     agent conversations" section and a "Recent agent conversations" one
//     (U8yn PeekSection: padding 16px 16px 8px, 13px bold content-sec).
//     A row is PeekSessionEntityRow (le1+: ctr-pry, surf-pry on hover,
//     padding 12px 16px, gap 12px) around SessionEntity (bhdT: a 36px tile,
//     radius 8, base-ter, gap 16px; the title 15px content-pry, black when
//     unread; the subtext 13px content-sec: "Working…", "Needs your input"
//     (bold, content-pry) or "Updated {ago}"). The tile is the agent's
//     picture (AgentSessionIcon: its agent_statuses icon override, else the
//     bot user's avatar) while idle, a spinner while working, the
//     needs-attention glyph when it needs input.
//   - A row closes the peek (maybeClosePeek) and opens the thread
//     (openThread) or the channel (toChannel).
//   - Workflows: WorkflowsFlatList over fetchAndUpsertRecentWorkflows
//     (core.RecentlyRunTriggers): ten skeleton rows while the first fetch
//     runs, then AutomationsItem per trigger (.p-automations_item_container:
//     ctr-pry, ctr-pry-hover on hover, an otl-ter line under it, padding
//     8px 24px 8px 12px; the workflow's 36px icon 12px before the bold
//     name over the 13px content-ter description, one line each). A click
//     closes the peek and runs handleWorkflowItemClick: Clack has no
//     Workflow Builder, so it's toWorkflowDetails for everyone.
//
// Not ported: the rows' ⋮ menu (Copy link, Close, Pin, Mute, Rename, Open
// in split view / new window), the "agents" unified view (agent_peek_view
// with Agentforce agents), the workflows view's Create button and the
// peek's view container forcing the Platform tab for what it opens.

const (
	agTabSessions  = "agent_sessions"
	agTabWorkflows = "workflows"
)

// agentsPeek is the view's state (UI goroutine only).
type agentsPeek struct {
	a   *App
	tab string

	sessions              []core.AgentSession
	sessFetched, sessBusy bool
	trigs                 []core.WorkflowTrigger
	wfFetched, wfLoading  bool
	sessGen, wfGen        int
	tabBtns               [2]widget.Clickable
	rows                  map[string]*widget.Clickable
	list                  layout.List
	lastTab               string
	openedAt              time.Time
	sessionsErr, wfErr    error
}

func (a *App) agentsPeekView() *agentsPeek {
	if a.agPeek == nil {
		a.agPeek = &agentsPeek{a: a, rows: map[string]*widget.Clickable{}}
		a.agPeek.list.Axis = layout.Vertical
	}
	return a.agPeek
}

// tabbed is agent_tasks (HLQ9 rh): Agent conversations | Workflows.
func (v *agentsPeek) tabbed() bool {
	c := v.a.Core()
	return c != nil && c.Experiment("agent_tasks") == "on"
}

// opened is the view's mount: the tab resets to Agent conversations, and
// both lists fetch (fetchAndUpsertRecentWorkflows, fetchAgentSessions).
func (v *agentsPeek) opened() {
	v.openedAt = time.Now()
	if v.tabbed() {
		v.tab = agTabSessions
	} else {
		v.tab = agTabWorkflows
	}
	v.list.Position = layout.Position{}
	c := v.a.Core()
	if c == nil {
		return
	}
	if !v.wfFetched { // isFetched: later opens keep the last list while it refetches
		v.wfLoading = true
	}
	v.wfGen++
	gen := v.wfGen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ts, err := c.RecentlyRunTriggers(ctx)
		v.a.Post(func() {
			if gen != v.wfGen {
				return
			}
			v.wfLoading, v.wfErr = false, err
			if err == nil {
				v.trigs, v.wfFetched = ts, true
			}
			v.a.Invalidate()
		})
	}()
	if !v.tabbed() || v.sessBusy { // useAgentSessions fetches only with agent_tasks; one at a time
		return
	}
	v.sessBusy = true
	v.sessGen++
	sg := v.sessGen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ss, err := c.AgentSessions(ctx)
		v.a.Post(func() {
			if sg != v.sessGen {
				return
			}
			v.sessBusy, v.sessionsErr = false, err
			if err == nil {
				v.sessions, v.sessFetched = ss, true
			}
			v.a.Invalidate()
		})
	}()
}

// agPal is the card's colours: Slack's tokens (kit.css defaults, the
// sheets' overrides through popups.ColorsOf and SetTok).
type agPal struct {
	bg, hover, rowHover, tile, line, rule color.NRGBA
	fg, sec, ter, maxSolid, accent        color.NRGBA
}

func (a *App) agentsPal() agPal {
	th := a.th
	cl := popups.ColorsOf(th)
	p := agPal{bg: cl.Bg, hover: cl.BgHover, fg: cl.Fg, sec: cl.Fg2, ter: cl.Fg3, line: cl.Divider, accent: cl.Aubergine,
		tile: th.C.Skeleton} // base-ter
	if th.Dark {
		p.rowHover = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0x0f} // --dt_color-surf-pry
		p.rule = color.NRGBA{R: 0xd1, G: 0xd2, B: 0xd3, A: 0x21}     // rgba(var(--sk_foreground_low),.13)
		p.maxSolid = color.NRGBA{R: 0xab, G: 0xab, B: 0xad, A: 0xff}
	} else {
		p.rowHover = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x0f}
		p.rule = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x21}
		p.maxSolid = color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}
	}
	theme.SetTok(th.Tokens, &p.rowHover, theme.Both("var(--dt_color-surf-pry)"), th.Dark)
	theme.SetTok(th.Tokens, &p.rule, theme.Both("rgba(var(--sk_foreground_low),0.13)"), th.Dark)
	theme.SetTok(th.Tokens, &p.maxSolid, theme.Both("rgb(var(--sk_foreground_max_solid))"), th.Dark)
	return p
}

// title is the header's name: the Platform tab's label.
func (v *agentsPeek) title() string { return v.a.railTabFor(core.TabPlatform).label }

// cardSize is .p-peek_card--as-card's height (65vh, at most 100vh - 8px)
// and --automations' width (fit-content within 360…400).
func (v *agentsPeek) cardSize(gtx layout.Context) image.Point {
	win := v.a.winSize
	h := min(int(float32(win.Y)*.65), win.Y-gtx.Dp(8))
	w := max(gtx.Dp(360), min(v.naturalW(gtx), gtx.Dp(400)))
	return image.Pt(w, max(h, gtx.Dp(120)))
}

// naturalW is the widest row's max-content width (fit-content).
func (v *agentsPeek) naturalW(gtx layout.Context) int {
	th := v.a.th
	w := gtx.Dp(16) + th.Measure(gtx, 15, theme.Black, v.title()) + gtx.Dp(8)
	switch v.tab {
	case agTabSessions:
		for _, s := range v.sessions {
			title, sub, unread := v.sessionText(s)
			wt := theme.Regular
			if unread {
				wt = theme.Black
			}
			tw := max(th.Measure(gtx, 15, wt, title), th.Measure(gtx, 13, theme.Bold, sub))
			w = max(w, gtx.Dp(16+36+16)+tw+gtx.Dp(12+24+16))
		}
	case agTabWorkflows:
		for _, t := range v.trigs {
			tw := max(th.Measure(gtx, 15, theme.Bold, t.Title()), th.Measure(gtx, 13, theme.Regular, t.Desc()))
			w = max(w, gtx.Dp(12+36+12)+tw+gtx.Dp(24))
		}
	}
	return w
}

// layout draws the card.
func (v *agentsPeek) layout(gtx layout.Context) layout.Dimensions {
	a := v.a
	p := a.agentsPal()
	sz := v.cardSize(gtx)
	gtx.Constraints = layout.Exact(sz)
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	theme.Fill(gtx, image.Rectangle{Max: sz}, p.bg)
	tabbed := v.tabbed()
	// the header
	hh := gtx.Dp(44)
	at(gtx, image.Pt(gtx.Dp(16), 0), image.Pt(sz.X-gtx.Dp(24), hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			if tabbed || len(v.trigs) == 0 {
				return a.th.Label(gtx, 15, theme.Black, p.fg, v.title())
			}
			tw := a.th.Measure(gtx, 15, theme.Black, v.title())
			a.th.Label(gtx, 15, theme.Black, p.fg, v.title())
			at(gtx, image.Pt(tw+gtx.Dp(8), gtx.Dp(2)), image.Pt(sz.X, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Regular, p.maxSolid, itoa(len(v.trigs))+" recently used")
			})
			return layout.Dimensions{Size: image.Pt(tw, gtx.Dp(22))}
		})
	})
	y := hh
	if tabbed {
		y += v.layoutTabs(gtx, y, sz.X, p)
	} else { // .p-peek_card__header's rule
		theme.Fill(gtx, image.Rect(0, hh, sz.X, hh+max(1, gtx.Dp(1))), p.rule)
	}
	if v.tab != v.lastTab {
		v.lastTab = v.tab
		v.list.Position = layout.Position{}
	}
	body := image.Rect(0, y, sz.X, sz.Y)
	at(gtx, body.Min, body.Size(), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(body.Size())
		defer clip.Rect{Max: body.Size()}.Push(gtx.Ops).Pop()
		if v.tab == agTabSessions {
			return v.layoutSessions(gtx, p)
		}
		return v.layoutWorkflows(gtx, p)
	})
	return layout.Dimensions{Size: sz}
}

// layoutTabs draws AutomationsPeekViewTabs at y and returns its height.
func (v *agentsPeek) layoutTabs(gtx layout.Context, y, w int, p agPal) int {
	a := v.a
	th := gtx.Dp(26) // padding 2 + 13px × 1.25 + 8
	labels := [2]string{"Agent conversations", "Workflows"}
	ids := [2]string{agTabSessions, agTabWorkflows}
	x := gtx.Dp(4) + gtx.Dp(8)
	for i := range labels {
		b := &v.tabBtns[i]
		if b.Clicked(gtx) {
			v.tab = ids[i]
			a.Invalidate()
		}
		lw := a.th.Measure(gtx, 13, theme.Bold, labels[i])
		tw := lw + gtx.Dp(8)
		on := v.tab == ids[i]
		org := image.Pt(x, y)
		at(gtx, org, image.Pt(tw, th), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
				col := p.sec
				if on || b.Hovered() {
					col = p.fg
				}
				at(gtx, image.Pt(gtx.Dp(4), gtx.Dp(2)), image.Pt(lw+2, gtx.Dp(17)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Bold, col, labels[i])
				})
				if on { // inset 0 -2px 0 0 theme-base-pry
					theme.Fill(gtx, image.Rect(0, th-gtx.Dp(2), tw, th), p.accent)
				}
				return layout.Dimensions{Size: image.Pt(tw, th)}
			})
		})
		x += tw + gtx.Dp(8) // margin-right 0, the next one's margin-left 8
	}
	// .c-tabs__tab_menu: inset 0 -1px 0 0 otl-ter, under the tabs
	theme.Fill(gtx, image.Rect(0, y+th-max(1, gtx.Dp(1)), w, y+th), p.line)
	return th
}

// sessionText is a session row's title, subtext and unread state.
func (v *agentsPeek) sessionText(s core.AgentSession) (title, sub string, unread bool) {
	a := v.a
	m := a.model()
	plain := func(msg *core.Message) string {
		t := richtext.PlainText(msg.Doc)
		if strings.TrimSpace(t) == "" {
			t = msg.Text
		}
		return stripCommonMarkInline(oneLineText(t))
	}
	title = s.Title(plain, func(ch string) string {
		if c := m.conv(ch); c != nil {
			return c.displayName()
		}
		if c := a.Core(); c != nil {
			return c.S.ChannelName(ch)
		}
		return ""
	})
	state, _ := v.sessionState(s)
	switch state {
	case "working":
		sub = "Working…"
	case "needs_input":
		sub = "Needs your input"
	default:
		if t := tsTime(s.ActivityTS()); !t.IsZero() {
			sub = "Updated " + capFirst(i18n.TimeAgo(i18n.Locale(), t, time.Now(), false, false))
		}
	}
	if s.Kind == "channel" {
		c := m.conv(s.Channel)
		unread = c != nil && c.Unread
	} else if s.Root != nil && s.Root.LatestReply != "" { // S5Rr useIsAgentSessionThreadUnread
		lr := s.RootLastRead
		if lr == "" {
			if c := m.conv(s.Channel); c != nil {
				lr = c.LastRead
			}
		}
		if lr == "" {
			lr = "0"
		}
		unread = tsAfter(s.Root.LatestReply, lr)
	}
	return title, sub, unread
}

// sessionState is getSessionEntityState: "working", "needs_input" or
// "idle", and the idle glyph.
func (v *agentsPeek) sessionState(s core.AgentSession) (state, glyph string) {
	if s.Kind == "channel" { // useChannelSessionEntityStatus
		c := v.a.model().conv(s.Channel)
		if c == nil || c.Code == nil {
			return "idle", "code"
		}
		icon, _ := core.AgentStatusDisplay(c.CodeStatus, c.Code.PRStatus)
		switch {
		case c.CodeStatus == core.AgentProcessing:
			return "working", ""
		case icon == "needs-attention":
			return "needs_input", ""
		case icon != "":
			return "idle", icon
		}
		return "idle", "code"
	}
	// useThreadSessionEntityStatus: the root's agent_session
	if s.Root != nil && s.Root.AgentSession != nil {
		as := s.Root.AgentSession
		switch as.Status {
		case core.AgentProcessing:
			exp, _ := as.ProcessingExpire.Int64()
			if exp == 0 || time.Now().Unix() <= exp {
				return "working", ""
			}
		case core.AgentSuspended:
			return "needs_input", ""
		}
	}
	return "idle", "threads"
}

// sessionAvatar is AgentSessionIcon's picture ("" for none): the
// agent_statuses override (an emoji or an image), else the bot user's.
func (v *agentsPeek) sessionAvatar(s core.AgentSession) (url string) {
	a := v.a
	if s.IconEmoji != "" {
		if c := a.Core(); c != nil {
			if u, _, ok := c.Emoji(strings.Trim(s.IconEmoji, ":")); ok {
				return u
			}
		}
	}
	if s.IconURL != "" {
		return s.IconURL
	}
	return a.userAvatar(s.BotUserID)
}

func (v *agentsPeek) row(key string) *widget.Clickable {
	b := v.rows[key]
	if b == nil {
		b = new(widget.Clickable)
		v.rows[key] = b
	}
	return b
}

// layoutSessions is AgentSessionsPeekList.
func (v *agentsPeek) layoutSessions(gtx layout.Context, p agPal) layout.Dimensions {
	a := v.a
	sz := gtx.Constraints.Max
	if !v.sessFetched { // five skeleton rows (aria-busy)
		for i := 0; i < 5; i++ {
			v.skeletonRow(gtx, i*gtx.Dp(60), p, i)
		}
		return layout.Dimensions{Size: sz}
	}
	if len(v.sessions) == 0 {
		lw := a.th.Measure(gtx, 15, theme.Regular, "No agent conversations yet")
		at(gtx, image.Pt((sz.X-lw)/2, (sz.Y-gtx.Dp(22))/2), image.Pt(lw+2, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, p.sec, "No agent conversations yet")
		})
		return layout.Dimensions{Size: sz}
	}
	type item struct {
		header string
		s      *core.AgentSession
	}
	var items []item
	var pinned, recent []*core.AgentSession
	for i := range v.sessions {
		if v.sessions[i].Pinned {
			pinned = append(pinned, &v.sessions[i])
		} else {
			recent = append(recent, &v.sessions[i])
		}
	}
	if len(pinned) == 0 {
		for _, s := range recent {
			items = append(items, item{s: s})
		}
	} else {
		items = append(items, item{header: "Pinned agent conversations"})
		for _, s := range pinned {
			items = append(items, item{s: s})
		}
		if len(recent) > 0 {
			items = append(items, item{header: "Recent agent conversations"})
			for _, s := range recent {
				items = append(items, item{s: s})
			}
		}
	}
	return v.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
		it := items[i]
		if it.header != "" { // PeekSection: padding 16px 16px 8px, 13px bold content-sec
			h := gtx.Dp(16 + 19 + 8)
			at(gtx, image.Pt(gtx.Dp(16), gtx.Dp(16)), image.Pt(sz.X-gtx.Dp(32), gtx.Dp(19)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Bold, p.sec, it.header)
			})
			return layout.Dimensions{Size: image.Pt(sz.X, h)}
		}
		return v.sessionRow(gtx, *it.s, sz.X, p)
	})
}

// sessionRow is PeekSessionEntityRow + SessionEntity.
func (v *agentsPeek) sessionRow(gtx layout.Context, s core.AgentSession, w int, p agPal) layout.Dimensions {
	a := v.a
	key := s.Kind + "/" + s.Channel + "/" + s.TS
	b := v.row(key)
	if b.Clicked(gtx) {
		v.openSession(s)
	}
	title, sub, unread := v.sessionText(s)
	state, glyph := v.sessionState(s)
	pad, tile := gtx.Dp(16), gtx.Dp(36)
	textH := gtx.Dp(22)
	if sub != "" {
		textH += gtx.Dp(19)
	}
	h := gtx.Dp(12)*2 + max(tile, gtx.Dp(22+19))
	return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		if b.Hovered() {
			theme.Fill(gtx, image.Rect(0, 0, w, h), p.rowHover)
		}
		ty := (h - tile) / 2
		at(gtx, image.Pt(pad, ty), image.Pt(tile, tile), func(gtx layout.Context) layout.Dimensions {
			url := v.sessionAvatar(s)
			switch {
			case state == "idle" && url != "": // .tileAvatar: the picture
				return a.avatar(gtx, url, title, tile, gtx.Dp(8))
			case state == "working":
				theme.RRect(gtx, image.Rect(0, 0, tile, tile), gtx.Dp(8), p.tile)
				d := gtx.Dp(16)
				at(gtx, image.Pt((tile-d)/2, (tile-d)/2), image.Pt(d, d), func(gtx layout.Context) layout.Dimensions {
					codeSpinner(gtx, d, p.fg)
					return layout.Dimensions{Size: image.Pt(d, d)}
				})
			default:
				if state == "needs_input" {
					glyph = "needs-attention"
				}
				theme.RRect(gtx, image.Rect(0, 0, tile, tile), gtx.Dp(8), p.tile)
				d := gtx.Dp(20)
				at(gtx, image.Pt((tile-d)/2, (tile-d)/2), image.Pt(d, d), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(glyph)(gtx, 20, p.fg)
				})
			}
			return layout.Dimensions{Size: image.Pt(tile, tile)}
		})
		tx := pad + tile + gtx.Dp(16)
		tw := max(1, w-tx-pad-gtx.Dp(24+12)) // the ⋮ slot (24px, 12px gap) stays reserved
		y := (h - textH) / 2
		wt := theme.Regular
		if unread {
			wt = theme.Black
		}
		at(gtx, image.Pt(tx, y), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wt, Color: p.fg, MaxLines: 1}, title)
		})
		if sub != "" {
			sc, sw := p.sec, theme.Regular
			if state == "needs_input" { // .subtextNeedsInput
				sc, sw = p.fg, theme.Bold
			}
			at(gtx, image.Pt(tx, y+gtx.Dp(22)), image.Pt(tw, gtx.Dp(19)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 19, Weight: sw, Color: sc, MaxLines: 1}, sub)
			})
		}
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

// skeletonRow is PeekSessionEntityRowSkeleton i at y.
func (v *agentsPeek) skeletonRow(gtx layout.Context, y int, p agPal, i int) {
	pad, tile := gtx.Dp(16), gtx.Dp(36)
	h := gtx.Dp(60)
	theme.RRect(gtx, image.Rect(pad, y+(h-tile)/2, pad+tile, y+(h+tile)/2), gtx.Dp(8), p.tile)
	tx := pad + tile + gtx.Dp(12)
	widths := []unit.Dp{180, 140, 210, 160, 120}
	theme.RRect(gtx, image.Rect(tx, y+gtx.Dp(14), tx+gtx.Dp(widths[i%len(widths)]), y+gtx.Dp(28)), gtx.Dp(4), p.tile)
	theme.RRect(gtx, image.Rect(tx, y+gtx.Dp(36), tx+gtx.Dp(80), y+gtx.Dp(46)), gtx.Dp(4), p.tile)
}

// openSession is the row's click: close the peek, then openThread or
// toChannel.
func (v *agentsPeek) openSession(s core.AgentSession) {
	a := v.a
	a.closeTabPeek("platform")
	if s.Kind == "thread" {
		a.OpenThread(s.Channel, s.TS)
		return
	}
	a.Navigate("c/" + s.Channel)
}

// layoutWorkflows is WorkflowsFlatList.
func (v *agentsPeek) layoutWorkflows(gtx layout.Context, p agPal) layout.Dimensions {
	a := v.a
	sz := gtx.Constraints.Max
	rh := gtx.Dp(8+22+18+8) + max(1, gtx.Dp(1))
	if v.wfLoading && !v.wfFetched { // ten skeleton rows
		for i := 0; i < 10; i++ {
			y := i * rh
			theme.RRect(gtx, image.Rect(gtx.Dp(12), y+(rh-gtx.Dp(36))/2, gtx.Dp(48), y+(rh+gtx.Dp(36))/2), gtx.Dp(8), p.tile)
			theme.RRect(gtx, image.Rect(gtx.Dp(60), y+gtx.Dp(14), gtx.Dp(60+[]unit.Dp{150, 120, 180}[i%3]), y+gtx.Dp(28)), gtx.Dp(4), p.tile)
			theme.RRect(gtx, image.Rect(gtx.Dp(60), y+gtx.Dp(34), gtx.Dp(60+90), y+gtx.Dp(44)), gtx.Dp(4), p.tile)
		}
		return layout.Dimensions{Size: sz}
	}
	return v.list.Layout(gtx, len(v.trigs), func(gtx layout.Context, i int) layout.Dimensions {
		t := v.trigs[i]
		b := v.row("wf/" + t.ID)
		if b.Clicked(gtx) {
			a.closeTabPeek("platform")
			a.OpenWorkflowDetails(t.ID, "") // handleWorkflowItemClick → toWorkflowDetails
		}
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			w := sz.X
			if b.Hovered() {
				theme.Fill(gtx, image.Rect(0, 0, w, rh), p.hover)
			}
			theme.Fill(gtx, image.Rect(0, rh-max(1, gtx.Dp(1)), w, rh), p.line) // inset 0 -1px otl-ter
			is := gtx.Dp(36)
			at(gtx, image.Pt(gtx.Dp(12), (rh-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
				if img, ok := a.Images().Get(t.Icon, is); ok && t.Icon != "" {
					return theme.Image(gtx, img, image.Pt(is, is), gtx.Dp(12))
				}
				return a.th.Placeholder(gtx, image.Pt(is, is), gtx.Dp(12))
			})
			tx := gtx.Dp(12) + is + gtx.Dp(12)
			tw := max(1, w-tx-gtx.Dp(24))
			at(gtx, image.Pt(tx, gtx.Dp(8)), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: p.fg, MaxLines: 1}, t.Title())
			})
			at(gtx, image.Pt(tx, gtx.Dp(30)), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: theme.Regular, Color: p.ter, MaxLines: 1}, t.Desc())
			})
			return layout.Dimensions{Size: image.Pt(w, rh)}
		})
	})
}

// stripCommonMarkInline is RkDi's: a leading "> ", a heading's #s and a
// leading **bold** / __bold__ run lose their markup.
func stripCommonMarkInline(s string) string {
	if strings.HasPrefix(s, "> ") || strings.HasPrefix(s, ">\t") {
		s = s[2:]
	} else if strings.HasPrefix(s, "&gt; ") || strings.HasPrefix(s, "&gt;\t") {
		s = s[5:]
	}
	if i := strings.IndexFunc(s, func(r rune) bool { return r != '#' }); i >= 1 && i <= 6 && i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		if rest := strings.TrimLeft(s[i:], " \t"); rest != "" && !(i == 1 && isIssueNumber(rest)) {
			s = rest
		}
	}
	for _, d := range []string{"***", "**", "__"} {
		if !strings.HasPrefix(s, d) {
			continue
		}
		body := s[len(d):]
		j := strings.Index(body, d)
		if j <= 0 || strings.ContainsRune(body[:j], '\n') {
			break
		}
		inner := body[:j]
		r0, _ := utf8.DecodeRuneInString(inner)
		r1, _ := utf8.DecodeLastRuneInString(inner)
		if unicode.IsSpace(r0) || r0 == '/' || unicode.IsSpace(r1) || r1 == '/' {
			break
		}
		after := body[j+len(d):]
		if a, _ := utf8.DecodeRuneInString(after); after != "" && (a == '(' || a == '*' || a == '_' || unicode.IsLetter(a) || unicode.IsDigit(a)) {
			break
		}
		if d == "__" && isWord(inner) {
			break
		}
		s = inner + after
		break
	}
	return s
}

// isIssueNumber: "#" followed by a number alone on the line isn't a heading.
func isIssueNumber(s string) bool {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isWord(s string) bool {
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return s != ""
}

func oneLineText(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func capFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// tsTime is a Slack ts as a time (zero for none).
func tsTime(ts string) time.Time {
	sec := ts
	if i := strings.IndexByte(ts, '.'); i >= 0 {
		sec = ts[:i]
	}
	n := int64(0)
	for _, r := range sec {
		if r < '0' || r > '9' {
			return time.Time{}
		}
		n = n*10 + int64(r-'0')
	}
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// tsAfter reports a > b for Slack timestamps.
func tsAfter(a, b string) bool {
	pa, pb := strings.SplitN(a, ".", 2), strings.SplitN(b, ".", 2)
	if len(pa[0]) != len(pb[0]) {
		return len(pa[0]) > len(pb[0])
	}
	if pa[0] != pb[0] {
		return pa[0] > pb[0]
	}
	fa, fb := "", ""
	if len(pa) > 1 {
		fa = pa[1]
	}
	if len(pb) > 1 {
		fb = pb[1]
	}
	for len(fa) < len(fb) {
		fa += "0"
	}
	for len(fb) < len(fa) {
		fb += "0"
	}
	return fa > fb
}
