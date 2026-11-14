package ui

// Dragging the channel tab bar's tabs, as Slack's DraggableDroppableTabs
// (boot-shared E/Yw, CDN build 859cadc0e0ab) has it:
//
//   - With useCanEditTabs every tab that isn't static (Messages) is a
//     DraggableDroppable (isDragDisabled otherwise): drag source and drop
//     target, of type TAB on the bar and OVERFLOW_MENU as a row of the More
//     tab's menu (DraggableOverflowMenuTabItem). The drag starts past the
//     drag slop; the source goes to opacity .5 (channel_tab_is_dragging)
//     and a copy of the tab follows the pointer (DraggingTabPreviewLayer:
//     its top-left at the pointer, .channel_tab_dragging: base-pry, a 1px
//     otl-ter border, radius 6, box shadow 0 4px 12px theme-surf-sec).
//   - A target's drop position starts "left" on the bar, "top" in the
//     menu. On the bar's last visible tab it turns "left" under 40% of its
//     width and "right" past 60%; on the menu's last row "top" and
//     "bottom" by height. Over a folder a dragged canvas or list (anything
//     but a folder, Workflows, Files & links or Pins:
//     shouldPreventDropIntoFolder) takes calculateHorizontalDropPosition /
//     calculateVerticalDropPosition (dead zone .1: the near side under
//     15%, the centre from 35% to 65%, the far side past 85% on the last
//     one, else unchanged).
//   - The indicator on the hovered target that isn't the source
//     (getHoverClassNames): a 2px base-inv-hgl-1 line at a tab's left
//     (margin-left -2px), at its right for "right"; in the menu at a row's
//     top, at its bottom for "bottom"; a folder's centre is base-ter
//     behind it (radius 4 4 1 1).
//   - The More tab (DroppableOverflowMenuTab) takes every tab: hovered it
//     is base-ter (draggable_overflow_menu_hovered); hovering it or the
//     open menu for 500 ms opens the menu, leaving both for 500 ms closes a
//     menu the drag opened; a drop on it moves the tab to the end.
//   - The drop (calculateDropIndex): the target's index; on the bar one
//     less for a tab's left side when it is the last one (a bar tab) or
//     when the bar tab came from before it, one more for the last tab's
//     right side from the menu; in the menu one less for the last row's
//     top. Then updateTabOrderByIndex (core.MoveTabByIndex) when the index
//     moved: the tab goes there at once and conversations.moveTab
//     {channel_id, id, after} goes out (a failure re-reads the bar). A drop
//     on a folder's centre is the add-to-folder thunk (E/Yw tf): the file's
//     bookmark in the folder and removeTab, then "Added {itemName} to
//     <link>{folderName}</link>" for 5 s (the link opens the folder),
//     "{folderName} already includes {itemName}" when it is there, "Unable
//     to add item to folder" on a failure.
//
// The pointer handlers cover the bar and the open menu and pass events
// through, so clicks and right-clicks still work; a drop swallows the bar
// source's click. Positions are the bar's frame (the handlers are clipped,
// not offset; the menu's is deferred above the overlays in that frame),
// and there is no grab: Gio keeps sending a pressed handler its drags
// outside its area.

import (
	"context"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tabRect is one drawn tab (or More menu row), last frame, bar frame.
type tabRect struct {
	key, id, typ  string
	label, fileID string
	folder        string // a folder tab's folder id
	idx           int    // in channelTabs
	r             image.Rectangle
	drag          bool // a DraggableDroppable
	last          bool // isLastVisibleTab / isLastItemInOverflowMenu
	inMenu        bool // an OVERFLOW_MENU row
	more          bool // the More tab (DroppableOverflowMenuTab)
}

type tabDragState struct {
	tag, menuTag int // the bar's and the open menu's pointer handlers
	ch           string
	rects        []tabRect
	all          []chTab
	last, cur    int
	more         image.Rectangle // the More tab, empty without one
	barOff       image.Point     // window − bar frame, from the press
	src          tabRect
	pressed      bool
	pressAt      f32.Point
	dragging     bool
	at           f32.Point         // the pointer, bar frame
	pos          map[string]string // each target's drop position
	swallow      bool              // a drop ended this frame: the source's click doesn't count
	overSince    time.Time         // over More or its menu since (the 500 ms open)
	awaySince    time.Time         // off both since (the 500 ms close)
	dragMenu     bool              // the drag opened the menu
	canEdit      bool
}

var tabDrags sync.Map // *App -> *tabDragState (UI goroutine only)

func (a *App) tabDrag() *tabDragState {
	if v, ok := tabDrags.Load(a); ok {
		return v.(*tabDragState)
	}
	d := &tabDragState{}
	tabDrags.Store(a, d)
	return d
}

// TabDragging reports a tab drag in ch (tests).
func (a *App) TabDragging(ch string) bool {
	d := a.tabDrag()
	return d.ch == ch && d.dragging
}

// tabDragSlop is the drag threshold (Chromium's, 4 px).
const tabDragSlop = 4

// tabsMoreHoverDelay is useDelayedValue's 500 ms on More's isOpen.
const tabsMoreHoverDelay = 500 * time.Millisecond

// preventDropIntoFolder is shouldPreventDropIntoFolder for a dragged tab.
func preventDropIntoFolder(t tabRect) bool {
	return t.typ == "folder" || t.id == "workflows" || t.id == "files" || t.id == "bookmarks"
}

// hoverDropPosition is DraggableDroppable's hover for dragged src over
// target tgt at rel (0..1 across a tab, down a menu row), from cur.
func hoverDropPosition(src, tgt tabRect, rel float32, cur string) string {
	near, far := "left", "right"
	if tgt.inMenu {
		near, far = "top", "bottom"
	}
	folder := tgt.typ == "folder"
	if !tgt.last && !folder {
		return cur
	}
	if folder && !preventDropIntoFolder(src) {
		const dz = .1
		switch {
		case rel < .25-dz:
			return near
		case tgt.last && rel > .75+dz:
			return far
		case rel > .25+dz && rel < .75-dz:
			return "center"
		}
		return cur
	}
	if !tgt.last {
		return near
	}
	switch {
	case rel < .4:
		return near
	case rel > .6:
		return far
	}
	return cur
}

// dropIndex is calculateDropIndex for src dropped on tgt at pos.
func dropIndex(src, tgt tabRect, pos string) int {
	l := tgt.idx
	if tgt.inMenu {
		if pos == "top" && tgt.last {
			l--
		}
		return l
	}
	if pos == "left" && tgt.last && !src.inMenu {
		l--
	}
	if pos == "right" && tgt.last && src.inMenu {
		l++
	}
	if !src.inMenu && src.idx < tgt.idx && !tgt.last {
		l--
	}
	return l
}

// menuRects are the open More menu's tab rows in the bar's frame.
func (a *App) menuRects(d *tabDragState) (rows []tabRect, card image.Rectangle) {
	m := a.tabsMoreFor(d.ch).openMenu()
	if m == nil {
		return nil, image.Rectangle{}
	}
	rr := m.RowRects()
	n := len(d.all) - 1 - d.last
	for i := 0; i < n && i < len(rr); i++ {
		t := d.all[d.last+1+i]
		id := strings.TrimPrefix(t.key, "tab:")
		rows = append(rows, tabRect{key: t.key, id: id, typ: t.typ, label: t.label, fileID: t.fileID, folder: t.folder,
			idx: d.last + 1 + i, r: rr[i].Sub(d.barOff), drag: d.canEdit && core.DraggableTab(id, t.typ), last: i == n-1, inMenu: true})
	}
	return rows, m.Rect().Sub(d.barOff)
}

// overMore reports a drag over the More tab.
func (d *tabDragState) overMore() bool {
	return d.dragging && !d.more.Empty() && d.at.Round().In(d.more)
}

// tabDragEvents reads the bar's and the menu's pointer events (before
// the tabs draw).
func (a *App) tabDragEvents(gtx layout.Context, ch string, canEdit bool) {
	d := a.tabDrag()
	d.swallow = false
	if d.ch != ch {
		d.ch, d.pressed, d.dragging, d.rects = ch, false, false, nil
	}
	d.canEdit = canEdit
	for _, tag := range []*int{&d.tag, &d.menuTag} {
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok {
				a.tabDragEvent(gtx, d, e, tag == &d.menuTag)
			}
		}
	}
	if d.dragging && !canEdit {
		d.pressed, d.dragging = false, false
	}
	if d.dragging {
		a.tabsMoreHover(gtx, d)
	}
}

func (a *App) tabDragEvent(gtx layout.Context, d *tabDragState, e pointer.Event, inMenu bool) {
	switch e.Kind {
	case pointer.Press:
		d.pressed, d.dragging = false, false
		if !d.canEdit || e.Buttons != pointer.ButtonPrimary || e.Modifiers != 0 {
			return
		}
		d.barOff = a.ptrWin.Sub(e.Position.Round())
		p := e.Position.Round()
		srcs := d.rects
		if inMenu {
			srcs, _ = a.menuRects(d)
		}
		for _, t := range srcs {
			if t.drag && p.In(t.r) {
				d.src, d.pressed, d.pressAt, d.at = t, true, e.Position, e.Position
			}
		}
	case pointer.Drag:
		if !d.pressed {
			return
		}
		d.at = e.Position
		if !d.dragging {
			dx, dy := e.Position.X-d.pressAt.X, e.Position.Y-d.pressAt.Y
			if s := float32(gtx.Dp(tabDragSlop)); dx*dx+dy*dy > s*s {
				d.dragging, d.pos = true, map[string]string{}
				d.overSince, d.awaySince, d.dragMenu = time.Time{}, time.Time{}, false
			}
		}
		if tgt, ok := a.dragTarget(d); ok && !tgt.more {
			var rel float32
			if tgt.inMenu {
				rel = (d.at.Y - float32(tgt.r.Min.Y)) / float32(max(tgt.r.Dy(), 1))
			} else {
				rel = (d.at.X - float32(tgt.r.Min.X)) / float32(max(tgt.r.Dx(), 1))
			}
			d.pos[tgt.key] = hoverDropPosition(d.src, tgt, rel, d.posOf(tgt))
		}
		a.Invalidate()
	case pointer.Release:
		if d.dragging {
			d.swallow = true
			if tgt, ok := a.dragTarget(d); ok {
				a.dropTab(d.ch, d.src, tgt, d.posOf(tgt))
			}
			if m := a.tabsMoreFor(d.ch).openMenu(); m != nil && (d.dragMenu || d.src.inMenu) {
				m.Dismiss()
			}
		}
		d.pressed, d.dragging = false, false
		a.Invalidate()
	case pointer.Cancel:
		d.pressed, d.dragging = false, false
		a.Invalidate()
	}
}

// tabsMoreHover is More's delayed isOpen while dragging: 500 ms over More
// or the menu opens it, 500 ms off both closes the one the drag opened.
func (a *App) tabsMoreHover(gtx layout.Context, d *tabDragState) {
	if d.more.Empty() {
		return
	}
	st := a.tabsMoreFor(d.ch)
	_, card := a.menuRects(d)
	over := d.overMore() || !card.Empty() && d.at.Round().In(card)
	now := gtx.Now
	if over {
		d.awaySince = time.Time{}
		if d.overSince.IsZero() {
			d.overSince = now
		}
		if st.openMenu() == nil {
			if now.Sub(d.overSince) >= tabsMoreHoverDelay {
				a.openTabsMore(d.ch, d.all, d.last, d.cur, d.more.Add(d.barOff))
				d.dragMenu = true
			} else {
				gtx.Execute(op.InvalidateCmd{At: d.overSince.Add(tabsMoreHoverDelay)})
			}
		}
		return
	}
	d.overSince = time.Time{}
	if m := st.openMenu(); m != nil && d.dragMenu {
		if d.awaySince.IsZero() {
			d.awaySince = now
		}
		if now.Sub(d.awaySince) >= tabsMoreHoverDelay {
			m.Dismiss()
			d.dragMenu = false
		} else {
			gtx.Execute(op.InvalidateCmd{At: d.awaySince.Add(tabsMoreHoverDelay)})
		}
	}
}

// dragTarget is the drop target under the pointer that isn't the source
// (isOver && canDrop && !(isDragging && isOver)): the menu's rows first
// (they're on top), then More, then the bar's tabs.
func (a *App) dragTarget(d *tabDragState) (tabRect, bool) {
	if !d.dragging {
		return tabRect{}, false
	}
	p := d.at.Round()
	rows, card := a.menuRects(d)
	if !card.Empty() && p.In(card) {
		for _, t := range rows {
			if t.drag && t.key != d.src.key && p.In(t.r) {
				return t, true
			}
		}
		return tabRect{}, false
	}
	if d.overMore() {
		return tabRect{key: "more", more: true, r: d.more}, true
	}
	for _, t := range d.rects {
		if t.drag && t.key != d.src.key && p.In(t.r) {
			return t, true
		}
	}
	return tabRect{}, false
}

func (d *tabDragState) posOf(t tabRect) string {
	if p := d.pos[t.key]; p != "" {
		return p
	}
	if t.inMenu {
		return "top"
	}
	return "left"
}

// dropTab is the target's drop: into a folder from its centre, the end
// from More, else updateTabOrderByIndex.
func (a *App) dropTab(ch string, src, tgt tabRect, pos string) {
	c := a.Core()
	if c == nil {
		return
	}
	if tgt.typ == "folder" && pos == "center" {
		if preventDropIntoFolder(src) || src.typ != "canvas" && src.typ != "list" || src.fileID == "" {
			return // mapTabToBookmarkData: only a file's tab has a bookmark to add
		}
		a.dropTabInFolder(ch, src, tgt)
		return
	}
	all := a.channelTabs(ch)
	dst := dropIndex(src, tgt, pos)
	if tgt.more {
		dst = len(all) - 1 // onDrop(index, tabsCount - 1)
	}
	if dst == src.idx {
		return
	}
	bar := make([]core.TabState, len(all))
	for i, t := range all {
		bar[i] = core.TabState{ID: strings.TrimPrefix(t.key, "tab:"), Type: t.typ}
		if t.key == "messages" {
			bar[i] = core.TabState{ID: "channel", Type: "channel"}
		}
	}
	id, after, ok := core.MoveTabByIndex(bar, src.idx, dst)
	if !ok {
		return
	}
	a.moveCachedTab(ch, id, after)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := c.MoveTab(ctx, ch, id, after); err != nil {
			a.TabsChanged(ch) // setTabs(): the server's order back
		}
	}()
}

// moveCachedTab is updateTabOrder's optimistic tabz: tab id right after
// after (first without one), the open tab kept.
func (a *App) moveCachedTab(ch, id, after string) {
	if a.data == nil {
		return
	}
	a.keepTabSelection(ch, func() {
		tabs := a.data.tabCache[ch]
		from := -1
		for i, t := range tabs {
			if t.key == "tab:"+id {
				from = i
			}
		}
		if from < 0 {
			return
		}
		t := tabs[from]
		rest := append(append([]chTab{}, tabs[:from]...), tabs[from+1:]...)
		at := 0
		for i, x := range rest {
			if x.key == "tab:"+after {
				at = i + 1
			}
		}
		a.data.tabCache[ch] = append(append(append([]chTab{}, rest[:at]...), t), rest[at:]...)
	})
	a.Invalidate()
}

// keepTabSelection runs change on ch's cached tabs and keeps the open tab
// open by its key (Messages when it went away).
func (a *App) keepTabSelection(ch string, change func()) {
	i, had := a.chTab[ch]
	key := ""
	if tabs := a.channelTabs(ch); had && i > 0 && i < len(tabs) {
		key = tabs[i].key
	}
	change()
	if key == "" {
		return
	}
	for j, t := range a.channelTabs(ch) {
		if t.key == key {
			a.chTab[ch] = j
			return
		}
	}
	a.chTab[ch] = 0
}

// dropTabInFolder is E/Yw tf for a tab: its file into the folder, the tab
// removed, and the toast.
func (a *App) dropTabInFolder(ch string, src, tgt tabRect) {
	c := a.Core()
	ns := i18n.NS("tabbed-channel")
	folderName := tgt.label
	tabID := strings.TrimPrefix(tgt.key, "tab:")
	label := ""
	for _, t := range a.channelTabs(ch) {
		if t.key == src.key {
			label = t.rawLabel
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		name, already, err := c.AddTabToFolder(ctx, ch, src.id, label, src.fileID, tgt.folder)
		if name == "" {
			name = ns.T("Tab")
		}
		a.Post(func() {
			switch {
			case err != nil:
				a.ToastParts([]i18n.Part{{Text: ns.T("Unable to add item to folder")}}, "", color.NRGBA{}, 5*time.Second)
			case already:
				a.ToastParts([]i18n.Part{{Text: ns.T("{folderName} already includes {itemName}", "folderName", folderName, "itemName", name)}},
					"", color.NRGBA{}, 5*time.Second)
			default:
				parts := ns.RT("Added {itemName} to <link>{folderName}</link>", "itemName", name, "folderName", folderName)
				a.ToastLink(parts, "slack://channel?id="+ch+"&tab="+tabID, 5*time.Second)
				a.TabRemoved(ch, src.id)
			}
		})
	}()
}

// tabDropColor is --dt_color-base-inv-hgl-1 (the theme's when it sets it).
func (a *App) tabDropColor() color.NRGBA {
	c := color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}
	if a.th.Dark {
		c = color.NRGBA{R: 0x0a, G: 0x77, B: 0xa7, A: 0xff}
	}
	if t := a.th.Tokens; t != nil && t.Overridden() {
		theme.SetColor(t, &c, "var(--dt_color-base-inv-hgl-1)")
	}
	return c
}

// fillDropTarget is a folder's centre or the hovered More: base-ter,
// radius 4 4 1 1, at alpha.
func (a *App) fillDropTarget(gtx layout.Context, r image.Rectangle, alpha uint8) {
	bt := color.NRGBA{R: 0xea, G: 0xea, B: 0xea, A: 0xff}
	if a.th.Dark {
		bt = color.NRGBA{R: 0x21, G: 0x24, B: 0x28, A: 0xff}
	}
	if t := a.th.Tokens; t != nil && t.Overridden() {
		theme.SetColor(t, &bt, "var(--dt_color-base-ter)")
	}
	bt.A = uint8(int(bt.A) * int(alpha) / 255)
	rr := clip.RRect{Rect: r, NE: gtx.Dp(4), NW: gtx.Dp(4), SE: gtx.Dp(1), SW: gtx.Dp(1)}
	paint.FillShape(gtx.Ops, bt, rr.Op(gtx.Ops))
}

// dropIndicator draws getHoverClassNames' mark for tgt at pos.
func (a *App) dropIndicator(gtx layout.Context, tgt tabRect, pos string) {
	col := a.tabDropColor()
	bw := gtx.Dp(2)
	r := tgt.r
	switch {
	case tgt.typ == "folder" && pos == "center":
		// .channel_tab_folder_hovered (behind the tab in Slack; a menu
		// row's label is already drawn, so it goes over it translucent)
		alpha := uint8(0xff)
		if tgt.inMenu {
			alpha = 0x99
		}
		a.fillDropTarget(gtx, r, alpha)
	case tgt.inMenu && pos == "bottom" && tgt.last:
		theme.Fill(gtx, image.Rect(r.Min.X, r.Max.Y, r.Max.X, r.Max.Y+bw), col)
	case tgt.inMenu:
		theme.Fill(gtx, image.Rect(r.Min.X, r.Min.Y-bw, r.Max.X, r.Min.Y), col)
	case pos == "right" && tgt.last:
		theme.Fill(gtx, image.Rect(r.Max.X, r.Min.Y, r.Max.X+bw, r.Max.Y), col)
	default:
		theme.Fill(gtx, image.Rect(r.Min.X-bw, r.Min.Y, r.Min.X, r.Max.Y), col)
	}
}

// tabDragOverlay draws the bar's indicator, registers the bar's handler
// (over the tabs, passing events through) and defers, above the overlays,
// the menu's handler, a menu row's indicator and the preview of the
// dragged tab at the pointer. sz is the bar's size.
func (a *App) tabDragOverlay(gtx layout.Context, sz image.Point) {
	d := a.tabDrag()
	tgt, hasTgt := a.dragTarget(d)
	if hasTgt && !tgt.inMenu && !tgt.more {
		a.dropIndicator(gtx, tgt, d.posOf(tgt))
	}
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &d.tag)
		if d.pressed && !d.dragging {
			pointer.CursorGrab.Add(gtx.Ops) // .channel_tab_draggable:active
		}
	}()
	_, card := a.menuRects(d)
	if card.Empty() && !d.dragging {
		return
	}
	m := op.Record(gtx.Ops)
	if !card.Empty() && d.canEdit {
		func() {
			defer clip.Rect(card).Push(gtx.Ops).Pop()
			defer pointer.PassOp{}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &d.menuTag)
		}()
	}
	if hasTgt && tgt.inMenu {
		a.dropIndicator(gtx, tgt, d.posOf(tgt))
	}
	if d.dragging {
		a.dragPreview(gtx, d, sz.Y)
	}
	op.Defer(gtx.Ops, m.Stop())
}

// dragPreview is DraggingTabPreviewLayer: the source tab (unselected) in
// .channel_tab_dragging at the pointer, h the bar's height.
func (a *App) dragPreview(gtx layout.Context, d *tabDragState, h int) {
	if d.src.idx < 0 || d.src.idx >= len(d.all) {
		return
	}
	t := d.all[d.src.idx]
	lw := a.th.Measure(gtx, 13, theme.Bold, t.label)
	if t.spans != nil {
		lw = a.spansWidth(gtx, t.spans, 13)
	}
	w := gtx.Dp(8) + gtx.Dp(16) + gtx.Dp(4) + lw + gtx.Dp(8)
	off := op.Offset(d.at.Round()).Push(gtx.Ops)
	defer off.Pop()
	r := image.Rectangle{Max: image.Pt(w, h)}
	sh := color.NRGBA{R: 97, G: 31, B: 105, A: 18} // theme-surf-sec: aubergine-80 at .07
	if a.th.Dark {
		sh = color.NRGBA{R: 125, G: 57, B: 134, A: 77} // aubergine-30 at .3
	}
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), gtx.Dp(6), gtx.Dp(12), sh)
	one := max(1, gtx.Dp(1))
	theme.RRect(gtx, r, gtx.Dp(6), theme.OtlTer(a.th))
	theme.RRect(gtx, r.Inset(one), gtx.Dp(6)-one, a.th.C.Bg)
	a.tabContent(gtx, t, lw, h, false)
}
