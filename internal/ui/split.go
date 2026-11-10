package ui

// Slack's split view (docs/SPLIT-VIEW.md): the window's one Split view
// container, a card right of the workspace showing a second channel, DM
// or thread beside the primary view. Read from the web client (build
// a84662d2d135):
//
//   - openInSplitView (client-boot Fndw, modern.vendor 9ciC) navigates
//     with options.forceRenderInSplitView. From outside the split view the
//     new view replaces the old one with no back stack; navigating inside
//     it keeps the old view as view.prev (maybeGenerateSplitBackStack).
//   - Channels, DMs and threads opened from inside the split view stay in
//     it (getTargetContainer: shouldForceOpenInSplitView is true for a
//     Split source container).
//   - ClientSplitView (boot-render B5vr) draws .p-view_contents--split
//     --card: margin-left 4px, radius 6, box-shadow -4px -4px 16px
//     rgba(0,0,0,.09) (dark: no shadow, a 1px otl-ter border).
//   - Its width (useCalculateMainWindowClientLayout b2oS, pyGT): half
//     the space beside the sidebar, 30% of the workspace above a 1200px
//     window, between 300 and min(that space - 300, 1000); a dragged
//     width ("manual") or the last default ("sticky") is kept, sticky
//     going back to the default when the window resizes (DUQ2). The
//     resizer (DFUN, BaseResizer) is min 300, max 1000, snaps to the
//     default within 15px and resets on a double click. The width is
//     kept on the device (localStorage viewLayoutSplitWidthIntent/Mode);
//     the view itself is not restored after a restart (window-restoration
//     skips the main window).
//   - The desktop app raises the window's minimum width by 300 while it
//     shows (minWindowWidth: rail + 320 + 300 + 300 + 4) and widens a
//     narrower window to it.
//   - ViewHeader closes it with a 28px "close" button after the actions
//     (CloseSplitViewButton's tooltip "Close"), and shows Slack's
//     FlexpaneBackButton while the view has a back stack. Escape in it
//     closes it (RenderedViewContents' escape handler) once its view has
//     nothing left to mark read (ThreadsFlexpane.onEsc).
//   - None of it exists in Simplified layout mode.

import (
	"image"
	"time"

	"gioui.org/app"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// paneSplit names the split view's view container (App.pane).
const paneSplit = "split"

// After mount_features.go's installHooks (file order): the secondary
// pane's menus anchor left of an open split view.
func init() {
	prev := installHooks
	installHooks = func(a *App) {
		if prev != nil {
			prev(a)
		}
		popups.SecondaryRight = func() int {
			if a.splitOpen() && a.split.col.Min.X > 0 {
				return a.split.col.Min.X
			}
			return a.winSize.X - a.px(int(theme.PanelInset))
		}
	}
}

// clsSplitView is the split view's element (RenderedViewContents in the
// Split container, with ClientSplitView's card class), for plugin CSS.
var clsSplitView = slackc.Class("p-view_contents p-view_contents--split p-view_contents--split--card")

// Slack's split column limits and layout constants (DFUN, pyGT, b2oS), in
// dp (Slack's CSS px).
const (
	splitMinW      = 300  // splitMin; also the primary view's floor beside it
	splitMaxW      = 1000 // splitMax
	splitGap       = 4    // .p-view_contents--split margin-left
	splitSnap      = 15   // calculateColumnWidth snaps to the default within this
	splitWideAt    = 1200 // windows wider than this default to 30% of the workspace
	splitSidebarLo = 180  // the sidebar's squished floor (W2CY minWidth)
	splitHandleW   = 8    // .p-resizer width
)

// splitState is the split view.
type splitState struct {
	// stack is the view and its back stack (Slack's view.prev chain): the
	// last route is shown. Empty: closed.
	stack []Route
	// focus is Slack's getFocusedContainer() === Split: the split view
	// got the last click inside the workspace, or was just opened.
	focus    bool
	focusReq bool      // focus its composer in its next frame (transitionFocus)
	pane     paneState // its own buttons, conversations and tabs
	card     image.Rectangle
	col      image.Rectangle // the column: the 4px gap and the card (window coords)

	// width is the column width the user or the layout chose (dp; Slack's
	// splitWidthIntent), mode its splitWidthMode: "auto", "sticky" or
	// "manual". Kept in prefs (ui.json split_w, split_w_mode).
	width int
	mode  string
	winW  int // the window width (dp) the mode last saw

	def, maxW float32 // this frame's default and largest widths (dp)

	drag     gesture.Drag
	dragOff  float32 // the press's distance from the column's left edge (px)
	resizing bool
	lastDown time.Time // the handle's last press: another within 500ms resets
	hover    bool

	minW int      // the window minimum width last applied (dp), 0: Clack's own
	data *backend // the session the pane's conversations belong to

	closeTip popups.TipState
}

// paneState is what a view container keeps of its own while the shell
// draws a view into it (Slack's ViewContainer context): the buttons of its
// header and tabs, its conversations (list and composer), the tab each
// channel shows and the trackers its menus anchor to. App holds the
// current container's in these fields; inPane swaps them.
type paneState struct {
	clicks       clickables
	convos       map[string]*convo
	convoOrder   []string
	chTab        map[string]int
	hdrCtx       map[string]*int
	hudOrigin    map[string]*originTracker
	starOrigin   map[string]*originTracker
	joinBannerCh string
}

// swapPane exchanges the App's view-container fields with p's.
func (a *App) swapPane(p *paneState) {
	if p.clicks == nil {
		p.clicks = clickables{}
	}
	a.clicks, p.clicks = p.clicks, a.clicks
	a.convos, p.convos = p.convos, a.convos
	a.convoOrder, p.convoOrder = p.convoOrder, a.convoOrder
	a.chTab, p.chTab = p.chTab, a.chTab
	a.hdrCtx, p.hdrCtx = p.hdrCtx, a.hdrCtx
	a.hudOrigin, p.hudOrigin = p.hudOrigin, a.hudOrigin
	a.starOrigin, p.starOrigin = p.starOrigin, a.starOrigin
	a.joinBannerCh, p.joinBannerCh = p.joinBannerCh, a.joinBannerCh
}

// inPane runs f in view container pane ("" the main window's views,
// paneSplit the split view): its state in the App's fields, and what f
// opens (threads, channels, menus) opening there, as Slack's components
// navigate in their container's context.
func (a *App) inPane(pane string, f func()) {
	if pane == a.pane {
		f()
		return
	}
	// two containers: a swap always toggles between them
	a.swapPane(&a.split.pane)
	prev := a.pane
	a.pane = pane
	defer func() {
		a.pane = prev
		a.swapPane(&a.split.pane)
	}()
	f()
}

// splitOpen reports whether the split view shows (never in Simplified
// layout mode).
func (a *App) splitOpen() bool { return len(a.split.stack) > 0 && !a.bbmOn() }

// splitTop is the split view's route ("" when closed).
func (a *App) splitTop() Route {
	if n := len(a.split.stack); n > 0 {
		return a.split.stack[n-1]
	}
	return ""
}

// SplitViewOK is popups.SplitViewer: Slack has no split view in
// Simplified layout mode.
func (a *App) SplitViewOK() bool { return !a.bbmOn() }

// InSplitView is popups.SplitViewer: whether the view container being
// drawn (or acted on) is the split view.
func (a *App) InSplitView() bool { return a.pane == paneSplit }

var _ popups.SplitViewer = (*App)(nil)

// OpenSplit is Slack's openInSplitView for a channel route ("c/<id>",
// "c/<id>/m/<ts>") or a thread ("c/<id>/t/<ts>"): the view goes in the
// split view, which gets the keyboard focus. Without a split view
// (Simplified layout mode) Slack's navigation opens it in place.
func (a *App) OpenSplit(route string) {
	r := Route(route)
	ch := r.Channel()
	if ch == "" || !a.SplitViewOK() {
		a.Navigate(route)
		return
	}
	s := &a.split
	if a.pane == paneSplit && len(s.stack) > 0 {
		// navigating inside the split view: the old view is its back stack
		if s.stack[len(s.stack)-1] == r {
			return
		}
		s.stack = append(s.stack, r)
	} else {
		s.stack = []Route{r}
	}
	s.focus, s.focusReq = true, true
	if ts := r.Message(); ts != "" {
		a.inPane(paneSplit, func() {
			if h, ok := a.convoFor(ch, "").list.(interface{ Highlight(string) }); ok {
				h.Highlight(ts)
			}
		})
	}
	a.Invalidate()
}

// CloseSplit closes the split view (closeSplitView): focus goes back to
// the primary view. Its conversations save their drafts and go, as
// Slack's view unmounts.
func (a *App) CloseSplit() {
	s := &a.split
	if len(s.stack) == 0 {
		return
	}
	s.stack, s.focus, s.focusReq = nil, false, false
	// the pane's state may be swapped in right now (its own close button):
	// let go of it before the next frame
	a.Post(a.dropSplitPane)
	if ch := a.router.Current().Channel(); ch != "" {
		a.inPane("", func() {
			if c := a.convos[ch+"/"+a.router.Current().Thread()]; c != nil && c.comp != nil {
				c.comp.Focus()
			}
		})
	}
	a.Invalidate()
}

// dropSplitPane closes the split view's conversations (their composers
// save their drafts) once it has closed.
func (a *App) dropSplitPane() {
	if len(a.split.stack) > 0 || a.pane == paneSplit {
		return
	}
	p := &a.split.pane
	for _, c := range p.convos {
		if c.comp != nil {
			c.comp.Close()
		}
	}
	p.convos, p.convoOrder, p.joinBannerCh = nil, nil, ""
}

// PaneBack is messages.PaneBack: the split view has a back stack.
func (a *App) PaneBack() bool { return a.pane == paneSplit && len(a.split.stack) > 1 }

// GoPaneBack goes back in the split view (FlexpaneBackButton).
func (a *App) GoPaneBack() {
	if s := &a.split; len(s.stack) > 1 {
		s.stack = s.stack[:len(s.stack)-1]
		s.focus = true
		a.Invalidate()
	}
}

// splitClick reports whether this frame's click was Slack's split
// modifier click (shouldSplit: ⌘⌥ on macOS, Ctrl+Alt elsewhere), which
// opens what was clicked in the split view.
func (a *App) splitClick() bool {
	m := a.clickMods
	return m.Contain(shortcut.Mod()) && m.Contain(key.ModAlt) && a.SplitViewOK()
}

// SplitClick is splitClick for the feature packages (views: Activity's
// rows open their item in the split view on that click).
func (a *App) SplitClick() bool { return a.splitClick() }

// splitPointer sees the root pointer events (readRootPointer): the
// modifiers of a click's release, and which container a press lands in
// (the split view's focus; menus over the workspace don't count).
func (a *App) splitPointer(pe pointer.Event) {
	switch pe.Kind {
	case pointer.Release:
		a.clickMods = pe.Modifiers
	case pointer.Press:
		if !a.splitOpen() || len(a.overlays) > 0 {
			return
		}
		p := a.ptrWin
		if p.In(a.split.card) {
			a.split.focus = true
		} else if p.In(image.Rect(a.px(int(theme.RailW)), a.split.card.Min.Y, a.split.col.Min.X, a.split.card.Max.Y)) {
			a.split.focus = false
		}
	}
}

// splitEscape is Escape while the split view has the focus: a channel with
// unread messages is marked read first, as Esc does in the main view;
// otherwise the view closes (RenderedViewContents' Escape for a closable
// container).
func (a *App) splitEscape() bool {
	if !a.splitOpen() || !a.split.focus {
		return false
	}
	r := a.splitTop()
	ch := r.Channel()
	if r.Thread() == "" {
		if c := a.model().conv(ch); c != nil && c.Unread {
			a.markRead(ch)
			return true
		}
	}
	a.CloseSplit()
	return true
}

// ---- layout ----

// splitColumn is Slack's main-window layout for the split column
// (useCalculateMainWindowClientLayout: pyGT, then the squish xeD9) beside
// a workspace eb px wide whose sidebar is sbW: the column's width (px, the
// 4px gap and the card) and the sidebar's once squished. 0 when the split
// view is closed. It also notes the default and largest widths (dp) for
// the resizer.
func (a *App) splitColumn(gtx layout.Context, eb, sbW int) (int, int) {
	if !a.splitOpen() {
		return 0, sbW
	}
	s := &a.split
	pd := gtx.Metric.PxPerDp
	dp := func(px int) float32 { return float32(px) / pd }
	win := dp(a.winSize.X)
	// a window resize ends a "sticky" width (setSplitStickyAutoResetOnResize)
	if w := int(win + .5); w != s.winW {
		if s.winW != 0 && s.mode == "sticky" && !s.resizing {
			s.mode = "auto"
		}
		s.winW = w
	}
	ebDp, sb := dp(eb), dp(sbW)
	avail := ebDp - sb // the space beside the sidebar
	maxW := float32(int(min(avail-splitMinW, splitMaxW) + .5))
	def := avail / 2
	if max(win, a.splitMinWindowW()) > splitWideAt {
		def = ebDp * .3
	}
	def = clampF(def, splitMinW, maxW)
	s.def, s.maxW = def, max(maxW, splitMinW)
	w := def
	if s.width > 0 && (s.mode == "sticky" || s.mode == "manual") {
		w = clampF(float32(s.width), splitMinW, maxW)
	}
	if !s.resizing && (s.mode == "" || s.mode == "auto") {
		// the default sticks until the window resizes (DUQ2)
		s.width, s.mode = int(w+.5), "sticky"
		a.saveSplitWidth()
	}
	// squish: the primary view keeps 300; under that the sidebar gives
	// way, down to 180
	if prim := avail - w; prim < splitMinW && sbW > 0 {
		sb = max(ebDp-w-splitMinW, splitSidebarLo)
	}
	if w > ebDp-sb { // narrower than Slack's minimum window: never past the workspace
		w = max(ebDp-sb, 0)
	}
	return gtx.Dp(unit.Dp(w)), min(gtx.Dp(unit.Dp(sb)), sbW)
}

func clampF(v, lo, hi float32) float32 { return max(lo, min(hi, v)) }

// splitMinWindowW is Slack's minWindowWidth while the split view shows
// (dp): the tab rail, a 320px sidebar, the 300px primary view, the 300px
// split column and the 4px right inset.
func (a *App) splitMinWindowW() float32 {
	return float32(theme.RailW) + 320 + splitMinW + splitMinW + splitGap
}

// syncSplitWindow raises the window's minimum width to Slack's while the
// split view shows, widening a narrower window as the desktop app does,
// and puts Clack's own minimum (Run: 800×500) back once it closes.
func (a *App) syncSplitWindow(gtx layout.Context) {
	want := 0
	if a.splitOpen() {
		want = int(a.splitMinWindowW()*a.zoomFactor() + .5)
		if want <= 800 {
			want = 0
		}
	}
	if want == a.split.minW || a.win == nil {
		a.split.minW = want
		return
	}
	a.split.minW = want
	if want == 0 {
		a.win.Option(app.MinSize(800, 500))
		return
	}
	a.win.Option(app.MinSize(unit.Dp(want), 500))
	// the window's own dp (Run's sizes are before View > Zoom)
	base := gtx.Metric.PxPerDp / a.zoomFactor()
	if base <= 0 {
		return
	}
	if w := float32(a.winSize.X) / base; w < float32(want) {
		a.win.Option(app.Size(unit.Dp(want), unit.Dp(float32(a.winSize.Y)/base)))
	}
}

// saveSplitWidth keeps the width and its mode (Slack's setSplitColumn:
// localStorage viewLayoutSplitWidthIntent / viewLayoutSplitWidthMode).
func (a *App) saveSplitWidth() {
	if a.prefs.SplitW == a.split.width && a.prefs.SplitWMode == a.split.mode {
		return
	}
	a.prefs.SplitW, a.prefs.SplitWMode = a.split.width, a.split.mode
	a.savePrefs()
}

// loadSplitWidth reads the saved width once (prefs load before the first
// frame).
func (a *App) loadSplitWidth() {
	if a.split.mode == "" && a.prefs.SplitWMode != "" {
		a.split.width, a.split.mode = a.prefs.SplitW, a.prefs.SplitWMode
	}
}

// layoutSplit draws the split column col (window coordinates, the shell's
// gtx): the card with its view, then the resizer on its left edge.
func (a *App) layoutSplit(gtx layout.Context, col image.Rectangle) {
	s := &a.split
	if s.data != a.data { // signed in again or switched accounts
		if s.data != nil && a.pane != paneSplit {
			p := &s.pane
			p.convos, p.convoOrder = nil, nil
		}
		s.data = a.data
	}
	rad := gtx.Dp(theme.PanelRadius)
	card := image.Rect(col.Min.X+gtx.Dp(splitGap), col.Min.Y, col.Max.X, col.Max.Y)
	s.col, s.card = col, card
	if !a.th.Dark { // ClientSplitView's card shadow (dark mode: the outline below)
		theme.Shadow(gtx, card.Add(image.Pt(-gtx.Dp(2), -gtx.Dp(2))), rad, gtx.Dp(8), a.th.C.PanelShadow)
	}
	route := a.splitTop()
	func() {
		defer clip.UniformRRect(card, rad).Push(gtx.Ops).Pop()
		theme.Fill(gtx, card, a.th.C.Bg)
		a.inPane(paneSplit, func() {
			prevOrigin, prevBodyW := a.primaryOrigin, a.chBodyW
			a.primaryOrigin, a.chBodyW = card.Min, 0
			defer func() { a.primaryOrigin, a.chBodyW = prevOrigin, prevBodyW }()
			atExact(gtx, card.Min, card.Size(), func(gtx layout.Context) layout.Dimensions {
				sv := clsSplitView.Enter(gtx, 0, 0)
				defer sv.Leave()
				return a.layoutSplitView(gtx, route)
			})
		})
	}()
	if b := a.th.C.WorkspaceBorder; b.A > 0 {
		theme.Border(gtx, card, rad, gtx.Dp(1), theme.Over(b, a.chromeBase(card.Min)))
	}
	a.layoutSplitResize(gtx, col, card)
}

// layoutSplitView draws route in the split card: a channel (header, tabs,
// messages and composer, as in the primary view) or a thread.
func (a *App) layoutSplitView(gtx layout.Context, route Route) layout.Dimensions {
	ch, ts := route.Channel(), route.Thread()
	if ch == "" {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	var d layout.Dimensions
	if ts != "" {
		c := a.convoFor(ch, ts)
		d = a.layoutConvo(gtx, c, a.split.card.Min)
	} else {
		d = a.layoutChannel(gtx, ch)
	}
	if a.split.focusReq {
		a.split.focusReq = false
		if c := a.convos[ch+"/"+ts]; c != nil && c.comp != nil {
			c.comp.Focus()
		}
	}
	return d
}

// layoutSplitResize is the split column's resizer (BaseResizer, className
// p-ia4_client__resizer--split): 8px on the card's left edge, a 2px
// highlight line while hovered or dragged, Slack's 300–1000 limits, a
// snap to the default within 15px and a reset on a double click.
func (a *App) layoutSplitResize(gtx layout.Context, col, card image.Rectangle) {
	s := &a.split
	pd := gtx.Metric.PxPerDp
	for {
		e, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch e.Kind {
		case pointer.Press:
			if !s.lastDown.IsZero() && gtx.Now.Sub(s.lastDown) < 500*time.Millisecond {
				// onReset: the default width, "auto"
				s.lastDown = time.Time{}
				s.width, s.mode, s.resizing = int(s.def+.5), "auto", false
				a.saveSplitWidth()
				a.Invalidate()
				continue
			}
			s.lastDown = gtx.Now
			s.dragOff, s.resizing = float32(a.ptrWin.X-col.Min.X), true
		case pointer.Drag:
			if !s.resizing {
				continue
			}
			// fromRight: the width is the pointer's distance from the
			// column's right edge (window coordinates, so the moving
			// handle doesn't matter)
			w := (float32(col.Max.X-a.ptrWin.X) + s.dragOff) / pd
			mode := "manual"
			if w >= s.def-splitSnap && w <= s.def+splitSnap {
				w, mode = s.def, "auto" // calculateColumnWidth's snap
			} else {
				w = clampF(w, splitMinW, s.maxW)
			}
			if int(w+.5) != s.width || mode != s.mode {
				s.width, s.mode = int(w+.5), mode
				a.Invalidate()
			}
		case pointer.Release, pointer.Cancel:
			if s.resizing {
				s.resizing = false
				if s.mode == "auto" { // the default then sticks (DUQ2)
					s.mode = "sticky"
				}
				a.saveSplitWidth()
			}
		}
	}
	hw := gtx.Dp(splitHandleW)
	r := image.Rect(card.Min.X-hw/2, card.Min.Y, card.Min.X+hw-hw/2, card.Max.Y)
	if s.hover || s.resizing {
		// .p-resizer:hover:after for --split: 2px, highlight accent, with a
		// 1px ring
		lx := card.Min.X - gtx.Dp(2)
		theme.Fill(gtx, image.Rect(lx-gtx.Dp(1), card.Min.Y, lx+gtx.Dp(3), card.Max.Y), a.th.C.Link)
	}
	func() {
		defer op.Offset(r.Min).Push(gtx.Ops).Pop()
		pointerArea(gtx, &s.drag, image.Rectangle{Max: r.Size()}, pointer.CursorColResize)
	}()
	s.hover = a.ptrOK && a.ptrWin.In(r) && len(a.overlays) == 0
}

// ---- the split view's channel header ----

// splitBackButton draws FlexpaneBackButton at x in the split view's
// channel header (h tall) while the view has a back stack, and returns
// where the header's next control goes.
func (a *App) splitBackButton(gtx layout.Context, x, h int) int {
	if !a.PaneBack() {
		return x
	}
	btn := gtx.Dp(28)
	c := &a.th.C
	at(gtx, image.Pt(x, (h-btn)/2), image.Pt(btn, btn), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, a.clicks.get("hdr.back"), "caret-left-full", 28, 20, c.FgMuted, c.BgHover, 8)
	})
	if clicked(a.clicks.get("hdr.back")) {
		a.GoPaneBack()
	}
	// margin-right 6px (backButtonPolish) and the header's 4px gap
	return x + btn + gtx.Dp(10)
}

// splitCloseButton draws the split view's close button (ViewHeader's
// close_view: 28px interaction button, icon close, tooltip "Close" after
// 300ms) with its right edge at right, and returns where the header's
// actions end (4px before it).
func (a *App) splitCloseButton(gtx layout.Context, right, h int) int {
	btn := gtx.Dp(28)
	c := &a.th.C
	x := right - btn
	b := a.clicks.get("hdr.close")
	at(gtx, image.Pt(x, (h-btn)/2), image.Pt(btn, btn), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, b, "close", 28, 20, c.FgMuted, c.BgHover, 8)
		a.split.closeTip.Delay = 300 * time.Millisecond
		popups.Tooltip(gtx, a.th, &a.split.closeTip, b.Hovered(), d.Size, popups.Below, "Close", "")
		return d
	})
	if clicked(b) {
		a.CloseSplit()
	}
	return x - gtx.Dp(4)
}

// paneRight is the right edge (window x) of the view container being
// drawn: the split card's, the workspace's beside an open split view, or
// the panel's at the window edge. Menus anchored to header controls use
// it.
func (a *App) paneRight() int {
	switch {
	case a.pane == paneSplit:
		return a.split.card.Max.X
	case a.splitOpen() && a.split.col.Min.X > 0:
		return a.split.col.Min.X
	}
	return a.winSize.X - a.px(int(theme.PanelInset))
}

// headerEnd is where a channel header's actions end (window x): the
// pane's right edge, less the split view's close button and its gap.
func (a *App) headerEnd() int {
	r := a.paneRight()
	if a.pane == paneSplit {
		r -= a.px(28 + 4)
	}
	return r
}

// headerFit is Slack's useActionsToMoveToMenu (view-header chunk, module
// 1DLG) for a channel header w px wide, without the
// channel_ia_header_refinements experiment: the view's column (the split
// view's whole column, the primary view with its nested pane) under 550px
// moves the search button out of the header, under 500px the bell too
// (only the bell for the Slackbot DM), and then
// getOverflowingActions keeps floor((width-340)/30) of the header's other
// movable actions in Slack's order (agents, huddle): the Huddle button
// goes once the column is under 370px. btns are layoutChannelHeader's
// buttons, right to left; it returns the ones that stay, whether the
// Huddle button (huddle: the header has one) stays, and whether the
// column is under 600px (Slack's isColumnNarrow: the topic container
// hides). Slack's ⋮ menu doesn't change for it.
func (a *App) headerFit(gtx layout.Context, w int, conv *convInfo, btns []string, huddle bool) ([]string, bool, bool) {
	col := float32(w) / gtx.Metric.PxPerDp
	if a.pane == paneSplit && a.split.col.Dx() > 0 {
		col = float32(a.split.col.Dx()) / gtx.Metric.PxPerDp
	}
	narrow := col < 600
	slackbot := conv != nil && conv.Kind == "im" && conv.User == "USLACKBOT"
	var moved []string
	switch {
	case col < 500 && slackbot:
		moved = []string{"bell"}
	case col < 500:
		moved = []string{"search", "bell"}
		// the movable actions left, in Slack's order: only the huddle here
		if huddle && int((col-340)/30) < 1 {
			huddle = false
		}
	case col < 550 && !slackbot:
		moved = []string{"search"}
	}
	if len(moved) == 0 {
		return btns, huddle, narrow
	}
	keep := make([]string, 0, len(btns))
	for _, b := range btns {
		if b != moved[0] && (len(moved) < 2 || b != moved[1]) {
			keep = append(keep, b)
		}
	}
	return keep, huddle, narrow
}

// splitSidebarItem is "Open in split view" for the sidebar's row menu
// (getTilesTemplate, entry point "sidebar"), or false where Slack has
// none.
func (a *App) splitSidebarItem(ch string) (popups.MenuItem, bool) {
	if !a.SplitViewOK() {
		return popups.MenuItem{}, false
	}
	return popups.SplitViewItem(a, "c/"+ch, true)
}

// withSplitItem puts "Open in split view" in the sidebar row menu where
// Slack's getContextMenuTemplate does: its own group, right before the
// Leave / Close group.
func withSplitItem(items []popups.MenuItem, split popups.MenuItem) []popups.MenuItem {
	at := len(items)
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Separator {
			at = i
			break
		}
	}
	out := make([]popups.MenuItem, 0, len(items)+2)
	out = append(out, items[:at]...)
	out = append(out, popups.Sep(), split)
	return append(out, items[at:]...)
}
