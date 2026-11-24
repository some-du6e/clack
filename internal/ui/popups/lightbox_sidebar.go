package popups

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The media viewer's sidebar: Slack's MediaViewer (bDav,
// gantry-v2-async-media-viewer.4072df5d8a167f72, build d4845cd3e2ea) lays
// its modal out as MediaViewerSidebarRow: the media column, then, while
// the sidebar is open (iU) and has a tab, a Slack Kit Tabs flexpane (tt5m
// Tabs with 4Tgw TabsItem: p-media_viewer_modal__flexpane
// p-media_viewer_modal__tab_flexpane) on the right, then the close button
// (renderCloseButton), which moves from the media column into the row.
//
//   - The tabs are bDav's a2, in order: Thread (channel and thread), the
//     Transcript, then Details, last. Details is pushed when
//     media_viewer_file_details_tab is in treatment (2mfZ zq) and the item
//     has details (tn: a real file id, Download and Copy link not hidden),
//     once it was chosen (iQ) or beside another tab. Clack has no Thread
//     or Transcript tab yet, so Details comes only from the More actions
//     menu ("View file details", "View details": td → aD) and is then the
//     only tab (--single_tab).
//   - The sidebar is min(max-width, half the row) wide (.p-media_viewer_
//     modal__tab_flexpane flex:1 beside the media column's flex:1, capped
//     at 400px, 360px in a window up to 1440 wide, 300px up to 1280),
//     sk_primary_background with a 1px rgba(sk_foreground_low,.13) line
//     over the media column's last pixels (box-shadow -1px 0, z-index 1).
//   - The tab bar (.p-media_viewer_modal__tabs) is 50px tall with 12px in
//     on the left and the otl-ter line on its bottom row; a tab is its 15px
//     Black label (margin 0 8px, 20px more after all but the last),
//     content-sec, content-pry hovered or selected, the selected one with a
//     2px theme-base-pry underline. A lone tab has no underline and the
//     arrow cursor, but a keyboard-focused one shows the ring and the
//     underline (.c-tabs__tab--active.c-button--focus-visible:focus).
//   - The tab panel under it is FileDetails (eg5B) with its header hidden,
//     the app's (FileDetailsHost.FileDetailsPane), for the item shown: a
//     new item asks for a new pane, as FileDetails refetches on a new
//     fileId.
//   - The close button sits at the row's top right (top 8, right 8), a
//     default-style 36px icon button: "Close details" when Details is the
//     selected tab and the viewer has no thread (it closes the sidebar
//     only, then focuses the media column's close), else "Close" (the
//     viewer).
//   - Escape closes the whole viewer, sidebar open or not; ← and → still
//     move between items and a selected Details follows the new item, or
//     resets (and the sidebar closes when no tab is left) on an item
//     without details. Nothing persists: every viewer opens closed.
//
// Not yet: the Thread and Transcript tabs (and Show thread opening the
// Thread tab), the resizable sidebar (media_viewer_resizable_thread, off
// for the account), and the 125ms underline transition.

// lbTabDetails is the Details tab's id.
const lbTabDetails = "details"

// lbTab is one of the sidebar's tabs (an entry of bDav's a2).
type lbTab struct {
	id, title string
}

// lbSidebar is the viewer's sidebar state: bDav's useStates, reset with
// each viewer (nothing persists).
type lbSidebar struct {
	open          bool   // iU: the sidebar is open
	detailsOpened bool   // iQ: Details was chosen
	tab           string // af: the selected tab (currentTabId, transcribe_opt_in_ui_v2 on)
	// focusClose is aL: focus the media column's close button once the
	// sidebar has gone (aO, "Close details")
	focusClose bool

	// shown is whether the sidebar is drawn this frame (open with a tab),
	// tabs its tabs and active the one shown
	shown  bool
	tabs   []lbTab
	active string

	pane  FileDetailsPane // the Details tab's FileDetails, for pane.FileID()
	block blocker         // the sidebar takes its pointer (the background's press closes the viewer)

	clicks   []gesture.Click // the tabs' presses
	tabFocus int             // the tablist's one tab stop (4Tgw's roving tabIndex)
	focusIdx int             // the tab holding it

	// as last drawn (window coordinates): the row (the modal's panel),
	// the sidebar in it, the media column, the close button
	row, rect, media, closeR image.Rectangle
	tabR                     []image.Rectangle
	underline                []bool // which tabs drew the selected underline
	closeLabel               string
}

// The close button's Slack Kit tokens (light, dark).
var (
	tokBaseTer        = chipTok{"var(--dt_color-base-ter)", rgb(0xeaeaea), rgb(0x212428)}
	tokBaseTerPressed = chipTok{"var(--dt_color-base-ter-pressed)", rgba8(0x454447, 0x21), rgba8(0xf8f8f8, 0x2e)}
)

// detailsAvailable is bDav's tr: the item is a real file (iP, oRye
// looksLikeFileId on its id) whose Download and Copy link the opener
// didn't hide (a9, a7). Clack's openers hide neither, and Slack's
// showDownloadAndShareActions (K8Ba ZR) isn't ported, so it reads as
// true. A pending file (not sent yet) has none.
func (lb *Lightbox) detailsAvailable() bool {
	if lb.pending || lb.idx < 0 || lb.idx >= len(lb.items) {
		return false
	}
	f := lb.cur().file
	return f != nil && LooksLikeFileID(f.ID)
}

// detailsTab is bDav's tn: the gate on (ia) and details available (tr).
func (lb *Lightbox) detailsTab() bool {
	return lb.detailsAvailable() && mediaViewerDetailsOn(lb.h)
}

// hasThread is bDav's e && i: the viewer has a channel and a thread
// (threadTs: the message's thread_ts or its own ts).
func (lb *Lightbox) hasThread() bool {
	return lb.m.Channel != "" && (lb.m.ThreadTS != "" || lb.m.TS != "")
}

// viewerChannel is the conversation the viewer was opened in, ""
// without one: FileDetails' channelId.
func (lb *Lightbox) viewerChannel() string {
	if lb.m.Channel != "" {
		return lb.m.Channel
	}
	return lb.channel
}

// defaultTab is bDav's aA: aw (the transcript, or the thread when the
// message has replies) or "thread" with a channel and a thread. Clack has
// neither tab yet; the id still decides what Details resets to.
func (lb *Lightbox) defaultTab() string {
	if lb.hasThread() {
		return "thread"
	}
	return ""
}

// sideTabs is bDav's a2 for the item shown: Thread and Transcript (not
// ported yet), then Details last when tn and it was chosen (iQ) or
// another tab exists (to).
func (lb *Lightbox) sideTabs() []lbTab {
	var tabs []lbTab
	other := len(tabs) > 0 // to, before Details is pushed
	if lb.detailsTab() && (lb.side.detailsOpened || other) {
		tabs = append(tabs, lbTab{id: lbTabDetails, title: nsMediaViewer.T("Details")})
	}
	return tabs
}

// activeTab is bDav's tc: the selected tab, or the default one when the
// selected Details isn't listed; Tabs shows the first when neither is.
func (lb *Lightbox) activeTab(tabs []lbTab) string {
	id := lb.side.tab
	if id == lbTabDetails && !lbHasTab(tabs, lbTabDetails) {
		id = lb.defaultTab()
	}
	if lbHasTab(tabs, id) || len(tabs) == 0 {
		return id
	}
	return tabs[0].id
}

func lbHasTab(tabs []lbTab, id string) bool {
	for _, t := range tabs {
		if t.id == id {
			return true
		}
	}
	return false
}

// selectTab is bDav's onTabChange aF (transcribe_opt_in_ui_v2 on):
// choosing Details marks it opened (iG), and the tab becomes the current
// one (ab).
func (lb *Lightbox) selectTab(id string) {
	if id == lbTabDetails {
		lb.side.detailsOpened = true
	}
	lb.side.tab = id
}

// openDetails is bDav's aD: Details chosen, selected and the sidebar
// open.
func (lb *Lightbox) openDetails() {
	lb.side.detailsOpened = true
	lb.selectTab(lbTabDetails)
	lb.side.open = true
}

// resetDetails is bDav's aM: Details no longer chosen, and a selected
// Details goes back to the default tab.
func (lb *Lightbox) resetDetails() {
	lb.side.detailsOpened = false
	if lb.side.tab == lbTabDetails {
		lb.side.tab = lb.defaultTab()
	}
}

// closeDetails is bDav's aO ("Close details"): reset Details, close the
// sidebar, and focus the media column's close once it has gone.
func (lb *Lightbox) closeDetails() {
	lb.resetDetails()
	lb.side.open = false
	lb.side.focusClose = true
}

// closeClosesDetails is renderCloseButton's a: the gate on, the sidebar
// open, no thread, and Details selected (ak, the current tab state).
func (lb *Lightbox) closeClosesDetails() bool {
	return lb.side.open && !lb.hasThread() && lb.side.tab == lbTabDetails && mediaViewerDetailsOn(lb.h)
}

// closeTip is renderCloseButton's r: "Close details", else "Close".
func (lb *Lightbox) closeTip() string {
	if lb.closeClosesDetails() {
		return nsMediaViewer.T("Close details")
	}
	return nsMediaViewer.T("Close")
}

// viewFileDetails is the menus' details item: bDav's td (an image's
// "View file details", Y4Ir, and a video's "View details", aRSD) opens
// the sidebar on Details (aD) when tn. Otherwise the menus fall back:
// an image's navigates to FileDetails in the secondary view
// (navigateFromMenu toFileDetails) with the viewer left open, a video's
// runs previewFile (uVLT: the same navigation, then closeModal).
func (lb *Lightbox) viewFileDetails(video bool) {
	if lb.detailsTab() {
		lb.openDetails()
		return
	}
	f := lb.cur().file
	if f != nil {
		if fh := fileDetailsHost(lb.h); fh != nil {
			fh.OpenFileDetails(f, lb.viewerChannel())
		} else if f.Permalink != "" {
			lbOpen(lb.h, f.Permalink) // a host without the FileDetails view
		}
	}
	if video {
		lb.Dismiss()
	}
}

// syncSide runs bDav's effect on the item shown:
// `iQ && !tn && (aM(), ts || iJ(false))`: an item without details resets
// a chosen Details, and the sidebar closes when no tab is left. The
// Details pane goes with the tab (FileDetails unmounts).
func (lb *Lightbox) syncSide() {
	s := &lb.side
	if s.detailsOpened && !lb.detailsTab() {
		lb.resetDetails()
		if len(lb.sideTabs()) == 0 {
			s.open = false
		}
	}
	s.tabs = lb.sideTabs()
	s.shown = s.open && len(s.tabs) > 0
	s.active = ""
	if s.shown {
		s.active = lb.activeTab(s.tabs)
	}
	if !s.shown || s.active != lbTabDetails {
		s.pane = nil
	}
}

// lbSideCap is .p-media_viewer_modal__flexpane's max-width for a window
// winW css px wide: 400px, 360px at 1440 or less, 300px at 1280 or less.
func lbSideCap(winW float32) float32 {
	switch {
	case winW <= 1280:
		return 300
	case winW <= 1440:
		return 360
	}
	return 400
}

// sideWidth is the sidebar's width in the row (the panel): flex 1 beside
// the media column's flex 1, so half the row, at most lbSideCap by the
// window's width.
func sideWidth(gtx layout.Context, win image.Point, panel image.Rectangle) int {
	pxd := gtx.Metric.PxPerDp
	if pxd <= 0 {
		pxd = 1
	}
	return min(dp(gtx, lbSideCap(float32(win.X)/pxd)), panel.Dx()/2)
}

// sideTabsInput reads the tabs' presses (last frame's) and the tablist's
// keys. A press focuses the tab (a button takes the focus on mousedown;
// no ring), the click selects it. tt5m's onKeyDown on the tablist, horizontal and
// without manualTabActivation: ← and → (wrapping), Home and End move the
// focus and select; with Shift, Meta, Ctrl or Alt held nothing happens.
// The viewer's own ← / → binding (Mousetrap on the overlay) sees the
// keydown too, as Tabs only preventDefaults it: the item moves as well.
func (lb *Lightbox) sideTabsInput(gtx layout.Context) {
	s := &lb.side
	for i := range s.clicks {
		for {
			e, ok := s.clicks[i].Update(gtx.Source)
			if !ok {
				break
			}
			if i >= len(s.tabs) {
				continue
			}
			switch e.Kind {
			case gesture.KindPress:
				s.focusIdx = i
				gtx.Execute(key.FocusCmd{Tag: &s.tabFocus})
				lb.quiet = &s.tabFocus
			case gesture.KindClick:
				lb.selectTab(s.tabs[i].id) // tt5m selectTab → onTabChange
			}
		}
	}
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: &s.tabFocus, Name: key.NameLeftArrow},
			key.Filter{Focus: &s.tabFocus, Name: key.NameRightArrow},
			key.Filter{Focus: &s.tabFocus, Name: key.NameHome},
			key.Filter{Focus: &s.tabFocus, Name: key.NameEnd},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press || ke.Modifiers != 0 {
			continue
		}
		n := len(s.tabs)
		if n > 0 {
			i := min(max(s.focusIdx, 0), n-1)
			switch ke.Name {
			case key.NameLeftArrow:
				i = (i - 1 + n) % n
			case key.NameRightArrow:
				i = (i + 1) % n
			case key.NameHome:
				i = 0
			case key.NameEnd:
				i = n - 1
			}
			s.focusIdx = i
			lb.selectTab(s.tabs[i].id)
		}
		switch ke.Name {
		case key.NameLeftArrow:
			lb.go2(lb.idx - 1)
		case key.NameRightArrow:
			lb.go2(lb.idx + 1)
		}
	}
}

// ringOn reports whether tag has the key focus from the keyboard (the
// c-button--focus-visible ring): not one the viewer focused itself.
func (lb *Lightbox) ringOn(gtx layout.Context, tag event.Tag) bool {
	return gtx.Focused(tag) && lb.quiet != tag
}

// layoutSide draws the sidebar at side in the row panel (Tabs: the tab
// bar over the tab panel), its line over the media column, and the
// close button at the row's top right.
func (lb *Lightbox) layoutSide(gtx layout.Context, th *theme.Theme, panel, side image.Rectangle) {
	s := &lb.side
	s.rect = side
	s.block.Layout(gtx, side) // under everything it draws: the background's press stays out
	dark := th.Dark
	rad := dp(gtx, 8)
	func() { // .c-sk-modal's 8px corners clip the sidebar's right ones
		cl := clipRRect(gtx, panel, rad)
		defer cl.Pop()
		fill(gtx, side, th.C.Bg)
		// box-shadow -1px 0 0 rgba(sk_foreground_low,.13): over the media
		// column's last pixels (the flexpane's z-index 1)
		line := rgba8(0x1d1c1d, 0x21)
		if dark {
			line = rgba8(0xe8e8e8, 0x21)
		}
		if th.Tokens != nil {
			theme.SetTok(th.Tokens, &line, theme.TokHairline, dark)
		}
		lw := max(1, dp(gtx, 1))
		fill(gtx, image.Rect(side.Min.X-lw, side.Min.Y, side.Min.X, side.Max.Y), line)
	}()

	// in the DOM's order (the focus order: Tab moves through the frame's
	// focusable widgets as drawn): the tablist, the tab panel, the close
	barH := dp(gtx, 50)
	body := image.Rect(side.Min.X, side.Min.Y+barH, side.Max.X, side.Max.Y)
	ring := lb.layoutTabBar(gtx, th, side, barH)
	if s.active == lbTabDetails {
		lb.layoutDetails(gtx, panel, body)
	}
	if !ring.Empty() {
		// the focused tab's ring (.c-button--focus-visible:focus), over
		// the panel's top as the tab bar's z-index 1 puts it
		solid := theme.ButtonV2Focus(th)
		halo := solid
		halo.A = 0x4d
		FocusRingColors(gtx, solid, halo, ring, dp(gtx, 4))
	}
	lb.layoutSideClose(gtx, th, panel)
}

// layoutDetails draws the Details tab's panel (R83k, role tabpanel):
// FileDetails for the item's file, filling body.
func (lb *Lightbox) layoutDetails(gtx layout.Context, panel, body image.Rectangle) {
	s := &lb.side
	fh := fileDetailsHost(lb.h)
	f := lb.cur().file
	if fh == nil || f == nil || body.Empty() {
		s.pane = nil
		return
	}
	if s.pane == nil || s.pane.FileID() != f.ID {
		s.pane = fh.FileDetailsPane(f, lb.viewerChannel())
	}
	if s.pane == nil {
		return
	}
	cl := clipRRect(gtx, panel, dp(gtx, 8))
	defer cl.Pop()
	defer clip.Rect(body).Push(gtx.Ops).Pop()
	at(gtx, body.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(body.Size())
		return s.pane.Layout(gtx)
	})
}

// layoutTabBar draws the tablist (role tablist, aria-label "Sidebar"):
// .p-media_viewer_modal__tabs and its tabs (4Tgw TabsItem). It returns
// the tab that shows the keyboard focus ring (empty for none).
func (lb *Lightbox) layoutTabBar(gtx layout.Context, th *theme.Theme, side image.Rectangle, barH int) (ringR image.Rectangle) {
	s := &lb.side
	lw := max(1, dp(gtx, 1))
	// .c-tabs__tab_menu: box-shadow inset 0 -1px 0 0 otl-ter
	fill(gtx, image.Rect(side.Min.X, side.Min.Y+barH-lw, side.Max.X, side.Min.Y+barH), theme.OtlTer(th))
	single := len(s.tabs) == 1 // --single_tab
	for len(s.clicks) < len(s.tabs) {
		s.clicks = append(s.clicks, gesture.Click{})
	}
	if !gtx.Focused(&s.tabFocus) {
		// the roving tab stop is the selected tab while no tab is focused
		for i, t := range s.tabs {
			if t.id == s.active {
				s.focusIdx = i
			}
		}
	}
	s.focusIdx = min(max(s.focusIdx, 0), max(len(s.tabs)-1, 0))
	s.tabR = s.tabR[:0]
	s.underline = s.underline[:0]
	x := side.Min.X + dp(gtx, 12)
	for i, t := range s.tabs {
		x += dp(gtx, 8) // .c-tabs__tab margin 0 8px
		w := textW(gtx, th, 15, theme.Black, t.title)
		r := image.Rect(x, side.Min.Y, x+w, side.Min.Y+barH)
		s.tabR = append(s.tabR, r)
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			if !single {
				pointer.CursorPointer.Add(gtx.Ops)
			}
			s.clicks[i].Add(gtx.Ops)
		}()
		if i == s.focusIdx {
			keyTarget(gtx, &s.tabFocus, r)
		}
		selected := t.id == s.active
		ring := i == s.focusIdx && lb.ringOn(gtx, &s.tabFocus)
		col := tokContentSec.of(th)
		if selected || s.clicks[i].Hovered() {
			col = tokContentPry.of(th)
		}
		at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, barH, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Black, col, t.title)
			})
		})
		// .c-tabs__tab--active: inset 0 -2px 0 0 theme-base-pry; a lone
		// tab's box-shadow is none unless it is focused from the keyboard
		under := selected && (!single || ring)
		s.underline = append(s.underline, under)
		if under {
			fill(gtx, image.Rect(r.Min.X, r.Max.Y-dp(gtx, 2), r.Max.X, r.Max.Y), th.C.Accent)
		}
		if ring {
			ringR = r // radius 4 4 1 1 in Slack's; Clack's ring is 4 all round
		}
		x = r.Max.X + dp(gtx, 20) // :not(:last-child) margin-right 20px
	}
	return ringR
}

// layoutSideClose is renderCloseButton with the sidebar open: in the row
// (p-media_viewer_modal__tabs_close: absolute, top 8, right 8), a
// c-icon_button--default of size medium: content-sec, content-pry on
// base-ter hovered (base-ter-pressed over it pressed), 8px corners, the
// 20px close glyph; its tooltip below.
func (lb *Lightbox) layoutSideClose(gtx layout.Context, th *theme.Theme, panel image.Rectangle) {
	s := &lb.side
	sz := dp(gtx, 36)
	cp := image.Pt(panel.Max.X-dp(gtx, 8)-sz, panel.Min.Y+dp(gtx, 8))
	s.closeR = image.Rectangle{Min: cp, Max: cp.Add(image.Pt(sz, sz))}
	tip := lb.closeTip()
	s.closeLabel = tip
	sc := "Esc"
	if lb.closeClosesDetails() {
		sc = "" // Escape closes the viewer, not the details
	}
	dark := th.Dark
	at(gtx, cp, func(gtx layout.Context) layout.Dimensions {
		d := clickBox(gtx, &lb.closeBtn, func(gtx layout.Context, bs image.Point, hovered bool) {
			if !hovered {
				return
			}
			bg := tokBaseTer.of(th)
			if lb.closeBtn.Pressed() {
				bg = theme.BlendInteraction(bg, tokBaseTerPressed.of(th), dark)
			}
			rrect(gtx, image.Rectangle{Max: bs}, dp(gtx, 8), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			col := tokContentSec.of(th)
			if lb.closeBtn.Hovered() {
				col = tokContentPry.of(th)
			}
			off := (sz - dp(gtx, 20)) / 2
			at(gtx, image.Pt(off, off), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "close", 20, col)
			})
			return layout.Dimensions{Size: image.Pt(sz, sz)}
		})
		if lb.ringOn(gtx, &lb.closeBtn) {
			solid := theme.ButtonV2Focus(th)
			halo := solid
			halo.A = 0x4d
			FocusRingColors(gtx, solid, halo, image.Rectangle{Max: d.Size}, dp(gtx, 8))
		}
		Tooltip(gtx, th, &lb.tips[8], lb.closeBtn.Hovered(), d.Size, Below, tip, sc)
		return d
	})
}

// keepFocus runs at the end of the viewer's frame. The viewer holds the
// key focus while it's on top (react-modal keeps it inside): it takes it
// back unless a widget the viewer drew this frame has it (a toolbar
// button, the sidebar's tab and close, the Details pane's links and
// buttons): ft, the focus at the frame's start, asked for focus events
// while the viewer laid out (before: it hadn't before). The media
// column's close takes it after "Close details" (aL).
func (lb *Lightbox) keepFocus(gtx layout.Context, top bool, ft event.Tag, before bool) {
	if lb.done {
		return
	}
	in := ft != nil && (ft == event.Tag(lb) || !before && slackc.TakesFocus(gtx, ft))
	lb.focusIn = nil
	if in {
		lb.focusIn = ft
	}
	if ft != lb.quiet {
		lb.quiet = nil
	}
	if lb.side.focusClose && !lb.side.shown {
		lb.side.focusClose = false
		gtx.Execute(key.FocusCmd{Tag: &lb.closeBtn})
		lb.quiet = &lb.closeBtn
		return
	}
	if top && !in && !gtx.Focused(lb) {
		gtx.Execute(key.FocusCmd{Tag: lb})
	}
}

// SidebarTabs are the sidebar's tabs as last drawn (their ids, in order;
// none while it's closed) and the one shown (tests and the inspect tool).
func (lb *Lightbox) SidebarTabs() (ids []string, active string) {
	if !lb.side.shown {
		return nil, ""
	}
	for _, t := range lb.side.tabs {
		ids = append(ids, t.id)
	}
	return ids, lb.side.active
}

// SidebarRect is the sidebar as last drawn (window coordinates; empty
// while it's closed), and the media column beside it (the whole panel
// while it's closed).
func (lb *Lightbox) SidebarRect() (side, media image.Rectangle) {
	if !lb.side.shown {
		return image.Rectangle{}, lb.side.media
	}
	return lb.side.rect, lb.side.media
}

// SidebarParts are the tabs' and the sidebar close button's rects as last
// drawn (window coordinates).
func (lb *Lightbox) SidebarParts() (tabs []image.Rectangle, close image.Rectangle) {
	if !lb.side.shown {
		return nil, image.Rectangle{}
	}
	return append([]image.Rectangle(nil), lb.side.tabR...), lb.side.closeR
}

// CloseLabel is the close button's label now: "Close details" or
// "Close".
func (lb *Lightbox) CloseLabel() string { return lb.closeTip() }

// DetailsFileID is the file the Details tab's pane shows ("" for none).
func (lb *Lightbox) DetailsFileID() string {
	if lb.side.pane == nil {
		return ""
	}
	return lb.side.pane.FileID()
}

// MoreRect is the footer's More actions button as last drawn (window
// coordinates; tests and the inspect tool).
func (lb *Lightbox) MoreRect() image.Rectangle { return lb.moreRect }
