package ui

// Slack component boundaries Slick plugins patch in the shell
// (docs/PLUGINS-components.md §9, owner shell):
//
//   - ChannelSidebar (BetterBlock): the whole sidebar. Its rows prop lists
//     every sidebar row ({type, id, key}); the native draws only the rows a
//     plugin leaves in it.
//   - The sidebar conversation row, outermost first: ConnectedChannel
//     (ChatTabs) > Channel (ChannelListTyping's {displayName: 'Channel',
//     filter: isSidebarChannel}) > ChannelContextMenu (ChatTabs) >
//     ContextMenuTriggerWithKeyboardFocus (ChatTabs' "Open in new Slick
//     tab"), whose children are the row's ListChannelEntity and
//     BaseChannelSuffix (ChannelListTyping's typing indicator). Under a
//     patch each level hands the next to JS as a child slot, so a context a
//     plugin provides at one level reaches the levels inside it.
//   - ClientPrimaryView > RenderedViewContents (ChatTabs: its layout
//     around the primary view, its tab bar first inside
//     .p-view_contents--primary), whose child is the view's body slot.
//   - ProgressiveDisclosureSuccessCoachmarkWrapper (ShowSendingBot): Clack
//     has no coachmarks, so it is an empty point.
//
// Unpatched, every boundary is an atomic load and a direct call, and the
// sidebar allocates nothing per frame.

import (
	"encoding/json"
	"image"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ---- ChannelSidebar (BetterBlock) ----

// sidebarProps: Slack's ChannelSidebar with its rows. The rows list is
// rebuilt only when the sidebar's rows are.
func (a *App) sidebarProps() slackc.Props {
	s := &a.sidebar
	m := a.model()
	if a.sbStale(m) {
		s.rebuild(m)
	}
	if s.propRows == nil || s.propsGen != s.gen {
		s.propRows = sbPropRows(s.rows)
		s.propsGen = s.gen
	}
	return slackc.Props{"$key": "sidebar", "rows": s.propRows, "windowId": "main"}
}

// sbPropRows is the sidebar as Slack-shaped rows: {type, id, key}, with
// type "channel" for conversations (BetterBlock drops blocked DMs by it).
func sbPropRows(rows []sbRow) []any {
	out := make([]any, len(rows))
	for i := range rows {
		r := &rows[i]
		typ, id := "", r.key
		switch r.kind {
		case sbLink:
			typ = "link"
		case sbHeading:
			typ = "section_heading"
		case sbConv:
			typ = "channel"
		case sbAction:
			typ = "section_footer"
		case sbGap:
			typ, id = "spacer", ""
		case sbDivider:
			typ, id = "divider", ""
		case sbNote:
			typ, id = "emptyStarredSectionBanner", sbStarredBannerKey // EmptyStarredSectionRow's type and id
		case sbNoResults:
			typ, id = "textFilterNoResults", "" // TextFilterNoResultsRow's type (id null)
		}
		p := slackc.Props{"type": typ, "id": id, "key": sbRowKey(r, i)}
		if r.section != "" {
			p["channelSectionId"] = r.section
		}
		out[i] = p
	}
	return out
}

// sbRowKey is a row's unique key in the rows prop.
func sbRowKey(r *sbRow, i int) string {
	if r.itemKey != "" {
		return r.itemKey
	}
	return "row-" + itoa(i)
}

// sidebarShown returns the indexes of the rows to draw (nil: all of them).
// Under a patch it keeps the rows whose key is still in p's rows; the
// result is cached per rows list, so an unchanged tree costs nothing.
func (s *sidebarState) sidebarShown(p slackc.Props) []int {
	if p == nil || !p.Has("rows") {
		s.shown, s.shownFrom, s.shownGen = nil, nil, -1
		return nil
	}
	list := p.List("rows")
	if s.shownGen == s.gen && sameList(list, s.shownFrom) {
		return s.shown
	}
	keep := make(map[string]bool, len(list))
	for _, v := range list {
		if r := asProps(v); r != nil {
			keep[r.StrOr("key", "")] = true
		}
	}
	shown := make([]int, 0, len(s.rows))
	for i := range s.rows {
		if keep[sbRowKey(&s.rows[i], i)] {
			shown = append(shown, i)
		}
	}
	s.shown, s.shownFrom, s.shownGen = shown, list, s.gen
	return shown
}

// sameList reports whether a and b are the same slice (not just equal).
func sameList(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	return len(a) == 0 || &a[0] == &b[0]
}

func asProps(v any) slackc.Props {
	switch m := v.(type) {
	case slackc.Props:
		return m
	case map[string]any:
		return m
	}
	return nil
}

// ---- the sidebar conversation row ----

// sbConvRow is one conversation row; its methods are the row's boundaries.
// Everything it points to is on the heap already (rows, clickables,
// convs), so passing it by value allocates nothing.
type sbConvRow struct {
	a   *App
	r   *sbRow
	btn *widget.Clickable
	rs  sbRowState
}

// sbNext lays out the boundary inside a row level: when p came from a
// patch, the children the plugin left (where JS rendered the next level
// as a slot); otherwise the next boundary itself.
func sbNext(gtx layout.Context, p slackc.Props, name string, r sbConvRow, props func(sbConvRow) slackc.Props, native func(sbConvRow, layout.Context, slackc.Props) layout.Dimensions) layout.Dimensions {
	if p != nil {
		switch c := p["children"].(type) {
		case slackc.Element:
			return slackc.LayoutElement(gtx, c)
		case nil:
			if p.Has("children") {
				return layout.Dimensions{} // the plugin dropped them
			}
		}
	}
	return slackc.Component(gtx, name, r, props, native)
}

func (r sbConvRow) id() string { return r.rs.conv.ID }

// ConnectedChannel: Slack's connected sidebar row (props-live: id,
// rowKey, channelSection, rowPosition, sectionChannelIndex…).
func (r sbConvRow) connectedProps() slackc.Props {
	c := r.rs.conv
	sec := slackc.Props{"id": r.r.section, "type": "", "name": ""}
	for _, s := range r.a.model().Sections {
		if s.ID == r.r.section {
			sec["type"], sec["name"], sec["emoji"] = s.Type, s.Name, s.Emoji
			break
		}
	}
	return slackc.Props{
		"$key":                           "cc:" + c.ID,
		"id":                             c.ID,
		"rowKey":                         r.r.itemKey,
		"channelSection":                 sec,
		"finalDisplayedChannelSectionId": r.r.section,
		"priorDisplayedChannelSectionId": r.r.section,
		"children":                       slackc.Child("Channel", r.channelProps(), r.layoutChannel),
	}
}

func (r sbConvRow) layoutConnected(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return sbNext(gtx, p, "Channel", r, sbConvRow.channelProps, sbConvRow.layoutChannel)
}

// Channel: the sidebar row itself (Slack's component whose source calls
// getSidebarChannelUiInfo). ChannelListTyping reads isMuted, isSelected
// and isTyping (Slack animates typing itself only in 1:1 DMs; Clack
// doesn't, so isTyping is false).
func (r sbConvRow) channelProps() slackc.Props {
	c := r.rs.conv
	k := c.Kind
	return slackc.Props{
		"$key":       "ch:" + c.ID,
		"id":         c.ID,
		"channelId":  c.ID,
		"isMuted":    c.Muted,
		"isSelected": r.rs.sel,
		"isTyping":   false,
		"isUnread":   c.Unread,
		"isIm":       k == "im" || k == "app",
		"isMpim":     k == "mpim",
		"isPrivate":  k == "private",
		"isSidebar":  true,
		"children":   slackc.Child("ChannelContextMenu", r.ctxMenuProps(), r.layoutCtxMenu),
	}
}

func (r sbConvRow) layoutChannel(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return sbNext(gtx, p, "ChannelContextMenu", r, sbConvRow.ctxMenuProps, sbConvRow.layoutCtxMenu)
}

// ChannelContextMenu: the row's channel menu (props-live: id, header,
// actionsToMoveToMenu, children).
func (r sbConvRow) ctxMenuProps() slackc.Props {
	c := r.rs.conv
	return slackc.Props{
		"$key":                "cm:" + c.ID,
		"id":                  c.ID,
		"header":              false,
		"actionsToMoveToMenu": []any{},
		"children":            slackc.Child("ContextMenuTriggerWithKeyboardFocus", r.triggerProps(), r.layoutTrigger),
	}
}

func (r sbConvRow) layoutCtxMenu(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return sbNext(gtx, p, "ContextMenuTriggerWithKeyboardFocus", r, sbConvRow.triggerProps, sbConvRow.layoutTrigger)
}

// ContextMenuTriggerWithKeyboardFocus: the row's right-click trigger.
// getTemplate returns the row menu's MenuTemplateItem[] synchronously
// (a taut Go function, so a plugin wrapping it gets the template back);
// its children are the row's name and suffix.
func (r sbConvRow) triggerProps() slackc.Props {
	c := r.rs.conv
	p := slackc.Props{
		"$key":                             "ct:" + c.ID,
		"desktopAppOnly":                   false,
		"showSlackKitContextMenuOnDesktop": true,
		"width":                            "",
		"menuAriaLabel":                    "Channel options",
		"isDisabled":                       false,
		"children": []any{
			slackc.Child("ListChannelEntity", r.entity(0).props(), r.layoutEntitySlot),
			slackc.Child("BaseChannelSuffix", r.suffix().props(), r.suffix().layout),
		},
	}
	if h := r.a.sbMenu.handle(r.a, c); h != nil {
		p["getTemplate"] = h
	}
	return p
}

// layoutTrigger draws the row. The getTemplate a plugin passed (ChatTabs)
// is kept for the row's right-click, which sbRowInput reads outside the
// boundaries.
func (r sbConvRow) layoutTrigger(gtx layout.Context, p slackc.Props) layout.Dimensions {
	var gt slackc.JSFunc
	if p != nil {
		gt, _ = p["getTemplate"].(slackc.JSFunc)
	}
	r.a.sbMenu.setTemplateFn(r.id(), gt)
	return r.a.sidebarRowBody(gtx, r.r, r.btn, r.rs, p)
}

// sbRowNested reports whether a conversation row's body is drawn inside
// another boundary's plugin tree. slackc draws natives nested that deep
// from its measuring pass, which has no input, so the row's clicks are
// taken at the row instead.
func sbRowNested() bool {
	return slackc.Patched("ConnectedChannel") || slackc.Patched("Channel") || slackc.Patched("ChannelContextMenu")
}

// sbRowInput handles a conversation row's input over its whole size, at
// the row (outside its boundaries): the right-click menu, and its clicks
// while the body is nested in a plugin tree.
func (a *App) sbRowInput(gtx layout.Context, id string, btn *widget.Clickable, sz image.Point) {
	tag := a.sbMenu.tag(id)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			a.openSidebarMenu(id, a.ptrWin, a.sbMenu.gts[id])
		}
	}
	if sbRowNested() {
		if ok, mods := ctxclick.ClickedMods(gtx, btn); ok {
			clickedSet[btn] = true
			lastClickMods[btn] = mods
		}
		g := gtx
		g.Constraints = layout.Exact(sz)
		btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// entity is the row's ListChannelEntity, width px wide.
func (r sbConvRow) entity(width int) sbEntity {
	return sbEntity{a: r.a, conv: r.rs.conv, label: r.rs.label, suffix: r.rs.suffix, fg: r.rs.fg, weight: r.rs.weight,
		sel: r.rs.sel, rowH: r.rs.rowH, width: width}
}

// layoutEntitySlot is ListChannelEntity as a slot: as wide as it's given.
func (r sbConvRow) layoutEntitySlot(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return r.entity(gtx.Constraints.Max.X).layout(gtx, p)
}

func (r sbConvRow) suffix() sbSuffix {
	return sbSuffix{conv: r.rs.conv, sel: r.rs.sel, hov: r.btn.Hovered()}
}

// ---- BaseChannelSuffix (ChannelListTyping) ----

// sbSuffix is the row's suffix. Clack draws no suffix icons (drafts,
// huddles) and draws the mention badge itself at the row's right edge, so
// the native is empty; what plugins add before <Original/> (the typing
// indicator) is drawn right-aligned, just before the badge.
type sbSuffix struct {
	conv *convInfo
	sel  bool
	hov  bool
}

// props: Slack's BaseChannelSuffix (props-live).
func (s sbSuffix) props() slackc.Props {
	c := s.conv
	return slackc.Props{
		"$key":                                "sx:" + c.ID,
		"channelId":                           c.ID,
		"className":                           "p-channel_sidebar__channel_suffix",
		"data-qa":                             "sidebar-channel-suffix",
		"badgeCount":                          c.Mentions,
		"dualMentionCount":                    0,
		"displayAsHovered":                    s.hov,
		"isIm":                                c.Kind == "im" || c.Kind == "app",
		"isRowSelected":                       s.sel,
		"hasDraftText":                        false,
		"hasFailedMessage":                    false,
		"hasUnreadThreads":                    false,
		"isHuddlesAllowed":                    false,
		"numDraftFiles":                       0,
		"showDraft":                           false,
		"showPending":                         false,
		"threadUnreadsVisibility":             "hidden",
		"experimentHuddlesStatusUltraGroupOn": false,
		"experimentMeetingsSidebarGroupOn":    false,
	}
}

func (s sbSuffix) layout(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}

// sbKids splits a patched trigger's children into the name part (up to
// the ListChannelEntity slot) and the suffix part (the rest: what plugins
// put after the name, and the BaseChannelSuffix slot). ok is false when p
// carries no rendered children (unpatched, or the first frame).
func sbKids(p slackc.Props) (ent, suf slackc.Element, ok bool) {
	if p == nil {
		return ent, suf, false
	}
	e, isEl := p["children"].(slackc.Element)
	if !isEl {
		return ent, suf, false
	}
	ns := e.Nodes
	cut := -1
	for i, n := range ns {
		if sbHolds(n, "ListChannelEntity") {
			cut = i + 1
			break
		}
	}
	if cut < 0 {
		for i, n := range ns {
			if sbHolds(n, "BaseChannelSuffix") {
				cut = i
				break
			}
		}
	}
	if cut < 0 {
		cut = len(ns)
	}
	return slackc.Element{Nodes: ns[:cut]}, slackc.Element{Nodes: ns[cut:]}, true
}

// sbHolds reports whether n is, or wraps, the native slot named name.
func sbHolds(n *slackc.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.IsComponent() {
		if s, ok := slackc.SlotOf(n); ok {
			return s.Name == name
		}
	}
	for _, k := range n.C {
		if sbHolds(k, name) {
			return true
		}
	}
	return false
}

// sbSuffixTap sees where each conversation row put its suffix (tests):
// row-local px. Nil outside tests.
var sbSuffixTap func(ch string, g sbSuffixGeom)

// sbSuffixGeom is a row's suffix placement: the suffix spans [X0, X1),
// the name ends (unclipped) at NameEnd, Right is where the suffix area
// ends (left of the mention badge) and RowW is the row's width.
type sbSuffixGeom struct{ X0, X1, NameEnd, Right, RowW int }

// layoutConvBody draws a conversation row's name (ListChannelEntity) from
// ix, and its suffix (BaseChannelSuffix) right-aligned at right, as in
// Slack's row: what the suffix holds (ChannelListTyping's indicator, a
// flex item with margin-left: auto) sits at the row's right edge, just
// before the mention badge (right is already left of it), and the name
// ellipsizes before it. p is the trigger's props under a patch.
func (a *App) layoutConvBody(gtx layout.Context, r sbConvRow, p slackc.Props, ix, right int) {
	rowH := r.rs.rowH
	ent, suf, kids := sbKids(p)
	if !kids && !slackc.Patched("BaseChannelSuffix") {
		// nothing after the name (the fast path)
		e := r.entity(max(right-ix, 0))
		at(gtx, image.Pt(ix, 0), image.Pt(e.width, rowH), func(gtx layout.Context) layout.Dimensions {
			return slackc.Component(gtx, "ListChannelEntity", e, sbEntity.props, sbEntity.layout)
		})
		return
	}
	// The suffix is as wide as its content: laid out unbounded, slackc
	// shrinks even a block-level root (display: flex) to fit. The name
	// keeps at least its icon.
	minX := ix + gtx.Dp(24)
	sg := gtx
	sg.Constraints = layout.Constraints{Max: image.Pt(sbUnbounded, rowH)}
	call, sd := measured(sg, func(gtx layout.Context) layout.Dimensions {
		if kids {
			return slackc.LayoutElement(gtx, suf)
		}
		s := r.suffix()
		return slackc.Component(gtx, "BaseChannelSuffix", s, sbSuffix.props, sbSuffix.layout)
	})
	sw := min(sd.Size.X, max(right-minX, 0))
	sx := right - sw
	ew := right - ix
	if sw > 0 {
		ew = sx - ix
	}
	e := r.entity(max(ew, 0))
	at(gtx, image.Pt(ix, 0), image.Pt(e.width, rowH), func(gtx layout.Context) layout.Dimensions {
		if kids {
			return slackc.LayoutElement(gtx, ent)
		}
		return slackc.Component(gtx, "ListChannelEntity", e, sbEntity.props, sbEntity.layout)
	})
	if sw > 0 {
		// wider than the row allows: its start shows, clipped at right
		cl := clip.Rect{Min: image.Pt(sx, 0), Max: image.Pt(right, rowH)}.Push(gtx.Ops)
		place(gtx, call, image.Pt(sx, (rowH-sd.Size.Y)/2))
		cl.Pop()
	}
	if f := sbSuffixTap; f != nil {
		nameEnd := minX + a.th.Measure(gtx, 15, r.rs.weight, r.rs.label)
		if r.rs.suffix != "" {
			nameEnd += gtx.Dp(6) + a.th.Measure(gtx, 15, theme.Regular, r.rs.suffix)
		}
		f(r.id(), sbSuffixGeom{X0: sx, X1: sx + sw, NameEnd: nameEnd, Right: right, RowW: gtx.Constraints.Max.X})
	}
}

// sbUnbounded is a width slackc treats as unbounded (shrink-to-fit).
const sbUnbounded = 1 << 24

// ---- the row menu (ChannelContextMenu / ContextMenuTriggerWithKeyboardFocus) ----

// sbMenus holds each row's right-click tag and, while a plugin runtime
// runs, its getTemplate function.
type sbMenus struct {
	tags map[string]*int          // UI goroutine
	gts  map[string]slackc.JSFunc // a plugin's getTemplate per row (UI goroutine)

	mu    sync.Mutex
	t     *taut.Taut
	fns   map[string]*taut.GoHandle
	info  map[string]popups.ChannelMenuInfo // a copy: the template is read from JS's goroutine
	split map[string]bool                   // the row's menu has "Open in split view" (split.go)
}

func (m *sbMenus) tag(id string) *int {
	t := m.tags[id]
	if t == nil {
		if m.tags == nil {
			m.tags = map[string]*int{}
		}
		t = new(int)
		m.tags[id] = t
	}
	return t
}

// setTemplateFn records the getTemplate a plugin passed to row id's
// trigger (zero: none). Writes only on change.
func (m *sbMenus) setTemplateFn(id string, f slackc.JSFunc) {
	if m.gts[id] == f {
		return
	}
	if m.gts == nil {
		m.gts = map[string]slackc.JSFunc{}
	}
	m.gts[id] = f
}

// sbMenuInfo is what row c's menu depends on. move adds the Move to…
// rows (the header's section picker, moveToItems) the section submenus
// show; the template a plugin reads without them gets the rows alone.
func (a *App) sbMenuInfo(c *convInfo, move bool) popups.ChannelMenuInfo {
	i := popups.ChannelMenuInfo{ID: c.ID, Kind: c.Kind, Muted: c.Muted, Starred: c.Starred,
		// getChannelSectionsTemplate's guard: a member, not archived
		NoSections:    c.Archived || c.Preview,
		MoveToSection: a.exp("channel_ia_header_refinements") == "on"}
	secs := a.model().Sections
	for k := range secs {
		i.Custom = i.Custom || custom(&secs[k])
	}
	ids := []string{c.ID}
	if a.sidebar.edit.on { // getChannels: organize mode's picks
		ids = a.sbPicked()
		i.Organize, i.MoveCount, i.MoveDM = true, len(ids), a.anyDM(ids)
	}
	if move && !i.NoSections && (i.Custom || i.MoveToSection) {
		// a sidebar row's menu passes onEnterEditModeClick: "Edit all
		// sections" at the end (renderChannelSectionsSubmenu)
		i.Move = toPopupItems(a.moveToMenuItems(ids, moveMenuOpts{edit: true, section: a.sbRowSection(c.ID)}))
	}
	return i
}

// sbRowSection is the section the sidebar shows conversation ch in.
func (a *App) sbRowSection(ch string) string {
	for _, r := range a.sidebar.rows {
		if r.kind == sbConv && r.key == ch {
			return r.section
		}
	}
	return ""
}

// handle returns the row's getTemplate (nil without a plugin runtime).
// Built on the patched path only.
func (m *sbMenus) handle(a *App, c *convInfo) *taut.GoHandle {
	t := a.Taut()
	if t == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t != t {
		for _, h := range m.fns {
			h.Release()
		}
		m.t, m.fns, m.info, m.split = t, map[string]*taut.GoHandle{}, map[string]popups.ChannelMenuInfo{}, map[string]bool{}
	}
	m.info[c.ID] = a.sbMenuInfo(c, false)
	_, m.split[c.ID] = a.splitSidebarItem(c.ID)
	h := m.fns[c.ID]
	if h == nil {
		id := c.ID
		h = t.Func(func([]json.RawMessage) (any, error) {
			m.mu.Lock()
			i, split := m.info[id], m.split[id]
			m.mu.Unlock()
			items := popups.ChannelMenuItems(i, nil)
			if split {
				it, _ := popups.SplitViewItem(nil, "c/"+id, true)
				items = withSplitItem(items, it)
			}
			return popups.MenuTemplate(items), nil
		})
		m.fns[c.ID] = h
	}
	return h
}

// sbMenuTap sees each sidebar menu's items as it opens (tests). Nil
// outside tests.
var sbMenuTap func(ch string, items []popups.MenuItem)

// openSidebarMenu opens a row's menu at anchor (window coordinates).
// With a plugin's getTemplate it opens what that returns: Clack's items
// by key, and the plugin's own items (their click calls back into JS).
// Either way the menu goes through popups' MenuFromTemplate. The items
// and the template round trip are popups' (ChannelMenuItems, MenuTemplate,
// ItemsFromTemplate); the actions are the shell's.
func (a *App) openSidebarMenu(ch string, anchor image.Point, getTemplate slackc.JSFunc) {
	c := a.model().conv(ch)
	if c == nil {
		return
	}
	items := popups.ChannelMenuItems(a.sbMenuInfo(c, true), func(key string) func() { return a.sbMenuDo(ch, key) })
	if it, ok := a.splitSidebarItem(ch); ok { // getTilesTemplate, entry point "sidebar"
		items = withSplitItem(items, it)
	}
	show := func(items []popups.MenuItem) {
		if f := sbMenuTap; f != nil {
			f(ch, items)
		}
		a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Anchor: image.Rectangle{Min: anchor, Max: anchor}}))
	}
	if getTemplate.ID == 0 {
		show(items)
		return
	}
	getTemplate.CallResult(func(res any, err error) {
		list, ok := res.([]any)
		if err != nil || !ok {
			show(items)
			return
		}
		show(popups.ItemsFromTemplate(a.th, list, items))
	})
}

// sbMenuDo is what the row menu's item key does for ch.
func (a *App) sbMenuDo(ch, key string) func() {
	// star, mute and leave/close are popups' channel-menu actions, as in
	// Slack: real star/mute, leave after its confirmation
	if f := popups.ChannelMenuAction(a, ch, key); f != nil {
		return f
	}
	switch key {
	case "view_details":
		return func() { a.OpenDetails(ch) }
	case "copy_link":
		return func() { a.copyChannelLink(ch) }
	case "copy_name":
		return func() {
			if c := a.model().conv(ch); c != nil {
				a.copyText(c.Name)
				a.Toast("Name copied")
			}
		}
	case "mark_read":
		return func() { a.markRead(ch) }
	case "move_to_new_section":
		// openCreateChannelSectionModal({channelIds: [ch]})
		return func() { a.openCreateSectionWith(ch) }
	}
	return nil
}

// ---- ClientPrimaryView > RenderedViewContents (ChatTabs) ----

func (a *App) primaryViewProps() slackc.Props {
	return slackc.Props{"$key": "main", "windowId": "main"}
}

// layoutPrimaryView is the primary view (channel or page): Slack's
// ClientPrimaryView, which draws its view through RenderedViewContents
// (the .p-view_contents--primary element). The view fills the panel.
//
// Under a patch a plugin may draw above it, so its window origin is
// learned from the pointer instead of the panel's.
func (a *App) layoutPrimaryView(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p != nil {
		a.pvOrigin.track(gtx, a)
		a.primaryOrigin = a.pvOrigin.get(a.primaryOrigin)
	}
	gtx.Constraints.Min = gtx.Constraints.Max
	return slackc.Component(gtx, "RenderedViewContents", a, (*App).viewContentsProps, (*App).layoutViewContents)
}

// viewContentsProps: Slack's RenderedViewContents for the primary view.
// Its children are the view's body (the channel or page) as a native
// slot, so what a plugin puts beside them (ChatTabs' tab bar, before
// them) lands inside .p-view_contents--primary, as in Slack. The JS side
// draws the component as Slack does, a div with the view's classes around
// its children (internal/taut/js/04_react.js, SHAPES); the slot fills
// what that div leaves (a flex item of the view's column).
func (a *App) viewContentsProps() slackc.Props {
	return slackc.Props{
		"$key":      "primary",
		"className": "p-workspace__primary_view",
		"viewType":  "primary",
		"children":  slackc.Child(viewBodySlot, slackc.Props{"style": viewBodyStyle}, a.layoutViewBody),
	}
}

// viewBodySlot names the native body inside RenderedViewContents. Slack's
// children there are the routed view's own components, which Clack draws
// as one widget; the name is Clack's, so it matches no Slack patch.
const viewBodySlot = "ClackPrimaryViewBody"

// viewBodyStyle makes the body slot take the view's remaining height
// (whatever a plugin adds above it, or its padding, comes off it).
var viewBodyStyle = map[string]any{"flex": "1 1 0", "minHeight": 0, "minWidth": 0}

// layoutViewContents is RenderedViewContents' native: the
// .p-view_contents--primary element around the body. Under a patch the
// JS side draws the element itself and calls the body slot; this native
// then only draws until JS delivers that tree.
func (a *App) layoutViewContents(gtx layout.Context, p slackc.Props) layout.Dimensions {
	pv := clsPrimaryView.Enter(gtx, 0, 0)
	defer pv.Leave()
	return a.drawViewBody(gtx)
}

// layoutViewBody is the body slot under a patch. A plugin may draw above
// it or pad the view (ChatTabs' tab bar), so its origin is learned from
// the pointer too.
func (a *App) layoutViewBody(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	a.vbOrigin.track(gtx, a)
	a.primaryOrigin = a.vbOrigin.get(a.primaryOrigin)
	return a.drawViewBody(gtx)
}

func (a *App) drawViewBody(gtx layout.Context) layout.Dimensions {
	// tests: frames whose tree left the body out (every call in the frame:
	// under a patch it's measured, then drawn; never the stand-in)
	if !a.standIn && (a.skipBody > 0 || gtx.Now.Equal(a.skipAt)) {
		if !gtx.Now.Equal(a.skipAt) {
			a.skipBody--
			a.skipAt = gtx.Now
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	r := a.router.Current()
	if gtx.Source.Enabled() && !a.standIn {
		a.body = bodyDraw{at: gtx.Now, route: r, cons: gtx.Constraints, off: a.primaryOrigin.Sub(a.panelOrigin)}
	}
	if r.Page() == "c" {
		a.layoutChannel(gtx, r.Channel())
	} else {
		a.layoutPage(gtx, r)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// ---- ProgressiveDisclosureSuccessCoachmarkWrapper (ShowSendingBot) ----

func (a *App) coachmarkProps() slackc.Props {
	return slackc.Props{"$key": "shell", "coachmarkName": "", "isOpen": false}
}

// layoutCoachmark: Clack shows no coachmarks. A plugin's children, if it
// renders any, are drawn; otherwise nothing.
func (a *App) layoutCoachmark(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return layout.Dimensions{}
}

// layoutShellPoints draws the shell's empty boundaries (at the primary
// view's origin, zero-sized unless a plugin draws there).
func (a *App) layoutShellPoints(gtx layout.Context) {
	g := gtx
	g.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	d := slackc.Component(g, "ProgressiveDisclosureSuccessCoachmarkWrapper", a, (*App).coachmarkProps, (*App).layoutCoachmark)
	call := m.Stop()
	if d.Size != (image.Point{}) {
		call.Add(gtx.Ops)
	}
}

// bodyDraw is the primary view body's last live draw (keepBody).
type bodyDraw struct {
	at    time.Time
	route Route
	cons  layout.Constraints
	off   image.Point // its top-left in the primary panel
}

// bodyKeepFor bounds how long after its last draw a missing body is kept.
const bodyKeepFor = time.Second

// keepBody runs right after the primary panel's ClientPrimaryView, with
// gtx at the panel's origin. If the view's body (the conversation: list,
// composer, header) was drawn recently but not in this frame, and the
// route is the same, it is drawn again where it was, as it was: Gio
// forgets a handler absent for one frame, so a focused composer silently
// loses its focus and a button pressed before the gap gets a reset Cancel
// when it's back, and the release after it isn't a click ("I have to
// click like 5 times", "I was getting deselected"). A plugin tree that
// leaves the body out for a frame (Chat Tabs wraps ClientPrimaryView and
// RenderedViewContents) did exactly that. The stand-in is a real draw at
// the same place in the frame (under the overlays), so hit areas stay
// where the pointer is (no Leave, no Cancel) and nothing flickers.
func (a *App) keepBody(gtx layout.Context) {
	b := a.body
	if a.noKeepBody || b.at.IsZero() || b.at.Equal(gtx.Now) || !gtx.Source.Enabled() {
		return
	}
	if gtx.Now.Sub(b.at) > bodyKeepFor || gtx.Now.Before(b.at) || a.router.Current() != b.route {
		return
	}
	a.standIns++
	defer op.Offset(b.off).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: b.cons.Max}.Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = b.cons
	a.standIn = true
	a.drawViewBody(g)
	a.standIn = false
	// field evidence (focus_log.go): did the stand-in keep a focused
	// composer's focus (its handler is back in this frame)?
	a.missedFrameLog.standIn(b.route, a.routeComposerFocused(gtx, b.route))
}
