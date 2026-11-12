package ui

import (
	"context"
	"image"
	"image/color"
	"log"
	"sort"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sidebar's organize mode (Slack's channel sections "edit mode"),
// ported from its web client (build 4d0c78085232):
//
//   - Entering: a conversation row's menu, Move channel ▸ … "Edit all
//     sections" (boot-shared EaYn ChannelSectionMoveToMenu's
//     renderEditModeMenuItem, shown when the menu came from a sidebar row:
//     boot-render 50XG renderChannelSectionsSubmenu's showEditButton). It
//     is ChannelList.onEnterEditModeClick (client-channel-list-view SYcU):
//     the row's section heading keeps its place on screen and takes the
//     focus (focusOnEditMode); setIsEditMode (ruIY) clears the selection
//     and expands every section for the mode (its own collapsed state,
//     editModeChannelSectionsCollapsedDict).
//   - Rows (boot-render B7kx, lJBI): the page links stay but are disabled
//     (PageRowElement: .64 opacity, no hover tile, no click); a section
//     collapsed for the mode shows no conversations; an open one shows
//     all of them, read, dormant and outside a DM window included, but not
//     muted ones under improve_mute_pref's "hide muted" (lJBI's per-row
//     filter with isEditMode), and not the previewed channel; no footers
//     ("Add channels"), no empty Starred note, no Show more.
//   - A conversation row (boot-render PZNh Channel) starts with Slack's
//     checkbox (renderEditModeCheckbox: the icon font's real-checkbox-empty
//     / -checked, 1em of the row's 15px in a 26px box 1px after the row's
//     10px padding, 3px before the 8px gap; .64 opacity until checked) and
//     is highlighted when picked, not when open; picked neighbours join
//     (selectedTop / selectedMiddle / selectedBottom square their inner
//     corners). A click picks or unpicks it (onEditModeChannelClick),
//     Shift+click picks the range from the last pick (…RangeClick).
//   - OrganizeModeMenu (SYcU) under the list: border-top 1px otl-ter,
//     padding 16 16 24; "{n} selected" (13px) with " | " and an underlined
//     "Clear selection" once something is picked, 16px above the actions:
//     outline (primary without bulk_leave_channels_from_sidebar) "Move to…"
//     when something is picked, else "New Section"; with the experiment an
//     outline "Leave" and a primary check-icon Done, else an outline
//     "Done". The actions share the row (flex 1 0 0, 50px minimum, 8px
//     apart; the icon Done is 50px), 36px medium buttons with a 1px otl-sec
//     border, their labels centred and cut with an ellipsis. Move to… opens
//     ChannelSectionMoveToMenu for the picks above it (nVsMe, position
//     "top"); New Section opens Create a section; Leave leaves the picks
//     that can be left (ConfirmLeaveChannelsModal first when some can't)
//     and ends the mode; Done ends it. The New Section and Done tooltips
//     only show when the label is cut.
//   - Leaving the mode puts the list back where it was before it
//     (focusOnExitEditMode) and the focus on the open conversation.
//   - A move in the mode unpicks what it moved (moveManyChannels); its
//     Undo picks them again.

// sbEdit is organize mode's state.
type sbEdit struct {
	on         bool
	sel        map[string]bool // editModeSelectedChannelIds
	order      []string        // the picks in the order they were made (Object.keys)
	start, end string          // editModeSelectedChannelIdRangeStart / RangeEnd
	collapsed  map[string]bool // editModeChannelSectionsCollapsedDict
	beforeTop  int             // beforeEnterEditModeScrollTop (px)
	keepKey    string          // focusOnEditMode: this row back to keepOff px under the top
	keepOff    int
	keep       bool

	move, create, leave, done, clear widget.Clickable
	createTip, doneTip               popups.TipState
	moveRect                         image.Rectangle // Move to…, window coordinates
}

// sbEditMode reports whether the sidebar is in organize mode.
func (a *App) sbEditMode() bool { return a.sidebar.edit.on }

// bulkLeaveOn is bulk_leave_channels_from_sidebar.
func (a *App) bulkLeaveOn() bool { return a.exp("bulk_leave_channels_from_sidebar") == "on" }

// sbEnterEditMode is onEnterEditModeClick({channelSection}) from a row in
// section.
func (a *App) sbEnterEditMode(section string) {
	s := &a.sidebar
	e := &s.edit
	key := "sectionHeading-" + section
	e.beforeTop = s.scrollTop()
	if p := s.sbPos(key); p >= 0 && p < len(s.kb.rowOff) {
		e.keepKey, e.keepOff, e.keep = key, s.kb.rowOff[p]-s.scrollTop(), true
	} else {
		e.keepKey, e.keep = "", false
	}
	if !e.on { // setIsEditMode: a new mode starts empty and expanded
		e.sel, e.order, e.start, e.end = map[string]bool{}, nil, "", ""
		e.collapsed = map[string]bool{}
	}
	e.on = true
	s.builtFor = -1
	// SIDEBAR_EDIT_MODE_KEY_OFFSET: the heading takes the focus (no ring:
	// the menu was clicked)
	s.kb.key, s.kb.ring = key, false
	s.kb.init = key
	s.kb.wantFocus = "row"
	a.Invalidate()
}

// sbExitEditMode is setIsEditMode({isEditMode: false}): the selection goes,
// the list goes back to where it was, the open conversation has the focus.
func (a *App) sbExitEditMode() {
	s := &a.sidebar
	e := &s.edit
	if !e.on {
		return
	}
	e.on = false
	e.sel, e.order, e.start, e.end = map[string]bool{}, nil, "", ""
	e.keep = false
	s.builtFor = -1
	s.restoreTop, s.restore = e.beforeTop, true
	s.kb.key, s.kb.ring = a.sbSelectedKey(), false
	s.kb.wantFocus = "row"
	a.Invalidate()
}

// sbClearSelection is clearEditModeSelectedChannelIds.
func (a *App) sbClearSelection() {
	e := &a.sidebar.edit
	e.sel, e.order, e.start, e.end = map[string]bool{}, nil, "", ""
	a.Invalidate()
}

func (e *sbEdit) setPicked(id string, on bool) {
	if e.sel == nil {
		e.sel = map[string]bool{}
	}
	if on == e.sel[id] {
		return
	}
	if on {
		e.sel[id] = true
		e.order = append(e.order, id)
		return
	}
	delete(e.sel, id)
	for i, x := range e.order {
		if x == id {
			e.order = append(e.order[:i:i], e.order[i+1:]...)
			break
		}
	}
}

// sbPicked are the picks, in the order they were made.
func (a *App) sbPicked() []string { return append([]string(nil), a.sidebar.edit.order...) }

// sbEditClick is a click on a conversation in organize mode
// (onEditModeChannelClick; with Shift onEditModeChannelRangeClick over
// the rows as drawn).
func (a *App) sbEditClick(id string, shift bool) {
	s := &a.sidebar
	e := &s.edit
	if !e.on {
		return
	}
	if !shift || e.start == "" {
		on := !e.sel[id]
		e.setPicked(id, on)
		if on {
			e.start = id
		} else {
			e.start = ""
		}
		e.end = ""
		a.Invalidate()
		return
	}
	pos := func(ch string) int {
		for p := range s.kb.order {
			if r := s.rowAtPos(p); r.kind == sbConv && r.key == ch {
				return p
			}
		}
		return -1
	}
	i, r := pos(e.start), pos(id)
	if e.end != "" {
		if j := pos(e.end); i >= 0 && j >= 0 {
			for p := min(i, j); p <= max(i, j); p++ {
				if row := s.rowAtPos(p); row.kind == sbConv {
					e.setPicked(row.key, false)
				}
			}
		}
	}
	e.end = id
	if i >= 0 && r >= 0 {
		for p := min(i, r); p <= max(i, r); p++ {
			if row := s.rowAtPos(p); row.kind == sbConv {
				e.setPicked(row.key, true)
			}
		}
	}
	a.Invalidate()
}

// sbEditRows are a section's conversations in organize mode
// (improveMute: improve_mute_pref is on).
func sbEditRows(m *model, sec *sbSection, improveMute bool) []string {
	rows := sec.All
	if len(rows) == 0 {
		rows = sec.Rows
	}
	hidden := map[string]bool{}
	if improveMute { // p && w && O: muted ones stay hidden
		for _, id := range sec.HiddenMuted {
			hidden[id] = true
		}
	}
	var out []string
	for _, id := range rows {
		c := m.conv(id)
		if c == nil || c.Preview || hidden[id] {
			continue
		}
		out = append(out, id)
	}
	return out
}

// ---- the bar ----

// organizeBarHeight is OrganizeModeMenu's height: border 1, padding 16,
// the 18px count line and its 16px margin, the 36px buttons, padding 24.
func organizeBarHeight(gtx layout.Context) int { return 1 + gtx.Dp(16+18+16+36+24) }

// layoutOrganizeBar draws OrganizeModeMenu in sz (sidebar coordinates
// translated to its top-left) and runs its actions.
func (a *App) layoutOrganizeBar(gtx layout.Context, sz image.Point, origin image.Point) {
	s := &a.sidebar
	e := &s.edit
	c := &a.sbPal
	dark := a.sbDarkTheme()
	n := len(e.order)
	leaveOn := a.bulkLeaveOn()

	if e.clear.Clicked(gtx) {
		a.sbClearSelection()
		s.kb.key, s.kb.ring, s.kb.wantFocus = a.sbSelectedKey(), false, "row" // SIDEBAR_CURRENT_CHANNEL
	}
	if e.move.Clicked(gtx) && n > 0 {
		a.openOrganizeMove()
	}
	if e.create.Clicked(gtx) && n == 0 {
		a.openCreateSection("", "")
	}
	if e.leave.Clicked(gtx) && leaveOn {
		a.organizeLeave()
		return
	}
	if e.done.Clicked(gtx) {
		a.sbExitEditMode()
		return
	}

	// a click anywhere in the bar is the list's (the c-virtual_list) no
	// more: block the sidebar's hover tiles under it
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	theme.Fill(gtx, image.Rect(0, 0, sz.X, max(1, gtx.Dp(1))), theme.Over(theme.OtlTer(dark), a.sbBase))
	pad := gtx.Dp(16)
	y := 1 + pad
	lh := gtx.Dp(18)
	label := i18n.T("channel_sidebar", "{selectedItemCount} selected", "selectedItemCount", n)
	x := pad
	x += a.sbBarText(gtx, image.Pt(x, y), lh, label, c.SidebarText)
	if n > 0 {
		x += a.sbBarText(gtx, image.Pt(x, y), lh, " | ", c.SidebarText)
		cs := i18n.T("channel_sidebar", "Clear selection")
		cw := a.th.Measure(gtx, 13, theme.Regular, cs)
		at(gtx, image.Pt(x, y), image.Pt(cw, lh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, &e.clear, nil, func(gtx layout.Context) layout.Dimensions {
				vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.SidebarText, cs)
				})
				// .c-link--inherit-color: always underlined, offset 1px
				theme.Fill(gtx, image.Rect(0, lh-gtx.Dp(3), cw, lh-gtx.Dp(3)+max(1, gtx.Dp(1))), c.SidebarText)
				return layout.Dimensions{Size: image.Pt(cw, lh)}
			})
		})
	}
	y += lh + gtx.Dp(16)
	bh := gtx.Dp(36)
	w := sz.X - 2*pad
	type act struct {
		btn     *widget.Clickable
		label   string
		icon    string
		primary bool
		fixed   int // px (0: shares the row)
		tip     *popups.TipState
		tipText string
	}
	var acts []act
	moveKind := !leaveOn
	if n > 0 {
		acts = append(acts, act{btn: &e.move, label: i18n.T("channel_sidebar", "Move to…"), primary: moveKind})
	} else {
		lbl := i18n.T("channel_sidebar", "New Section")
		acts = append(acts, act{btn: &e.create, label: lbl, primary: moveKind, tip: &e.createTip, tipText: lbl})
	}
	if leaveOn {
		acts = append(acts, act{btn: &e.leave, label: i18n.T("channel_sidebar", "Leave")},
			act{btn: &e.done, icon: "check-filled", primary: true, fixed: gtx.Dp(50), tip: &e.doneTip, tipText: i18n.T("channel_sidebar", "Done")})
	} else {
		lbl := i18n.T("channel_sidebar", "Done")
		acts = append(acts, act{btn: &e.done, label: lbl, tip: &e.doneTip, tipText: lbl})
	}
	gap := gtx.Dp(8)
	flex, fixedW := 0, 0
	for _, ac := range acts {
		if ac.fixed > 0 {
			fixedW += ac.fixed
		} else {
			flex++
		}
	}
	share := 0
	if flex > 0 {
		share = max(gtx.Dp(50), (w-fixedW-gap*(len(acts)-1))/flex)
	}
	bx := pad
	for _, ac := range acts {
		bw := share
		if ac.fixed > 0 {
			bw = ac.fixed
		}
		r := image.Rect(bx, y, bx+bw, y+bh)
		truncated := a.organizeButton(gtx, dark, ac.btn, r, ac.label, ac.icon, ac.primary)
		if ac.btn == &e.move {
			e.moveRect = r.Add(origin)
		}
		if ac.tip != nil {
			// the tooltip only shows when the label is cut (onShow's
			// isTruncated); an icon-only Done never is
			at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
				popups.Tooltip(gtx, a.th, ac.tip, truncated && ac.btn.Hovered(), r.Size(), popups.Above, ac.tipText, "")
				return layout.Dimensions{Size: r.Size()}
			})
		}
		bx += bw + gap
	}
}

// sbBarText draws 13px text at p (lh tall) and returns its width.
func (a *App) sbBarText(gtx layout.Context, p image.Point, lh int, txt string, col color.NRGBA) int {
	tw := a.th.Measure(gtx, 13, theme.Regular, txt)
	at(gtx, p, image.Pt(tw+gtx.Dp(2), lh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Regular, col, txt)
		})
	})
	return tw
}

// organizeButton draws one of the bar's c-button--medium actions in r
// (organizeModeMenuOrganizeAction: a 1px otl-sec border, the label
// centred, cut with an ellipsis), in the sidebar's dark ramp. It reports
// whether the label was cut.
func (a *App) organizeButton(gtx layout.Context, th *theme.Theme, btn *widget.Clickable, r image.Rectangle, label, icon string, primary bool) bool {
	cl := popups.ColorsOf(th)
	bg, fg := cl.BtnBg, cl.Fg
	if btn.Hovered() {
		bg = cl.BtnHover
	}
	if primary {
		bg, fg = cl.Primary, cl.PrimaryText
		if btn.Hovered() {
			bg = theme.Mix(bg, color.NRGBA{A: 0xff}, .1)
		}
	}
	rad := gtx.Dp(8)
	cut := false
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		sz := r.Size()
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			theme.RRect(gtx, image.Rectangle{Max: sz}, rad, cl.BtnBorder)
			theme.RRect(gtx, image.Rectangle{Max: sz}.Inset(max(1, gtx.Dp(1))), rad-1, bg)
			inner := sz.X - 2*gtx.Dp(12) - 2*max(1, gtx.Dp(1))
			// padding 0 12px 1px: the label sits half a pixel high
			cy := (sz.Y - gtx.Dp(1)) / 2
			if icon != "" {
				is := gtx.Dp(20)
				at(gtx, image.Pt((sz.X-is)/2, cy-is/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 20, fg)
				})
			} else {
				tw := a.th.Measure(gtx, 15, theme.Bold, label)
				cut = tw > inner
				lw := min(tw, inner)
				lx := (sz.X - lw) / 2
				lh := gtx.Dp(18)
				at(gtx, image.Pt(lx, cy-lh/2), image.Pt(lw, lh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 15, theme.Bold, fg, label)
					})
				})
			}
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
	})
	return cut
}

// openOrganizeMove is the bar's Move to…: ChannelSectionMoveToMenu for
// the picks, above the button (BaseChannelSectionSelectMenuTrigger:
// position "top", offsetY -8, allowanceX 12; the menu 280px).
func (a *App) openOrganizeMove() {
	ids := a.sbPicked()
	if len(ids) == 0 {
		return
	}
	items := toPopupItems(a.moveToMenuItems(ids, moveMenuOpts{}))
	a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Width: 280, Anchor: a.sidebar.edit.moveRect,
		Popover: "top", OffsetY: -8, AllowanceX: 12}))
}

// ---- Leave ----

// organizeLeave is the bar's Leave (OrganizeModeMenu f): the picks that
// can't be left (getChannelsThatCannotBeLeft) go to
// ConfirmLeaveChannelsModal with the rest; with none of those the rest is
// left at once (leaveChannelsInBulk); the mode ends either way.
func (a *App) organizeLeave() {
	c := a.Core()
	ids := a.sbPicked()
	if c == nil {
		a.sbExitEditMode()
		return
	}
	cannot := c.ChannelsThatCannotBeLeft(ids)
	skip := map[string]bool{}
	for _, id := range cannot {
		skip[id] = true
	}
	var leave []string
	for _, id := range ids {
		if !skip[id] {
			leave = append(leave, id)
		}
	}
	switch {
	case len(cannot) > 0:
		names := make([]string, 0, len(cannot))
		for _, id := range cannot {
			names = append(names, a.leaveListName(id))
		}
		a.ShowOverlay(popups.NewConfirmLeaveChannelsModal(a, len(leave), names, func() {
			if len(leave) == 0 { // "You can’t leave these channels.": OK only closes
				return
			}
			a.leaveChannelsInBulk(leave)
			a.sbFocusFirstVisible() // SIDEBAR_FIRST_VISIBLE_ROW
		}))
	case len(leave) > 0:
		a.leaveChannelsInBulk(leave)
	}
	a.sbExitEditMode()
}

// leaveListName is getDisplayNameForChannelList's name for a row
// (getDisplayNameForChannel with forceMpimNames): a public channel with
// its #, a private one without, a DM the person's name with @.
func (a *App) leaveListName(id string) string {
	cv := a.model().conv(id)
	if cv == nil {
		return id
	}
	switch cv.Kind {
	case "channel":
		return "#" + cv.Name
	case "im", "app":
		return "@" + cv.Name
	}
	return cv.Name
}

// leaveChannelsInBulk shows "We’re tidying up …" and leaves ids in one
// conversations.bulkLeave (a failure is only logged, as in Slack).
func (a *App) leaveChannelsInBulk(ids []string) {
	c := a.Core()
	if c == nil || len(ids) == 0 {
		return
	}
	ns := i18n.NS("sidebar-sweeper-leave-channels")
	parts := []i18n.Part{{Text: ns.T("We’re tidying up …"), Tag: "strong"}, {Text: " "}, {Text: ns.T("This might take a minute, but an organized list is worth it.")}}
	a.ToastParts(parts, "", color.NRGBA{}, 6*time.Second)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := c.LeaveChannelsBulk(ctx, ids); err != nil {
			a.Post(func() { leaveBulkFailed(err) })
		}
	}()
}

// leaveBulkFailed is onLeaveChannels' catch: the error goes to the log.
var leaveBulkFailed = func(err error) { log.Printf("ui: sidebar: bulk leave: %v", err) }

// sbFocusFirstVisible is focusOnFirstSection without the ring.
func (a *App) sbFocusFirstVisible() {
	s := &a.sidebar
	for p := range s.kb.order {
		if sbFocusable(s.rowAtPos(p)) {
			s.kb.key, s.kb.ring = s.rowAtPos(p).itemKey, false
			s.kb.wantFocus = "row"
			break
		}
	}
	a.Invalidate()
}

// ---- moves ----

// moveManyTo is performMoveManyChannelsToSection (boot-shared epiQ) for
// ids into to (nil: a Starred the sidebar doesn't have yet): out of their
// custom sections and Starred, into to, through moveMany.
func (a *App) moveManyTo(ids []string, to *sbSection) {
	m := a.model()
	var stars *sbSection
	removeBy := map[string][]string{}
	var starred []string
	for i := range m.Sections {
		s := &m.Sections[i]
		switch s.Type {
		case "stars":
			stars = s
		case "standard":
			for _, id := range ids {
				if secHas(s, id) {
					removeBy[s.ID] = append(removeBy[s.ID], id)
				}
			}
		}
	}
	for _, id := range ids {
		if a.channelSections(id).starred {
			starred = append(starred, id)
		}
	}
	var rem []core.SectionItems
	secIDs := make([]string, 0, len(removeBy))
	for id := range removeBy {
		secIDs = append(secIDs, id)
	}
	sort.Strings(secIDs)
	for _, id := range secIDs {
		rem = append(rem, core.SectionItems{Section: id, Channels: removeBy[id]})
	}
	if len(starred) > 0 && stars != nil {
		rem = append(rem, core.SectionItems{Section: stars.ID, Channels: starred})
	}
	if to == nil { // the synthetic Starred: stars.add for each
		a.moveMany(ids, nil, rem, func(ctx context.Context, c *core.App) (core.SectionMove, error) {
			var mv core.SectionMove
			for _, id := range ids {
				if err := c.Star(ctx, id, true); err != nil {
					return mv, err
				}
				mv = mv.Merge(core.SectionMove{Starred: []string{id}})
			}
			r, err := c.MoveMany(ctx, nil, rem)
			return mv.Merge(r), err
		})
		return
	}
	a.moveMany(ids, []core.SectionItems{{Section: to.ID, Channels: ids}}, rem, nil)
}

// moveMany is performBulkChannelMove (eq1K) over moveManyChannels: in
// organize mode the picks it moves are unpicked at once (and picked again
// by the toast's Undo); a failure shows enqueueChannelSectionErrorToast.
// run, when set, makes the move instead of core.MoveMany(insert, remove).
func (a *App) moveMany(ids []string, insert, remove []core.SectionItems, run func(ctx context.Context, c *core.App) (core.SectionMove, error)) {
	e := &a.sidebar.edit
	var unpicked []string
	if e.on {
		moving := map[string]bool{}
		for _, it := range append(append([]core.SectionItems(nil), insert...), remove...) {
			for _, ch := range it.Channels {
				moving[ch] = true
			}
		}
		for _, id := range ids {
			moving[id] = true
		}
		for _, id := range append([]string(nil), e.order...) {
			if moving[id] {
				unpicked = append(unpicked, id)
				e.setPicked(id, false)
			}
		}
	}
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var mv core.SectionMove
		var err error
		if run != nil {
			mv, err = run(ctx, c)
		} else {
			mv, err = c.MoveMany(ctx, insert, remove)
		}
		a.Post(func() {
			if err != nil {
				a.sectionErrorToast(core.ErrCode(err))
				return
			}
			if mv.Count == 0 {
				return
			}
			a.ToastUndo(a.bulkMoveText(mv), func() {
				for _, id := range unpicked {
					a.sidebar.edit.setPicked(id, true)
				}
				a.sectionRun("Couldn’t undo", func(ctx context.Context, c *core.App) error { return c.UndoSectionMove(ctx, mv) }, nil)
			})
		})
	}()
}

// sectionErrorToast is enqueueChannelSectionErrorToast (boot-shared wAP7)
// for a move's error code: too_many_channels says the 500-conversation
// limit; too_many_operations and anything else is renderGenericError's
// "Something went wrong." with its "drop us a line" link; each in
// enqueueChannelSectionToast's error Toast (A46L: warning-filled in
// sk_raspberry_red, 5s).
func (a *App) sectionErrorToast(code string) {
	ns := i18n.NS("channel_sidebar")
	var parts []i18n.Part
	switch code {
	case "too_many_channels":
		parts = []i18n.Part{{Text: ns.T("Unfortunately, you can have up to {numMaxChannelsPerSection} conversations in a section \u2014\u00a0but no more.",
			"numMaxChannelsPerSection", i18n.FormatNumber(i18n.Locale(), 500))}}
	case "too_many_entities":
		parts = []i18n.Part{{Text: ns.T("Unfortunately, you can have up to {numMaxEntitiesPerUser} entites in the sidebar \u2014\u00a0but no more.",
			"numMaxEntitiesPerUser", i18n.FormatNumber(i18n.Locale(), 100))}}
	default:
		parts = append([]i18n.Part{{Text: ns.T("Something went wrong."), Tag: "strong"}},
			ns.RT(" Reloading Slack might clear things up. If it doesn’t, <a>drop us a line</a>.")...)
	}
	a.ToastParts(parts, "warning-filled", slackRed, 5*time.Second)
}

// sbPickNeighbours reports whether the conversation rows right above and
// below r are picked too (getPropsForItem's d and u).
func (a *App) sbPickNeighbours(r *sbRow) (up, down bool) {
	s := &a.sidebar
	p := s.sbPos(r.itemKey)
	if p < 0 {
		return false, false
	}
	picked := func(q int) bool {
		row := s.rowAtPos(q)
		return row != nil && row.kind == sbConv && s.edit.sel[row.key]
	}
	return picked(p - 1), picked(p + 1)
}
