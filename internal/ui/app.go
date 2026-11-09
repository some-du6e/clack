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
	"gioui.org/widget"
	"gioui.org/x/explorer"

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/huddleui"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
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
	// "Check for updates" is on; ok=true starts the update's download
	// (update_ui.go shows it). Nil: no update checks (dev builds).
	CheckUpdate func(ctx context.Context) (u UpdateInfo, ok bool, err error)
	// DownloadUpdate fetches and verifies release u in the background
	// (cmd/clack: the package for this install, checked against the
	// release's SHA256SUMS) while "Download updates automatically" is on.
	// nil error: Help gets its dot and the restart dialog. Nil: Preferences
	// only links the release page.
	DownloadUpdate func(ctx context.Context, u UpdateInfo) error
	// InstallUpdate installs the downloaded u, called only when the user
	// chooses to restart (the dialog's Restart Clack, the menus' Restart to
	// Apply Update, Preferences' Install and restart). nil error: Clack quits and the
	// update helper starts the new version; ErrUpdateManual: the system
	// installer opened instead.
	InstallUpdate func(u UpdateInfo) error
	// OnWindowEvent sees every window event before the UI (cmd/clack:
	// window icons from the platform view events).
	OnWindowEvent func(e event.Event)
	// NoRealtime keeps Slack's realtime websocket closed whatever the
	// saved preference says (cmd/clack -no-realtime, CLACK_NO_REALTIME=1).
	NoRealtime bool
	// LogDir is the folder of Clack's log file (cmd/clack: applog.Dir),
	// for Help > Open Logs Folder. "": no log file.
	LogDir string
	// LogTail returns up to n of the newest redacted log lines, for Copy
	// Diagnostics. Called off the UI goroutine. Nil: none.
	LogTail func(n int) []string
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
	hdrCtx          map[string]*int // channel header title right-click tags (channel.go)
	secondaryOrigin image.Point     // window position of the secondary pane
	chBodyW         int             // >0: layoutChannel's messages+composer width (nested secondary pane)
	convos          map[string]*convo
	convoOrder      []string
	ptrTag          int
	ptrWin          image.Point
	ptrOK           bool
	hover           profileHover // the profile hover card (hover_card.go)
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
	// previewFrom is the channel-browser route previewFromCh was opened
	// from ("Back to All channels", joinpane.go)
	previewFrom   Route
	previewFromCh string
	pxPerDp       float32
	chTab         map[string]int
	clipboard     string
	prefs         prefs
	prefsDirty    bool
	prefsSave     prefsSaver // ui.json writer (prefs.go)
	slickMode     struct {   // slickSetDark's worker (taut_wire.go)
		mu   sync.Mutex
		s    *slick.Slick
		dark bool
		busy bool
	}
	notify         notifier
	focused        bool
	winSize        image.Point
	sbBase         color.NRGBA
	sbPal          theme.Palette
	styleVer       uint64    // slackc.Styles.Version the palette was derived at
	acct           acctTheme // the Slack account's theme (theme_account.go)
	shaperCh       chan [2]*text.Shaper
	explorer       *explorer.Explorer    // native file chooser (set in onWindow)
	tautGen        int                   // bumps when a (new) plugin runtime attaches
	tautTabs       []taut.PreferencesTab // plugins' Preferences tabs
	switchAt       time.Time             // navigation time, for the channel-switch latency log
	switchTo       Route
	winLive        atomic.Pointer[app.Window] // a.win once Run made it, for other goroutines
	inval          chan struct{}              // redraw requests for invalidator (cap 1: they coalesce)
	update         updateState                // "Update available" banner
	rtAsk          realtimeState              // first-run "Live updates are off" banner
	updatesOff     atomic.Bool                // prefs.NoUpdateCheck, for updateLoop
	updNow         chan struct{}              // Check now → updateLoop (cap 1)
	updWake        chan struct{}              // focus came back: is a check due? (cap 1)
	hud            *huddleui.Controller       // the native huddle (huddle_wire.go)
	hudOrigin      map[string]*originTracker  // headers' huddle buttons by channel (caret menus)
	ccStopping     map[string]bool            // code channels whose agent is being stopped (codechannel.go)
	ccPending      map[string]bool            // context bar actions in flight, "channel:key"
	skipBody       int                        // tests: frames drawViewBody leaves the body out
	skipAt         time.Time                  // tests: the frame being skipped
	noKeepBody     bool                       // tests: no keepBody (KeepFocus alone)
	body           bodyDraw                   // the primary body's last live draw (keepBody)
	standIn        bool                       // keepBody's draw is running
	standIns       int                        // keepBody draws so far
	missedFrameLog focusLog                   // the missed-frame log line (focus_log.go)
	panelOrigin    image.Point                // the primary panel's window origin this frame

	swipe         swiper          // trackpad history swipes (swipe.go, nav.go)
	swipeNow      swipeState      // this frame's swipe progress, for the arrow
	navTag        int             // the window-wide scroll handler (navGestures)
	ptrBtns       pointer.Buttons // mouse buttons held, for back/forward (navButtons)
	railOverflow  []string        // tabs under the rail's More (layoutRail)
	teamIconShown string          // the last workspace icon URL that drew (kept while a new one loads)
	mbar          menuBar         // the macOS menu bar's state (macmenu.go)
}

// New creates the app. Call Run on the UI goroutine (main).
func New(opts Options, images *imgcache.Cache) *App {
	if opts.Started.IsZero() {
		opts.Started = time.Now()
	}
	a := &App{
		opts:    opts,
		th:      theme.Bare(opts.Dark),
		images:  images,
		clicks:  clickables{},
		views:   map[Route]View{},
		inval:   make(chan struct{}, 1),
		updNow:  make(chan struct{}, 1),
		updWake: make(chan struct{}, 1),
	}
	go a.invalidator()
	a.sidebar.width = theme.SidebarW
	a.secondary.width = theme.SecondaryW
	a.keys = a.defaultShortcuts()
	a.loadPrefs()
	a.settleThemeSync()
	// The shaper (font parsing + system font index) is the slowest part of
	// startup; build it while the window and GPU context come up.
	a.shaperCh = make(chan [2]*text.Shaper, 1)
	go func() {
		t := time.Now()
		sh := theme.NewShaper() // one collection serves both modes
		a.shaperCh <- [2]*text.Shaper{sh, sh}
		a.timing("shapers ready", t)
		a.Invalidate() // the first frames painted the background only
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

// Invalidate requests a redraw. It is safe from any goroutine, including the
// UI goroutine mid-layout and AppKit callbacks: it never calls into Gio itself.
//
// Gio's Window.Invalidate holds the window's invMu while it calls
// driver.Invalidate, and on macOS that wakes the loop with runOnMain, which runs
// inline when already on the main thread and re-locks invMu in nextEvent. So a
// Window.Invalidate from the main thread (layout, or the swipe hook in
// nav_darwin.go) deadlocks the app for good, before any frame finishes and so
// before the freeze watchdog can report it. That was the macOS launch
// beachball. Only invalidator, on its own goroutine, may call it.
func (a *App) Invalidate() {
	select {
	case a.inval <- struct{}{}:
	default: // one is already queued; it covers this one
	}
}

// invalidator forwards Invalidate requests to the window from a goroutine that
// is never the main thread (see Invalidate).
func (a *App) invalidator() {
	for range a.inval {
		if w := a.winLive.Load(); w != nil {
			w.Invalidate()
		}
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
	go a.updateLoop(a.updateSchedule(), realUpdateClock())
	a.watchOSTheme()
	if a.images != nil {
		a.images.SetInvalidate(a.Invalidate)
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
	a.startMenuBar()
	if a.data != nil {
		a.data.start(a)
	}
	var ops op.Ops
	uiguard.MarkUI() // this goroutine handles every window event (uiguard)
	wd := newWatchdog(a.opts.ConfigDir, a.opts.Started)
	go wd.run()
	for {
		ev := a.win.Event()
		// The OS waits while this goroutine handles ev (macOS: the AppKit
		// main thread, every event and frame), so the watchdog times it.
		wd.begin(&stageEvent)
		if a.opts.OnWindowEvent != nil {
			a.opts.OnWindowEvent(ev)
		}
		if onEvent != nil {
			onEvent(ev)
		}
		if menuViewEvent != nil {
			menuViewEvent(ev)
		}
		switch e := ev.(type) {
		case app.DestroyEvent:
			wd.end()
			wd.stop()
			if a.hud != nil {
				a.hud.Close() // leave the huddle cleanly (Chime LEAVE)
			}
			if a.data != nil {
				select { // plugins' stop hooks, the metadata cache
				case <-a.data.stop():
				case <-time.After(5 * time.Second):
				}
			}
			a.flushPrefs() // the window is gone: nothing waits on this
			return e.Err
		case app.ConfigEvent:
			if e.Config.Focused && !a.focused {
				a.nudgeUpdates() // back from sleep, often: is a check due?
			}
			a.focused = e.Config.Focused
		case app.FrameEvent:
			t0 := time.Now()
			if a.th.Shaper == nil && !a.takeShaper() {
				// The shaper (fonts) is still loading: paint the window's
				// background and come back when it's ready, instead of
				// waiting here (that would hold the OS's main thread).
				wd.next(&stageShaper)
				gtx := app.NewContext(&ops, e)
				a.grad.layout(gtx, a.th.C.ChromeA, a.th.C.ChromeB)
				e.Frame(gtx.Ops)
				break
			}
			// patch points (slackc) are shared with the huddle call window
			slackc.LockFrame()
			wd.next(&stagePosted)
			a.drain()
			wd.next(&stageLayout)
			wd.notePage(a.router.Current().Page())
			gtx := a.newContext(&ops, e)
			before := a.router.Current()
			a.frame(gtx)
			if a.router.Current() != before {
				// A click navigated mid-frame (after the sidebar/panel split
				// was computed): lay out again so the new page never shows
				// one frame with the old layout. Layout is ~2 ms.
				ops.Reset()
				gtx = a.newContext(&ops, e)
				a.frame(gtx)
			}
			slackc.UnlockFrame()
			tl := time.Now()
			wd.next(&stageSubmit)
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
		wd.end()
	}
}

// takeShaper installs the text shaper once New's goroutine built it, without
// waiting. UI goroutine.
func (a *App) takeShaper() bool {
	select {
	case sh := <-a.shaperCh:
		a.timing("first FrameEvent", a.opts.Started)
		a.th.SetShapers(sh[0], sh[1])
		return true
	default:
		return false
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

// newContext is app.NewContext at View > Zoom's scale (Slack's zoom
// level), so everything lays out larger or smaller.
func (a *App) newContext(ops *op.Ops, e app.FrameEvent) layout.Context {
	gtx := app.NewContext(ops, e)
	if f := a.zoomFactor(); f != 1 {
		gtx.Metric.PxPerDp *= f
		gtx.Metric.PxPerSp *= f
	}
	return gtx
}

// frame lays out the whole window.
func (a *App) frame(gtx layout.Context) {
	sz := gtx.Constraints.Max
	a.pxPerDp = gtx.Metric.PxPerDp
	a.winSize = sz
	a.syncThemeTokens()
	a.syncMenuBar()

	if a.login != nil {
		a.grad.layout(gtx, a.th.C.ChromeA, a.th.C.ChromeB)
		a.layoutLogin(gtx)
		return
	}

	// first: an ancestor of every other input area (popped at the end)
	navArea := a.navGestures(gtx)
	defer navArea.Pop()
	a.readRootPointer(gtx)
	a.hoverBegin(gtx) // before any widget reads keys (Escape)
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
	bannerH := a.realtimeAskH(gtx) // the workspace starts below the live-updates question
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
	if bannerH > 0 {
		a.layoutRealtimeBanner(gtx, image.Rect(ws.Min.X, navH, ws.Max.X, ws.Min.Y-gtx.Dp(bannerGap)))
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
	a.layoutHoverCard(gtx) // above the content, under the overlays
	a.layoutOverlays(gtx)
	a.layoutHoverCardOver(gtx) // over a dialog whose own list has triggers
	a.slackcOverlays(gtx)
	a.layoutToast(gtx)
	a.rootPointer(gtx)
	a.keys.processGlobal(gtx) // last: only the keys no widget took
	composer.KeepFocus(gtx)   // a focused editor this frame didn't draw stays focused
	a.logMissedFrames(gtx)    // after KeepFocus: what it held this frame
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
		a.panelOrigin = panelOrigin
		d := slackc.Component(gtx, "ClientPrimaryView", a, (*App).primaryViewProps, (*App).layoutPrimaryView)
		a.keepBody(gtx) // the body, if this frame's tree left it out (slick_shell.go)
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

// ToggleDark flips light/dark mode and persists the choice. Like picking
// Light or Dark in Appearance it ends "Sync with OS setting" (else the OS
// scheme would put the old mode back at the next start).
func (a *App) ToggleDark() { a.pickMode(!a.th.Dark) }

// setDark goes through Taut's setUserPrefByApi thunk (dark_mode) so
// plugins see the change; the native applies it locally only. Slack keeps
// light/dark per device in localStorage ("userTheme"), not in users.prefs,
// so there is no account pref to write (theme_account.go).
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
				a.slickSetDark(a.Slick(), on) // themes with a "mode" apply only in it
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
	undo  func()            // ToastUndo's action ("" button when nil)
	btn   *widget.Clickable // its "Undo"
}

// Toast shows a short message at the bottom of the window.
func (a *App) Toast(text string) {
	a.toast = toastState{text: text, until: time.Now().Add(3500 * time.Millisecond)}
	a.Invalidate()
}

// ToastUndo is Slack's toast with an "Undo" button (sections deleted,
// conversations muted or marked read); undo runs once, on click.
func (a *App) ToastUndo(text string, undo func()) {
	a.toast = toastState{text: text, until: time.Now().Add(6 * time.Second), undo: undo, btn: new(widget.Clickable)}
	a.Invalidate()
}

// undoToast clicks the toast's Undo (tests).
func (a *App) undoToast() {
	if u := a.toast.undo; u != nil {
		a.toast = toastState{}
		u()
	}
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
	if a.toast.btn != nil && a.toast.btn.Clicked(gtx) {
		a.undoToast()
		return
	}
	sz := gtx.Constraints.Max
	c := &a.th.C
	tw := a.th.Measure(gtx, 15, theme.Bold, a.toast.text)
	uw := 0
	if a.toast.undo != nil {
		uw = a.th.Measure(gtx, 15, theme.Bold, "Undo") + gtx.Dp(16)
	}
	if max := sz.X - gtx.Dp(80) - uw; tw > max {
		tw = max
	}
	w, h := tw+uw+gtx.Dp(32), gtx.Dp(44)
	r := image.Rect((sz.X-w)/2, sz.Y-h-gtx.Dp(28), (sz.X+w)/2, sz.Y-gtx.Dp(28))
	theme.Shadow(gtx, r, gtx.Dp(8), gtx.Dp(6), theme.WithAlpha(c.PanelShadow, 0x40))
	theme.RRect(gtx, r, gtx.Dp(8), c.Tooltip)
	at(gtx, image.Pt(r.Min.X+gtx.Dp(16), r.Min.Y), image.Pt(tw, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Bold, c.TooltipText, a.toast.text)
		})
	})
	if a.toast.undo != nil {
		at(gtx, image.Pt(r.Max.X-gtx.Dp(16)-uw+gtx.Dp(16), r.Min.Y), image.Pt(uw-gtx.Dp(16), h), func(gtx layout.Context) layout.Dimensions {
			return a.toast.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Bold, c.Link, "Undo")
				})
			})
		})
	}
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
