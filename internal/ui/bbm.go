package ui

// Simplified layout mode: Slack's "big button mode" (BBM), from the web
// client on the public CDN (build a84662d2d135, bv1-13-br, 2026-11-08).
// docs/PREFS-AUDIT.md "Simplified layout mode" has the rules and the
// modules they come from; the comments here name the module of each.
//
// The mode shows one thing at a time. The tab rail goes away; the window
// is the top bar, a breadcrumb row and one rounded panel (max 1200px,
// centred) showing a single view container: a tab's list (the channel
// sidebar, the DMs list, the Files nav, or the BBM list of tabs), its
// conversation, or its thread. Picking something in the list fills the
// panel with it; the breadcrumbs go back up.

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// bbmCont is the view container the panel shows: Slack's
// fullscreen.containers.main (qIxd setContainerInFullScreenMode).
type bbmCont uint8

const (
	bbmSidebar   bbmCont = iota // the tab's list (pl6F's mq.Sidebar)
	bbmPrimary                  // its conversation or page
	bbmSecondary                // its thread (or other right-hand pane)
)

// bbmState is the mode's per-window state (none of it persists: Slack's
// fullscreen store has persistenceEnabled false).
type bbmState struct {
	cont bbmCont
	// tabSwitch marks the next navigation as a tab switch (Slack's
	// ACTIVE_TAB_UPDATE trigger): it lands on the tab's list.
	tabSwitch bool
	// secOpen is whether the secondary pane was open last frame: opening
	// one fills the panel with it, closing it goes back to the primary.
	secOpen bool
	list    widget.List // the BBM views' ListViewer (bbm_views.go)
}

// bbmUnsupported is I/bR isBBMUnsupported: the agentic_ia experiment
// ("Slack 6") has no simplified layout; Preferences says so.
func (a *App) bbmUnsupported() bool {
	c := a.Core()
	return c != nil && c.Experiment("agentic_ia") == "on"
}

// bbmOn is I/bR VA: the mode is on and supported.
func (a *App) bbmOn() bool { return a.prefs.BBM && !a.bbmUnsupported() }

// isBBMRoute: Slack's BBM tab (pl6F k6.BBM) and its three views, BBMMain
// ("bbm"), BBMMore ("bbm/more") and BBMWorkspaceSwitcher
// ("bbm/workspaces").
func isBBMRoute(r Route) bool { return r.Page() == "bbm" }

// bbmPersistActions is zgdY.b: messages always show the collapsed
// actions menu (the default, "Always show collapsed menu").
func (a *App) bbmPersistActions() bool { return a.bbmOn() && !a.prefs.BBMHoverActions }

// startBBM is HnPE startBBM: from Preferences (step "accessibility_pref")
// it shows the welcome dialog the first time, then turns the mode on,
// moves to the BBM tab and applies the mode's settings (setBBMSettings:
// its own zoom, the sidebar shown). Slack also enables fullscreen mode
// and clears the megaphone spaces; Clack has no megaphone.
func (a *App) startBBM(step string) {
	if a.bbmUnsupported() || a.prefs.BBM {
		return
	}
	if step != "" && !a.prefs.BBMOnboarded {
		a.openBBMNux() // maybeOpenBBMNUXModal (bbm_nux.go)
	}
	a.prefs.BBM = true
	// setZoomBBM: the mode's zoom (slack-zoom-bbm-setting), when one was set
	if z := a.prefs.BBMZoom; z != nil {
		a.setZoomLevel(*z)
	}
	// setIsSidebarVisible(true); nonBBMSettings keeps the old value
	h := a.sidebar.hidden
	a.prefs.NonBBMSidebarHidden = &h
	a.sidebar.hidden = false
	a.bbm = bbmState{}
	a.bbmNavigate("bbm")
	a.savePrefs()
	a.Invalidate()
}

// stopBBM is NoNg stopBBM: the mode ends, resetBBMSettings puts the zoom
// and the sidebar back as they were outside it, and maybeNavigateAwayFromBBM
// leaves the BBM tab for the latest other tab (else Home).
func (a *App) stopBBM() {
	if !a.bbmOn() {
		return
	}
	a.prefs.BBM = false
	z := 0
	if a.prefs.NonBBMZoom != nil { // resetZoomBBM: nonBBMSettings.zoom ?? 0
		z = *a.prefs.NonBBMZoom
	}
	a.setZoomLevel(z)
	if h := a.prefs.NonBBMSidebarHidden; h != nil {
		a.sidebar.hidden = *h
	}
	a.prefs.NonBBMSidebarHidden = nil
	if isBBMRoute(a.router.Current()) {
		a.Navigate(string(a.latestNonBBMRoute()))
	}
	a.bbm = bbmState{}
	a.savePrefs()
	a.Invalidate()
}

// latestNonBBMRoute is RxPj getLatestNonBBMTab: the most recent history
// entry outside the BBM tab, else Home.
func (a *App) latestNonBBMRoute() Route {
	h := &a.router
	for i := h.pos; i >= 0; i-- {
		if r := h.stack[i]; !isBBMRoute(r) {
			return r
		}
	}
	if a.lastChannel != "" {
		return Route("c/" + a.lastChannel)
	}
	return "threads"
}

// setZoomLevel sets View > Zoom's level (macmenu.go zoom keeps BBMZoom /
// NonBBMZoom in step, as 2bwp's onUpdateFromDesktop handlers do).
func (a *App) setZoomLevel(z int) {
	z = min(max(z, zoomMin), zoomMax)
	if z == a.prefs.Zoom {
		return
	}
	a.prefs.Zoom = z
	a.Invalidate()
}

// noteZoom is 2bwp's two updateFromDesktop handlers: a zoom change in the
// mode is the mode's zoom (bbmSettings.zoom), outside it the normal one
// (nonBBMSettings.zoom), so each comes back when the mode starts or ends.
func (a *App) noteZoom() {
	z := a.prefs.Zoom
	if a.bbmOn() {
		a.prefs.BBMZoom = &z
	} else {
		a.prefs.NonBBMZoom = &z
	}
}

// bbmNavigate is a tab switch inside the mode (the BBM list, the
// breadcrumbs' first crumb): it lands on the tab's list.
func (a *App) bbmNavigate(route string) {
	a.bbm.tabSwitch = true
	a.Navigate(route)
	a.bbm.tabSwitch = false
}

// bbmTabHasList: the tabs with a Sidebar view in Slack's default view
// sets (pl6F): Home (ChannelList), DMs (DmsList), Files (the files
// sidebar) and the BBM tab (BBMMain). Activity and To-dos are one
// primary view each.
func (a *App) bbmTabHasList(r Route) bool {
	if isBBMRoute(r) {
		return true
	}
	switch a.routeTab(r) {
	case "home", "dms", "files":
		return true
	}
	return false
}

// routeTab is the rail tab a route belongs to (activeRail for any route).
func (a *App) routeTab(r Route) string {
	switch p := r.Page(); p {
	case "bbm":
		return "bbm"
	case "dms", "activity", "files", "later":
		return p
	case "more":
		return "platform"
	}
	return "home"
}

// bbmAfterRoute is zluD getDestinationContainer + updateFullscreenContainer
// for a navigation: a tab switch shows the new tab's list (when it has
// one), a thread its secondary pane, anything else its primary view.
func (a *App) bbmAfterRoute(prev, r Route) {
	if !a.bbmOn() {
		return
	}
	tabSwitch := a.bbm.tabSwitch || a.routeTab(prev) != a.routeTab(r) && r == Route(a.routeTab(r))
	switch {
	case isBBMRoute(r):
		a.bbm.cont = bbmSidebar
	case tabSwitch && a.bbmTabHasList(r):
		a.bbm.cont = bbmSidebar
	case r.Thread() != "":
		a.bbm.cont = bbmSecondary
	default:
		a.bbm.cont = bbmPrimary
	}
}

// PaneChosen is views' hook for a pick in a tab's own list (the DMs list,
// the Files nav): in the mode the panel moves on to what was picked, as
// Slack's navigation to the primary container does.
func (a *App) PaneChosen() {
	if a.bbmOn() && a.bbm.cont == bbmSidebar {
		a.bbm.cont = bbmPrimary
		a.Invalidate()
	}
}

// bbmSyncSecondary follows the secondary pane opening and closing: open
// fills the panel with it (fullscreenNavigateListener: the destination is
// the secondary container), closed goes back to the primary view.
func (a *App) bbmSyncSecondary() {
	open := a.secondary.open()
	switch {
	case open && !a.bbm.secOpen:
		a.bbm.cont = bbmSecondary
	case !open && a.bbm.cont == bbmSecondary:
		a.bbm.cont = bbmPrimary
	}
	a.bbm.secOpen = open
}

// bbmNoListKeys: with the channel or DMs list filling the panel, Alt+↑/↓
// don't step conversations (N8Zu cannotMoveTo…: u = BBM && the
// fullscreen container is the sidebar's list).
func (a *App) bbmNoListKeys() bool {
	return a.bbmOn() && a.bbm.cont == bbmSidebar
}

// ---- window layout ----

// Slack's fullscreen client (26v2 FullscreenContainer, client-boot-styles):
// .p-ia4_client--fullscreen{border-radius:12px;position:absolute;inset:40px
// 4px 4px} -main{max-width:1200px;margin:auto} -breadcrumbs{top:72px}.
const (
	bbmMaxW      = 1200
	bbmInset     = 4
	bbmRadius    = 12
	bbmCrumbsTop = 72
)

// bbmPanel is the panel's rectangle for a window of size sz.
func bbmPanel(gtx layout.Context, sz image.Point, crumbs bool) image.Rectangle {
	in := gtx.Dp(bbmInset)
	w := min(sz.X-2*in, gtx.Dp(bbmMaxW))
	x := (sz.X - w) / 2
	top := gtx.Dp(theme.TopNavH)
	if crumbs {
		top = gtx.Dp(bbmCrumbsTop)
	}
	return image.Rect(x, top, x+w, sz.Y-in)
}

// layoutBBM draws the window in the mode: the chrome, the top bar, the
// breadcrumbs (shouldDisplayBreadcrumbs: not on the BBM tab) and the
// panel with the one container. It returns the panel.
func (a *App) layoutBBM(gtx layout.Context) image.Rectangle {
	sz := gtx.Constraints.Max
	r := a.router.Current()
	a.bbmSyncSecondary()
	a.grad.draw(gtx, sz, image.Point{}, clip.Rect{Max: sz}.Op(), a.th.C.ChromeA, a.th.C.ChromeB)
	crumbs := !isBBMRoute(r)
	panel := bbmPanel(gtx, image.Pt(sz.X, sz.Y-a.huddles().DockHeight(gtx)), crumbs) // the huddle bar's row
	a.th.Chrome = true
	a.layoutBBMTopNav(gtx, sz.X)
	if crumbs {
		a.layoutBreadcrumbs(gtx, sz.X)
	}
	a.th.Chrome = false

	rad := gtx.Dp(bbmRadius)
	list := isBBMRoute(r) || a.bbm.cont == bbmSidebar && a.bbmTabHasList(r)
	sec := a.bbm.cont == bbmSecondary && a.secondary.open()
	a.primaryOrigin = panel.Min
	if list {
		// the list views keep the sidebar's surface (.p-view_contents--sidebar)
		a.grad.draw(gtx, sz, image.Point{}, clip.UniformRRect(panel, rad).Op(gtx.Ops), a.th.C.ChromeA, a.th.C.ChromeB, a.th.C.SidebarBg)
	}
	func() {
		defer clip.UniformRRect(panel, rad).Push(gtx.Ops).Pop()
		if !list {
			theme.Fill(gtx, panel, a.th.C.Bg)
		}
		switch {
		case isBBMRoute(r):
			a.sidebar.hover = a.ptrWin.In(panel)
			sv := clsSidebarView.Enter(gtx, 0, 0)
			a.th.Chrome = true
			a.bbmListBase = a.bbmSurface(panel)
			atExact(gtx, panel.Min, panel.Size(), a.layoutBBMView)
			a.th.Chrome = false
			sv.Leave()
		case list && a.routeTab(r) == "home":
			a.sidebar.hover = a.ptrWin.In(panel)
			sv := clsSidebarView.Enter(gtx, 0, 0)
			a.th.Chrome = true
			atExact(gtx, panel.Min, panel.Size(), a.layoutSidebar)
			a.th.Chrome = false
			sv.Leave()
		case sec:
			a.secondaryOrigin = panel.Min
			atExact(gtx, panel.Min, panel.Size(), func(gtx layout.Context) layout.Dimensions {
				sv := clsSecondary.Enter(gtx, 0, 0)
				defer sv.Leave()
				return a.layoutSecondary(gtx)
			})
		default: // a page's own list (DMs, Files) or any primary view
			atExact(gtx, panel.Min, panel.Size(), a.layoutPrimary)
		}
	}()
	if b := a.th.C.WorkspaceBorder; b.A > 0 {
		theme.Border(gtx, panel, rad, gtx.Dp(1), theme.Over(b, a.chromeBase(panel.Min)))
	}
	return panel
}

// bbmSurface is the list surface's effective colour mid-panel, for
// flattening the translucent sidebar colours drawn on it.
func (a *App) bbmSurface(panel image.Rectangle) color.NRGBA {
	c := panel.Min.Add(panel.Max).Div(2)
	return theme.Over(a.th.C.SidebarBg, gradientColor(a.winSize, c.X, c.Y, a.th.C.ChromeA, a.th.C.ChromeB))
}

// ---- top bar ----

// The mode's top bar (26v2 TopNav with L = BBM, client-boot-styles):
// .p-ia4_bbm_top_nav{justify-content:center;padding:0}, its wrapper
// max-width 1200px; the left container holds Back, Forward (RPnY
// HistoryButtons, 16px icons) and History; the search fills the middle
// (min-width 192px, margin-right 8px, 28px tall); the right container
// (topNavRightContainerButtons, gap 8px) holds the user menu (28px
// avatar), Create, Help (16px) and Exit (ExitBBMButton: sign-out 16,
// tip "Exit", label "Exit Simplified layout mode"). The tab rail's
// workspace switcher, megaphone and agent buttons are left out.
func (a *App) layoutBBMTopNav(gtx layout.Context, width int) {
	c := &a.th.C
	h := gtx.Dp(theme.TopNavH)
	tn := clsTopNav.Enter(gtx, 0, 0)
	defer tn.Leave()
	if tn.Hidden() {
		return
	}
	// the wrapper: 1200 wide, centred; leftContainerMargin /
	// rightContainerMargin are 8px below a 1208px window (26v2 s(e)).
	ww := min(width, gtx.Dp(bbmMaxW))
	x0 := (width - ww) / 2
	x1 := x0 + ww
	if width < gtx.Dp(1208) {
		x0, x1 = gtx.Dp(8), width-gtx.Dp(8)
	}
	bh := gtx.Dp(28)
	y := (h - bh) / 2

	// left: Back, Forward (.p-ia4_history_buttons gap 4px, each with a 1px
	// margin), then History (margin-inline -4px 0 after the 8px gap)
	x := x0 + gtx.Dp(1)
	for i, name := range []string{"back", "forward"} {
		dis := i == 0 && !a.router.CanBack() || i == 1 && !a.router.CanForward()
		org := image.Pt(x, y)
		d := a.bbmNavButton(gtx, org, "nav."+name, name, dis)
		x += d.X + gtx.Dp(4) + gtx.Dp(2)
	}
	x += -gtx.Dp(4) - gtx.Dp(1) + gtx.Dp(8) - gtx.Dp(4) + gtx.Dp(1)
	hd := a.bbmNavButton(gtx, image.Pt(x, y), "nav.history", "clock", false)
	histBtn := image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x, y).Add(hd)}
	x += hd.X + gtx.Dp(1)
	if clicked(a.clicks.get("nav.back")) {
		a.Back()
	}
	if clicked(a.clicks.get("nav.forward")) {
		a.Forward()
	}
	if clicked(a.clicks.get("nav.history")) {
		a.openHistoryMenu(histBtn)
	}

	// right, from the right edge (margin-right 4px): Exit, Help, Create,
	// the user menu; 8px apart
	rx := x1 - gtx.Dp(4)
	place := func(key, icon string) image.Point {
		w := gtx.Dp(8)*2 + gtx.Dp(16)
		org := image.Pt(rx-w, y)
		a.bbmNavButton(gtx, org, key, icon, false)
		rx = org.X - gtx.Dp(8)
		return org
	}
	place("bbm.exit", "sign-out")
	horg := place("nav.help", "help")
	corg := place("bbm.create", "plus")
	// the user menu: 28px avatar (.userMenu height 28px), its presence
	// in content-inv-pry / base-inv-hgl-2 (.mainHeaderUser)
	as := gtx.Dp(28)
	aorg := image.Pt(rx-as, y)
	at(gtx, aorg, image.Pt(as, as), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("bbm.me"), nil, func(gtx layout.Context) layout.Dimensions {
			// .c-base_icon radius clamp(4px, 22.222%, 12px): 6px at 28
			a.avatar(gtx, a.selfAvatar(), a.selfName(), as, as*2/9)
			pc := image.Pt(as-gtx.Dp(3), as-gtx.Dp(3))
			theme.Circle(gtx, pc, gtx.Dp(11), a.chromeBase(aorg.Add(pc)))
			if a.selfActive() {
				theme.Circle(gtx, pc, gtx.Dp(8), c.Presence)
			} else {
				theme.Circle(gtx, pc, gtx.Dp(8), c.ChromeText)
				theme.Circle(gtx, pc, gtx.Dp(4), a.chromeBase(aorg.Add(pc)))
			}
			return layout.Dimensions{Size: image.Pt(as, as)}
		})
	})
	rx = aorg.X - gtx.Dp(8)
	if clicked(a.clicks.get("bbm.exit")) {
		a.stopBBM()
	}
	if clicked(a.clicks.get("nav.help")) {
		a.helpClick()
	}
	if clicked(a.clicks.get("bbm.create")) {
		// CreateMenuTrigger in the mode: position bottom-right, no offsets
		a.openCreateMenu(image.Rectangle{Min: corg, Max: corg.Add(image.Pt(gtx.Dp(32), bh))})
	}
	if clicked(a.clicks.get("bbm.me")) {
		a.openSelfMenu(image.Pt(aorg.X, aorg.Y+as))
	}
	_ = horg

	// middle: the search (flex auto, margin-right 8px)
	sw := rx - x
	if sw < gtx.Dp(192) {
		sw = gtx.Dp(192)
	}
	a.layoutSearchPill(gtx, image.Pt(x, y), sw, bh)
}

// bbmNavButton is .topNavButton: 28px tall, padding-inline 8px, radius 4,
// background theme-surf-inv-pry (the search's), hover surf-inv
// (#ffffff2e); aria-disabled ones at .45 opacity. It returns the size.
func (a *App) bbmNavButton(gtx layout.Context, org image.Point, key, icon string, disabled bool) image.Point {
	c := &a.th.C
	btn := a.clicks.get(key)
	sz := image.Pt(gtx.Dp(8)*2+gtx.Dp(16), gtx.Dp(28))
	at(gtx, org, sz, func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
			bg := c.SearchBg
			if hovered && !disabled {
				bg = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x2e}
			}
			a.chrome(gtx, org, image.Rectangle{Max: s}, gtx.Dp(4), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			col := c.ChromeText
			if disabled {
				col = theme.WithAlpha(col, 0x73)
			}
			at(gtx, image.Pt(gtx.Dp(8), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 16, col)
			})
			return layout.Dimensions{Size: sz}
		})
	})
	return sz
}

// layoutSearchPill is the top bar's search box ("Search <team>"), w×h at
// org; it opens the search.
func (a *App) layoutSearchPill(gtx layout.Context, sorg image.Point, sw, sh int) {
	c := &a.th.C
	a.searchPill = image.Rectangle{Min: sorg, Max: sorg.Add(image.Pt(sw, sh))}
	at(gtx, sorg, image.Pt(sw, sh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("nav.search"), func(gtx layout.Context, sz image.Point, hovered bool) {
			bg := c.SearchBg
			if hovered {
				bg = c.SearchHover
			}
			// Slack: rgba(249,237,255,.25) plus a rgba(131,56,138,.08) tint.
			a.chrome(gtx, sorg, image.Rectangle{Max: sz}, gtx.Dp(4), bg, theme.SearchTint)
		}, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(sw, sh)
			at(gtx, image.Pt(gtx.Dp(10), (sh-gtx.Dp(16))/2), sz, func(gtx layout.Context) layout.Dimensions {
				return icons.Search(gtx, 16, c.SearchText)
			})
			label := "Search " + a.teamName()
			if q := a.searchQuery; q != "" {
				label = q
			}
			at(gtx, image.Pt(gtx.Dp(34), 0), image.Pt(sw-gtx.Dp(44), sh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.SearchText, label)
				})
			})
			return layout.Dimensions{Size: sz}
		})
	})
	if clicked(a.clicks.get("nav.search")) {
		a.openSearch("")
	}
}

// ---- breadcrumbs ----

// bbmCrumb is one breadcrumb (26v2 Breadcrumb: a caret-right 16 before
// all but the first; the label underlined; aria-current on the last).
type bbmCrumb struct {
	key    string
	label  string
	icon   string // the channel's icon before a conversation's name
	bold   bool   // .highlightedBreadcrumb (unreads): 600
	badge  int    // a mention count (the workspace and tab crumbs)
	dot    bool   // an unread dot (CZTc)
	last   bool
	action func()
}

// bbmCrumbs is Breadcrumbs: BreadcrumbForMain (the workspace: back to the
// BBM tab), BreadcrumbForTab (the tab's list, when the tab has one) and
// BreadcrumbForViewContainer for the primary and the secondary views, each
// only up to the container on screen (shouldIncludeBreadcrumb).
func (a *App) bbmCrumbs() []bbmCrumb {
	r := a.router.Current()
	m := a.model()
	cont := a.bbm.cont
	var cs []bbmCrumb
	team := bbmCrumb{key: "main", label: a.teamName(), action: func() { a.bbmNavigate("bbm") }}
	// LGx0 mention counts / zxm6 unread workspaces: the workspace's
	// mentions, else a dot for its unreads
	if n := m.mentionTotal(); n > 0 {
		team.badge = n
	} else if m.HomeUnread || m.DMUnread {
		team.dot = true
	}
	cs = append(cs, team)
	tab := a.routeTab(r)
	if a.bbmTabHasList(r) {
		rt := a.railTabFor(railTabID(tab))
		n, dot := a.railBadge(tab)
		cs = append(cs, bbmCrumb{key: "tab", label: rt.label, badge: n, dot: dot && n == 0, last: cont == bbmSidebar,
			action: func() { a.bbm.cont = bbmSidebar; a.Invalidate() }})
	}
	if cont == bbmSidebar && a.bbmTabHasList(r) {
		return markLast(cs)
	}
	// the primary view: a conversation's name (no #, its icon before it),
	// a DM's person, a page's title
	pc := bbmCrumb{key: "primary", last: cont == bbmPrimary, action: func() { a.bbm.cont = bbmPrimary; a.Invalidate() }}
	if ch := r.Channel(); ch != "" {
		if cv := m.conv(ch); cv != nil {
			pc.label = cv.displayName()
			pc.bold = cv.Unread && !cv.Muted
			switch cv.Kind { // useViewBreadcrumbIcon: a channel's icon
			case "channel":
				pc.icon = "hash"
			case "private":
				pc.icon = "lock"
			}
		}
	} else if v, ok := a.views[r].(interface{ Selected() string }); ok && v.Selected() != "" {
		if cv := m.conv(v.Selected()); cv != nil {
			pc.label = cv.displayName()
			pc.bold = cv.Unread && !cv.Muted
		}
	} else {
		pc.label = pageTitle(r)
	}
	if pc.label != "" {
		cs = append(cs, pc)
	}
	if cont == bbmSecondary && a.secondary.open() {
		label := "Thread" // o4[Thread]
		switch a.secondary.kind {
		case secProfile:
			label = "Profile"
		case secDetails:
			label = "Details"
		case secCode: // no breadcrumb label: getAriaLabelForViewContent (session_channel_views)
			label = a.codeDetailsLabel()
		case secSearch:
			label = "Search"
		case secHelp:
			label = "Help"
		}
		cs = append(cs, bbmCrumb{key: "secondary", label: label, last: true})
	}
	return markLast(cs)
}

func markLast(cs []bbmCrumb) []bbmCrumb {
	for i := range cs {
		cs[i].last = i == len(cs)-1
	}
	return cs
}

// railTabID maps a rail key back to Slack's tab id for its label.
func railTabID(key string) string {
	switch key {
	case "activity":
		return "activity"
	case "files":
		return "unified-files"
	}
	return key
}

// layoutBreadcrumbs is .breadcrumbs: max-width 1200px, centred,
// padding-inline 12px, between the top bar and the panel (40–72px);
// each crumb 26px tall, its label underlined (not the current one,
// aria-disabled), a mention badge (16px, 11px) or an unread dot after
// the workspace and tab names.
func (a *App) layoutBreadcrumbs(gtx layout.Context, width int) {
	c := &a.th.C
	top := gtx.Dp(theme.TopNavH)
	rowH := gtx.Dp(bbmCrumbsTop) - top
	ww := min(width, gtx.Dp(bbmMaxW))
	x := (width-ww)/2 + gtx.Dp(12)
	ch := gtx.Dp(26)
	y := top + (rowH-ch)/2
	for i, cr := range a.bbmCrumbs() {
		cr := cr
		if i > 0 {
			at(gtx, image.Pt(x, y+(ch-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("caret-right")(gtx, 16, c.ChromeText)
			})
			x += gtx.Dp(16)
		}
		weight := theme.Regular
		if cr.bold {
			weight = theme.Semi
		}
		lw := a.th.Measure(gtx, 15, weight, cr.label)
		w := lw
		if cr.icon != "" {
			w += gtx.Dp(18)
		}
		bw := 0
		if cr.badge > 0 {
			bw = gtx.Dp(4) + gtx.Dp(22)
		} else if cr.dot {
			bw = gtx.Dp(4) + gtx.Dp(8)
		}
		btn := a.clicks.get("bbm.crumb." + cr.key)
		org := image.Pt(x, y)
		at(gtx, org, image.Pt(w+bw, ch), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				lx := 0
				if cr.icon != "" {
					at(gtx, image.Pt(0, (ch-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(cr.icon)(gtx, 16, c.ChromeText)
					})
					lx = gtx.Dp(18)
				}
				at(gtx, image.Pt(lx, 0), image.Pt(lw+2, ch), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 15, weight, c.ChromeText, cr.label)
					})
				})
				if !cr.last { // .label{text-decoration:underline}
					uy := ch/2 + gtx.Dp(8)
					theme.Fill(gtx, image.Rect(lx, uy, lx+lw, uy+max(1, gtx.Dp(1))), c.ChromeText)
				}
				bx := lx + lw + gtx.Dp(4)
				switch {
				case cr.badge > 0:
					a.bbmBadge(gtx, image.Pt(bx, (ch-gtx.Dp(16))/2), cr.badge, 22, 16)
				case cr.dot:
					theme.Circle(gtx, image.Pt(bx+gtx.Dp(4), ch/2-gtx.Dp(6)+gtx.Dp(4)), gtx.Dp(8), c.ChromeText)
				}
				return layout.Dimensions{Size: image.Pt(w+bw, ch)}
			})
		})
		if clicked(btn) && !cr.last && cr.action != nil {
			cr.action()
		}
		x += w + bw
	}
}

// bbmBadge is the mode's mention badge (.mentionBadge: theme-base-imp,
// theme-content-imp, 11px): w×h dp at org, wider for longer counts.
func (a *App) bbmBadge(gtx layout.Context, org image.Point, n, w, h int) {
	c := &a.th.C
	txt := itoa(n)
	if n > 99 {
		txt = "99+"
	}
	tw := a.th.Measure(gtx, 11, theme.Bold, txt)
	bw := max(gtx.Dp(unit.Dp(w)), tw+gtx.Dp(8))
	bh := gtx.Dp(unit.Dp(h))
	theme.RRect(gtx, image.Rectangle{Min: org, Max: org.Add(image.Pt(bw, bh))}, bh/2, c.Badge)
	at(gtx, org.Add(image.Pt((bw-tw)/2, 0)), image.Pt(tw+2, bh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 11, theme.Bold, c.BadgeText, txt)
		})
	})
}
