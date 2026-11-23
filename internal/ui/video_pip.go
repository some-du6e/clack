package ui

// The mini player's window part: Slack's PipShell and PipControls (yF7D,
// build 2c0d02ca130f) with client-boot-styles' rules (shell__eekXn and
// its corners, videoOnly__fvSEQ, controls__whU5Y, chrome__jPCpt,
// darkButton__UuGW3, the resize handles). videoplayer docks a video into
// it (pip.go); the window draws it over its content, under the overlays
// (z-index 1001), and gives its controls their actions:
//   - Expand (expand-diagonal): the media viewer at the mini player's time,
//     playing if it played; the mini player closes.
//   - Jump to message (move-to-main-window, with clips_pip): the message,
//     in its thread for a reply, and the inline player plays on from
//     there if this played (usePipUndock's onUndock).
//   - Close: the inline player takes the time, paused.
//
// With clips_pip the shell moves: a press and a drag past 5px follows the
// pointer, and the release snaps it to the corner of the window's quarter
// the pointer is in (getCornerForPosition), easing there over .2s; the
// arrow keys move it between the corners while it has the focus; and the
// handles on its open sides resize it (usePipResize: from its minimum to
// 1200px, the aspect kept; a double click resets it). Its width is 360px,
// 480 in a window 1680px wide or more (a portrait video's scaled by its
// aspect), at least 150px tall, top right for each new video.

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// pipUI is PipShell's state for the video it shows.
type pipUI struct {
	media  string // the video: PipShell is keyed by it, so a new one starts over
	corner string // "top-right" (the first), "top-left", "bottom-right", "bottom-left"
	tag    int    // the shell's input
	origin image.Point
	// a press on the shell and its drag (PipShell's onMouseDown and the
	// document's mousemove and mouseup)
	pressed, dragging bool
	pressAt, startPos image.Point     // window coordinates
	video             image.Rectangle // the video's place in the shell
	dragPos           image.Point     // the shell's top-left while dragged
	// the snap's transition from where the drag left it (.2s top/left/
	// right/bottom)
	snapFrom image.Point
	snapAt   time.Time
	// a resize (usePipResize)
	edge     string
	edgeAt   image.Point
	edgeW    int // the width at the press, dp
	edgeTags [3]int
	// click counts the handles' presses by the platform's rule (their
	// double click; gesture.ClickRun, Gio patch 48), in window px: a
	// drag ends the run
	click   gesture.ClickRun
	focused bool
	btns    [3]widget.Clickable
	tips    [3]popups.TipState
	// as the last frame drew them, in the window (tests)
	rect     image.Rectangle
	ctlRects []image.Rectangle // Expand, Jump to message (clips_pip), Close
}

const pipSnap = 200 * time.Millisecond

// pipCorners are the corners' insets (top, right, bottom, left in dp; -1
// is auto), client-boot-styles' topRight__COGEH and the others.
var pipCorners = map[string][4]int{
	"top-right":    {56, 20, -1, -1},
	"top-left":     {56, -1, -1, 20},
	"bottom-right": {-1, 20, 20, -1},
	"bottom-left":  {-1, -1, 20, 20},
}

// pipNextCorner is getNextCorner: an arrow's corner from c, "" for none.
func pipNextCorner(c string, k key.Name) string {
	switch k {
	case key.NameLeftArrow:
		return map[string]string{"top-right": "top-left", "bottom-right": "bottom-left"}[c]
	case key.NameRightArrow:
		return map[string]string{"top-left": "top-right", "bottom-left": "bottom-right"}[c]
	case key.NameUpArrow:
		return map[string]string{"bottom-left": "top-left", "bottom-right": "top-right"}[c]
	case key.NameDownArrow:
		return map[string]string{"top-left": "bottom-left", "top-right": "bottom-right"}[c]
	}
	return ""
}

// pipCornerAt is getCornerForPosition: the corner of the window's quarter
// p is in.
func pipCornerAt(p, win image.Point) string {
	left, top := p.X < win.X/2, p.Y < win.Y/2
	switch {
	case top && left:
		return "top-left"
	case top:
		return "top-right"
	case left:
		return "bottom-left"
	}
	return "bottom-right"
}

// pipEdges are the resize handles by corner (the sides away from it).
var pipEdges = map[string][3]string{
	"top-right":    {"s", "w", "sw"},
	"top-left":     {"s", "e", "se"},
	"bottom-right": {"n", "w", "nw"},
	"bottom-left":  {"n", "e", "ne"},
}

// pipProject is projectDelta: the width a handle's drag of (dx, dy) adds.
func pipProject(edge string, dx, dy, aspect float64) float64 {
	dom := func(a, b float64) float64 {
		if math.Abs(a) >= math.Abs(b) {
			return a
		}
		return b
	}
	switch edge {
	case "e":
		return dx
	case "w":
		return -dx
	case "s":
		return dy * aspect
	case "n":
		return -dy * aspect
	case "se":
		return dom(dx, dy*aspect)
	case "sw":
		return dom(-dx, dy*aspect)
	case "ne":
		return dom(dx, -dy*aspect)
	case "nw":
		return dom(-dx, -dy*aspect)
	}
	return 0
}

// pipSize is the shell's size in dp: VideoPipContent's width (360, 480
// in a window 1680 wide or more; a portrait video's scaled by its
// aspect; the resized width when larger), capped as buildPipWidthValue
// caps it with clips_pip (the window less 20px, and the height the window
// leaves), and at least 150 tall (videoOnly__fvSEQ). minW is the width a
// resize can't go under.
func pipSize(f *richtext.File, nat image.Point, win image.Point, resized int, exp bool) (w, h, minW int, aspect float64) {
	if nat.X <= 0 || nat.Y <= 0 {
		nat = image.Pt(f.OrigW, f.OrigH)
	}
	if nat.X <= 0 || nat.Y <= 0 {
		nat = image.Pt(16, 9)
	}
	aspect = float64(nat.X) / float64(nat.Y)
	u := 360
	if win.X >= 1680 {
		u = 480
	}
	minW = u
	if nat.X <= nat.Y { // "landscape" == (L > O ? …): a square one is a portrait too
		minW = int(float64(u) * aspect)
	}
	w = max(resized, minW)
	if exp {
		vs := win.Y - 44 - (44 + 12) - 20 // --pip-vertical-space
		w = min(w, win.X-20, max(minW, int(float64(vs)*aspect)))
	}
	w = max(w, 1)
	h = max(int(math.Round(float64(w)/aspect)), 150)
	return w, h, minW, aspect
}

// layoutVideoPip draws the mini player over the window's content.
func (a *App) layoutVideoPip(gtx layout.Context) {
	v := a.vids
	if v == nil {
		return
	}
	row, m, ok := v.Pip()
	if !ok || m.File == nil {
		a.pip = pipUI{}
		return
	}
	p := &a.pip
	if id := m.Channel + "/" + m.TS + "/" + m.File.ID; p.media != id {
		*p = pipUI{media: id, corner: "top-right"}
		if v.PipTrigger() == "manual" { // focusOnMount: "manual" === trigger
			gtx.Execute(key.FocusCmd{Tag: &p.tag})
		}
	}
	exp := v.PipOn()
	sc := max(gtx.Metric.PxPerDp, 0.01)
	dp := func(x int) int { return gtx.Dp(unit.Dp(float32(x))) }
	win := gtx.Constraints.Max
	winDp := image.Pt(int(float32(win.X)/sc), int(float32(win.Y)/sc))
	_, _, playing := v.PipPlayback()
	nat := image.Point{}
	wDp, hDp, minW, aspect := pipSize(m.File, nat, winDp, v.PipWidth(), exp)
	size := image.Pt(dp(wDp), dp(hDp))
	home := a.pipCornerPos(p.corner, size, win, dp)
	pos := home
	now := gtx.Now

	// input, from the last frame's places
	a.pipInput(gtx, v, p, m, win, size, minW, aspect, exp, dp)
	switch {
	case p.dragging:
		pos = p.dragPos
	case !p.snapAt.IsZero() && now.Sub(p.snapAt) < pipSnap:
		f := pipEase(float64(now.Sub(p.snapAt)) / float64(pipSnap))
		pos = image.Pt(p.snapFrom.X+int(float64(home.X-p.snapFrom.X)*f), p.snapFrom.Y+int(float64(home.Y-p.snapFrom.Y)*f))
		a.pipAnimate(p.snapAt.Add(pipSnap))
	}
	// a resize keeps the corner's sides where they are
	wDp, hDp, _, _ = pipSize(m.File, nat, winDp, v.PipWidth(), exp)
	size = image.Pt(dp(wDp), dp(hDp))
	if !p.dragging && (p.snapAt.IsZero() || now.Sub(p.snapAt) >= pipSnap) {
		pos = a.pipCornerPos(p.corner, size, win, dp)
	}
	r := image.Rectangle{Min: pos, Max: pos.Add(size)}
	p.origin, p.rect = pos, r

	// the shell: its shadow, then its box (black, 8px radius)
	rad := dp(8)
	pipShadow(gtx.Ops, r, rad, dp(20), dp(25), -dp(5), color.NRGBA{A: 0x19})
	pipShadow(gtx.Ops, r, rad, dp(8), dp(10), -dp(6), color.NRGBA{A: 0x19})
	off := op.Offset(pos).Push(gtx.Ops)
	box := image.Rectangle{Max: size}
	cl := clip.UniformRRect(box, rad).Push(gtx.Ops)
	paint.Fill(gtx.Ops, color.NRGBA{A: 0xff}) // constants-black
	// the video, contained in the shell (p-video_mini_player: 100% of it)
	vr := videoplayer.Contain(box, image.Pt(int(math.Round(aspect*1000)), 1000))
	p.video = vr
	if !playing && m.File.Thumb != nil && m.File.Thumb.URL != "" && a.images != nil {
		if img, ok := a.images.Get(m.File.Thumb.URL, 0); ok { // the poster until its frames
			pipDrawStretched(gtx, img, vr)
		}
	}
	vo := op.Offset(vr.Min).Push(gtx.Ops)
	vg := gtx
	vg.Constraints = layout.Exact(vr.Size())
	row.LayoutMedia(vg, richview.MediaSlot{File: m.File, Poster: image.Rectangle{Max: vr.Size()}})
	vo.Pop()
	cl.Pop()
	// the shell's input over the video, passing the press on to it (a
	// press bubbles up to PipShell's onMouseDown unless the controls
	// container stopped it): the drag, the focus, its keys
	pass := pointer.PassOp{}.Push(gtx.Ops)
	sa := clip.Rect(box).Push(gtx.Ops)
	event.Op(gtx.Ops, &p.tag)
	if p.dragging {
		pointer.CursorGrabbing.Add(gtx.Ops)
	}
	sa.Pop()
	pass.Pop()

	// PipControls: top 12, right 12, 4px apart
	a.pipControls(gtx, v, p, m, size, exp, dp)
	// the resize handles on the sides away from the corner
	if exp {
		for i, e := range pipEdges[p.corner] {
			hr := pipEdgeRect(e, size, dp)
			ha := clip.Rect(hr).Push(gtx.Ops)
			event.Op(gtx.Ops, &p.edgeTags[i])
			pipEdgeCursor(e).Add(gtx.Ops)
			ha.Pop()
		}
	}
	off.Pop()
}

// pipCornerPos is the shell's top-left at corner c.
func (a *App) pipCornerPos(c string, size, win image.Point, dp func(int) int) image.Point {
	in, ok := pipCorners[c]
	if !ok {
		in = pipCorners["top-right"]
	}
	x, y := dp(in[3]), dp(in[0])
	if in[1] >= 0 {
		x = win.X - dp(in[1]) - size.X
	}
	if in[2] >= 0 {
		y = win.Y - dp(in[2]) - size.Y
	}
	return image.Pt(x, y)
}

// pipEdgeRect is a handle's area in the shell: 8px along a side (12px in
// from the corners), 12px squares at the corners.
func pipEdgeRect(e string, sz image.Point, dp func(int) int) image.Rectangle {
	t, c, in := dp(8), dp(12), dp(12)
	switch e {
	case "n":
		return image.Rect(in, 0, sz.X-in, t)
	case "s":
		return image.Rect(in, sz.Y-t, sz.X-in, sz.Y)
	case "e":
		return image.Rect(sz.X-t, in, sz.X, sz.Y-in)
	case "w":
		return image.Rect(0, in, t, sz.Y-in)
	case "ne":
		return image.Rect(sz.X-c, 0, sz.X, c)
	case "nw":
		return image.Rect(0, 0, c, c)
	case "se":
		return image.Rect(sz.X-c, sz.Y-c, sz.X, sz.Y)
	case "sw":
		return image.Rect(0, sz.Y-c, c, sz.Y)
	}
	return image.Rectangle{}
}

func pipEdgeCursor(e string) pointer.Cursor {
	switch e {
	case "n", "s":
		return pointer.CursorNorthSouthResize
	case "e", "w":
		return pointer.CursorEastWestResize
	case "ne", "sw":
		return pointer.CursorNorthEastSouthWestResize
	}
	return pointer.CursorNorthWestSouthEastResize
}

// pipInput reads the shell's and the handles' events (their places are
// the last frame's: window coordinates are the events' plus its origin).
func (a *App) pipInput(gtx layout.Context, v *videoplayer.Controller, p *pipUI, m videoplayer.PipMedia, win, size image.Point, minW int, aspect float64, exp bool, dp func(int) int) {
	sc := max(gtx.Metric.PxPerDp, 0.01)
	winPt := func(e pointer.Event, o image.Point) image.Point {
		return image.Pt(int(e.Position.X)+o.X, int(e.Position.Y)+o.Y)
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel},
			key.FocusFilter{Target: &p.tag},
			key.Filter{Focus: &p.tag, Name: key.NameLeftArrow}, key.Filter{Focus: &p.tag, Name: key.NameRightArrow},
			key.Filter{Focus: &p.tag, Name: key.NameUpArrow}, key.Filter{Focus: &p.tag, Name: key.NameDownArrow})
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			p.focused = e.Focus
		case key.Event:
			if e.State != key.Press || !exp || e.Modifiers != 0 {
				continue
			}
			if n := pipNextCorner(p.corner, e.Name); n != "" {
				p.corner, p.snapAt = n, time.Time{} // a class to a class: no transition
			}
		case pointer.Event:
			switch e.Kind {
			case pointer.Press:
				if e.Buttons != pointer.ButtonPrimary {
					continue
				}
				gtx.Execute(key.FocusCmd{Tag: &p.tag}) // tabIndex 0: the press focuses it
				// only from the video's own area: the controls container
				// stops mousedown (fIJO), so a scrub or the volume drags nothing
				lp := image.Pt(int(e.Position.X), int(e.Position.Y)).Sub(p.video.Min)
				if !exp || !v.PipFreeAt(lp) {
					continue
				}
				p.pressed, p.dragging = true, false
				p.pressAt, p.startPos = winPt(e, p.origin), p.origin
			case pointer.Drag:
				if !p.pressed {
					continue
				}
				at := winPt(e, p.origin)
				d := at.Sub(p.pressAt)
				if !p.dragging {
					if pipAbs(d.X) < dp(5) && pipAbs(d.Y) < dp(5) {
						continue
					}
					p.dragging = true
					gtx.Execute(pointer.GrabCmd{Tag: &p.tag, ID: e.PointerID})
				}
				p.dragPos = p.startPos.Add(d)
				p.origin = p.dragPos
			case pointer.Release, pointer.Cancel:
				if p.dragging && e.Kind == pointer.Release {
					p.corner = pipCornerAt(winPt(e, p.origin), win)
					p.snapFrom, p.snapAt = p.dragPos, gtx.Now
				}
				p.pressed, p.dragging = false, false
			}
		}
	}
	for i := range p.edgeTags {
		e := pipEdges[p.corner][i]
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &p.edgeTags[i], Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok || !exp {
				continue
			}
			switch pe.Kind {
			case pointer.Press:
				if pe.Buttons != pointer.ButtonPrimary {
					continue
				}
				wp := pe
				wp.Position = layout.FPt(winPt(pe, p.origin))
				if p.click.Press(gtx.Source, wp) == 2 { // onDoubleClick (a dblclick): back to the default
					v.SetPipWidth(0)
					p.edge = ""
					continue
				}
				p.edge, p.edgeAt = e, winPt(pe, p.origin)
				p.edgeW = int(float32(size.X) / sc)
				gtx.Execute(pointer.GrabCmd{Tag: &p.edgeTags[i], ID: pe.PointerID})
			case pointer.Drag:
				if p.edge != e {
					continue
				}
				wp := pe
				wp.Position = layout.FPt(winPt(pe, p.origin))
				p.click.Dragged(wp)
				d := winPt(pe, p.origin).Sub(p.edgeAt)
				add := pipProject(e, float64(d.X)/float64(sc), float64(d.Y)/float64(sc), aspect)
				v.SetPipWidth(int(math.Round(min(max(float64(p.edgeW)+add, float64(minW)), 1200))))
			case pointer.Release:
				wp := pe
				wp.Position = layout.FPt(winPt(pe, p.origin))
				p.click.Released(wp)
				p.edge = ""
			case pointer.Cancel:
				p.click.Cancelled()
				p.edge = ""
			}
		}
	}
}

// pipControls draws PipControls at the shell's top right and runs them.
func (a *App) pipControls(gtx layout.Context, v *videoplayer.Controller, p *pipUI, m videoplayer.PipMedia, size image.Point, exp bool, dp func(int) int) {
	type ctl struct {
		i     int
		icon  string
		label string
		do    func()
	}
	ctls := []ctl{{0, "expand", "Expand", func() { a.pipExpand(v, m) }}}
	if exp {
		ctls = append(ctls, ctl{1, "move-to-main-window", "Jump to message", func() { a.pipJump(v, m) }})
	}
	ctls = append(ctls, ctl{2, "close", "Close", func() { v.ClosePip(false) }})
	bs, gap := dp(36), dp(4) // Kit's medium IconButton (36px, radius 8, a 20px glyph)
	x := size.X - dp(12) - len(ctls)*bs - (len(ctls)-1)*gap
	p.ctlRects = p.ctlRects[:0]
	for _, c := range ctls {
		p.ctlRects = append(p.ctlRects, image.Rect(x, dp(12), x+bs, dp(12)+bs).Add(p.origin))
		b := &p.btns[c.i]
		if b.Clicked(gtx) {
			c.do()
		}
		o := op.Offset(image.Pt(x, dp(12))).Push(gtx.Ops)
		bg := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xd9} // darkButton__UuGW3
		ink := a.th.C.TooltipSub                              // c-icon_button--dark: content-inv-sec
		if b.Hovered() {
			bg.A, ink = 0xe6, a.th.C.TooltipText // :hover: content-inv-pry
		}
		dims := b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, bs, bs)
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, dp(8)).Op(gtx.Ops))
			io := op.Offset(image.Pt((bs-dp(20))/2, (bs-dp(20))/2)).Push(gtx.Ops)
			icons.Get(c.icon)(gtx, unit.Dp(20), ink)
			io.Pop()
			return layout.Dimensions{Size: r.Max}
		})
		pointer.CursorPointer.Add(gtx.Ops)
		popups.Tooltip(gtx, a.th, &p.tips[c.i], b.Hovered(), dims.Size, popups.Above, c.label, "")
		o.Pop()
		x += bs + gap
	}
}

// pipExpand is VideoPipContent's onFullscreen: the viewer at the mini
// player's time, playing if it played; the mini player closes.
func (a *App) pipExpand(v *videoplayer.Controller, m videoplayer.PipMedia) {
	at, _, playing := v.PipPlayback()
	msg, ok := a.pipMessage(m)
	v.ClosePip(false)
	if ok {
		a.OpenVideoViewer(m.File, msg, at, playing)
	}
}

// pipJump is usePipUndock's onUndock: the message (its thread for a
// reply), and the inline player playing on if this played.
func (a *App) pipJump(v *videoplayer.Controller, m videoplayer.PipMedia) {
	thread := ""
	if msg, ok := a.pipMessage(m); ok && msg.ThreadTS != "" && msg.ThreadTS != msg.TS {
		thread = msg.ThreadTS
	}
	v.ClosePip(true)
	a.jumpToMessage(m.Channel, m.TS, thread, false)
}

// DockVideo is popups.VideoDocker's: the media viewer's Dock to corner
// (the viewer has closed).
func (a *App) DockVideo(f *richtext.File, m core.Message, at time.Duration, playing, gallery bool) bool {
	v := a.videos()
	if !v.Available() || f == nil {
		return false
	}
	v.DockViewer(videoplayer.PipMedia{File: f, Channel: m.Channel, TS: m.TS, Gallery: gallery}, at, playing)
	return true
}

// pipMessage is the docked video's message from the store.
func (a *App) pipMessage(m videoplayer.PipMedia) (msg core.Message, ok bool) {
	c := a.Core()
	if c == nil {
		return msg, false
	}
	sm, ok := c.S.Find(m.Channel, m.TS)
	if !ok {
		return msg, false
	}
	return c.MessageFromSlack(m.Channel, &sm), true
}

// pipAnimate asks for a frame until t (the snap's transition), off the
// layout (no InvalidateCmd mid-layout).
func (a *App) pipAnimate(until time.Time) {
	if a.pipWake.After(time.Now()) {
		return
	}
	a.pipWake = time.Now().Add(16 * time.Millisecond)
	time.AfterFunc(16*time.Millisecond, func() {
		if time.Now().Before(until.Add(16 * time.Millisecond)) {
			a.Invalidate()
		}
	})
}

// pipEase is CSS's ease (cubic-bezier(.25, .1, .25, 1)) at t.
func pipEase(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	// solve x(s) = t by bisection, then y(s)
	bez := func(s, p1, p2 float64) float64 { u := 1 - s; return 3*u*u*s*p1 + 3*u*s*s*p2 + s*s*s }
	lo, hi := 0.0, 1.0
	for i := 0; i < 30; i++ {
		mid := (lo + hi) / 2
		if bez(mid, .25, .25) < t {
			lo = mid
		} else {
			hi = mid
		}
	}
	return bez((lo+hi)/2, .1, 1)
}

// pipShadow approximates box-shadow 0 dy blur spread c around r.
func pipShadow(ops *op.Ops, r image.Rectangle, radius, dy, blur, spread int, c color.NRGBA) {
	steps := max(blur, 1)
	base := r.Add(image.Pt(0, dy)).Inset(-spread)
	for i := steps; i >= 1; i-- {
		grow := i - steps/2 - 1
		col := c
		col.A = uint8(float32(c.A)/float32(steps) + .5)
		rr := base.Inset(-grow)
		if col.A == 0 || rr.Empty() {
			continue
		}
		paint.FillShape(ops, col, clip.UniformRRect(rr, max(0, radius+grow)).Op(ops))
	}
}

// pipDrawStretched draws img over r.
func pipDrawStretched(gtx layout.Context, img paint.ImageOp, r image.Rectangle) {
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 || r.Empty() {
		return
	}
	sx, sy := float32(r.Dx())/float32(is.X), float32(r.Dy())/float32(is.Y)
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx, sy)).Offset(layout.FPt(r.Min))).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func pipAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
