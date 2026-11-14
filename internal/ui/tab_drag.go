package ui

// Dragging the channel tab bar's tabs, as Slack's DraggableDroppableTabs
// (boot-shared E/Yw, CDN build 859cadc0e0ab) has it:
//
//   - With useCanEditTabs every tab that isn't static (Messages) is a
//     DraggableDroppable (isDragDisabled otherwise): drag source and drop
//     target. The drag starts past the platform's drag slop; the source tab
//     goes to opacity .5 (channel_tab_is_dragging) and a copy of it follows
//     the pointer (DraggingTabPreviewLayer: its top-left at the pointer,
//     .channel_tab_dragging: base-pry, a 1px otl-ter border, radius 6, box
//     shadow 0 4px 12px theme-surf-sec).
//   - A target's drop position starts "left". Over the last visible tab it
//     turns "left" below 40% of its width and "right" past 60%. Over a
//     folder tab a dragged canvas or list (anything but a folder, Workflows,
//     Files & links or Pins: shouldPreventDropIntoFolder) takes
//     calculateHorizontalDropPosition (dead zone .1: left under 15%, centre
//     from 35% to 65%, right past 85% on the last tab, else unchanged).
//   - The indicator, on the hovered target that isn't the source
//     (getHoverClassNames): a 2px base-inv-hgl-1 line at the tab's left
//     (margin-left -2px), at its right for "right"; a folder's centre is
//     base-ter behind the tab (radius 4 4 1 1).
//   - The drop (calculateDropIndex): the target's index, one less for its
//     left side when it is the last tab or when the source came from
//     before it; then updateTabOrderByIndex (core.MoveTabByIndex) when the
//     index moved: the tab goes there at once and conversations.moveTab
//     {channel_id, id, after} goes out (a failure re-reads the bar). A drop
//     on a folder's centre is the add-to-folder thunk (E/Yw tf): the file's
//     bookmark in the folder and removeTab, then "Added {itemName} to
//     <link>{folderName}</link>" for 5 s (the link opens the folder), "{folderName}
//     already includes {itemName}" when it is there, "Unable to add item to
//     folder" on a failure.
//
// The pointer handler covers the bar and passes events through, so the
// tabs' clicks and right-clicks still work; a drop swallows the source
// tab's click. Positions are the bar's frame (the handler is clipped, not
// offset), and there is no grab: Gio keeps sending a pressed handler its
// drags outside its area.

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

// tabRect is one drawn tab of the bar, last frame.
type tabRect struct {
	key, id, typ  string
	label, fileID string
	folder        string // a folder tab's folder id
	idx           int    // in channelTabs
	r             image.Rectangle
	drag          bool // a DraggableDroppable
	last          bool // isLastVisibleTab
}

type tabDragState struct {
	tag      int // the bar's pointer handler
	ch       string
	rects    []tabRect
	press    int // rects index pressed (-1 none)
	pressAt  f32.Point
	dragging bool
	at       f32.Point         // the pointer, bar frame
	pos      map[string]string // each target's drop position
	swallow  bool              // a drop ended this frame: the source's click doesn't count
}

var tabDrags sync.Map // *App -> *tabDragState (UI goroutine only)

func (a *App) tabDrag() *tabDragState {
	if v, ok := tabDrags.Load(a); ok {
		return v.(*tabDragState)
	}
	d := &tabDragState{press: -1}
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

// preventDropIntoFolder is shouldPreventDropIntoFolder for a dragged tab.
func preventDropIntoFolder(t tabRect) bool {
	return t.typ == "folder" || t.id == "workflows" || t.id == "files" || t.id == "bookmarks"
}

// hoverDropPosition is DraggableDroppable's hover for dragged src over
// target tgt at rel (0..1 across it), from the position cur.
func hoverDropPosition(src, tgt tabRect, rel float32, cur string) string {
	folder := tgt.typ == "folder"
	if !tgt.last && !folder {
		return cur
	}
	if folder && !preventDropIntoFolder(src) {
		const dz = .1
		switch {
		case rel < .25-dz:
			return "left"
		case tgt.last && rel > .75+dz:
			return "right"
		case rel > .25+dz && rel < .75-dz:
			return "center"
		}
		return cur
	}
	if !tgt.last {
		return "left"
	}
	switch {
	case rel < .4:
		return "left"
	case rel > .6:
		return "right"
	}
	return cur
}

// dropIndex is calculateDropIndex for a tab dropped on target at pos.
func dropIndex(srcIdx int, tgt tabRect, pos string) int {
	l := tgt.idx
	if pos == "left" && tgt.last {
		l--
	}
	if srcIdx < tgt.idx && !tgt.last {
		l--
	}
	return l
}

// tabDragEvents reads the bar's pointer events (before the tabs draw).
func (a *App) tabDragEvents(gtx layout.Context, ch string, canEdit bool) {
	d := a.tabDrag()
	d.swallow = false
	if d.ch != ch {
		d.ch, d.press, d.dragging, d.rects = ch, -1, false, nil
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &d.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			d.press, d.dragging = -1, false
			if !canEdit || e.Buttons != pointer.ButtonPrimary || e.Modifiers != 0 {
				continue
			}
			for i, t := range d.rects {
				if t.drag && e.Position.Round().In(t.r) {
					d.press, d.pressAt, d.at = i, e.Position, e.Position
				}
			}
		case pointer.Drag:
			if d.press < 0 {
				continue
			}
			d.at = e.Position
			if !d.dragging {
				dx, dy := e.Position.X-d.pressAt.X, e.Position.Y-d.pressAt.Y
				if dx*dx+dy*dy > float32(gtx.Dp(tabDragSlop)*gtx.Dp(tabDragSlop)) {
					d.dragging, d.pos = true, map[string]string{}
				}
			}
			if d.dragging {
				if tgt, ok := d.target(); ok {
					rel := (d.at.X - float32(tgt.r.Min.X)) / float32(max(tgt.r.Dx(), 1))
					cur := d.pos[tgt.key]
					if cur == "" {
						cur = "left"
					}
					d.pos[tgt.key] = hoverDropPosition(d.rects[d.press], tgt, rel, cur)
				}
			}
			a.Invalidate()
		case pointer.Release:
			if d.dragging {
				d.swallow = true
				if tgt, ok := d.target(); ok {
					a.dropTab(ch, d.rects[d.press], tgt, d.posOf(tgt))
				}
			}
			d.press, d.dragging = -1, false
			a.Invalidate()
		case pointer.Cancel:
			d.press, d.dragging = -1, false
			a.Invalidate()
		}
	}
	if d.dragging && !canEdit {
		d.press, d.dragging = -1, false
	}
}

// target is the drop target under the pointer that isn't the source
// (isOver && canDrop && !(isDragging && isOver)).
func (d *tabDragState) target() (tabRect, bool) {
	if !d.dragging || d.press < 0 || d.press >= len(d.rects) {
		return tabRect{}, false
	}
	p := d.at.Round()
	for i, t := range d.rects {
		if t.drag && i != d.press && p.In(t.r) {
			return t, true
		}
	}
	return tabRect{}, false
}

func (d *tabDragState) posOf(t tabRect) string {
	if p := d.pos[t.key]; p != "" {
		return p
	}
	return "left"
}

// dropTab is the target's drop: into a folder from its centre, else
// updateTabOrderByIndex.
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
	dst := dropIndex(src.idx, tgt, pos)
	if dst == src.idx {
		return
	}
	all := a.channelTabs(ch)
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

// tabDragOverlay draws the indicator on the hovered target, registers
// the bar's handler (over the tabs, passing events through) and defers
// the preview of the dragged tab at the pointer. sz is the bar's size.
func (a *App) tabDragOverlay(gtx layout.Context, sz image.Point, drawTab func(gtx layout.Context, t tabRect)) {
	d := a.tabDrag()
	if tgt, ok := d.target(); ok {
		pos := d.posOf(tgt)
		col := a.tabDropColor()
		bw := gtx.Dp(2)
		r := tgt.r
		switch {
		case tgt.typ == "folder" && pos == "center":
			// .channel_tab_folder_hovered: base-ter, radius 4 4 1 1 (under
			// the tab's own drawing in Slack; Clack's tab has no fill there)
			bt := color.NRGBA{R: 0xea, G: 0xea, B: 0xea, A: 0xff}
			if a.th.Dark {
				bt = color.NRGBA{R: 0x21, G: 0x24, B: 0x28, A: 0xff}
			}
			if t := a.th.Tokens; t != nil && t.Overridden() {
				theme.SetColor(t, &bt, "var(--dt_color-base-ter)")
			}
			rr := clip.RRect{Rect: r, NE: gtx.Dp(4), NW: gtx.Dp(4), SE: gtx.Dp(1), SW: gtx.Dp(1)}
			paint.FillShape(gtx.Ops, bt, rr.Op(gtx.Ops))
		case pos == "right" && tgt.last:
			theme.Fill(gtx, image.Rect(r.Max.X, r.Min.Y, r.Max.X+bw, r.Max.Y), col)
		default:
			theme.Fill(gtx, image.Rect(r.Min.X-bw, r.Min.Y, r.Min.X, r.Max.Y), col)
		}
	}
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &d.tag)
		if d.press >= 0 && !d.dragging {
			pointer.CursorGrab.Add(gtx.Ops) // .channel_tab_draggable:active
		}
	}()
	if !d.dragging || d.press < 0 || d.press >= len(d.rects) {
		return
	}
	src := d.rects[d.press]
	m := op.Record(gtx.Ops)
	off := op.Offset(d.at.Round()).Push(gtx.Ops)
	r := image.Rectangle{Max: src.r.Size()}
	sh := color.NRGBA{R: 97, G: 31, B: 105, A: 18} // theme-surf-sec: aubergine-80 at .07
	if a.th.Dark {
		sh = color.NRGBA{R: 125, G: 57, B: 134, A: 77} // aubergine-30 at .3
	}
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), gtx.Dp(6), gtx.Dp(12), sh)
	bg, line := a.th.C.Bg, theme.OtlTer(a.th)
	theme.RRect(gtx, r, gtx.Dp(6), line)
	one := max(1, gtx.Dp(1))
	theme.RRect(gtx, r.Inset(one), gtx.Dp(6)-one, bg)
	drawTab(gtx, src)
	off.Pop()
	op.Defer(gtx.Ops, m.Stop())
}
