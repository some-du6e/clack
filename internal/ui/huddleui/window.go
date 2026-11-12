package huddleui

import (
	"image"
	"image/color"
	"strconv"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// window is the huddle window: Slack's separate huddle window with the
// stage (tiles, screen share), captions and the control row. It runs its
// own Gio event loop and theme (text shapers aren't shared across windows).
type window struct {
	c  *Controller
	w  *app.Window
	th *theme.Theme

	mu     sync.Mutex
	closed bool

	vids   videoOps
	clicks map[string]*widget.Clickable
	quick  *quickPicker // the emoji button's HuddleQuickEmojiPicker (quickpicker.go)
	stick  bool         // the sticker picker (hand.go)
	mic    micBanner
	notice string
	noteAt time.Time
	full   bool
	cam    camUI        // window_camera.go
	phase  huddle.Phase // this frame's (tiles: your spinner until connected)

	more      *popups.Menu    // the ⋯ More actions menu (window_menu.go)
	moreBtn   image.Rectangle // its button, in the toolbar
	reactBtn  image.Rectangle // the emoji button, in the toolbar
	video     *popups.Menu    // the camera's Video settings menu (window_camera.go)
	videoBtn  image.Rectangle // its caret, in the toolbar
	leaveMenu *popups.Menu    // Leave options (leave_options.go)
	leaveBtn  image.Rectangle // its caret, in the toolbar
	audio     *popups.Menu    // the mic's Audio settings menu (audiomenu.go)
	audioBtn  image.Rectangle // its caret, in the toolbar
	barTop    int             // the toolbar's top in the window
	cc        ccState         // the closed captions' fade (captions.go)
	banner    banner          // HuddlesInformationalBanner (captions.go)
	tips      map[string]*popups.TipState

	overlays []popups.Overlay // modals over the window (winHost.ShowOverlay; under mu)
	posts    []func()         // winHost.post (under mu)

	shareAlert alertState // the screen share alert's link and close (share.go)
	theme      bgShown    // the huddle theme on the stage (background.go)

	tweak func(*huddle.State) // tests: edits each frame's state

	// popout is the video key of the screen share this window pops out
	// ("" = the huddle window; popout.go), popoutUser its presenter.
	popout, popoutUser string
}

// OpenWindow shows the huddle window (creating it), when you're in a huddle.
func (c *Controller) OpenWindow() {
	c.mu.Lock()
	if c.NoWindow || c.win != nil || (c.s == nil && c.joining == "") {
		c.mu.Unlock()
		return
	}
	w := &window{c: c, clicks: map[string]*widget.Clickable{}}
	c.win = w
	c.mu.Unlock()
	go w.run()
}

// WindowOpen reports whether the huddle window is showing.
func (c *Controller) WindowOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.win != nil
}

// invalidateHook, when set, sees every invalidate (the idle-frame probe,
// idle_frames_test.go). Set before the window opens.
var invalidateHook func(*window)

func (w *window) invalidate() {
	if h := invalidateHook; h != nil {
		h(w)
	}
	w.mu.Lock()
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		// Not inline: callers include the main window's frame, and on macOS a
		// Window.Invalidate from the main thread deadlocks on Gio's invMu
		// (see ui.App.Invalidate).
		go ww.Invalidate()
	}
}

func (w *window) close() {
	w.mu.Lock()
	w.closed = true
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		// Not from here: callers include the main window's frame, and on
		// macOS one window's Perform waits for the main thread, which is
		// waiting for that frame (deadlock).
		go ww.Perform(system.ActionClose)
	}
}

func (w *window) click(id string) *widget.Clickable {
	c := w.clicks[id]
	if c == nil {
		c = new(widget.Clickable)
		w.clicks[id] = c
	}
	return c
}

func (w *window) note(s string) {
	w.notice, w.noteAt = s, time.Now()
}

func (w *window) run() {
	ww := new(app.Window)
	if w.popout != "" {
		ww.Option(app.Title("Huddle - "+AppName), app.Size(unit.Dp(960), unit.Dp(600)), app.MinSize(unit.Dp(480), unit.Dp(320)))
	} else {
		// the compact window: getHuddleCompactWindowInitialWidthV2 (v1CF)
		// is 565px, 24 more in the multi-view window; Slack sizes its
		// height to the content (32 + 44 + 222 + 53); .p-huddle_window's
		// min-width 455px and min-height 302px
		ww.Option(app.Title("Huddle - "+AppName), app.Size(unit.Dp(589), unit.Dp(351)), app.MinSize(unit.Dp(455), unit.Dp(302)))
	}
	main := w.c.h.Theme()
	th := theme.Bare(true)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	th.Images, th.EmojiStyle = main.Images, main.EmojiStyle
	w.th = th
	w.mu.Lock()
	w.w = ww
	closed := w.closed
	w.mu.Unlock()
	if closed {
		ww.Perform(system.ActionClose)
	}
	// No ticker: the window draws when something changes (the session's
	// OnChange, a loaded image: Controller.ImagesChanged) and at the
	// deadlines its layout asks for (speaking bars, spinners, a notice
	// going, the captions' fade), like Slack's, which draws nothing in a
	// still call.
	var ops op.Ops
	var caption captionState // Windows: the caption takes the window's colour (caption.go)
	for {
		ev := ww.Event()
		caption.sync(ev, ww, hudCaption)
		switch e := ev.(type) {
		case app.DestroyEvent:
			if w.popout != "" {
				w.c.popoutGone(w)
				return
			}
			w.c.mu.Lock()
			if w.c.win == w {
				w.c.win = nil
			}
			w.c.mu.Unlock()
			w.c.h.Invalidate()
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			// Slack's huddle components here are plugin patch points
			// (slackc), shared with the main window: one frame at a time.
			slackc.LockFrame()
			restore := slackc.SubFrame(gtx, "p-multi_view_window p-huddle_window", hudWhite)
			w.layout(gtx)
			restore()
			slackc.UnlockFrame()
			e.Frame(gtx.Ops)
		}
	}
}

// AppName is the window title suffix (the ui root sets it to its own).
var AppName = "Clack"

// The compact huddle window, Slack's default huddle window surface
// (huddle_window_compact; HuddleWindowComponent and HuddleWindowContent in
// gantry-v2-async-client-boot-shared PWaJ, build 274f7c7f1b69, with
// client-boot-styles' rules):
//
//   - the title bar, 32px (.p-huddle_window_titlebar; HuddleMvaWindowTitle
//     shows no "{n} people" subtitle in compact mode);
//   - HuddlesInformationalBanner (7tWB, entryPoint baby_window) in the flow
//     above the content: .p-huddles_alert_banner_base--baby_window, radius
//     8px, margin 4px 4px 6px, its text left-aligned;
//   - the stage: HuddleBackground's .p-huddle_background__container--
//     compact_mode, radius 8px, margin 0 4px, height calc(100% - 56px),
//     with the huddle's theme (background.go);
//   - HuddleCompactHeader (XuJY): margin 16px 16px 0, padding 0 8px, the
//     connection-quality button at its right;
//   - the people: HuddleCompactAudioPane (aM8U) in a pane at most 222px tall
//     (.p-huddle_compact_av_pane--footer_below_content);
//   - HuddleFooter (PWaJ) with HuddleToolbar (9d6D) in compact mode below
//     the gallery (.p-huddle_window_content__gallery_toolbar is calc(100% -
//     53px)): the buttons in one centred row with Leave last, padding 4px 0.
const (
	winTitleH    = 32  // .p-huddle_window_titlebar
	winFooterH   = 53  // what the gallery (calc(100% - 53px)) leaves the footer
	winStageGap  = 56  // the background's calc(100% - 56px)
	winPaneMaxH  = 222 // .p-huddle_compact_av_pane--footer_below_content
	winBarH      = 48  // .toolbarContainerCompactMode: 40px buttons, padding 4px 0
	winShareMinW = 550 // HuddleToolbar's eC: narrower, no screen share button (ed.aJ)
)

func (w *window) layout(gtx C) {
	w.runPosts()
	sz := gtx.Constraints.Max
	theme.Fill(gtx, image.Rectangle{Max: sz}, hudFrame)
	s := w.c.Session()
	if s == nil {
		huddleLoading(gtx, w.th, sz)
		w.layoutOverlays(gtx)
		return
	}
	st := s.State()
	if w.tweak != nil {
		w.tweak(&st)
	}
	now := time.Now()
	w.handle(gtx, s, st)
	if w.popout != "" {
		w.layoutPopout(gtx, s, st)
		return
	}
	w.cameraWatch(st)

	// the title: HuddleMvaWindowTitle (CallTimer adds its timer after it):
	// .p-multi_view_window_title__huddle, the headphones, 8px, the title,
	// all at the title's .8 opacity; measured, then centred. No subtitle:
	// the window is in compact mode (getIsCompactMode).
	titleH := gtx.Dp(winTitleH)
	title, _ := w.c.windowTitle(st)
	ink := theme.WithAlpha(hudWhite, 0xcc)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(max(0, sz.X-gtx.Dp(24)-gtx.Dp(120)), titleH)}
	tm := op.Record(gtx.Ops)
	td := slackc.Render(tg, "HuddleMvaWindowTitle", w.titleProps(s, st, title, ""), func(gtx C, p slackc.Props) D {
		t := w.th.Label(gtx, 13, theme.Bold, ink, p.StrOr("title", title))
		sb := p.StrOr("subtitle", "")
		if sb == "" {
			return t
		}
		so := op.Offset(image.Pt(t.Size.X+gtx.Dp(8), 0)).Push(gtx.Ops)
		gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-t.Size.X-gtx.Dp(8))
		sd := w.th.Label(gtx, 13, theme.Regular, ink, sb)
		so.Pop()
		return D{Size: image.Pt(t.Size.X+gtx.Dp(8)+sd.Size.X, max(t.Size.Y, sd.Size.Y)), Baseline: t.Baseline}
	})
	tcall := tm.Stop()
	ts := td.Size
	ix := (sz.X - ts.X - gtx.Dp(24)) / 2
	at(gtx, image.Pt(ix, (titleH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx C) D {
		iconAt(gtx, image.Pt(gtx.Dp(16), gtx.Dp(16)), "headphones", 16, ink)
		return D{Size: image.Pt(gtx.Dp(16), gtx.Dp(16))}
	})
	tt := op.Offset(image.Pt(ix+gtx.Dp(24), (titleH-ts.Y)/2)).Push(gtx.Ops)
	tcall.Add(gtx.Ops)
	tt.Pop()
	// No elapsed time here: Slack's huddle window shows none (its only
	// huddle timer is the Huddles page's HuddlesActiveTimer); Slick's
	// CallTimer adds one after the title.

	top := titleH
	if bh := w.layoutBanner(gtx, s, image.Rect(gtx.Dp(4), top+gtx.Dp(4), sz.X-gtx.Dp(4), top+gtx.Dp(4)+gtx.Dp(bannerH))); bh > 0 {
		top += gtx.Dp(4) + bh + gtx.Dp(6) // margin 4px 4px 6px
	}
	stage := image.Rect(gtx.Dp(4), top, sz.X-gtx.Dp(4), sz.Y-gtx.Dp(winStageGap))
	share := w.stageShare(st)
	if !stage.Empty() {
		w.layoutTheme(gtx, st.RoomID, st.Background, stage, gtx.Dp(8))
		if share != nil {
			// a share on the stage: .p-huddle_background__overlay--dark
			theme.RRect(gtx, stage, gtx.Dp(8), bgOverlayDark)
		}
	}
	// HuddleCompactHeader: hidden in the screen share view
	// (.p-huddle_compact_header--screenshare_view) and, with
	// huddles_new_grids, the troubleshooting button isn't drawn
	hdrBottom := top
	if share == nil {
		hdrBottom = top + gtx.Dp(16) + gtx.Dp(connBtnH)
		if !w.c.on(ExpNewGrids) {
			w.connectionButton(gtx, st, image.Pt(sz.X-gtx.Dp(16)-gtx.Dp(8)-gtx.Dp(connBtnW), top+gtx.Dp(16)))
		}
	}
	barTop := sz.Y - gtx.Dp(winFooterH)
	gallery := image.Rect(0, hdrBottom, sz.X, barTop)
	if share == nil && !w.hasVideo(st) {
		// the audio pane is at most 222px tall; Slack sizes the window to
		// it, Clack's window keeps its size, so the pane is centred
		if ph := gtx.Dp(winPaneMaxH); gallery.Dy() > ph {
			gallery.Min.Y += (gallery.Dy() - ph) / 2
			gallery.Max.Y = gallery.Min.Y + ph
		}
	} else {
		gallery = image.Rect(stage.Min.X, hdrBottom, stage.Max.X, stage.Max.Y)
	}
	if gallery.Dx() > 0 && gallery.Dy() > 0 {
		w.stage(gtx, s, st, gallery, now)
	}
	if !stage.Empty() {
		if st.CaptionsOn {
			w.captions(gtx, st, stage, now)
		}
		if w.stick {
			w.stickerPicker(gtx, stage)
		}
	}
	w.barTop = barTop
	bar := image.Rect(0, barTop, sz.X, barTop+gtx.Dp(winBarH))
	w.bottomBar(gtx, s, st, bar)
	if _, bad := w.mic.visible(st); !bad && w.c.noAudioAlert() && stage.Dx() > gtx.Dp(40) {
		bw := min(stage.Dx()-gtx.Dp(24), gtx.Dp(480))
		at(gtx, image.Pt((sz.X-bw)/2, stage.Min.Y+gtx.Dp(12)), image.Pt(bw, stage.Dy()), func(gtx C) D {
			return w.layoutNoAudio(gtx, s, bw)
		})
	}
	n, micShown := w.mic.visible(st)
	if micShown && stage.Dx() > gtx.Dp(40) {
		w.mic.handle(gtx, s, st, n)
		bw := min(stage.Dx()-gtx.Dp(24), gtx.Dp(480))
		at(gtx, image.Pt((sz.X-bw)/2, stage.Min.Y+gtx.Dp(12)), image.Pt(bw, stage.Dy()), func(gtx C) D {
			return layoutMicBanner(gtx, w.th, &w.mic, n, bw)
		})
	}
	if !micShown && !w.c.noAudioAlert() && stage.Dx() > gtx.Dp(40) { // the alerts show one at a time
		bw := min(stage.Dx()-gtx.Dp(24), gtx.Dp(480))
		at(gtx, image.Pt((sz.X-bw)/2, stage.Min.Y+gtx.Dp(12)), image.Pt(bw, stage.Dy()), func(gtx C) D {
			return w.c.layoutShareAlert(gtx, w.th, &w.shareAlert, bw)
		})
	}
	if w.notice != "" && now.Sub(w.noteAt) < 4*time.Second {
		gtx.Execute(op.InvalidateCmd{At: w.noteAt.Add(4 * time.Second)}) // it goes
		ns := labelSize(gtx, w.th, 13, theme.Bold, w.notice)
		pad := gtx.Dp(10)
		r := image.Rect((sz.X-ns.X)/2-pad, stage.Min.Y+gtx.Dp(10), (sz.X+ns.X)/2+pad, stage.Min.Y+gtx.Dp(10)+ns.Y+pad)
		theme.RRect(gtx, r, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xee})
		at(gtx, image.Pt(r.Min.X+pad, r.Min.Y+pad/2), ns, func(gtx C) D {
			return w.th.Label(gtx, 13, theme.Bold, hudWhite, w.notice)
		})
	}
	w.cameraNotice(gtx, st, stage)
	w.toolbarTip(gtx, bar)
	w.layoutMore(gtx)
	w.layoutOverlays(gtx)
}

// bgOverlayDark is .p-huddle_background__overlay--dark.
var bgOverlayDark = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}

// stageShare is the screen share the stage shows (nil: none, or it's
// popped out).
func (w *window) stageShare(st huddle.State) *huddle.Video {
	for i := range st.Videos {
		if v := st.Videos[i]; v.Content && !w.c.popped(v.Key) {
			return &st.Videos[i]
		}
	}
	return nil
}

// hasVideo reports whether anyone's camera is on (HuddleCompactAvPane:
// the video pane instead of the audio pane).
func (w *window) hasVideo(st huddle.State) bool {
	for _, v := range st.Videos {
		if !v.Content {
			return true
		}
	}
	return st.Camera.State == huddle.CameraOn && st.SelfAttendee != ""
}

func (w *window) handle(gtx C, s *huddle.Session, st huddle.State) {
	if w.click("mic").Clicked(gtx) {
		w.c.micClick(s, st, winHost{w})
		w.mic.dismissed = huddle.MicOK // a broken mic: show why again
	}
	if w.click("micmenu").Clicked(gtx) {
		w.toggleAudio(s)
	}
	if w.click("camera").Clicked(gtx) {
		w.cameraClick(s, st)
	}
	if w.click("share").Clicked(gtx) { // HuddleToolbarScreenshareButton
		h := winHost{w}
		w.c.shareClick(s, st, h, h.post)
	}
	if w.click("react").Clicked(gtx) { // HuddleToolbarEmojiPickerButton
		w.toggleQuick(s)
	}
	if w.click("videomenu").Clicked(gtx) { // Video settings
		w.toggleVideo(s, st)
	}
	if w.click("leavemenu").Clicked(gtx) { // Leave options
		w.toggleLeaveOptions(s)
	}
	if w.click("hand").Clicked(gtx) { // Raise hand / Lower hand / Remove sticker
		go func() { _ = w.c.ToggleHand() }()
	}
	if w.click("stickers").Clicked(gtx) { // Choose sticker
		w.stick = !w.stick
		w.closeQuick()
	}
	for _, st := range stickers {
		if w.click("pick:" + st.Emoji).Clicked(gtx) {
			w.stick = false
			e := st.Emoji
			go func() { _ = w.c.PickSticker(e) }()
		}
	}
	if w.click("more").Clicked(gtx) {
		w.toggleMore(s, st)
	}
	if w.click("thread").Clicked(gtx) {
		h := w.c.h
		open := func() {
			if st.ThreadTS != "" {
				h.OpenThread(firstNonEmpty(st.ThreadChannel, st.Channel), st.ThreadTS)
			} else {
				h.Navigate("c/" + st.Channel)
			}
			h.Invalidate()
		}
		// The main window's state belongs to its UI goroutine: hand it over
		// (this is the huddle window's goroutine).
		if p, ok := h.(interface{ Post(func()) }); ok {
			p.Post(open)
		} else {
			open()
		}
	}
	if w.click("leave").Clicked(gtx) {
		w.c.Leave()
	}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// stage draws the participants in r (and the screen share, when there is
// one): with nobody's camera on, HuddleCompactAudioPane's tiles
// (compactTiles); with cameras or a share, Clack's own grid (Slack's
// compact video pane, yT9P, isn't ported).
func (w *window) stage(gtx C, s *huddle.Session, st huddle.State, r image.Rectangle, now time.Time) {
	ps := galleryOrder(st, w.c.handQueue())
	if w.c.hideSelf.Load() && len(ps) > 1 { // "Hide self-view": your tile leaves the gallery
		ps = withoutSelf(ps)
	}
	cams := map[string]string{} // attendee -> video key
	var share *huddle.Video
	for i := range st.Videos {
		v := st.Videos[i]
		if v.Content {
			// a popped-out share leaves the stage (the self content view
			// "none"): the people show instead
			if share == nil && !w.c.popped(v.Key) {
				share = &st.Videos[i]
			}
		} else {
			cams[v.AttendeeID] = v.Key
		}
	}
	selfCam(st, cams) // your own tile shows your (mirrored) preview
	w.phase = st.Phase
	n := len(ps) + len(st.Invites)
	if share == nil && len(cams) == 0 {
		g := compactTiles(gtx, r, n)
		rad := gtx.Dp(compactTileRadius)
		for i, tr := range g.tiles {
			if i < len(ps) {
				w.tileR(gtx, s, ps[i], "", tr, now, rad)
			} else {
				w.inviteTileR(gtx, st.Invites[i-len(ps)], tr, rad)
			}
		}
		if g.more > 0 {
			overflowCount(gtx, w.th, r, g.more)
		}
		return
	}
	area := r.Inset(gtx.Dp(12))
	if share != nil {
		// the share takes the stage; the people sit in a column on the right
		col := min(gtx.Dp(132), area.Dx()/4)
		sr := image.Rect(area.Min.X, area.Min.Y, area.Max.X-col-gtx.Dp(10), area.Max.Y)
		theme.RRect(gtx, sr, gtx.Dp(8), color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff})
		if img, ok := w.vids.get(s, share.Key); ok {
			at(gtx, sr.Min, sr.Size(), func(gtx C) D { fit(gtx, img, sr.Size(), gtx.Dp(8)); return D{} })
		} else {
			at(gtx, sr.Min, sr.Size(), func(gtx C) D {
				centerLabel(gtx, w.th, sr.Size(), 13, theme.Bold, hudWhite, "Loading screen share…") // no Slack key
				return D{}
			})
		}
		// Slack's drawing layer over a shared screen (BaseDrawRenderer):
		// Clack draws no annotations, so natively it's empty
		drawP := slackc.Props{"$key": "huddle-draw-" + share.Key, "huddleId": s.RoomID(), "screenShareId": share.Key,
			"isScreenSharing": true, "width": sr.Dx(), "height": sr.Dy()}
		at(gtx, sr.Min, sr.Size(), func(gtx C) D { return slackc.Render(gtx, "BaseDrawRenderer", drawP, noNative) })
		w.popoutButton(gtx, share, sr)
		// no presenter label on the shared screen: Slack names the presenter
		// only on a screen share tile's footer in the grid
		tile := min(col, (area.Dy()-gtx.Dp(8)*(n-1))/max(n, 1))
		y := area.Min.Y
		for i := 0; i < n; i++ {
			if y+tile > area.Max.Y {
				break
			}
			tr := image.Rect(area.Max.X-col, y, area.Max.X-col+tile, y+tile)
			if i < len(ps) {
				w.tile(gtx, s, ps[i], cams[ps[i].AttendeeID], tr, now)
			} else {
				w.inviteTile(gtx, st.Invites[i-len(ps)], tr)
			}
			y += tile + gtx.Dp(8)
		}
	} else if n > 0 {
		gap := gtx.Dp(8)
		best, bc := 0, 1
		for cols := 1; cols <= n; cols++ {
			rows := (n + cols - 1) / cols
			side := min((area.Dx()-gap*(cols-1))/cols, (area.Dy()-gap*(rows-1))/rows)
			if side > best {
				best, bc = side, cols
			}
		}
		best = min(best, gtx.Dp(220))
		rows := (n + bc - 1) / bc
		for i := 0; i < n; i++ {
			row, col := i/bc, i%bc
			inRow := min(bc, n-row*bc)
			x0 := area.Min.X + (area.Dx()-(inRow*best+(inRow-1)*gap))/2
			y0 := area.Min.Y + (area.Dy()-(rows*best+(rows-1)*gap))/2
			x, y := x0+col*(best+gap), y0+row*(best+gap)
			tr := image.Rect(x, y, x+best, y+best)
			if i < len(ps) {
				w.tile(gtx, s, ps[i], cams[ps[i].AttendeeID], tr, now)
			} else {
				w.inviteTile(gtx, st.Invites[i-len(ps)], tr)
			}
		}
	}
}

// compactTileRadius is the compact audio pane's tiles' 4px corners
// (.p-huddle_compact_audio_pane--container .p-huddle_grid_component__tile
// .p-huddle_peer_tile and the avatar inside).
const compactTileRadius = 4

// compactGrid is where HuddleCompactAudioPane puts n people.
type compactGrid struct {
	tiles []image.Rectangle // the tiles drawn, in gallery order
	more  int               // the people beyond them (the "+N" badge)
}

// compactTiles is HuddleCompactAudioPane's sizing (aM8U), in r: padding
// 16px (8px on top: --container__top_padding's !important beats the
// inline 16px); the tiles' available size is r less 32px each way; a
// tile is at most 150px, gaps 8px; one row when its tiles are at least
// 99px (48 + (150 - 48) / 2), else two rows of at least 48px, else
// 48px tiles, floor((width - 32) / 56) a row, two rows, and the rest as
// "+N". The tiles sit centred (flex, centre, centre) row by row, the
// last row centred too (p-huddle_grid_component).
func compactTiles(gtx C, r image.Rectangle, n int) compactGrid {
	if n <= 0 {
		return compactGrid{}
	}
	gap := gtx.Dp(8)
	ah, aw := r.Dy()-gtx.Dp(32), r.Dx()-gtx.Dp(32)
	maxT := gtx.Dp(150)
	minT := gtx.Dp(48)
	if ah < minT || aw < minT {
		return compactGrid{more: n}
	}
	perRow := aw / gtx.Dp(56)
	size := func(rows, tiles int) int {
		cols := (tiles + rows - 1) / rows
		h := ah
		if rows > 1 {
			h -= (rows - 1) * gap
		}
		return min(h/rows, (aw-(cols-1)*gap)/cols, maxT)
	}
	rows, shown, cols, side := 1, n, n, size(1, n)
	switch {
	case side >= minT+(maxT-minT)/2:
	case size(2, n) >= minT:
		rows, side = 2, size(2, n)
		cols = (n + 1) / 2
	default:
		shown = min(perRow*2, n)
		rows, cols, side = 2, perRow, minT
		if shown <= perRow {
			rows = 1
		}
	}
	if cols <= 0 || side <= 0 {
		return compactGrid{more: n}
	}
	inner := image.Rect(r.Min.X+gtx.Dp(16), r.Min.Y+gtx.Dp(8), r.Max.X-gtx.Dp(16), r.Max.Y-gtx.Dp(16))
	gh := rows*side + (rows-1)*gap
	y0 := inner.Min.Y + (inner.Dy()-gh)/2
	g := compactGrid{more: n - shown}
	for i := 0; i < shown; i++ {
		row, col := i/cols, i%cols
		inRow := min(cols, shown-row*cols)
		x0 := inner.Min.X + (inner.Dx()-(inRow*side+(inRow-1)*gap))/2
		x, y := x0+col*(side+gap), y0+row*(side+gap)
		g.tiles = append(g.tiles, image.Rect(x, y, x+side, y+side))
	}
	return g
}

// overflowCount is the compact audio pane's "+N"
// (.p-huddle_compact_audio_pane--additional_peers, bottom 4px right 4px;
// .p-peer_tile__overflow_text: 14px, weight 900, white on constants-black
// in this pane, radius 4px, padding 2px 4px).
func overflowCount(gtx C, th *theme.Theme, r image.Rectangle, more int) {
	txt := "+" + strconv.Itoa(more)
	ls := labelSize(gtx, th, 14, theme.Black, txt)
	b := image.Rect(r.Max.X-gtx.Dp(4)-ls.X-gtx.Dp(8), r.Max.Y-gtx.Dp(4)-ls.Y-gtx.Dp(4), r.Max.X-gtx.Dp(4), r.Max.Y-gtx.Dp(4))
	theme.RRect(gtx, b, gtx.Dp(4), color.NRGBA{A: 0xff})
	at(gtx, image.Pt(b.Min.X+gtx.Dp(4), b.Min.Y+gtx.Dp(2)), ls, func(gtx C) D { return th.Label(gtx, 14, theme.Black, hudWhite, txt) })
}

// stickerPicker is the sticker picker (the emoji picker's stickers tab,
// onlyShowStickers): Slack's stickers in a row, labelled on hover.
func (w *window) stickerPicker(gtx C, stage image.Rectangle) {
	bs := gtx.Dp(36)
	pad := gtx.Dp(6)
	wd := len(stickers)*bs + 2*pad
	r := image.Rect((stage.Min.X+stage.Max.X-wd)/2, stage.Max.Y-bs-2*pad-gtx.Dp(6), (stage.Min.X+stage.Max.X+wd)/2, stage.Max.Y-gtx.Dp(6))
	theme.RRect(gtx, r, gtx.Dp(10), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
	mine := ""
	if s := w.c.Session(); s != nil {
		mine = stickerBase(s.Sticker())
	}
	hover := ""
	for i, st := range stickers {
		p := image.Pt(r.Min.X+pad+i*bs, r.Min.Y+pad)
		txt := w.emojiText(w.c.withSkinTone(st.Emoji))
		on := mine == st.Emoji
		cl := w.click("pick:" + st.Emoji)
		if cl.Hovered() {
			hover = nsHuddles.T(st.Label)
		}
		at(gtx, p, image.Pt(bs, bs), func(gtx C) D {
			return button(gtx, cl, image.Pt(bs, bs), func(gtx C, hov bool) {
				switch {
				case on:
					theme.RRect(gtx, image.Rectangle{Max: image.Pt(bs, bs)}, gtx.Dp(8), hudBtnOn)
				case hov:
					theme.RRect(gtx, image.Rectangle{Max: image.Pt(bs, bs)}, gtx.Dp(8), hudBtnHover)
				}
			}, func(gtx C) D { centerLabel(gtx, w.th, image.Pt(bs, bs), 20, theme.Regular, hudWhite, txt); return D{} })
		})
	}
	if hover != "" {
		ls := labelSize(gtx, w.th, 13, theme.Bold, hover)
		tp := gtx.Dp(8)
		tr := image.Rect((r.Min.X+r.Max.X-ls.X)/2-tp, r.Min.Y-ls.Y-2*tp-gtx.Dp(4), (r.Min.X+r.Max.X+ls.X)/2+tp, r.Min.Y-gtx.Dp(4))
		theme.RRect(gtx, tr, gtx.Dp(6), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff})
		at(gtx, image.Pt(tr.Min.X+tp, tr.Min.Y+tp), ls, func(gtx C) D { return w.th.Label(gtx, 13, theme.Bold, hudWhite, hover) })
	}
}

// emojiText turns an emoji name into its character ("+1" → 👍) when Slack
// knows one, else ":name:".
func (w *window) emojiText(name string) string { return w.c.emojiText(name) }

// emojiText is the Controller's emoji lookup (the sidebar bar's faces).
func (c *Controller) emojiText(name string) string {
	if app := c.h.Core(); app != nil {
		if _, text, ok := app.Emoji(name); ok && text != "" {
			return text
		}
	}
	return ":" + name + ":"
}

type ctl struct {
	id, icon string
	on       bool // toggled on (.buttonToggledOn: the mic while it's live)
	wide     int  // dp; 0 = 40
	label    string
	warn     bool   // the warning badge (the microphone doesn't work)
	dim      bool   // unavailable (greyed)
	tip      string // tooltip on hover
	emoji    string // an emoji face instead of the icon (the sticker button)
	hover    string // the icon while hovered or on (the emoji button's emoji-filled)
	seg      int    // segMain / segCaret: a half of a segmented button
}

// The halves of Slack's segmented toolbar buttons (.segmentedButton: a
// 36px main part and a 28px caret, a 1px otl-inv-pry divider between).
const (
	segMain  = 1
	segCaret = 2
)

// toolbar is HuddleToolbar (gantry-v2-async-client-boot-shared 9d6D):
// the center buttons (HuddleToolbarMuteButton, HuddleToolbarCameraButton,
// HuddleToolbarScreenshareButton, the segmented sticker button,
// HuddleToolbarEmojiPickerButton, HuddleToolbarThreadButton,
// HuddleToolbarMoreActionsButton), each 40px with a 4px margin, centred.
// In the compact window (the huddle window) HuddleToolbarLeaveButton is
// the centre row's last button and there are no left or right buttons; in
// the expanded mode (the screen share popout) Leave sits at the right
// (rightButtons, margin-right 2px). The screen share button needs a
// toolbar at least 550px wide (eC; ed.aJ hides it below). Slack's canvas
// ("Take meeting notes") and Meeting Assistant buttons need what Clack
// doesn't have (huddle canvases in the window, Slack AI), so they aren't
// drawn (docs/HUDDLES-AUDIT.md).
func (w *window) toolbar(gtx C, st huddle.State, r image.Rectangle) {
	// HuddleToolbarMuteButton, segmented in the window (audiomenu.go)
	mics := w.c.micCtls(st, w.audio != nil)
	// HuddleToolbarSegmentedStickerPickerButton: your sticker (or the
	// raised hand) and the picker's caret
	mine := ""
	for _, p := range st.Participants {
		if p.Self {
			mine = p.Sticker
		}
	}
	face := mine
	if face == "" {
		face = w.c.handEmoji()
	}
	hand := ctl{id: "hand", emoji: w.emojiText(face), on: mine != "", tip: handTip(mine), seg: segMain}
	caret := "caret-up"
	if w.stick {
		caret = "caret-down"
	}
	// the caret is toggled on while the picker is open or a sticker is set
	// (isEmojiMenuOpen || P)
	sticks := ctl{id: "stickers", icon: caret, on: w.stick || mine != "", tip: nsHuddles.T("Choose sticker"), seg: segCaret}
	react := ctl{id: "react", icon: "emoji", hover: "emoji-filled", on: w.quick != nil, tip: nsHuddles.T("Add reaction")}
	thread := ctl{id: "thread", icon: "threads", tip: nsHuddles.T("Open thread")}
	more := ctl{id: "more", icon: "ellipsis-vertical-filled", on: w.more != nil, tip: nsHuddles.T("More actions")}
	compact := w.popout == ""
	ctls := append(mics, w.c.cameraCtls(st, w.video != nil)...)
	endAll := w.c.canEndForAll(st)
	shareMin := winShareMinW
	if endAll { // ek: the Leave options caret takes 28px more
		shareMin += leaveCaretW
	}
	if !compact || r.Dx() >= gtx.Dp(unit.Dp(shareMin)) {
		ctls = append(ctls, w.c.shareCtl(st))
	}
	ctls = append(ctls, hand, sticks, react, thread, more)

	h := gtx.Dp(40)
	m := gtx.Dp(4) // each button's margin
	total := 0
	for _, c := range ctls {
		total += w.ctlW(gtx, c)
		if c.seg != segMain {
			total += 2 * m // a segmented pair shares one margin box
		}
	}
	// the leave button: Leave in a c-button--medium (min-width 80px)
	lw := max(gtx.Dp(80), labelSize(gtx, w.th, 15, theme.Bold, nsHuddles.T("Leave")).X+2*gtx.Dp(12))
	lt := lw // with the Leave options caret (.segmentedLeaveButton)
	if endAll {
		lt += gtx.Dp(leaveCaretW)
	}
	lx := r.Dx() - gtx.Dp(2) - m - lt
	x := (r.Dx() - total) / 2
	if compact { // Leave ends the centred row
		x = max(0, (r.Dx()-total-lt-2*m)/2)
	} else if x+total > lx-m { // a narrow window: the center row moves left of Leave
		x = max(0, lx-m-total)
	}
	y := r.Min.Y + (r.Dy()-h)/2
	for _, c := range ctls {
		cw := w.ctlW(gtx, c)
		if c.seg != segCaret {
			x += m
		}
		c := c
		cl := w.click(c.id)
		if c.tip != "" && cl.Hovered() {
			w.cam.tip, w.cam.tipAnchor = c.tip, image.Rect(x, y, x+cw, y+h).Sub(r.Min)
		}
		if c.id == "more" {
			w.moreBtn = image.Rect(x, y, x+cw, y+h)
		}
		switch c.id {
		case "micmenu":
			w.audioBtn = image.Rect(x, y, x+cw, y+h)
		case "videomenu":
			w.videoBtn = image.Rect(x, y, x+cw, y+h)
		case "react":
			w.reactBtn = image.Rect(x, y, x+cw, y+h)
		}
		at(gtx, image.Pt(x, y), image.Pt(cw, h), func(gtx C) D {
			return button(gtx, cl, image.Pt(cw, h), func(gtx C, hov bool) {
				bg := hudBtn
				if hov {
					bg = hudBtnHover
				}
				if c.on {
					bg = hudBtnOn
				}
				segFill(gtx, image.Rectangle{Max: image.Pt(cw, h)}, c.seg, gtx.Dp(8), bg)
			}, func(gtx C) D {
				fg := hudWhite
				if c.on {
					fg = hudInk
				}
				if c.dim {
					fg.A = 0x66
				}
				icon := c.icon
				if c.hover != "" && (c.on || cl.Hovered()) {
					icon = c.hover
				}
				switch {
				case c.label != "":
					centerLabel(gtx, w.th, image.Pt(cw, h), 15, theme.Bold, hudWhite, c.label)
				case c.emoji != "":
					centerLabel(gtx, w.th, image.Pt(cw, h), 18, theme.Regular, fg, c.emoji)
				default:
					iconAt(gtx, image.Pt(cw, h), icon, 20, fg)
				}
				if c.seg == segMain { // .segmentedButtonMain:after, 20px tall
					theme.Fill(gtx, image.Rect(cw-max(1, gtx.Dp(1)), (h-gtx.Dp(20))/2, cw, (h+gtx.Dp(20))/2), segDivider)
				}
				if c.warn {
					warnBadge(gtx, image.Pt(cw, 0), gtx.Dp(14))
				}
				return D{}
			})
		})
		x += cw
		if c.seg != segMain {
			x += m
		}
	}
	// HuddleToolbarLeaveButton: base-inv-imp, "Leave", tooltip Leave Huddle
	// with its shortcut
	if compact {
		lx = x + m
	}
	cl := w.click("leave")
	if cl.Hovered() {
		w.cam.tip, w.cam.tipAnchor = nsHuddles.T("Leave Huddle")+"  "+leaveShortcut(), image.Rect(lx, y, lx+lw, y+h).Sub(r.Min)
	}
	seg := 0
	if endAll {
		seg = segMain // .segmentedLeaveButtonMain: 8px 0 0 8px
	}
	at(gtx, image.Pt(lx, y), image.Pt(lw, h), func(gtx C) D {
		return button(gtx, cl, image.Pt(lw, h), func(gtx C, hov bool) {
			bg := hudLeave
			if hov {
				bg = hudLeaveHov
			}
			segFill(gtx, image.Rectangle{Max: image.Pt(lw, h)}, seg, gtx.Dp(8), bg)
		}, func(gtx C) D {
			centerLabel(gtx, w.th, image.Pt(lw, h), 15, theme.Bold, hudWhite, nsHuddles.T("Leave"))
			return D{}
		})
	})
	if endAll {
		cr := image.Rect(lx+lw, y, lx+lt, y+h)
		w.leaveBtn = cr
		lc := w.click("leavemenu") // aria-label "Leave options", no tooltip
		at(gtx, cr.Min, cr.Size(), func(gtx C) D {
			return leaveCaret(gtx, lc, cr.Size(), w.leaveMenu != nil)
		})
	}
}

// segFill paints a toolbar button's background: radius 8, or a
// segmented half's (8px 0 0 8px for the main part, 0 8px 8px 0 for the
// caret).
func segFill(gtx C, r image.Rectangle, seg, rad int, bg color.NRGBA) {
	rr := clip.RRect{Rect: r, NW: rad, SW: rad, NE: rad, SE: rad}
	switch seg {
	case segMain:
		rr.NE, rr.SE = 0, 0
	case segCaret:
		rr.NW, rr.SW = 0, 0
	}
	defer rr.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// segDivider is .segmentedButtonMain:after's otl-inv-pry.
var segDivider = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x33}

// leaveShortcut is addJoinLeaveKeyboardShortcuts' hint: Mod+Shift+H on
// macOS, Mod+Alt+Shift+H elsewhere.
func leaveShortcut() string {
	if shortcut.GOOS == "darwin" {
		return shortcut.Format("Mod+Shift+H")
	}
	return shortcut.Format("Mod+Alt+Shift+H")
}

func (w *window) ctlW(gtx C, c ctl) int {
	switch {
	case c.wide > 0:
		return gtx.Dp(unit.Dp(c.wide))
	case c.seg == segMain:
		return gtx.Dp(36)
	case c.seg == segCaret:
		return gtx.Dp(28)
	}
	return gtx.Dp(40)
}

// ---- Slack's huddle window components (plugin patch points) ----

func (w *window) titleProps(s *huddle.Session, st huddle.State, title, sub string) slackc.Props {
	return slackc.Props{"$key": "huddle-title", "huddleId": s.RoomID(), "channelId": st.Channel, "title": title,
		"subtitle": sub, "className": "p-multi_view_window_title__huddle"}
}

// bottomBar is Slack's HuddleBottomBarContent: the call controls, with
// HuddleRecordingIndicator at the left (Clack doesn't record huddles, so
// natively it's empty; CallTimer draws its timer there).
func (w *window) bottomBar(gtx C, s *huddle.Session, st huddle.State, r image.Rectangle) {
	size := r.Size()
	rec := slackc.Props{"$key": "huddle-recording", "huddleId": s.RoomID(), "isRecording": false}
	props := slackc.Props{"$key": "huddle-bottom-bar", "huddleId": s.RoomID(), "channelId": st.Channel,
		"children": []any{slackc.Child("HuddleRecordingIndicator", rec, noNative)}}
	at(gtx, r.Min, size, func(gtx C) D {
		return slackc.Render(gtx, "HuddleBottomBarContent", props, func(gtx C, p slackc.Props) D {
			w.toolbar(gtx, st, image.Rectangle{Max: size})
			// the recording indicator, left of the controls, centered
			ig := gtx
			ig.Constraints = layout.Constraints{Max: image.Pt(size.X/4, size.Y)}
			im := op.Record(gtx.Ops)
			var id D
			if e, ok := slackc.PropChildren(p); ok {
				id = slackc.LayoutElement(ig, e)
			} else {
				id = slackc.Render(ig, "HuddleRecordingIndicator", rec, noNative)
			}
			ic := im.Stop()
			t := op.Offset(image.Pt(gtx.Dp(14), (size.Y-id.Size.Y)/2)).Push(gtx.Ops)
			ic.Add(gtx.Ops)
			t.Pop()
			return D{Size: size}
		})
	})
}

func noNative(gtx C, p slackc.Props) D { return D{} }
