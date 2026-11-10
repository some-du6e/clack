package huddleui

import (
	"image"
	"image/color"
	"runtime"
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
	picker bool
	stick  bool // the sticker picker (hand.go)
	mic    micBanner
	notice string
	noteAt time.Time
	full   bool
	cam    camUI        // window_camera.go
	phase  huddle.Phase // this frame's (tiles: your spinner until connected)

	more    *popups.Menu    // the ⋯ More actions menu (window_menu.go)
	moreBtn image.Rectangle // its button, in the toolbar
	barTop  int             // the toolbar's top in the window
	cc      ccState         // the closed captions' fade (captions.go)
	banner  banner          // HuddlesInformationalBanner (captions.go)
	tips    map[string]*popups.TipState
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

func (w *window) invalidate() {
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
	ww.Option(app.Title("Huddle - "+AppName), app.Size(unit.Dp(620), unit.Dp(400)), app.MinSize(unit.Dp(520), unit.Dp(300)))
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
	tick := time.NewTicker(250 * time.Millisecond) // speaking bars, timer, images
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-tick.C:
				ww.Invalidate()
			case <-done:
				return
			}
		}
	}()
	var ops op.Ops
	for {
		switch e := ww.Event().(type) {
		case app.DestroyEvent:
			tick.Stop()
			close(done)
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

func (w *window) layout(gtx C) {
	sz := gtx.Constraints.Max
	theme.Fill(gtx, image.Rectangle{Max: sz}, hudFrame)
	s := w.c.Session()
	if s == nil {
		huddleLoading(gtx, w.th, sz)
		return
	}
	st := s.State()
	now := time.Now()
	w.handle(gtx, s, st)
	w.cameraWatch(st)

	// the title is Slack's HuddleMvaWindowTitle (CallTimer adds its timer
	// after it): .p-multi_view_window_title__huddle, the headphones, 8px,
	// the title, 8px, the "{n} people" subtitle (regular), all at the
	// title's .8 opacity; measured, then centered
	titleH := gtx.Dp(34)
	title, sub := w.c.windowTitle(st)
	ink := theme.WithAlpha(hudWhite, 0xcc)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(max(0, sz.X-gtx.Dp(24)-gtx.Dp(120)), titleH)}
	tm := op.Record(gtx.Ops)
	td := slackc.Render(tg, "HuddleMvaWindowTitle", w.titleProps(s, st, title, sub), func(gtx C, p slackc.Props) D {
		t := w.th.Label(gtx, 14, theme.Bold, ink, p.StrOr("title", title))
		sb := p.StrOr("subtitle", sub)
		if sb == "" {
			return t
		}
		so := op.Offset(image.Pt(t.Size.X+gtx.Dp(8), 0)).Push(gtx.Ops)
		gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-t.Size.X-gtx.Dp(8))
		sd := w.th.Label(gtx, 14, theme.Regular, ink, sb)
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
	bs := gtx.Dp(28)
	at(gtx, image.Pt(sz.X-bs-gtx.Dp(10), (titleH-bs)/2), image.Pt(bs, bs), func(gtx C) D {
		return button(gtx, w.click("full"), image.Pt(bs, bs), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(6), hudBtn)
			}
		}, func(gtx C) D { iconAt(gtx, image.Pt(bs, bs), "expand", 16, hudWhite); return D{} })
	})

	barH := gtx.Dp(56) // .toolbarContainerExpandedMode: height 56px
	stage := image.Rect(gtx.Dp(8), titleH, sz.X-gtx.Dp(8), sz.Y-barH)
	if stage.Dx() > 0 && stage.Dy() > 0 {
		w.stage(gtx, s, st, stage, now)
	}
	w.barTop = sz.Y - barH
	w.bottomBar(gtx, s, st, image.Rect(0, sz.Y-barH, sz.X, sz.Y))
	w.layoutBanner(gtx, stage, now)
	if n, ok := w.mic.visible(st); ok && stage.Dx() > gtx.Dp(40) {
		w.mic.handle(gtx, s, st, n)
		bw := min(stage.Dx()-gtx.Dp(24), gtx.Dp(480))
		at(gtx, image.Pt((sz.X-bw)/2, stage.Min.Y+gtx.Dp(12)), image.Pt(bw, stage.Dy()), func(gtx C) D {
			return layoutMicBanner(gtx, w.th, &w.mic, n, bw)
		})
	}
	if w.notice != "" && now.Sub(w.noteAt) < 4*time.Second {
		ns := labelSize(gtx, w.th, 13, theme.Bold, w.notice)
		pad := gtx.Dp(10)
		r := image.Rect((sz.X-ns.X)/2-pad, stage.Min.Y+gtx.Dp(10), (sz.X+ns.X)/2+pad, stage.Min.Y+gtx.Dp(10)+ns.Y+pad)
		theme.RRect(gtx, r, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xee})
		at(gtx, image.Pt(r.Min.X+pad, r.Min.Y+pad/2), ns, func(gtx C) D {
			return w.th.Label(gtx, 13, theme.Bold, hudWhite, w.notice)
		})
	}
	w.cameraNotice(gtx, st, stage)
	w.toolbarTip(gtx, image.Rect(0, sz.Y-barH, sz.X, sz.Y))
	w.layoutMore(gtx)
}

func (w *window) handle(gtx C, s *huddle.Session, st huddle.State) {
	if w.click("mic").Clicked(gtx) {
		s.SetMuted(!st.Muted)
		w.mic.dismissed = huddle.MicOK // a broken mic: show why again
	}
	if w.click("camera").Clicked(gtx) {
		w.cameraClick(s, st)
	}
	if w.click("react").Clicked(gtx) {
		w.picker = !w.picker
		w.stick = false
	}
	if w.click("hand").Clicked(gtx) { // Raise hand / Lower hand / Remove sticker
		go func() { _ = w.c.ToggleHand() }()
	}
	if w.click("stickers").Clicked(gtx) { // Choose sticker
		w.stick = !w.stick
		w.picker = false
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
	if w.click("full").Clicked(gtx) {
		w.full = !w.full
		if w.full {
			w.w.Option(app.Fullscreen.Option())
		} else {
			w.w.Option(app.Windowed.Option())
		}
	}
	for _, e := range quickEmoji {
		if w.click("emoji:" + e).Clicked(gtx) {
			w.picker = false
			_ = s.React(e, "")
		}
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

var quickEmoji = []string{"+1", "heart", "joy", "tada", "wave", "clap", "raised_hands", "fire"}

// stage draws the participants (and the screen share, when there is one).
func (w *window) stage(gtx C, s *huddle.Session, st huddle.State, r image.Rectangle, now time.Time) {
	gradient(gtx, r, gtx.Dp(10), hudStageA, hudStageB)
	ps := orderedParticipants(st)
	if w.c.hideSelf.Load() && len(ps) > 1 { // "Hide self-view": your tile leaves the gallery
		ps = withoutSelf(ps)
	}
	cams := map[string]string{} // attendee -> video key
	var share *huddle.Video
	for i := range st.Videos {
		v := st.Videos[i]
		if v.Content {
			if share == nil {
				share = &st.Videos[i]
			}
		} else {
			cams[v.AttendeeID] = v.Key
		}
	}
	selfCam(st, cams) // your own tile shows your (mirrored) preview
	w.phase = st.Phase
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
				centerLabel(gtx, w.th, sr.Size(), 13, theme.Bold, hudWhite, "Loading screen share…")
				return D{}
			})
		}
		// Slack's drawing layer over a shared screen (BaseDrawRenderer):
		// Clack draws no annotations, so natively it's empty
		drawP := slackc.Props{"$key": "huddle-draw-" + share.Key, "huddleId": s.RoomID(), "screenShareId": share.Key,
			"isScreenSharing": true, "width": sr.Dx(), "height": sr.Dy()}
		at(gtx, sr.Min, sr.Size(), func(gtx C) D { return slackc.Render(gtx, "BaseDrawRenderer", drawP, noNative) })
		// no presenter label on the shared screen: Slack names the presenter
		// only on a screen share tile's footer in the grid
		n := len(ps) + len(st.Invites)
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
	} else if n := len(ps) + len(st.Invites); n > 0 {
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
	if st.CaptionsOn {
		w.captions(gtx, st, r, now)
	}
	if w.picker {
		w.emojiPicker(gtx, r)
	}
	if w.stick {
		w.stickerPicker(gtx, r)
	}
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
			hover = st.Label
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

func (w *window) emojiPicker(gtx C, stage image.Rectangle) {
	bs := gtx.Dp(36)
	pad := gtx.Dp(6)
	wd := len(quickEmoji)*bs + 2*pad
	r := image.Rect((stage.Min.X+stage.Max.X-wd)/2, stage.Max.Y-bs-2*pad-gtx.Dp(6), (stage.Min.X+stage.Max.X+wd)/2, stage.Max.Y-gtx.Dp(6))
	theme.RRect(gtx, r, gtx.Dp(10), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
	for i, e := range quickEmoji {
		p := image.Pt(r.Min.X+pad+i*bs, r.Min.Y+pad)
		txt := w.emojiText(e)
		at(gtx, p, image.Pt(bs, bs), func(gtx C) D {
			return button(gtx, w.click("emoji:"+e), image.Pt(bs, bs), func(gtx C, hov bool) {
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: image.Pt(bs, bs)}, gtx.Dp(8), hudBtnHover)
				}
			}, func(gtx C) D { centerLabel(gtx, w.th, image.Pt(bs, bs), 20, theme.Regular, hudWhite, txt); return D{} })
		})
	}
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

// toolbar is HuddleToolbar in the expanded window: the center buttons
// (HuddleToolbarMuteButton, HuddleToolbarCameraButton, the segmented
// sticker button, HuddleToolbarEmojiPickerButton, HuddleToolbarThreadButton,
// HuddleToolbarMoreActionsButton), each 40px with a 4px margin, centered,
// and HuddleToolbarLeaveButton at the right (rightButtons, margin-right
// 2px). Slack's screen share, canvas and Meeting Assistant buttons need
// what Clack doesn't have (a screen capture, huddle canvases, Slack AI),
// so they aren't drawn; the device carets are open (docs/HUDDLES-AUDIT.md).
func (w *window) toolbar(gtx C, st huddle.State, r image.Rectangle) {
	// useMicrophoneButton: "Mute mic" while it's live, else "Unmute mic"
	mic := ctl{id: "mic", icon: "mic", on: true, tip: "Mute mic"}
	if st.Muted || st.Mic.Problem() {
		mic = ctl{id: "mic", icon: "mic-off", warn: st.Mic.Problem(), tip: "Unmute mic"}
	}
	if st.Mic == huddle.MicDenied {
		mic.tip = "Microphone permission denied"
	}
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
	sticks := ctl{id: "stickers", icon: caret, on: w.stick, tip: "Choose sticker", seg: segCaret}
	react := ctl{id: "react", icon: "emoji", hover: "emoji-filled", on: w.picker, tip: "Add reaction"}
	thread := ctl{id: "thread", icon: "threads", tip: "Open thread"}
	more := ctl{id: "more", icon: "ellipsis-vertical-filled", on: w.more != nil, tip: "More actions"}
	ctls := []ctl{mic, w.cameraCtl(st), hand, sticks, react, thread, more}

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
	lw := max(gtx.Dp(80), labelSize(gtx, w.th, 15, theme.Bold, "Leave").X+2*gtx.Dp(12))
	lx := r.Dx() - gtx.Dp(2) - m - lw
	x := (r.Dx() - total) / 2
	if x+total > lx-m { // a narrow window: the center row moves left of Leave
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
	cl := w.click("leave")
	if cl.Hovered() {
		w.cam.tip, w.cam.tipAnchor = "Leave Huddle  "+leaveShortcut(), image.Rect(lx, y, lx+lw, y+h).Sub(r.Min)
	}
	at(gtx, image.Pt(lx, y), image.Pt(lw, h), func(gtx C) D {
		return button(gtx, cl, image.Pt(lw, h), func(gtx C, hov bool) {
			bg := hudLeave
			if hov {
				bg = hudLeaveHov
			}
			theme.RRect(gtx, image.Rectangle{Max: image.Pt(lw, h)}, gtx.Dp(8), bg)
		}, func(gtx C) D {
			centerLabel(gtx, w.th, image.Pt(lw, h), 15, theme.Bold, hudWhite, "Leave")
			return D{}
		})
	})
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

// leaveShortcut is the leave button's shortcut hint (Cmd+Shift+H on
// macOS, Ctrl+Alt+Shift+H elsewhere).
func leaveShortcut() string {
	if runtime.GOOS == "darwin" {
		return "⌘⇧H"
	}
	return "Ctrl+Alt+Shift+H"
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
