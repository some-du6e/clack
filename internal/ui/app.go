// Package ui is the root of yoloslack's native (Gio) client: the window and
// event loop, the Slack shell (top nav, tab rail, sidebar, primary panel,
// secondary pane), routing and history, keyboard shortcuts, notifications and
// the Host implementation every feature package talks to.
package ui

import (
	"image"
	"log"
	"os"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Overlay is anything drawn above the shell (popups.Overlay satisfies it).
type Overlay interface {
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
	// with the workspace and d cookie; it returns nil once signed in.
	Login func(workspace, cookie string) error
	// Started is the process start time, for the startup-latency log.
	Started time.Time
}

// App is the whole UI. It implements every feature package's Host.
type App struct {
	opts   Options
	win    *app.Window
	th     *theme.Theme
	images *imgcache.Cache
	data   *backend // nil until signed in

	router      router
	clicks      clickables
	grad        gradient
	sidebar     sidebarState
	secondary   secondaryState
	searchQuery string
	overlays    []Overlay
	toast       toastState
	keys        *shortcuts
	login       *loginScreen

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
}

// New creates the app. Call Run on the UI goroutine (main).
func New(opts Options, images *imgcache.Cache) *App {
	if opts.Started.IsZero() {
		opts.Started = time.Now()
	}
	a := &App{
		opts:   opts,
		th:     theme.New(opts.Dark),
		images: images,
		clicks: clickables{},
		views:  map[Route]View{},
	}
	a.sidebar.width = theme.SidebarW
	a.secondary.width = theme.SecondaryW
	a.keys = a.defaultShortcuts()
	a.loadPrefs()
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
	a.win = new(app.Window)
	a.win.Option(
		app.Title("yoloslack"),
		app.Size(1440, 900),
		app.MinSize(800, 500),
	)
	if a.images != nil {
		a.images.SetInvalidate(a.win.Invalidate)
	}
	if a.data != nil {
		a.data.start(a)
	}
	var ops op.Ops
	for {
		switch e := a.win.Event().(type) {
		case app.DestroyEvent:
			if a.data != nil {
				a.data.stop()
			}
			return e.Err
		case app.FrameEvent:
			t0 := time.Now()
			a.drain()
			gtx := app.NewContext(&ops, e)
			a.frame(gtx)
			e.Frame(gtx.Ops)
			a.frameStats.add(time.Since(t0))
			if !a.firstPaint {
				a.firstPaint = true
				log.Printf("ui: first frame painted %v after start (%d sidebar rows)", time.Since(a.opts.Started).Round(time.Millisecond), a.sidebar.rowCount)
			}
		}
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
	a.grad.layout(gtx, a.th.C.ChromeA, a.th.C.ChromeB)

	if a.login != nil {
		a.layoutLogin(gtx)
		return
	}

	a.keys.process(gtx, a)
	a.updateTitle()

	navH := gtx.Dp(theme.TopNavH)
	railW := gtx.Dp(theme.RailW)
	inset := gtx.Dp(theme.PanelInset)
	sbW := gtx.Dp(unit.Dp(a.sidebar.width))
	if a.sidebar.hidden {
		sbW = 0
	}
	ws := image.Rect(railW, navH, sz.X-inset, sz.Y-inset)
	primary := image.Rect(ws.Min.X+sbW, ws.Min.Y, ws.Max.X, ws.Max.Y)

	a.layoutTopNav(gtx, primary.Min.X, sz.X)
	a.layoutRail(gtx, navH, sz.Y-navH)

	// workspace: sidebar (translucent) + primary panel, one rounded unit
	// with Slack's soft shadow (-4px -4px 16px rgba(0,0,0,.09)).
	rad := gtx.Dp(theme.PanelRadius)
	theme.Shadow(gtx, ws.Add(image.Pt(-gtx.Dp(2), -gtx.Dp(2))), rad, gtx.Dp(8), a.th.C.PanelShadow)
	if sbW > 0 {
		sb := image.Rect(ws.Min.X, ws.Min.Y, primary.Min.X, ws.Max.Y)
		func() {
			defer clip.RRect{Rect: sb, NW: rad, SW: rad}.Push(gtx.Ops).Pop()
			theme.Fill(gtx, sb, a.th.C.SidebarBg)
		}()
		atExact(gtx, sb.Min, sb.Size(), a.layoutSidebar)
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

	a.layoutOverlays(gtx)
	a.layoutToast(gtx)
}

// ---- primary panel ----

func (a *App) layoutPrimary(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	r := a.router.Current()
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
	atExact(gtx, image.Pt(0, 0), image.Pt(mainW, sz.Y), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		if r.Page() == "c" {
			return a.layoutChannel(gtx, r.Channel())
		}
		return a.layoutPage(gtx, r)
	})
	if secW > 0 {
		x := mainW
		theme.Fill(gtx, image.Rect(x, 0, x+gtx.Dp(1), sz.Y), a.th.C.Border)
		atExact(gtx, image.Pt(x+gtx.Dp(1), 0), image.Pt(secW-gtx.Dp(1), sz.Y), func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
			return a.layoutSecondary(gtx)
		})
		a.layoutSecondaryResize(gtx, x, sz.Y)
	}
	return layout.Dimensions{Size: sz}
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
func (a *App) ToggleDark() {
	a.th.SetDark(!a.th.Dark)
	a.savePrefs()
	a.Invalidate()
}

func (a *App) updateTitle() {
	t := a.windowTitle()
	if t != a.title {
		a.title = t
		a.win.Option(app.Title(t))
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
	n     int
	total time.Duration
	max   time.Duration
	last  time.Time
}

func (s *frameStats) add(d time.Duration) {
	s.n++
	s.total += d
	if d > s.max {
		s.max = d
	}
	if os.Getenv("YOLOSLACK_FRAMESTATS") != "" && time.Since(s.last) > 5*time.Second && s.n > 0 {
		log.Printf("ui: %d frames, avg %v, max %v", s.n, (s.total / time.Duration(s.n)).Round(10*time.Microsecond), s.max.Round(10*time.Microsecond))
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
