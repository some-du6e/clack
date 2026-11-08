// Package ui is the root of yoloslack's native (Gio) client: the window and
// event loop, the Slack shell (top nav, tab rail, sidebar, primary panel,
// secondary pane), routing and history, keyboard shortcuts, notifications and
// the Host implementation every feature package talks to.
package ui

import (
	"context"
	"image"
	"image/color"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/x/explorer"

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/huddleui"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// AppName is the product name (Karim picked "Clack" on 2026-11-06). The
// binary, config dir (~/.config/clack) and CI artifacts are named clack; the
// Go module path keeps its old name.
const AppName = "Clack"

// Overlay is anything drawn above the shell. An alias of an unnamed
// interface (like popups.Overlay) so ShowOverlay's signature is identical
// across every package's Host interface.
type Overlay = interface {
	Layout(gtx layout.Context) layout.Dimensions
	Dismissed() bool
}

// View is a mounted feature widget.
type View interface {
	Layout(gtx layout.Context) layout.Dimensions
}

// Options configures the app.
type Options struct {
	ConfigDir string
	Dark      bool
	// Login, when set, shows the native sign-in screen first and is called
	// with what the user pasted; it returns nil once signed in.
	Login func(in slack.SignInInput) error
	// Started is the process start time, for the startup-latency log.
	Started time.Time
	// SignOut forgets the stored credentials.
	SignOut func()
	// SwitchAccount signs in with other credentials (Taut's api.accounts):
	// save them and reconnect. Nil: plugins can't switch accounts.
	SwitchAccount func(c slack.Creds)
	// CheckUpdate asks whether a newer release exists (cmd/clack: GitHub
	// Releases). Called off the UI goroutine while Preferences > Advanced >
	// "Check for updates" is on; ok=true shows the "Update available"
	// banner. Nil: no update checks (dev builds).
	CheckUpdate func(ctx context.Context) (u UpdateInfo, ok bool, err error)
	// OnWindowEvent sees every window event before the UI (cmd/clack:
	// window icons from the platform view events).
	OnWindowEvent func(e event.Event)
	// NoRealtime keeps Slack's realtime websocket closed whatever the
	// saved preference says (cmd/clack -no-realtime, CLACK_NO_REALTIME=1).
	NoRealtime bool
}

// SetLogin shows the sign-in screen, calling login on submit.
func (a *App) SetLogin(login func(in slack.SignInInput) error) {
	a.opts.Login = login
	a.login = newLoginScreen()
}

// SetLoginHandler sets the sign-in function used after "Sign out" without
// showing the screen now.
func (a *App) SetLoginHandler(login func(in slack.SignInInput) error) {
	if a.opts.Login == nil {
		a.opts.Login = login
	}
}

// App is the whole UI. It implements every feature package's Host.
type App struct {
	opts   Options
	win    *app.Window
	th     *theme.Theme
	images *imgcache.Cache
	data   *backend // nil until signed in

	router          router
	clicks          clickables
	grad            gradient
	sidebar         sidebarState
	secondary       secondaryState
	searchQuery     string
	searchPill      image.Rectangle // window coords, for anchoring the search popover
	primaryOrigin   image.Point     // window position of the primary panel
	pvOrigin        originTracker   // ClientPrimaryView's origin under a patch
	vbOrigin        originTracker   // the primary view body's, inside a patched RenderedViewContents
	sbMenu          sbMenus         // sidebar row menus (slick_shell.go)
	secondaryOrigin image.Point     // window position of the secondary pane
	chBodyW         int             // >0: layoutChannel's messages+composer width (nested secondary pane)
	convos          map[string]*convo
	convoOrder      []string
	ptrTag          int
	ptrWin          image.Point
	ptrOK           bool
	overlays        []Overlay
	slackc          slackcState
	toast           toastState
	keys            *shortcuts
	login           *loginScreen

	views     map[Route]View // cached primary views (instant switch)
	viewOrder []Route

	mu      sync.Mutex
	pending []func()

	title       string
	firstPaint  bool
	frameStats  frameStats
	lastChannel string
	pxPerDp     float32
	chTab       map[string]int
	clipboard   string
	prefs       prefs
	prefsDirty  bool
	notify      notifier
	focused     bool
	winSize     image.Point
	sbBase      color.NRGBA
	sbPal       theme.Palette
	styleVer    uint64 // slackc.Styles.Version the palette was derived at
	shaperCh    chan [2]*text.Shaper
	explorer    *explorer.Explorer    // native file chooser (set in onWindow)
	tautGen     int                   // bumps when a (new) plugin runtime attaches
	tautTabs    []taut.PreferencesTab // plugins' Preferences tabs
	switchAt    time.Time             // navigation time, for the channel-switch latency log
	switchTo    Route
	winLive     atomic.Pointer[app.Window] // a.win once Run made it, for other goroutines
	update      updateState                // "Update available" banner
	rtAsk       realtimeState              // first-run "Live updates are off" banner
	updatesOff  atomic.Bool                // prefs.NoUpdateCheck, for updateLoop
	hud         *huddleui.Controller       // the native huddle (huddle_wire.go)

	swipe    swiper          // trackpad history swipes (swipe.go, nav.go)
	swipeNow swipeState      // this frame's swipe progress, for the arrow
	navTag   int             // the window-wide scroll handler (navGestures)
	ptrBtns  pointer.Buttons // mouse buttons held, for back/forward (navButtons)
}

// New creates the app. Call Run on the UI goroutine (main).
func New(opts Options, images *imgcache.Cache) *App {
	if opts.Started.IsZero() {
		opts.Started = time.Now()
	}
	a := &App{
		opts:   opts,
		th:     theme.Bare(opts.Dark),
		images: images,
		clicks: clickables{},
		views:  map[Route]View{},
	}
	a.sidebar.width = theme.SidebarW
	a.secondary.width = theme.SecondaryW
	a.keys = a.defaultShortcuts()
	a.loadPrefs()
	// The shaper (font parsing + system font index) is the slowest part of
	// startup; build it while the window and GPU context come up.
	a.shaperCh = make(chan [2]*text.Shaper, 1)
	go func() {
		t := time.Now()
		sh := theme.NewShaper() // one collection serves both modes
		a.shaperCh <- [2]*text.Shaper{sh, sh}
		a.timing("shapers ready", t)
	}()
	if opts.Login != nil {
		a.login = newLoginScreen()
	}
	return a
}

// Post runs f on the UI goroutine before the next frame (safe from any
// goroutine) and wakes the window.
func (a *App) Post(f func()) {
	a.mu.Lock()
	a.pending = append(a.pending, f)
	a.mu.Unlock()
	a.Invalidate()
}

// Invalidate requests a redraw (safe from any goroutine).
func (a *App) Invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// Run opens the window and blocks until it closes.
func (a *App) Run() error {
	a.timing("run", a.opts.Started)
	a.win = new(app.Window)
	a.win.Option(
		app.Title(AppName),
		app.Size(1440, 900),
		app.MinSize(800, 500),
	)
	a.winLive.Store(a.win)
	go a.updateLoop()
	if a.images != nil {
		a.images.SetInvalidate(a.win.Invalidate)
		a.th.Images = a.images
	}
	if installHooks != nil {
		installHooks(a)
	}
	if onWindow != nil {
		onWindow(a)
	}
	if startNavGlue != nil {
		startNavGlue(a)
	}
	if a.data != nil {
		a.data.start(a)
	}
	var ops op.Ops
	for {
		ev := a.win.Event()
		if a.opts.OnWindowEvent != nil {
			a.opts.OnWindowEvent(ev)
		}
		if onEvent != nil {
			onEvent(ev)
		}
		switch e := ev.(type) {
		case app.DestroyEvent:
			if a.hud != nil {
				a.hud.Close() // leave the huddle cleanly (Chime LEAVE)
			}
			if a.data != nil {
				a.data.stop()
			}
			return e.Err
		case app.ConfigEvent:
			a.focused = e.Config.Focused
		case app.FrameEvent:
			t0 := time.Now()
			if a.th.Shaper == nil {
				a.timing("first FrameEvent", a.opts.Started)
				sh := <-a.shaperCh
				a.th.SetShapers(sh[0], sh[1])
				a.timing("shaper waited", t0)
			}
			// patch points (slackc) are shared with the huddle call window
			slackc.LockFrame()
			a.drain()
			gtx := app.NewContext(&ops, e)
			before := a.router.Current()
			a.frame(gtx)
			if a.router.Current() != before {
				// A click navigated mid-frame (after the sidebar/panel split
				// was computed): lay out again so the new page never shows
				// one frame with the old layout. Layout is ~2 ms.
				ops.Reset()
				gtx = app.NewContext(&ops, e)
				a.frame(gtx)
			}
			slackc.UnlockFrame()
			tl := time.Now()
			e.Frame(gtx.Ops)
			if !a.firstPaint {
				a.timing("first layout", t0)
				a.timing("first GPU submit", tl)
			}
			a.frameStats.add(time.Since(t0), tl.Sub(t0))
			if frameLog {
				log.Printf("frame: layout %.2fms total %.1fms", float64(tl.Sub(t0).Microseconds())/1000, float64(time.Since(t0).Microseconds())/1000)
			}
			if !a.switchAt.IsZero() {
				log.Printf("ui: switch to %s: %v to frame submitted (layout %v, gpu %v)", a.switchTo.Page(), time.Since(a.switchAt).Round(10*time.Microsecond), tl.Sub(t0).Round(10*time.Microsecond), time.Since(tl).Round(10*time.Microsecond))
				a.switchAt = time.Time{}
			}
			if !a.firstPaint {
				a.firstPaint = true
				log.Printf("ui: first frame painted %v after start (%d sidebar rows)", time.Since(a.opts.Started).Round(time.Millisecond), a.sidebar.rowCount)
			}
		}
	}
}

// envFlag reads CLACK_<name> (or the pre-rename YOLOSLACK_<name>).
func envFlag(name string) bool {
	return os.Getenv("CLACK_"+name) != "" || os.Getenv("YOLOSLACK_"+name) != ""
}

// frameLog (CLACK_FRAMELOG=1) logs every frame's layout/total time.
var frameLog = envFlag("FRAMELOG")

// timing logs a startup/latency measurement when YOLOSLACK_TIMING is set
// (always for the first frame).
func (a *App) timing(what string, since time.Time) {
	if envFlag("TIMING") {
		log.Printf("timing: %s: %v", what, time.Since(since).Round(100*time.Microsecond))
	}
}

func (a *App) drain() {
	a.mu.Lock()
	p := a.pending
	a.pending = nil
	a.mu.Unlock()
	for _, f := range p {
		f()
	}
}

// frame lays out the whole window.
func (a *App) frame(gtx layout.Context) {
	sz := gtx.Constraints.Max
	a.pxPerDp = gtx.Metric.PxPerDp
	a.winSize = sz
	a.syncThemeTokens()

	if a.login != nil {
		a.grad.layout(gtx, a.th.C.ChromeA, a.th.C.ChromeB)
		a.layoutLogin(gtx)
		return
	}

	// first: an ancestor of every other input area (popped at the end)
	navArea := a.navGestures(gtx)
	defer navArea.Pop()
	a.readRootPointer(gtx)
	a.slackcFrame(gtx)
	a.keys.process(gtx, a)
	a.updateTitle()

	// Slack's ancestor chain for plugin CSS (.p-client_container > .p-ia…);
	// overlays draw outside it, like Slack's modal portals.
	client := clsClient.Enter(gtx, 0, 0)
	ia := clsIA.Enter(gtx, 0, 0)

	navH := gtx.Dp(theme.TopNavH)
	railW := gtx.Dp(theme.RailW)
	inset := gtx.Dp(theme.PanelInset)
	sbW := gtx.Dp(unit.Dp(a.sidebar.width))
	if a.sidebar.hidden || pageOwnsWorkspace(a.router.Current()) {
		sbW = 0
	}
	bannerH := a.updateBannerH(gtx) // the workspace starts below the update banner
	askRT := a.realtimeAskH(gtx)    // or the live-updates question, which comes first
	if askRT > 0 {
		bannerH = askRT
	}
	ws := image.Rect(railW, navH+bannerH, sz.X-inset, sz.Y-inset)
	primary := image.Rect(ws.Min.X+sbW, ws.Min.Y, ws.Max.X, ws.Max.Y)
	a.primaryOrigin = primary.Min

	// Paint the gradient only where it shows (nav, rail, insets and the
	// panel's rounded corners): full-window fills are the main per-frame
	// cost on software GL.
	rad := gtx.Dp(theme.PanelRadius)
	for _, r := range []image.Rectangle{
		image.Rect(0, 0, sz.X, ws.Min.Y+rad),
		image.Rect(0, navH, railW+rad, sz.Y),
		image.Rect(ws.Max.X-rad, navH, sz.X, sz.Y),
		image.Rect(railW, ws.Max.Y-rad, sz.X, sz.Y),
	} {
		a.grad.draw(gtx, sz, image.Point{}, clip.Rect(r).Op(), a.th.C.ChromeA, a.th.C.ChromeB)
	}

	// The top nav stays where the Home layout puts it, even on pages that
	// hide the sidebar (Files, DMs, Activity, To-dos), like Slack.
	navX := primary.Min.X
	if !a.sidebar.hidden {
		navX = ws.Min.X + gtx.Dp(unit.Dp(a.sidebar.width))
	}
	a.th.Chrome = true // light-on-aubergine text fade (theme.lightTextColor)
	a.layoutTopNav(gtx, navX, sz.X)
	a.layoutRail(gtx, navH, sz.Y-navH)
	a.th.Chrome = false
	if askRT > 0 {
		a.layoutRealtimeBanner(gtx, image.Rect(ws.Min.X, navH, ws.Max.X, ws.Min.Y-gtx.Dp(bannerGap)))
	} else if bannerH > 0 {
		a.layoutUpdateBanner(gtx, image.Rect(ws.Min.X, navH, ws.Max.X, ws.Min.Y-gtx.Dp(bannerGap)))
	}

	// workspace: sidebar (translucent) + primary panel, one rounded unit
	// with Slack's soft shadow (-4px -4px 16px rgba(0,0,0,.09)).
	if !a.th.Dark { // dark mode uses Slack's 1px outline instead (below)
		theme.Shadow(gtx, ws.Add(image.Pt(-gtx.Dp(2), -gtx.Dp(2))), rad, gtx.Dp(8), a.th.C.PanelShadow)
	}
	if sbW > 0 {
		sb := image.Rect(ws.Min.X, ws.Min.Y, primary.Min.X, ws.Max.Y)
		a.grad.draw(gtx, sz, image.Point{}, clip.RRect{Rect: sb, NW: rad, SW: rad}.Op(gtx.Ops), a.th.C.ChromeA, a.th.C.ChromeB, a.th.C.SidebarBg)
		a.sidebar.hover = a.ptrWin.In(sb)
		sv := clsSidebarView.Enter(gtx, 0, 0)
		a.th.Chrome = true
		atExact(gtx, sb.Min, sb.Size(), a.layoutSidebar)
		a.th.Chrome = false
		sv.Leave()
	}
	nw, sw := 0, 0
	if sbW == 0 {
		nw, sw = rad, rad
	}
	func() {
		defer clip.RRect{Rect: primary, NW: nw, SW: sw, NE: rad, SE: rad}.Push(gtx.Ops).Pop()
		theme.Fill(gtx, primary, a.th.C.Bg)
		atExact(gtx, primary.Min, primary.Size(), a.layoutPrimary)
	}()
	if sbW > 0 {
		a.layoutSidebarResize(gtx, primary.Min.X, ws.Min.Y, ws.Dy())
	}
	if b := a.th.C.WorkspaceBorder; b.A > 0 {
		// Slack dark mode outlines the workspace and the sidebar/panel seam.
		bc := theme.Over(b, a.chromeBase(ws.Min))
		theme.Border(gtx, ws, rad, gtx.Dp(1), bc)
		if sbW > 0 {
			theme.Fill(gtx, image.Rect(primary.Min.X, ws.Min.Y, primary.Min.X+gtx.Dp(1), ws.Max.Y), bc)
		}
	}

	ia.Leave()
	client.Leave()

	a.layoutSwipe(gtx, primary)
	a.layoutOverlays(gtx)
	a.slackcOverlays(gtx)
	a.layoutToast(gtx)
	a.rootPointer(gtx)
	a.keys.processGlobal(gtx) // last: only the keys no widget took
}

// ---- primary panel ----

func (a *App) layoutPrimary(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	secW := 0
	if a.secondary.open() {
		secW = gtx.Dp(unit.Dp(a.secondary.width))
		if max := sz.X - gtx.Dp(360); secW > max {
			secW = max
		}
		if secW < gtx.Dp(theme.SecondaryMin) {
			secW = gtx.Dp(theme.SecondaryMin)
		}
	}
	mainW := sz.X - secW
	// Slack 2026 nests a channel's thread/profile pane inside the channel
	// view, below its header and tabs (p-ia4_client--nested-secondary-view):
	// the header and tabs keep the full width.
	nested := secW > 0 && a.router.Current().Page() == "c" && (a.secondary.kind == secThread || a.secondary.kind == secProfile)
	primW, secTop := mainW, 0
	if nested {
		primW, secTop = sz.X, gtx.Dp(theme.HeaderH)+gtx.Dp(theme.TabsH)
	}
	// the panel's window origin: a.primaryOrigin becomes the body's when a
	// plugin draws above it, the secondary pane stays at the panel's
	panelOrigin := a.primaryOrigin
	atExact(gtx, image.Pt(0, 0), image.Pt(primW, sz.Y), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		if nested { // the channel's messages + composer stop at the nested pane
			a.chBodyW = mainW
			defer func() { a.chBodyW = 0 }()
		}
		d := slackc.Component(gtx, "ClientPrimaryView", a, (*App).primaryViewProps, (*App).layoutPrimaryView)
		a.layoutShellPoints(gtx)
		return d
	})
	if secW > 0 && nested {
		x := mainW
		// Slack: box-shadow 0 4px 12px rgba(0,0,0,.1), no border.
		pane := image.Rect(x, secTop, sz.X, sz.Y)
		theme.Shadow(gtx, pane.Add(image.Pt(0, gtx.Dp(4))), 0, gtx.Dp(12), theme.WithAlpha(a.th.C.PanelShadow, 0x1a))
		theme.Fill(gtx, pane, a.th.C.Bg)
		a.secondaryOrigin = panelOrigin.Add(pane.Min)
		atExact(gtx, pane.Min, pane.Size(), func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
			sv := clsSecondary.Enter(gtx, 0, 0)
			defer sv.Leave()
			return a.layoutSecondary(gtx)
		})
		func() {
			defer op.Offset(image.Pt(0, secTop)).Push(gtx.Ops).Pop()
			a.layoutSecondaryResize(gtx, x, sz.Y-secTop)
		}()
	} else if secW > 0 {
		x := mainW
		// Slack 2026 floats the thread/details pane: a soft shadow on its
		// left edge over the conversation, plus a hairline border.
		pane := image.Rect(x, 0, sz.X, sz.Y)
		theme.Shadow(gtx, pane, 0, gtx.Dp(8), theme.WithAlpha(a.th.C.PanelShadow, 0x30))
		theme.Fill(gtx, pane, a.th.C.Bg)
		theme.Fill(gtx, image.Rect(x, 0, x+gtx.Dp(1), sz.Y), a.th.C.Border)
		a.secondaryOrigin = panelOrigin.Add(image.Pt(x+gtx.Dp(1), 0))
		atExact(gtx, image.Pt(x+gtx.Dp(1), 0), image.Pt(secW-gtx.Dp(1), sz.Y), func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
			sv := clsSecondary.Enter(gtx, 0, 0)
			defer sv.Leave()
			return a.layoutSecondary(gtx)
		})
		a.layoutSecondaryResize(gtx, x, sz.Y)
	}
	return layout.Dimensions{Size: sz}
}

// pageOwnsWorkspace: Slack hides the Home sidebar on these rail pages; the
// view draws its own column (Files, DMs list) or uses the full width.
func pageOwnsWorkspace(r Route) bool {
	switch r.Page() {
	case "files", "dms", "activity", "later":
		return true
	}
	return false
}

// view returns the cached view for route r, creating it with mk.
func (a *App) view(r Route, mk func() View) View {
	if v, ok := a.views[r]; ok {
		return v
	}
	v := mk()
	a.views[r] = v
	a.viewOrder = append(a.viewOrder, r)
	if len(a.viewOrder) > 24 { // keep the 24 most recent views warm
		old := a.viewOrder[0]
		a.viewOrder = a.viewOrder[1:]
		if old != a.router.Current() {
			delete(a.views, old)
		}
	}
	return v
}

// ---- dark mode / title ----

// ToggleDark flips light/dark mode and persists the choice.
func (a *App) ToggleDark() { a.setDark(!a.th.Dark) }

// setDark goes through Taut's setUserPrefByApi thunk (Slack's dark_mode
// pref) so plugins see the change; the native applies it locally only
// (Clack's theme is a local setting, the account pref is left alone).
func (a *App) setDark(dark bool) {
	v := "light"
	if dark {
		v = "dark"
	}
	a.dispatch("setUserPrefByApi", map[string]any{"pref": "dark_mode", "value": v},
		func(ctx context.Context, args map[string]any) (any, error) {
			on := argStr(args, "value") == "dark"
			a.Post(func() {
				a.th.SetDark(on)
				if s := a.Slick(); s != nil {
					s.SetDark(on) // themes with a "mode" apply only in it
				}
				a.savePrefs()
				a.Invalidate()
			})
			return nil, nil
		})
}

func (a *App) updateTitle() {
	t := a.windowTitle()
	if t != a.title {
		a.title = t
		if a.win != nil { // nil in headless benchmarks
			a.win.Option(app.Title(t))
		}
	}
}

// ---- overlays & toast ----

func (a *App) layoutOverlays(gtx layout.Context) {
	live := a.overlays[:0]
	for _, o := range a.overlays {
		if !o.Dismissed() {
			live = append(live, o)
		}
	}
	for i := len(live); i < len(a.overlays); i++ {
		a.overlays[i] = nil
	}
	a.overlays = live
	for _, o := range a.overlays {
		g := gtx
		g.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
		o.Layout(g)
	}
}

type toastState struct {
	text  string
	until time.Time
}

// Toast shows a short message at the bottom of the window.
func (a *App) Toast(text string) {
	a.toast = toastState{text: text, until: time.Now().Add(3500 * time.Millisecond)}
	a.Invalidate()
}

func (a *App) layoutToast(gtx layout.Context) {
	if a.toast.text == "" {
		return
	}
	now := time.Now()
	if now.After(a.toast.until) {
		a.toast.text = ""
		return
	}
	gtx.Execute(op.InvalidateCmd{At: a.toast.until})
	sz := gtx.Constraints.Max
	c := &a.th.C
	tw := a.th.Measure(gtx, 15, theme.Bold, a.toast.text)
	if max := sz.X - gtx.Dp(80); tw > max {
		tw = max
	}
	w, h := tw+gtx.Dp(32), gtx.Dp(44)
	r := image.Rect((sz.X-w)/2, sz.Y-h-gtx.Dp(28), (sz.X+w)/2, sz.Y-gtx.Dp(28))
	theme.Shadow(gtx, r, gtx.Dp(8), gtx.Dp(6), theme.WithAlpha(c.PanelShadow, 0x40))
	theme.RRect(gtx, r, gtx.Dp(8), c.Tooltip)
	at(gtx, image.Pt(r.Min.X+gtx.Dp(16), r.Min.Y), image.Pt(tw, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Bold, c.TooltipText, a.toast.text)
		})
	})
}

// ---- frame stats (for the speed report) ----

type frameStats struct {
	n      int
	total  time.Duration
	layout time.Duration
	max    time.Duration
	last   time.Time
}

func (s *frameStats) add(d, layout time.Duration) {
	s.n++
	s.total += d
	s.layout += layout
	if d > s.max {
		s.max = d
	}
	if envFlag("FRAMESTATS") && time.Since(s.last) > 5*time.Second && s.n > 0 {
		n := time.Duration(s.n)
		log.Printf("ui: %d frames, avg %v (layout %v), max %v", s.n, (s.total / n).Round(10*time.Microsecond), (s.layout / n).Round(10*time.Microsecond), s.max.Round(10*time.Microsecond))
		s.last = time.Now()
		*s = frameStats{last: s.last}
	}
}

// fullscreen pointer passthrough helper: registers tag for pointer events
// over r (used by resize handles).
func pointerArea(gtx layout.Context, tag event.Tag, r image.Rectangle, cursor pointer.Cursor) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
	cursor.Add(gtx.Ops)
}
