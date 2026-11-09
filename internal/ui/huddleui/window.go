package huddleui

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	mic    micBanner
	notice string
	noteAt time.Time
	full   bool
	cam    camUI        // window_camera.go
	phase  huddle.Phase // this frame's (tiles: your spinner until connected)
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
	ww.Option(app.Title("Huddle - "+AppName), app.Size(unit.Dp(620), unit.Dp(400)), app.MinSize(unit.Dp(440), unit.Dp(300)))
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
		centerLabel(gtx, w.th, sz, 14, theme.Bold, hudWhite, "Joining the huddle…")
		return
	}
	st := s.State()
	now := time.Now()
	w.handle(gtx, s, st)
	w.cameraWatch(st)

	// title: headphones + "Huddle in #channel", centered
	titleH := gtx.Dp(34)
	title := "Huddle in " + w.c.channelTitle(st.Channel)
	switch st.Phase {
	case huddle.Connecting:
		title = "Connecting… " + title
	case huddle.Reconnecting:
		title = "Reconnecting… " + title
	}
	// the title is Slack's HuddleMvaWindowTitle (CallTimer adds its timer
	// after it): measured, then centered with the headphones before it
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(max(0, sz.X-gtx.Dp(22)-gtx.Dp(120)), titleH)}
	tm := op.Record(gtx.Ops)
	td := slackc.Render(tg, "HuddleMvaWindowTitle", w.titleProps(s, st, title), func(gtx C, p slackc.Props) D {
		return w.th.Label(gtx, 14, theme.Bold, hudWhite, p.StrOr("title", title))
	})
	tcall := tm.Stop()
	ts := td.Size
	ix := (sz.X - ts.X - gtx.Dp(22)) / 2
	at(gtx, image.Pt(ix, (titleH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx C) D {
		iconAt(gtx, image.Pt(gtx.Dp(16), gtx.Dp(16)), "headphones", 16, hudWhite)
		return D{Size: image.Pt(gtx.Dp(16), gtx.Dp(16))}
	})
	tt := op.Offset(image.Pt(ix+gtx.Dp(22), (titleH-ts.Y)/2)).Push(gtx.Ops)
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

	barH := gtx.Dp(60)
	stage := image.Rect(gtx.Dp(8), titleH, sz.X-gtx.Dp(8), sz.Y-barH)
	if stage.Dx() > 0 && stage.Dy() > 0 {
		w.stage(gtx, s, st, stage, now)
	}
	w.bottomBar(gtx, s, st, image.Rect(0, sz.Y-barH, sz.X, sz.Y))
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
}

func (w *window) handle(gtx C, s *huddle.Session, st huddle.State) {
	if w.click("mic").Clicked(gtx) {
		s.SetMuted(!st.Muted)
		w.mic.dismissed = huddle.MicOK // a broken mic: show why again
	}
	if w.click("deafen").Clicked(gtx) {
		s.SetDeafened(!st.Deafened)
		if !st.Deafened {
			w.note("You won't hear the huddle until you undeafen")
		}
	}
	if w.click("camera").Clicked(gtx) {
		w.cameraClick(s, st)
	}
	if w.click("screen").Clicked(gtx) {
		w.note(AppName + " can't share your screen yet")
	}
	if w.click("react").Clicked(gtx) {
		w.picker = !w.picker
	}
	if w.click("cc").Clicked(gtx) {
		s.SetCaptions(!st.CaptionsOn)
		if !st.CaptionsOn {
			w.note("Captions show when someone in the huddle turns on transcription")
		}
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
			if err := s.React(e, ""); err != nil {
				w.note("Couldn't send the reaction")
			}
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
		w.captions(gtx, st, r)
	}
	if w.picker {
		w.emojiPicker(gtx, r)
	}
}

// emojiText turns an emoji name into its character ("+1" → 👍) when Slack
// knows one, else ":name:".
func (w *window) emojiText(name string) string {
	if app := w.c.h.Core(); app != nil {
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

func (w *window) captions(gtx C, st huddle.State, stage image.Rectangle) {
	lines := st.Captions
	if len(lines) > 2 {
		lines = lines[len(lines)-2:]
	}
	if len(lines) == 0 {
		return
	}
	maxW := stage.Dx() * 4 / 5
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		if n := w.c.userName(l.UserID); n != "" {
			b.WriteString(n + ": ")
		}
		b.WriteString(l.Text)
	}
	txt := b.String()
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(maxW, stage.Dy()/2)}
	d := w.th.Para(g, 14, 20, theme.Bold, hudWhite, txt)
	call := m.Stop()
	pad := gtx.Dp(10)
	r := image.Rect((stage.Min.X+stage.Max.X-d.Size.X)/2-pad, stage.Max.Y-d.Size.Y-2*pad-gtx.Dp(10), (stage.Min.X+stage.Max.X+d.Size.X)/2+pad, stage.Max.Y-gtx.Dp(10))
	theme.RRect(gtx, r, gtx.Dp(8), color.NRGBA{A: 0xcc})
	defer op.Offset(image.Pt(r.Min.X+pad, r.Min.Y+pad)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

type ctl struct {
	id, icon string
	on       bool // lit (mic on)
	wide     int  // dp; 0 = 40
	label    string
	warn     bool   // the warning badge (the microphone doesn't work)
	dim      bool   // unavailable (greyed)
	tip      string // tooltip on hover
}

func (w *window) toolbar(gtx C, st huddle.State, r image.Rectangle) {
	mic := ctl{id: "mic", icon: "mic", on: true}
	if st.Muted || st.Mic.Problem() {
		mic = ctl{id: "mic", icon: "mic-off", warn: st.Mic.Problem()}
	}
	deaf := ctl{id: "deafen", icon: "headphones"}
	if st.Deafened {
		deaf = ctl{id: "deafen", icon: "headphones-off", on: true}
	}
	cc := ctl{id: "cc", icon: "captions", on: st.CaptionsOn}
	ctls := []ctl{mic, deaf, w.cameraCtl(st), {id: "screen", icon: "screen-share"},
		{id: "react", icon: "emoji", on: w.picker}, cc, {id: "thread", icon: "threads"}, {id: "leave", wide: 84, label: "Leave"}}
	gap := gtx.Dp(8)
	h := gtx.Dp(40)
	total := 0
	for _, c := range ctls {
		total += w.ctlW(gtx, c) + gap
	}
	total -= gap
	x := (r.Dx() - total) / 2
	y := r.Min.Y + (r.Dy()-h)/2
	for _, c := range ctls {
		cw := w.ctlW(gtx, c)
		c := c
		if c.tip != "" && w.click(c.id).Hovered() {
			w.cam.tip, w.cam.tipAnchor = c.tip, image.Rect(x, y, x+cw, y+h).Sub(r.Min)
		}
		at(gtx, image.Pt(x, y), image.Pt(cw, h), func(gtx C) D {
			return button(gtx, w.click(c.id), image.Pt(cw, h), func(gtx C, hov bool) {
				bg := hudBtn
				if hov {
					bg = hudBtnHover
				}
				if c.on {
					bg = hudBtnOn
				}
				if c.id == "leave" {
					bg = hudLeave
					if hov {
						bg = hudLeaveHov
					}
				}
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(cw, h)}, gtx.Dp(8), bg)
			}, func(gtx C) D {
				fg := hudWhite
				if c.on {
					fg = hudInk
				}
				if c.dim {
					fg.A = 0x66
				}
				if c.label != "" {
					centerLabel(gtx, w.th, image.Pt(cw, h), 15, theme.Bold, hudWhite, c.label)
				} else {
					iconAt(gtx, image.Pt(cw, h), c.icon, 20, fg)
				}
				if c.warn {
					warnBadge(gtx, image.Pt(cw, 0), gtx.Dp(14))
				}
				return D{}
			})
		})
		x += cw + gap
	}
}

func (w *window) ctlW(gtx C, c ctl) int {
	if c.wide > 0 {
		return gtx.Dp(unit.Dp(c.wide))
	}
	return gtx.Dp(40)
}

// ---- Slack's huddle window components (plugin patch points) ----

func (w *window) titleProps(s *huddle.Session, st huddle.State, title string) slackc.Props {
	return slackc.Props{"$key": "huddle-title", "huddleId": s.RoomID(), "channelId": st.Channel, "title": title,
		"className": "p-multi_view_window_title__huddle"}
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
