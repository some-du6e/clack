package huddleui

import (
	"image"
	"image/color"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// HuddleMiniPanel (boot-render zuQH, HuddleMiniPanelMounter), the
// presenter's panel while you share in the desktop app:
//
//   - its own frameless window, 620×60, always on top at the
//     "screen-saver" level (relative level 1), on every desktop, left out
//     of captures (setContentProtection), centred at the bottom of the
//     shared screen 100px up (for a window share, Slack's bounds come
//     back empty and it uses the screen its main window is on; Clack
//     takes the screen the shared window is on). The body is
//     sk-client-theme--dark.
//   - it opens hidden and shows with showInactive, so the focus stays
//     where it was; on macOS it is Electron's type "panel" (a click on it
//     doesn't activate the app, hidden in Mission Control) with
//     acceptFirstMouse, so one click presses a button while you present
//     another app. Gio's app.Panel does all that (Clack's Gio patch §8).
//   - .p-huddle_mini_panel is -webkit-app-region: drag and its buttons
//     no-drag: the rest of the panel moves the window (a Gio ActionMove
//     area under the buttons). Presses there go to the OS, so the page's
//     onDoubleClick (focus the main window) only sees double clicks on
//     the buttons, which bubble to it.
//   - .p-huddle_mini_panel: padding 12px, theme-base-inv-pry, text
//     theme-content-inv-pry. From the left: HuddleActivity (isMiniPanel:
//     who's talking, or the huddle's name, over "{n} people in the
//     huddle"), or a hovered button's hint in its place; the mute button
//     ("Mute microphone" / "Unmute microphone"), the video button, and
//     Stop Sharing (stop-screen-sharing, "Stop sharing your screen"),
//     each 12px apart (.p-huddle_mini_panel__button: min-width 36px,
//     radius 8, hover theme-surf-inv-pry; a switch that's on is
//     theme-base-pry with theme-content-ter; Stop Sharing is
//     base-inv-hgl-1).
//   - the presenter's own drawing button ("Draw on your own screen")
//     needs the host draw window (HostDrawWindow), which Clack doesn't
//     have, so it isn't drawn; isDrawEnabled goes out false, as Slack's
//     Linux app sends it.

// The panel's palette (7uI9's theme tokens, Slack Kit's dark values).
var (
	panelBg     = color.NRGBA{R: 32, G: 2, B: 33, A: 0xff}        // theme-base-inv-pry
	panelInk    = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff} // theme-content-inv-pry
	panelHover  = color.NRGBA{R: 216, G: 186, B: 225, A: 0x40}    // theme-surf-inv-pry (.25)
	panelOn     = color.NRGBA{R: 239, G: 225, B: 245, A: 0xff}    // theme-base-pry
	panelOnInk  = color.NRGBA{R: 32, G: 2, B: 33, A: 0xff}        // theme-content-ter
	panelStop   = hexA(0x0a77a7ff)                                // base-inv-hgl-1
	panelStopHv = hexA(0xd8eef614)                                // base-inv-hgl-1-hover
)

const (
	panelW = 620
	panelH = 60
)

// miniPanel is the open panel window.
type miniPanel struct {
	c   *Controller
	src huddle.ShareSource

	mu     sync.Mutex
	w      *app.Window
	closed bool
	handle huddle.PanelHandle
	have   bool
	at     image.Rectangle // its place on screen (the OS's units)
	placed bool

	mic, video, stop widget.Clickable

	seen   map[string]bool // joins
	joined []string
	joinAt time.Time
}

// syncMiniPanel opens the panel while you share and closes it after
// (HuddleMiniPanelMounter: isScreenshareEnabled in a running huddle).
func (c *Controller) syncMiniPanel(s *huddle.Session, st huddle.State) {
	want := s != nil && st.Phase != huddle.Ended && st.Share.State.Sharing() && !c.NoWindow
	c.share.mu.Lock()
	p := c.share.panel
	switch {
	case want && p == nil:
		p = &miniPanel{c: c, src: st.Share.Source}
		c.share.panel = p
		c.share.mu.Unlock()
		go p.run()
		return
	case !want && p != nil:
		c.share.panel = nil
		c.share.mu.Unlock()
		p.close()
		return
	}
	c.share.mu.Unlock()
	if p != nil {
		p.invalidate()
	}
}

func (p *miniPanel) invalidate() {
	p.mu.Lock()
	w := p.w
	p.mu.Unlock()
	if w != nil {
		go w.Invalidate()
	}
}

func (p *miniPanel) close() {
	p.mu.Lock()
	p.closed = true
	w := p.w
	p.mu.Unlock()
	if w != nil {
		go w.Perform(system.ActionClose)
	}
}

// panelPlace is where HuddleMiniPanelMounter puts the panel on bounds
// (the shared screen): centred, its bottom 100px above the screen's, in
// units of scale per dp.
func panelPlace(bounds image.Rectangle, scale float32) image.Rectangle {
	w, h := int(panelW*scale+0.5), int(panelH*scale+0.5)
	x := bounds.Min.X + (bounds.Dx()-w)/2
	y := bounds.Min.Y + bounds.Dy() - h - int(100*scale+0.5)
	return image.Rect(x, y, x+w, y+h)
}

// screenOf is the bounds of the screen src is (or is on).
func screenOf(src huddle.ShareSource) image.Rectangle {
	if src.Kind() == huddle.ShareScreen {
		return src.Bounds
	}
	c := src.Bounds.Min.Add(src.Bounds.Size().Div(2))
	screens, _ := huddle.ListShareSources(huddle.ShareScreen)
	for _, s := range screens {
		if c.In(s.Bounds) {
			return s.Bounds
		}
	}
	if len(screens) > 0 {
		return screens[0].Bounds
	}
	return src.Bounds
}

func (p *miniPanel) run() {
	ww := new(app.Window)
	ww.Option(app.Title(AppName), app.Size(unit.Dp(panelW), unit.Dp(panelH)), app.MinSize(unit.Dp(panelW), unit.Dp(panelH)),
		app.MaxSize(unit.Dp(panelW), unit.Dp(panelH)), app.Decorated(false), app.TopMost(true), app.Panel(true))
	th := theme.Bare(true)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	p.mu.Lock()
	p.w = ww
	closed := p.closed
	p.mu.Unlock()
	if closed {
		ww.Perform(system.ActionClose)
	}
	screen := make(chan image.Rectangle, 1)
	go func() { screen <- screenOf(p.src) }()
	var bounds image.Rectangle
	var ops op.Ops
	for {
		switch e := ww.Event().(type) {
		case app.DestroyEvent:
			p.mu.Lock()
			p.closed = true
			p.mu.Unlock()
			return
		case app.FrameEvent:
			select {
			case bounds = <-screen:
			default:
			}
			gtx := app.NewContext(&ops, e)
			// the next frame: who's talking, or sooner while the window
			// waits for its handle and the screen to be placed on
			next := 500 * time.Millisecond
			if !p.place(bounds, gtx.Metric) {
				next = 50 * time.Millisecond
			}
			gtx.Execute(op.InvalidateCmd{At: e.Now.Add(next)})
			slackc.LockFrame()
			restore := slackc.SubFrame(gtx, "sk-client-theme--dark", panelInk)
			p.layout(gtx, th)
			restore()
			slackc.UnlockFrame()
			e.Frame(gtx.Ops)
		default:
			if h, ok := panelHandle(e); ok { // placed by the next frame
				p.mu.Lock()
				p.handle, p.have, p.placed = h, true, false
				p.mu.Unlock()
			}
		}
	}
}

// place moves the window once its native handle and the screen are
// known, and reports whether it has been.
func (p *miniPanel) place(bounds image.Rectangle, m unit.Metric) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.placed {
		return true
	}
	if !p.have || bounds.Empty() {
		return false
	}
	p.placed = true
	p.at = panelPlace(bounds, panelScale(m))
	h := p.handle
	at := p.at
	go huddle.SetupPresenterPanel(h, at)
	return true
}

// activityText is HuddleActivity's two lines in the mini panel, for the
// huddle you're in: HuddleChannelName over getMemberCountText. The first
// line is, in Slack's order: who just joined (joined is joiningMembers,
// miniPanel.joins), who's talking, a DM's "Waiting for", the huddle's
// topic, the conversation's name (getDisplayNameForChannel without the #
// or @), the others' names, "Waiting for others…".
func (c *Controller) activityText(st huddle.State, joined []string) (line, sub string) {
	now := time.Now()
	n := len(st.Participants)
	if n == 1 && st.Participants[0].Self {
		sub = nsHuddles.T("Just you in the huddle")
	} else {
		sub = nsHuddles.T("{memberCount, plural, =1 {# person} other {# people}} in the huddle", "memberCount", n)
	}
	switch {
	case len(joined) > 1:
		return nsHuddles.T("Several people joined"), sub
	case len(joined) == 1:
		return nsHuddles.T("{memberName} joined", "memberName", c.userName(joined[0])), sub
	}
	for _, q := range st.Participants {
		if q.Speaking(now) {
			if name := c.userName(q.UserID); name != "" {
				return nsHuddles.T("{speakingMemberName} is talking", "speakingMemberName", name), sub
			}
		}
	}
	var name string
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(st.Channel); ok {
			switch conv.Kind {
			case "im":
				if n <= 1 {
					return nsHuddles.T("Waiting for {memberName}", "memberName", c.userName(conv.User)), sub
				}
				name = c.userName(conv.User)
			case "mpim": // getMpimDisplayNameAlphabetical
				self := c.selfID()
				var names []string
				for _, m := range conv.Members {
					if m != self {
						if s := c.userName(m); s != "" {
							names = append(names, s)
						}
					}
				}
				sort.SliceStable(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
				name = strings.Join(names, ", ")
			default:
				name = conv.Name
			}
		}
	}
	if t := strings.TrimSpace(st.Name); t != "" { // the huddle's topic
		return t, sub
	}
	if name != "" {
		return name, sub
	}
	var others []string
	for _, q := range st.Participants {
		if !q.Self {
			others = append(others, c.userName(q.UserID))
		}
	}
	switch {
	case len(others) > 1:
		return listifyNoConj(others), sub
	case len(others) == 1:
		return others[0], sub
	}
	return nsHuddles.T("Waiting for others…"), sub
}

// joins is HuddleActivity's joiningMembers: who joined since the panel
// opened, while you're in the huddle, until 2s after the last of them.
func (p *miniPanel) joins(st huddle.State, now time.Time) []string {
	in := false
	for _, q := range st.Participants {
		in = in || q.Self
	}
	if p.seen == nil {
		p.seen = map[string]bool{}
		for _, q := range st.Participants {
			p.seen[q.UserID] = true
		}
	} else if in {
		cur := make(map[string]bool, len(st.Participants))
		for _, q := range st.Participants {
			if !p.seen[q.UserID] && !cur[q.UserID] {
				p.joined = append(p.joined, q.UserID)
				p.joinAt = now
			}
			cur[q.UserID] = true
		}
		p.seen = cur
	}
	if len(p.joined) > 0 && now.Sub(p.joinAt) >= 2*time.Second {
		p.joined = nil
	}
	return p.joined
}

func (p *miniPanel) layout(gtx C, th *theme.Theme) {
	c := p.c
	sz := gtx.Constraints.Max
	theme.Fill(gtx, image.Rectangle{Max: sz}, panelBg)
	s := c.Session()
	if s == nil {
		return
	}
	st := s.State()
	for p.clicked(gtx, &p.mic) {
		c.micClick(s, st, nil)
	}
	for p.clicked(gtx, &p.video) {
		s.SetCamera(!(st.Camera.State == huddle.CameraOn || st.Camera.State == huddle.CameraStarting))
	}
	for p.clicked(gtx, &p.stop) { // toggleScreenShare
		c.stopShare(s)
	}
	// the drag region: everything the buttons (laid out after it) don't cover
	area := clip.Rect{Max: sz}.Push(gtx.Ops)
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	area.Pop()
	slackc.Render(gtx, "HuddleMiniPanel", slackc.Props{"$key": "huddle-mini-panel", "huddleId": s.RoomID(), "channelId": st.Channel,
		"className": "p-huddle_mini_panel"}, func(gtx C, _ slackc.Props) D {
		p.content(gtx, th, s, st, sz)
		return D{Size: sz}
	})
}

// clicked is cl's next click. The second click of a double click is also
// the panel's onDoubleClick, which focuses the main window
// (focusWindow(getMainWindowId())).
func (p *miniPanel) clicked(gtx C, cl *widget.Clickable) bool {
	k, ok := cl.Update(gtx)
	if ok && k.NumClicks == 2 {
		if r, ok := p.c.h.(interface{ Raise() }); ok {
			r.Raise()
		}
	}
	return ok
}

func (p *miniPanel) content(gtx C, th *theme.Theme, s *huddle.Session, st huddle.State, sz image.Point) {
	c := p.c
	pad := gtx.Dp(12)
	bh := sz.Y - 2*pad
	micOn := !st.Muted
	camOn := st.Camera.State == huddle.CameraOn || st.Camera.State == huddle.CameraStarting
	micTip := nsHuddles.T("Unmute microphone")
	if micOn {
		micTip = nsHuddles.T("Mute microphone")
	}
	camTip := nsHuddles.T("Turn on camera")
	if camOn {
		camTip = nsHuddles.T("Turn off camera")
	}
	stopLabel := nsHuddles.T("Stop Sharing")
	stopW := gtx.Dp(12) + gtx.Dp(20) + gtx.Dp(12) + labelSize(gtx, th, 15, theme.Bold, stopLabel).X + gtx.Dp(12)
	iw := gtx.Dp(12) + gtx.Dp(20) + gtx.Dp(12)
	micIcon, camIcon := "mic-off", "video-off"
	if micOn {
		micIcon = "mic"
	}
	if camOn {
		camIcon = "video"
	}
	type pbtn struct {
		cl   *widget.Clickable
		w    int
		on   bool
		icon string
		tip  string
		stop bool
	}
	btns := []pbtn{{&p.mic, iw, micOn, micIcon, micTip, false}, {&p.video, iw, camOn, camIcon, camTip, false},
		{&p.stop, stopW, false, "stop-screen-sharing", nsHuddles.T("Stop sharing your screen"), true}}
	right := sz.X - pad
	for i := range btns {
		right -= btns[i].w
		if i < len(btns)-1 {
			right -= gtx.Dp(12)
		}
	}
	hint := ""
	x := right
	for _, b := range btns {
		b := b
		if b.cl.Hovered() {
			hint = b.tip
		}
		at(gtx, image.Pt(x, pad), image.Pt(b.w, bh), func(gtx C) D {
			return button(gtx, b.cl, image.Pt(b.w, bh), func(gtx C, hov bool) {
				r := image.Rectangle{Max: image.Pt(b.w, bh)}
				switch {
				case b.stop:
					theme.RRect(gtx, r, gtx.Dp(8), panelStop)
					if hov {
						theme.RRect(gtx, r, gtx.Dp(8), panelStopHv)
					}
				case b.on:
					theme.RRect(gtx, r, gtx.Dp(8), panelOn)
				case hov:
					theme.RRect(gtx, r, gtx.Dp(8), panelHover)
				}
			}, func(gtx C) D {
				ink := panelInk
				if b.on {
					ink = panelOnInk
				}
				if !b.stop {
					iconAt(gtx, image.Pt(b.w, bh), b.icon, 20, ink)
					return D{}
				}
				at(gtx, image.Pt(gtx.Dp(12), (bh-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx C) D {
					iconAt(gtx, image.Pt(gtx.Dp(20), gtx.Dp(20)), b.icon, 20, panelInk)
					return D{}
				})
				ls := labelSize(gtx, th, 15, theme.Bold, stopLabel)
				at(gtx, image.Pt(gtx.Dp(12)+gtx.Dp(20)+gtx.Dp(12), (bh-ls.Y)/2), ls, func(gtx C) D {
					return th.Label(gtx, 15, theme.Bold, panelInk, stopLabel)
				})
				return D{}
			})
		})
		x += b.w + gtx.Dp(12)
	}
	// the activity (or the hint) fills the rest
	tw := max(0, right-pad-gtx.Dp(12))
	if hint != "" { // .p-huddle_mini_panel__hint_message
		ls := labelSize(gtx, th, 15, theme.Bold, hint)
		at(gtx, image.Pt(pad, (sz.Y-ls.Y)/2), image.Pt(tw, ls.Y), func(gtx C) D { return th.Label(gtx, 15, theme.Bold, panelInk, hint) })
		return
	}
	line, sub := c.activityText(st, p.joins(st, gtx.Now))
	l1 := labelSize(gtx, th, 15, theme.Bold, line)
	l2 := labelSize(gtx, th, 13, theme.Regular, sub)
	top := (sz.Y - (l1.Y + l2.Y - gtx.Dp(4))) / 2
	at(gtx, image.Pt(pad, top), image.Pt(tw, l1.Y), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, l1.Y)}
		return th.Label(gtx, 15, theme.Bold, panelInk, line)
	})
	at(gtx, image.Pt(pad, top+l1.Y-gtx.Dp(4)), image.Pt(tw, l2.Y), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, l2.Y)}
		return th.Label(gtx, 13, theme.Regular, panelInk, sub)
	})
}
