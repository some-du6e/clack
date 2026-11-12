package ui

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sidebar's keyboard model, as Slack's ChannelList has it
// (client-channel-list-view SYcU ChannelList over the virtual list
// client-boot 67/z BaseKeyboardNavigableList, OtAy, layout 72/m; build
// 4d0c78085232):
//
//   - The rows are a roving-focus tree: page links, section headings,
//     conversations, the section footers ("Add channels") and the text
//     filter's no-results row are its treeitems (isRowObjectFocusable);
//     spacers, the divider and the empty Starred note aren't. Tab into the
//     list lands on the open conversation's row (getTabIndex: the selected
//     row is the tab stop), else the last row it left, else the first in
//     view.
//   - Up and Down move to the previous / next treeitem, no wrapping
//     (getNextActiveItem); a row whose top leaves the view scrolls to 6px
//     under the list's top (scrollToKey with arrowNavigationOffset -6). Home
//     and End go to the first / last treeitem; PageUp and PageDown by the
//     list's height less 26 (pageHeight). Without keyboard focus (after a
//     click) Up and Down scroll the list by 40px instead.
//   - onItemKeyDown: Enter (and Space) toggles a heading or clicks the
//     row's own link (getPrimaryActionChildElement), Shift carried along;
//     Right expands a heading (and goes into it when it was open), Left
//     collapses one or goes up to a row's heading; Alt+Left / Alt+Right
//     collapse / expand every section, "*" expands them all; Shift+PageUp /
//     Shift+PageDown go to the previous / next heading; Delete (Backspace on
//     macOS) closes a direct message; ⌘⇧H / Ctrl+Shift+H toggles a huddle in
//     the row's conversation; with sidebar_text_filter, Shift+Tab goes back
//     to the filter input.
//   - The focused row shows Slack's focus ring only with keyboard focus
//     (.p-channel_sidebar__static_list--keyboard_focus): the item's
//     :after 0 8px in, radius 8, 1px otl-hgl-1 and its 5px halo at 30%. A
//     press takes the focus without it (onItemMouseDown).
//   - The text filter's ArrowDown (SidebarTextFilterInput) focuses the
//     first treeitem (SIDEBAR_FIRST_VISIBLE_ROW: focusOnFirstSection).

// sbKbTag is the list's key focus target (not zero-sized: its address
// must be its own).
type sbKbTag struct{ _ byte }

// sbKeys is the list's keyboard state (Slack's activeItem,
// initialActiveItem, hasKeyboardFocus).
type sbKeys struct {
	tag     sbKbTag
	key     string // activeItem: the focused row's item key ("" none)
	init    string // initialActiveItem: where the focus goes back to
	ring    bool   // hasKeyboardFocus in keyboard modality: the ring shows
	focused bool   // the list has the key focus
	via     string // what asked for the focus: "press", "first", "row" ("" Tab)
	// wantFocus asks for the focus on the next frame (via, as above): a
	// transitionFocusTo from outside the list's events
	wantFocus string
	listH     int   // the list's height last frame (px)
	rowOff    []int // the rows' tops in display order last frame (px)
	order     []int // display order: row indexes (the plugin-shown ones)
	// the key filters, boxed once (the sidebar allocates nothing per
	// frame): the fixed ones, and the conditional ones added to buf
	fixed, tab, del, huddle []event.Filter
	buf                     []event.Filter
	press                   event.Filter
}

// sbFilters are the list's key filters for this frame.
func (a *App) sbFilters() []event.Filter {
	k := &a.sidebar.kb
	t := &k.tag
	if k.fixed == nil {
		k.fixed = []event.Filter{
			key.FocusFilter{Target: t},
			key.Filter{Focus: t, Name: key.NameUpArrow},
			key.Filter{Focus: t, Name: key.NameDownArrow},
			key.Filter{Focus: t, Name: key.NameHome},
			key.Filter{Focus: t, Name: key.NameEnd},
			key.Filter{Focus: t, Name: key.NamePageUp, Optional: key.ModShift},
			key.Filter{Focus: t, Name: key.NamePageDown, Optional: key.ModShift},
			key.Filter{Focus: t, Name: key.NameReturn, Optional: key.ModShift},
			key.Filter{Focus: t, Name: key.NameEnter, Optional: key.ModShift},
			key.Filter{Focus: t, Name: key.NameSpace, Optional: key.ModShift},
			key.Filter{Focus: t, Name: key.NameLeftArrow, Optional: key.ModAlt},
			key.Filter{Focus: t, Name: key.NameRightArrow, Optional: key.ModAlt},
			key.Filter{Focus: t, Name: key.NameDeleteForward},
			key.Filter{Focus: t, Name: "*", Optional: key.ModShift},
		}
		k.tab = []event.Filter{key.Filter{Focus: t, Name: key.NameTab, Required: key.ModShift}}
		k.del = []event.Filter{key.Filter{Focus: t, Name: key.NameDeleteBackward}}
		// isHuddleKeyboardShortcutPressed
		k.huddle = []event.Filter{key.Filter{Focus: t, Name: "H", Required: key.ModShortcut | key.ModShift},
			key.Filter{Focus: t, Name: "H", Required: key.ModShortcut | key.ModShift | key.ModAlt}}
	}
	k.buf = append(k.buf[:0], k.fixed...)
	if shortcut.GOOS == "darwin" {
		k.buf = append(k.buf, k.del...)
	}
	if a.sbFilterOn() {
		k.buf = append(k.buf, k.tab...)
	}
	if r := a.sidebar.rowAtPos(a.sidebar.sbPos(k.key)); r != nil && r.kind == sbConv {
		k.buf = append(k.buf, k.huddle...)
	}
	return k.buf
}

// sbFocusable is isRowObjectFocusable: a treeitem row.
func sbFocusable(r *sbRow) bool {
	switch r.kind {
	case sbLink, sbHeading, sbConv, sbAction, sbNoResults:
		return r.itemKey != ""
	}
	return false
}

// sbRowHeight is the height layoutSidebarRow gives r.
func (a *App) sbRowHeight(gtx layout.Context, r *sbRow) int {
	switch r.kind {
	case sbGap, sbDivider:
		return gtx.Dp(unit.Dp(r.gap))
	case sbNote:
		return gtx.Dp(44)
	case sbNoResults:
		return gtx.Dp(130)
	}
	h := a.sbRowH(gtx)
	if a.sbBBMBox(r) {
		h += gtx.Dp(12)
	}
	return h
}

// sbLayoutRows records the display order and the rows' offsets for the
// keyboard (the list draws order).
func (a *App) sbLayoutRows(gtx layout.Context, order []int) {
	k := &a.sidebar.kb
	k.order = order
	k.rowOff = k.rowOff[:0]
	y := 0
	for _, i := range order {
		k.rowOff = append(k.rowOff, y)
		y += a.sbRowHeight(gtx, &a.sidebar.rows[i])
	}
	k.rowOff = append(k.rowOff, y) // the content's height
}

// sbPos is the display position of the row with item key ("" none: -1).
func (s *sidebarState) sbPos(itemKey string) int {
	if itemKey == "" {
		return -1
	}
	for p, i := range s.kb.order {
		if s.rows[i].itemKey == itemKey {
			return p
		}
	}
	return -1
}

func (s *sidebarState) rowAtPos(p int) *sbRow {
	if p < 0 || p >= len(s.kb.order) || s.kb.order[p] >= len(s.rows) {
		return nil
	}
	return &s.rows[s.kb.order[p]]
}

// scrollTop is the list's scroll offset in px.
func (s *sidebarState) scrollTop() int {
	k := &s.kb
	pos := s.list.Position
	if pos.First < 0 || pos.First >= len(k.rowOff) {
		return 0
	}
	return k.rowOff[pos.First] + pos.Offset
}

// setScrollTop scrolls the list to px (bracketScrollTop: within the
// content).
func (s *sidebarState) setScrollTop(px int) {
	k := &s.kb
	if len(k.rowOff) < 2 {
		return
	}
	total := k.rowOff[len(k.rowOff)-1]
	px = min(px, total-k.listH)
	px = max(px, 0)
	first := 0
	for p := 0; p+1 < len(k.rowOff); p++ {
		if k.rowOff[p+1] > px {
			first = p
			break
		}
	}
	s.list.Position.First = first
	s.list.Position.Offset = px - k.rowOff[first]
	s.list.Position.BeforeEnd = true
}

// inView is isItemInView: the row's top within the view, t px in.
func (s *sidebarState) inView(p, t int) bool {
	k := &s.kb
	if p < 0 || p+1 >= len(k.rowOff) {
		return false
	}
	top, st := k.rowOff[p], s.scrollTop()
	return top >= st+t && top <= st+k.listH-t
}

// scrollToPos is scrollToKey: the row's top at offset px under the view's
// top.
func (s *sidebarState) scrollToPos(p, offset int) {
	if p >= 0 && p+1 < len(s.kb.rowOff) {
		s.setScrollTop(s.kb.rowOff[p] + offset)
	}
}

// focusablePos are the treeitems' display positions in [from, to).
func (s *sidebarState) focusableIn(from, to int) []int {
	var out []int
	for p := max(from, 0); p < min(to, len(s.kb.order)); p++ {
		if sbFocusable(s.rowAtPos(p)) {
			out = append(out, p)
		}
	}
	return out
}

// visibleRange is getVisibleRange: the rows overlapping [top, top+listH).
func (s *sidebarState) visibleRange(top int) (int, int) {
	k := &s.kb
	start, end := 0, 0
	for p := 0; p+1 < len(k.rowOff); p++ {
		if k.rowOff[p+1] <= top {
			start = p + 1
		}
		if k.rowOff[p] < top+k.listH {
			end = p + 1
		}
	}
	return start, max(end, start)
}

// firstInView is getFocusableItemsInView()[0] for a view at top.
func (s *sidebarState) firstInView(top int) int {
	a, b := s.visibleRange(top)
	if f := s.focusableIn(a, b); len(f) > 0 {
		return f[0]
	}
	return -1
}

func (s *sidebarState) lastInView(top int) int {
	a, b := s.visibleRange(top)
	if f := s.focusableIn(a, b); len(f) > 0 {
		return f[len(f)-1]
	}
	return -1
}

// setActive is setActiveItem (with keyboard focus).
func (s *sidebarState) setActive(p int) {
	r := s.rowAtPos(p)
	if r == nil {
		return
	}
	s.kb.key, s.kb.ring = r.itemKey, true
	if sbFocusable(r) {
		s.kb.init = r.itemKey
	}
}

// nextActive is getNextActiveItem: the treeitem after (dir 1) or before
// (-1) the active row; without one, the initial item when in view, else
// the first in view.
func (s *sidebarState) nextActive(dir int) int {
	k := &s.kb
	at := s.sbPos(k.key)
	if at < 0 {
		if ip := s.sbPos(k.init); ip >= 0 && s.inView(ip, 0) {
			return ip
		}
		return s.firstInView(s.scrollTop())
	}
	for p := at + dir; p >= 0 && p < len(k.order); p += dir {
		if sbFocusable(s.rowAtPos(p)) {
			return p
		}
	}
	return -1
}

// sbFocusList asks for the list's key focus (via: what for).
func (a *App) sbFocusList(gtx layout.Context, via string) {
	k := &a.sidebar.kb
	k.via = via
	gtx.Execute(key.FocusCmd{Tag: &k.tag})
	a.Invalidate()
}

// sbFocusFirstRow is the text filter's ArrowDown: the first treeitem
// (lK), with the ring, scrolled to if it isn't in view, once this frame's
// rows are laid out (sbBeforeList).
func (a *App) sbFocusFirstRow(gtx layout.Context) {
	a.sidebar.kb.wantFocus = "first"
	a.Invalidate()
}

// sbListFocusArea registers the list's focus target over r (list
// coordinates); the rows draw inside it, so a press on a row reaches it.
func (a *App) sbListFocusArea(gtx layout.Context, sz image.Point) clip.Stack {
	area := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, &a.sidebar.kb.tag)
	return area
}

// sbListEvents handles the list's presses and keys (before its rows are
// laid out).
func (a *App) sbListEvents(gtx layout.Context) {
	s := &a.sidebar
	k := &s.kb
	for {
		if k.press == nil {
			k.press = pointer.Filter{Target: &k.tag, Kinds: pointer.Press}
		}
		ev, ok := gtx.Event(k.press)
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			// onItemMouseDown: no keyboard focus, the pressed row is where
			// the focus goes back to
			k.ring, k.key = false, ""
			if p := s.posAtY(int(e.Position.Y) + s.scrollTop()); p >= 0 && sbFocusable(s.rowAtPos(p)) {
				k.init = s.rowAtPos(p).itemKey
			}
			if !gtx.Focused(&k.tag) {
				a.sbFocusList(gtx, "press")
			}
		}
	}
	filters := a.sbFilters()
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			a.sbFocusChanged(e.Focus)
		case key.Event:
			if e.State == key.Press {
				a.sbKey(gtx, e)
			}
		}
	}
}

// posAtY is the display position of the row at content offset y.
func (s *sidebarState) posAtY(y int) int {
	k := &s.kb
	for p := 0; p+1 < len(k.rowOff); p++ {
		if y >= k.rowOff[p] && y < k.rowOff[p+1] {
			return p
		}
	}
	return -1
}

// sbFocusChanged is onFocusEnter / onFocusLeave.
func (a *App) sbFocusChanged(in bool) {
	s := &a.sidebar
	k := &s.kb
	via := k.via
	k.via = ""
	k.focused = in
	if !in {
		// onFocusLeave: no active item, the last one is where Tab comes back
		if k.key != "" {
			k.init = k.key
		}
		k.key, k.ring = "", false
		return
	}
	switch via {
	case "press":
		return // the row pressed has it, without the ring
	case "first", "row":
		return // the caller set the active row
	}
	// Tab: the selected row is the tab stop (getTabIndex), else the
	// initial item, else the first in view (focus)
	p := -1
	if cur := a.sbSelectedKey(); cur != "" {
		p = s.sbPos(cur)
	}
	if p < 0 {
		if ip := s.sbPos(k.init); ip >= 0 && s.inView(ip, 0) {
			p = ip
		}
	}
	if p < 0 {
		p = s.firstInView(s.scrollTop())
	}
	if p >= 0 {
		s.setActive(p)
		if !s.inView(p, 0) {
			s.scrollToPos(p, 0)
		}
	}
}

// sbSelectedKey is the selected row's item key (selectedItemId): the open
// conversation's or page's, not in organize mode.
func (a *App) sbSelectedKey() string {
	if a.sidebar.edit.on {
		return ""
	}
	r := a.router.Current()
	if ch := r.Channel(); ch != "" {
		return ch
	}
	return string(r)
}

// sbKey handles one key press with the list focused.
func (a *App) sbKey(gtx layout.Context, e key.Event) {
	s := &a.sidebar
	k := &s.kb
	shift, alt := e.Modifiers.Contain(key.ModShift), e.Modifiers.Contain(key.ModAlt)
	at := s.sbPos(k.key)
	row := s.rowAtPos(at)
	if row == nil && k.key == "" { // after a press: the row pressed keeps the DOM focus
		row = s.rowAtPos(s.sbPos(k.init))
	}
	switch e.Name {
	case key.NameUpArrow, key.NameDownArrow:
		dir := 1
		if e.Name == key.NameUpArrow {
			dir = -1
		}
		if !k.ring && k.key == "" {
			// onKeyDownArrowKey without keyboard focus: scroll 40px
			s.setScrollTop(s.scrollTop() + dir*gtx.Dp(40))
			break
		}
		if p := s.nextActive(dir); p >= 0 {
			s.setActive(p)
			if !s.inView(p, 0) {
				s.scrollToPos(p, gtx.Dp(-6)) // arrowNavigationOffset
			}
		}
	case key.NameHome:
		if !k.ring {
			s.setScrollTop(0)
			break
		}
		if s.scrollTop() <= 1 {
			if p := s.firstInView(0); p >= 0 {
				s.setActive(p)
			}
			break
		}
		if f := s.focusableIn(0, len(k.order)); len(f) > 0 {
			s.setActive(f[0])
			s.scrollToPos(f[0], 0)
		}
	case key.NameEnd:
		total := k.rowOff[len(k.rowOff)-1]
		if !k.ring {
			s.setScrollTop(total)
			break
		}
		if s.scrollTop()+k.listH >= total-1 {
			if p := s.lastInView(s.scrollTop()); p >= 0 {
				s.setActive(p)
			}
			break
		}
		if f := s.focusableIn(0, len(k.order)); len(f) > 0 {
			s.setActive(f[len(f)-1])
			s.scrollToPos(f[len(f)-1], 0)
		}
	case key.NamePageUp, key.NamePageDown:
		if shift {
			a.sbHeadingStep(row, e.Name == key.NamePageDown)
			break
		}
		page := k.listH - gtx.Dp(26)
		if !k.ring {
			if e.Name == key.NamePageUp {
				page = -page
			}
			s.setScrollTop(s.scrollTop() + page)
			break
		}
		if e.Name == key.NamePageUp {
			t := s.scrollTop() - page
			if s.scrollTop() <= 1 {
				if p := s.firstInView(0); p >= 0 {
					s.setActive(p)
				}
				break
			}
			// scrollToAndFocusFirstItemInView
			p := s.firstInView(max(t, 0))
			if p < 0 || p == at {
				p = s.nextActive(-1)
			}
			if p >= 0 {
				s.setActive(p)
				s.scrollToPos(p, 0)
			}
			break
		}
		t := s.scrollTop() + page
		total := k.rowOff[len(k.rowOff)-1]
		if s.scrollTop()+k.listH >= total-1 {
			if p := s.lastInView(s.scrollTop()); p >= 0 {
				s.setActive(p)
			}
			break
		}
		p := s.lastInView(t)
		if p < 0 || p == at {
			p = s.nextActive(1)
		}
		if p >= 0 {
			s.setActive(p)
		}
		s.setScrollTop(t)
	case key.NameReturn, key.NameEnter, key.NameSpace:
		a.sbActivate(gtx, row, e.Name == key.NameSpace, shift)
	case key.NameRightArrow:
		if row == nil || row.kind != sbHeading {
			break
		}
		if alt {
			a.sbSetAllCollapsed(false)
			break
		}
		open := !a.sbCollapsed(row)
		a.sbSetCollapsed(row, false)
		if open { // aria-expanded: into the section
			if p := at + 1; p < len(k.order) && s.rowAtPos(p).section == row.key && sbFocusable(s.rowAtPos(p)) && s.rowAtPos(p).kind != sbHeading {
				s.setActive(p)
			}
		}
	case key.NameLeftArrow:
		if row == nil {
			break
		}
		if row.kind == sbHeading {
			if alt {
				a.sbSetAllCollapsed(true)
				break
			}
			a.sbSetCollapsed(row, true)
			break
		}
		if alt {
			break
		}
		if row.kind == sbConv || row.kind == sbAction {
			if p := s.sbPos("sectionHeading-" + row.section); p >= 0 {
				s.setActive(p)
				if !s.inView(p, 0) {
					s.scrollToPos(p, gtx.Dp(-6))
				}
			}
		}
	case "*":
		if row != nil && row.kind == sbHeading {
			a.sbSetAllCollapsed(false)
		}
	case key.NameDeleteForward, key.NameDeleteBackward:
		// onDeleteKeyPressed: closeIM for a direct message's row
		if row != nil && row.kind == sbConv {
			if c := a.model().conv(row.key); c != nil && (c.Kind == "im" || c.Kind == "mpim" || c.Kind == "app") {
				if f := popups.ChannelMenuAction(a, row.key, "close"); f != nil {
					f()
				}
			}
		}
	case key.NameTab:
		// Shift+Tab with sidebar_text_filter: the filter input
		k.key, k.ring = "", false
		gtx.Execute(key.FocusCmd{Tag: &s.filter.ed})
	case "H":
		if row != nil && row.kind == sbConv {
			a.huddles().ShortcutToggle(row.key)
		}
	}
	a.Invalidate()
}

// sbHeadingStep is onShiftKeyAndPageDirectionPressed: from a heading the
// heading before or after it; from a row its own heading (up) or the next
// one (down).
func (a *App) sbHeadingStep(row *sbRow, down bool) {
	s := &a.sidebar
	if row == nil {
		return
	}
	var heads []int
	for p := range s.kb.order {
		if s.rowAtPos(p).kind == sbHeading {
			heads = append(heads, p)
		}
	}
	idx := func(key string) int {
		for i, p := range heads {
			if s.rowAtPos(p).itemKey == key {
				return i
			}
		}
		return -1
	}
	target := -1
	switch row.kind {
	case sbHeading:
		i := idx(row.itemKey)
		if down && i+1 < len(heads) {
			target = heads[i+1]
		} else if !down && i > 0 {
			target = heads[i-1]
		}
	case sbConv:
		i := idx("sectionHeading-" + row.section)
		if i < 0 {
			return
		}
		if down && i+1 < len(heads) {
			target = heads[i+1]
		} else if !down {
			target = heads[i]
		}
	}
	if target >= 0 {
		s.setActive(target)
		if !s.inView(target, 0) {
			s.scrollToPos(target, 0)
		}
	}
}

// sbActivate is onEnterKeyPressed: a heading toggles; another row clicks
// its link (a conversation in organize mode toggles its selection, Shift
// picking a range).
func (a *App) sbActivate(gtx layout.Context, row *sbRow, space, shift bool) {
	if row == nil {
		return
	}
	switch row.kind {
	case sbHeading:
		a.sbSetCollapsed(row, !a.sbCollapsed(row))
	case sbLink:
		if !a.sidebar.edit.on {
			a.Navigate(row.key)
		}
	case sbConv:
		if a.sidebar.edit.on {
			a.sbEditClick(row.key, shift)
			return
		}
		a.Navigate("c/" + row.key)
	case sbAction:
		a.sidebarActionAt(row.key, a.sbRowCenter(gtx, row))
	}
}

// sbRowCenter is a row's centre in window coordinates (a menu a key opens
// sits there: getTargetBoundsForContextMenu with keyboard focus).
func (a *App) sbRowCenter(gtx layout.Context, row *sbRow) image.Point {
	s := &a.sidebar
	p := s.sbPos(row.itemKey)
	if p < 0 || p+1 >= len(s.kb.rowOff) {
		return a.ptrWin
	}
	y := s.kb.rowOff[p] - s.scrollTop() + (s.kb.rowOff[p+1]-s.kb.rowOff[p])/2
	return s.origin.Add(image.Pt(gtx.Dp(unit.Dp(s.width))/2, s.listTop+y))
}

// sbCollapsed is a heading's state: organize mode's own, else the
// sidebar's.
func (a *App) sbCollapsed(row *sbRow) bool {
	if row.open {
		return false
	}
	if a.sidebar.edit.on {
		return a.sidebar.edit.collapsed[row.key]
	}
	return a.sidebar.collapsed[row.key]
}

// sbSetCollapsed is onChannelSectionCollapseToggle for one heading:
// organize mode's state there (setEditModeChannelSectionCollapsed), else
// the sidebar's.
func (a *App) sbSetCollapsed(row *sbRow, collapsed bool) {
	s := &a.sidebar
	if row.open { // the text filter shows every match expanded
		return
	}
	if s.edit.on {
		if s.edit.collapsed == nil {
			s.edit.collapsed = map[string]bool{}
		}
		s.edit.collapsed[row.key] = collapsed
	} else {
		s.collapsed[row.key] = collapsed
	}
	s.builtFor = -1
}

// sbSetAllCollapsed is the toggleAll case (bulkSet…Collapsed).
func (a *App) sbSetAllCollapsed(collapsed bool) {
	s := &a.sidebar
	for _, sec := range a.model().Sections {
		if s.edit.on {
			if s.edit.collapsed == nil {
				s.edit.collapsed = map[string]bool{}
			}
			s.edit.collapsed[sec.ID] = collapsed
		} else {
			s.collapsed[sec.ID] = collapsed
		}
	}
	s.builtFor = -1
}

// sbFocusRing draws the keyboard focus ring on a row (sz: the row, px):
// the item's :after 0 8px in (12px in the simplified layout), radius 8,
// one px of --sk_focused-shadow-color and its 5px halo at 30% outside it.
// That colour is --dt_color-otl-hgl-1, except in a light client's dark
// sidebar (.sk-client-theme--light-inverted-sidebar), where it's the
// sidebar's --dt_color-theme-content-inv-pry.
func (a *App) sbFocusRing(gtx layout.Context, r *sbRow, sz image.Point) {
	k := &a.sidebar.kb
	if !k.ring || r.itemKey == "" || r.itemKey != k.key || !gtx.Focused(&k.tag) {
		return
	}
	in := gtx.Dp(8)
	h := sz.Y
	if a.sbBBMBox(r) {
		in, h = gtx.Dp(12), a.sbRowH(gtx)
	}
	ring := a.sbFocusColor()
	// the halo stays translucent (it lies over the neighbours' tiles), its
	// alpha the one Gio's linear-light blend needs to land on the browser's
	// 30% mix over the sidebar
	halo := theme.LinearAlpha(theme.WithAlpha(ring, 0x4d), a.sbBase)
	popups.FocusRingColors(gtx, ring, halo, image.Rect(in, 0, sz.X-in, h), gtx.Dp(8))
}

// sbFocusColor is the sidebar's --sk_focused-shadow-color, the solid part
// of its focus rings (rows, the filter input): the sidebar's
// --dt_color-theme-content-inv-pry under
// .sk-client-theme--light-inverted-sidebar, else unset, so the rings fall
// back to --dt_color-otl-hgl-1.
func (a *App) sbFocusColor() color.NRGBA {
	if a.sbInverted() {
		return a.th.C.SidebarTextUnread
	}
	return theme.ButtonV2Focus(a.th)
}

// sbInverted reports whether the sidebar is Slack's light-inverted-sidebar
// theme scope: a light client with the theme's dark sidebar (focus mode's
// silent theme included).
func (a *App) sbInverted() bool {
	t := a.accountTheme()
	if ft, ok := a.focusTheme(); ok {
		t = ft
	}
	return !a.th.Dark && t.SidebarInverted
}

// sbBeforeList applies, once the rows are laid out for this frame, what
// changed outside the list: the scroll to put back (focusOnExitEditMode),
// the heading to keep in place (focusOnEditMode) and a focus asked for.
func (a *App) sbBeforeList(gtx layout.Context) {
	s := &a.sidebar
	if s.restore {
		s.restore = false
		s.setScrollTop(s.restoreTop)
	}
	if e := &s.edit; e.keep {
		e.keep = false
		if p := s.sbPos(e.keepKey); p >= 0 && p < len(s.kb.rowOff) {
			s.setScrollTop(s.kb.rowOff[p] - e.keepOff)
		}
	}
	if w := s.kb.wantFocus; w != "" {
		s.kb.wantFocus = ""
		if w == "first" {
			for p := range s.kb.order {
				if sbFocusable(s.rowAtPos(p)) {
					s.setActive(p)
					if !s.inView(p, 0) {
						s.scrollToPos(p, 0)
					}
					break
				}
			}
		}
		a.sbFocusList(gtx, w)
	}
}
