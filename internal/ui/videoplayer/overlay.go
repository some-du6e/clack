package videoplayer

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// VideoControlsOverlay for a lone video in a channel or thread (fIJO
// :578-950 with the --design_polish classes; css-overlay.txt): it covers
// the tile, and shows its controls while hovered.
//
//   - The overlay's button: the whole tile, whose click is tE.
//   - The controls container, 8px in from the bottom and the sides,
//     padding 4, radius 8, #1d1c1d at .7 while hovered: the scrubber (a
//     4px #ffffff80 track, 8px above and below it and 8px in at the
//     sides; the elapsed part #1d9bd1 with its 8px thumb at half size
//     until the scrubber is hovered), then the controls row (32px).
//   - The pill, first in the row: padding 4px 8px 4px 6px, radius 4,
//     #1d1c1d at .8 (none while hovered), play-filled, pause-bold or the
//     spinner (20px), then the time in 15px bold 8px after it; while it
//     plays it shows only while hovered (--playing).
//   - Mute and volume (lGYe), 4px after the pill while hovered: the
//     32px mute button (#1d1c1d at .85 under the pointer), sound-off,
//     sound-up (volume .6 or more), sound-medium (.3 or more) or
//     sound-down; its 70px slider shows while the pointer is on the
//     control, beside it (the popover: 40px from the button's left, 8px
//     down, a #ffffff80 track with the #1d9bd1 fill), or above it in a
//     #000000b3 box (vertical, players 330px wide or less, the scrubber
//     hidden meanwhile).
//   - The corner, top right while hovered: More actions (28px, a 1px
//     otl-inv-ter border, radius 4, #1d1c1d at .85, .9 hovered;
//     ellipsis-vertical-filled at 16px in content-inv-sec, content-inv-pry
//     hovered).
//
// Not drawn yet: playback speed, captions and the transcript (milestone
// 2; at 250px or less their gear menu), Expand (with the media viewer),
// Dock to corner (the mini player), and the controls' tooltips.

// zone is a part of the overlay a press can be on.
type zone uint8

const (
	zNone   zone = iota
	zTile        // the overlay's button and the pill: tE
	zScrub       // the scrubber
	zMute        // the mute button
	zSlider      // the volume slider
	zMore        // More actions
	zExpand      // Expand (FullscreenButton, QC+B)
	zDock        // Dock to corner (PipButton)
)

// zones are the overlay's parts as the last layout drew them (tile
// coordinates).
type zones struct {
	scrub, track         image.Rectangle
	mute, volume, slider image.Rectangle // volume: the control, its slider included, as :hover sees it
	vertical             bool
	more, expand, dock   image.Rectangle
	// pill and cont: the pill (its click is the overlay's), and the
	// controls container, whose empty parts take no click
	pill, cont image.Rectangle
}

func (z *zones) at(p image.Point, volOpen bool) zone {
	switch {
	case p.In(z.more):
		return zMore
	case p.In(z.expand):
		return zExpand
	case p.In(z.dock):
		return zDock
	case p.In(z.mute):
		return zMute
	case volOpen && p.In(z.slider):
		return zSlider
	case p.In(z.scrub):
		return zScrub
	case p.In(z.pill):
		return zTile // the pill's click is the overlay's (tE)
	case volOpen && z.vertical && p.In(z.volume), p.In(z.cont):
		// the controls container (and the vertical volume box) take
		// presses but have no click of their own (fIJO's container stops
		// mousedown; only its controls act)
		return zNone
	}
	return zTile
}

// timeAt is the scrubber's value at p (X7vT: the press's place along the
// track, of the duration).
func (z *zones) timeAt(p image.Point, d time.Duration) time.Duration {
	w := z.track.Dx()
	if w <= 0 || d <= 0 {
		return 0
	}
	f := min(max(float64(p.X-z.track.Min.X)/float64(w), 0), 1)
	return time.Duration(f * float64(d))
}

// volumeAt is the volume slider's value at p: rounded to hundredths
// (MediaVolumeSlider's Math.round(e)/100), from the bottom when vertical.
func (z *zones) volumeAt(p image.Point) float64 {
	var f float64
	if z.vertical {
		h := z.slider.Dy()
		if h <= 0 {
			return 0
		}
		f = 1 - float64(p.Y-z.slider.Min.Y)/float64(h)
	} else {
		w := z.slider.Dx()
		if w <= 0 {
			return 0
		}
		f = float64(p.X-z.slider.Min.X) / float64(w)
	}
	return math.Round(min(max(f, 0), 1)*100) / 100
}

// LayoutMedia is richview.Media's: the tile's player and overlay, in
// gtx.Constraints (exactly the tile).
func (r *Row) LayoutMedia(gtx layout.Context, s richview.MediaSlot) layout.Dimensions {
	if s.File != nil && s.File.IsAudio {
		return r.layoutAudio(gtx, s) // audio.go
	}
	c := r.c
	sz := gtx.Constraints.Max
	if s.File == nil || sz.X <= 0 || sz.Y <= 0 {
		return layout.Dimensions{Size: sz}
	}
	poster := s.Poster.Intersect(image.Rectangle{Max: sz})
	if poster.Empty() {
		poster = image.Rectangle{Max: sz}
	}
	c.mu.Lock()
	t := c.tileLocked(r.key(s.File), s.File)
	t.laid = c.frame
	if t.unmount != nil {
		t.unmount.Stop()
		t.unmount = nil
	}
	c.pendingResumeLocked(t) // a closed mini player's time, now it's laid out
	c.syncPrefsLocked()
	if ps := poster.Size(); ps != t.size {
		t.size = ps
		if t.h != nil {
			t.h.SetDrawSize(ps)
		}
	}
	c.applyLocked(t)
	c.mu.Unlock()
	// useScrollDockTrigger (YPBt): the share of the tile its scrollers
	// showed in the last frame, wherever its list is (Gio patch 53)
	if full, shown, ok := gtx.Source.AreaBounds(t); ok && full.Dx() > 0 && full.Dy() > 0 {
		r.MediaVisible(s.File, float64(shown.Dx()*shown.Dy())/float64(full.Dx()*full.Dy()))
	}
	r.input(gtx, t, s)
	more, expand := r.keyInput(gtx, t)
	r.draw(gtx, t, s, sz, poster)
	// the input area: the overlay's button covers the tile, above the
	// doc's own areas (richview skips presses on it: inSlot)
	area := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()
	r.focusAreas(gtx, t) // the controls' focus, over it
	if more || expand {
		r.keyAction(gtx, t, s, more)
	}
	return layout.Dimensions{Size: sz}
}

// keyAction is More actions' or Expand's key press: what their clicks do.
func (r *Row) keyAction(gtx layout.Context, t *tile, s richview.MediaSlot, more bool) {
	c := r.c
	c.mu.Lock()
	btn := t.z.more.Add(s.At)
	at, playing := t.elapsed, t.playing
	if !more && t.h != nil && t.playing {
		t.h.Pause()
	}
	c.mu.Unlock()
	if more {
		win := image.Rectangle{}
		if full, _, ok := gtx.Source.AreaBounds(t); ok {
			win = btn.Sub(s.At).Add(full.Min.Sub(c.frameOrigin()))
		}
		if mo, ok := r.h.(MenuOpener); ok {
			c.mu.Lock()
			t.menuOpen = true
			c.mu.Unlock()
			closed := func() {
				c.mu.Lock()
				t.menuOpen = false
				c.mu.Unlock()
				if c.env.Repaint != nil {
					c.env.Repaint()
				}
			}
			if !mo.OpenFileMenu(s.File, win, btn, closed) {
				closed()
			}
		} else if r.h != nil {
			r.h.FileAction(richview.FileMore, s.File, btn)
		}
		return
	}
	if ex, ok := r.h.(Expander); ok {
		ex.ExpandVideo(s.File, at, playing)
	}
}

// input takes the tile's pointer events (the last frame's zones).
func (r *Row) input(gtx layout.Context, t *tile, s richview.MediaSlot) {
	c := r.c
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: t, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Press | pointer.Release | pointer.Drag | pointer.Cancel})
		if !ok {
			return
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		p := image.Pt(int(e.Position.X), int(e.Position.Y))
		more, expand := false, false
		var at time.Duration
		var playing bool
		c.mu.Lock()
		switch e.Kind {
		case pointer.Enter, pointer.Move:
			t.hover, t.ptr = true, e.Position
			c.volHoverLocked(t, p)
		case pointer.Leave:
			t.hover = t.scrub || t.vol
			t.volOver = t.vol
			t.stopVolWait()
		case pointer.Cancel:
			c.endDragsLocked(t, p)
			t.hover, t.volOver, t.press = false, false, zNone
			t.stopVolWait()
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary || t.inPip { // isThisPlayerInPip: its click does nothing
				break
			}
			t.hover, t.ptr = true, e.Position
			switch z := t.z.at(p, t.volOver || t.volFocused()); z {
			case zScrub: // X7vT's mousedown: the time there at once
				t.scrub, t.resume = true, t.playing
				if t.h == nil {
					c.mountLocked(t, false) // tF: a scrub mounts the video, without autoplay
				} else if t.playing {
					t.h.Pause()
				}
				c.scrubToLocked(t, p)
			case zSlider: // X7vT's mousedown: onChange (the volume written) before onMouseDown (the drag)
				c.setVolumeLocked(t.z.volumeAt(p), false)
				t.vol = true
				c.applyLocked(t)
			default:
				t.press = z
			}
		case pointer.Drag:
			t.ptr = e.Position
			if t.scrub {
				c.scrubToLocked(t, p)
			}
			if t.vol {
				c.setVolumeLocked(t.z.volumeAt(p), true)
				c.applyLocked(t)
			}
		case pointer.Release:
			drag := t.scrub || t.vol
			c.endDragsLocked(t, p)
			if z := t.z.at(p, t.volOver || t.volFocused()); !drag && t.press != zNone && z == t.press {
				switch z {
				case zTile:
					c.clickLocked(t)
				case zMute:
					c.toggleMuteLocked()
					c.applyLocked(t)
				case zMore:
					more = true
				case zDock: // useManualPipButton: the mini player from here, playing if this played
					c.dockLocked(t, "manual", t.playing)
				case zExpand: // tR: pause, then the viewer at the time, playing if this played
					expand, at, playing = true, t.elapsed, t.playing
					if t.h != nil && t.playing {
						t.h.Pause()
					}
				}
			}
			t.press = zNone
		}
		c.idleLocked(t) // a scrub keeps it mounted; its end may leave it idle
		btn, win := t.z.more.Add(s.At), image.Rectangle{}
		c.mu.Unlock()
		if more && r.h != nil { // VideoActionsButton's menu
			if full, _, ok := gtx.Source.AreaBounds(t); ok {
				win = btn.Sub(s.At).Add(full.Min.Sub(c.frameOrigin()))
			}
			if mo, ok := r.h.(MenuOpener); ok {
				c.mu.Lock()
				t.menuOpen = true
				c.mu.Unlock()
				closed := func() {
					c.mu.Lock()
					t.menuOpen = false
					c.mu.Unlock()
					if c.env.Repaint != nil {
						c.env.Repaint()
					}
				}
				if !mo.OpenFileMenu(s.File, win, btn, closed) {
					closed()
				}
			} else {
				r.h.FileAction(richview.FileMore, s.File, btn)
			}
		}
		if ex, ok := r.h.(Expander); expand && ok {
			ex.ExpandVideo(s.File, at, playing)
		}
	}
}

// volDelay is the volume popover's transition-delay (.16s): its slider
// shows once the pointer has been on the mute button that long.
const volDelay = 160 * time.Millisecond

// volHoverLocked is the volume control's :hover (css-volume-pillow.txt).
// Its hidden slider takes no hover (visibility:hidden), so the slider
// opens from the mute button alone: at once in the vertical box, volDelay
// later in the popover. Once open, it stays open while the pointer is
// anywhere on the control.
func (c *Controller) volHoverLocked(t *tile, p image.Point) {
	switch {
	case t.vol:
		t.volOver = true
	case t.volOver:
		t.volOver = p.In(t.z.volume)
	case !p.In(t.z.mute):
	case t.z.vertical:
		t.volOver = true
	case t.volWait == nil:
		var tm *time.Timer
		tm = time.AfterFunc(volDelay, func() {
			c.mu.Lock()
			open := t.volWait == tm && t.hover && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.mute)
			if t.volWait == tm {
				t.volWait = nil
			}
			if open {
				t.volOver = true
			}
			c.mu.Unlock()
			if open && c.env.Repaint != nil {
				c.env.Repaint()
			}
		})
		t.volWait = tm
		return
	default:
		return
	}
	if t.volOver || !p.In(t.z.mute) {
		t.stopVolWait()
	}
}

// stopVolWait ends the popover's wait (the pointer left the mute).
func (t *tile) stopVolWait() {
	if t.volWait != nil {
		t.volWait.Stop()
		t.volWait = nil
	}
}

// scrubToLocked is the scrubber's change (tP): the shown time follows,
// and the player seeks there (mounting it if it isn't yet).
func (c *Controller) scrubToLocked(t *tile, p image.Point) {
	d := t.durLocked()
	if d <= 0 {
		return
	}
	v := t.z.timeAt(p, d)
	t.elapsed = v
	if t.h == nil {
		c.mountLocked(t, false)
	}
	if t.h != nil {
		t.h.Seek(v)
	}
}

// endDragsLocked ends a scrub (it plays on if it played, and the
// progress is kept: tO) or a volume drag (its mouseup writes the
// volume).
func (c *Controller) endDragsLocked(t *tile, p image.Point) {
	if t.scrub {
		t.scrub = false
		if t.resume && t.h != nil {
			t.h.Play()
		}
		t.resume = false
		if t.f != nil {
			c.reportLocked(t, true, t.elapsed.Milliseconds()) // tO: the scrub's end, forced
		}
	}
	if t.vol {
		t.vol = false
		c.setVolumeLocked(c.vol, false)
		c.applyLocked(t)
		t.volOver = p.In(t.z.volume)
	}
}

// Colours of the overlay (css-overlay.txt, css-volume-pillow.txt).
var (
	white      = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	pillBg     = color.NRGBA{R: 29, G: 28, B: 29, A: 204}    // #1d1c1dcc
	contBg     = color.NRGBA{R: 29, G: 28, B: 29, A: 179}    // #1d1c1db3
	btnHover   = color.NRGBA{R: 29, G: 28, B: 29, A: 217}    // #1d1c1dd9
	cornerHot  = color.NRGBA{R: 29, G: 28, B: 29, A: 230}    // #1d1c1de6
	trackCol   = color.NRGBA{R: 255, G: 255, B: 255, A: 128} // #ffffff80
	elapsedCol = color.NRGBA{R: 29, G: 155, B: 209, A: 255}  // #1d9bd1, sk_highlight_accent
	vertBg     = color.NRGBA{A: 179}                         // #000000b3
)

// draw paints the tile: the video once mounted (over the poster in the
// doc's recording), then the overlay.
func (r *Row) draw(gtx layout.Context, t *tile, s richview.MediaSlot, sz image.Point, poster image.Rectangle) {
	c := r.c
	dp := func(v float32) int { return gtx.Dp(unit.Dp(v)) }
	// the player's width in CSS px, for its size classes (fIJO, in gallery
	// contexts, which are all Clack's): x-small at 154 or less (the 64x28
	// pill, a 16px icon, 13px time; the 28px mute), no mute or volume at
	// 138 or less (Slack moves it into the settings gear, not drawn here)
	w := float32(sz.X) / max(gtx.Metric.PxPerDp, 0.01)
	xs, noVol := w <= 154, w <= 138
	c.mu.Lock()
	h := t.h
	still, stillOK := t.still, t.stillOK
	inPip, mini := t.inPip, t.mini
	// isThisPlayerInPip: the mini player shows this video, so here only
	// the pill (no controls, no corner)
	hot := (t.hover || t.scrub || t.vol || t.menuOpen || t.focus >= 0) && !inPip
	playing, loading, pillOn := t.playing, t.loading, t.pillShownLocked()
	label := pillTime(t, hot)
	iconPx := dp(20)
	if xs {
		iconPx = dp(16)
	}
	if loading && (t.spin == nil || len(t.spin.frames) == 0 || t.spin.frames[0].Size().X != iconPx) {
		t.spin = &spinLive{frames: spinImages(iconPx), t0: gtx.Now}
	}
	spin := t.spin
	elapsed, dur := t.elapsed, t.durLocked()
	volOver := t.volOver || t.vol || t.volFocused() // --keyboard_focus:focus-within opens it too
	scrubHot := t.scrub || hot && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.scrub)
	muteHot := hot && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.mute)
	sliderHot := t.vol || volOver && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.slider)
	ptrF := t.ptr
	muted, vol := c.muted, c.vol
	c.mu.Unlock()

	if h != nil || stillOK {
		ps := poster.Size()
		cl := clip.RRect{Rect: poster, NW: s.Radii[0], NE: s.Radii[1], SE: s.Radii[2], SW: s.Radii[3]}.Push(gtx.Ops)
		off := op.Offset(poster.Min).Push(gtx.Ops)
		// the frame a player let go of idle showed, until a new one's first
		if is := still.Size(); stillOK && is.X > 0 && is.Y > 0 {
			sc := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(ps.X)/float32(is.X), float32(ps.Y)/float32(is.Y)))).Push(gtx.Ops)
			still.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			sc.Pop()
		}
		if h != nil {
			h.Op(paint.ImageOp{}, ps).Add(gtx.Ops) // the poster underneath shows until a frame does
			paint.PaintOp{}.Add(gtx.Ops)
		}
		off.Pop()
		cl.Pop()
		// the tile's inset outline, which Slack draws over the video too
		if bw := s.BorderW; bw > 0 && s.Border.A > 0 {
			ring(gtx.Ops, image.Rectangle{Max: sz}, s.Radius, bw, s.Border)
		}
	}

	vertical := w <= 330
	ultra := w <= 180
	var z zones
	m8, p4 := dp(8), dp(4)
	scrubH, rowH := dp(20), dp(32)
	cont := image.Rect(m8, sz.Y-m8-p4-scrubH-rowH-p4, sz.X-m8, sz.Y-m8)
	if hot {
		paint.FillShape(gtx.Ops, contBg, clip.UniformRRect(cont, m8).Op(gtx.Ops))
	}
	z.scrub = image.Rect(cont.Min.X+p4+m8, cont.Min.Y+p4, cont.Max.X-p4-m8, cont.Min.Y+p4+scrubH)
	z.track = image.Rect(z.scrub.Min.X, z.scrub.Min.Y+m8, z.scrub.Max.X, z.scrub.Min.Y+m8+dp(4))
	row := image.Rect(cont.Min.X+p4, z.scrub.Max.Y, cont.Max.X-p4, z.scrub.Max.Y+rowH)

	// the pill
	iconS, textH, gap, tsp := iconPx, dp(22), dp(8), float32(15)
	if xs {
		gap, tsp = dp(4), 13
	}
	call, tsz := r.label(gtx, label, tsp)
	pw := dp(6) + iconS + gap + tsz.X + dp(8)
	ph := dp(4) + max(iconS, textH) + dp(4)
	if xs { // --size-x-small: 64x28
		pw, ph = dp(64), dp(28)
	}
	pill := image.Rect(row.Min.X, row.Min.Y+(rowH-ph)/2, row.Min.X+pw, row.Min.Y+(rowH-ph)/2+ph)

	// mute and volume: their place, drawn while hovered
	ml := p4
	if ultra {
		ml = 0
	}
	mb := rowH // the mute button: 32px, 28 at x-small
	if xs {
		mb = dp(28)
	}
	z.mute = image.Rect(pill.Max.X+ml, row.Min.Y+(rowH-mb)/2, pill.Max.X+ml+mb, row.Min.Y+(rowH-mb)/2+mb)
	z.vertical = vertical
	if vertical {
		box := image.Rect(z.mute.Min.X, z.mute.Max.Y-dp(118), z.mute.Max.X, z.mute.Max.Y)
		z.slider = image.Rect(box.Min.X, box.Min.Y+dp(12), box.Max.X, box.Min.Y+dp(12)+dp(70))
		z.volume = z.mute.Union(box)
	} else {
		z.slider = image.Rect(z.mute.Min.X+dp(40), z.mute.Min.Y+m8, z.mute.Min.X+dp(40)+dp(70), z.mute.Min.Y+m8+dp(16))
		z.volume = z.mute.Union(image.Rect(z.mute.Min.X, z.slider.Min.Y, z.slider.Max.X+m8, z.slider.Max.Y))
	}
	if noVol {
		z.mute, z.slider, z.volume = image.Rectangle{}, image.Rectangle{}, image.Rectangle{}
	}
	z.pill, z.cont = pill, cont

	// the scrubber, hidden while the vertical volume shows
	if hot && !(vertical && volOver) {
		tr := z.track
		paint.FillShape(gtx.Ops, trackCol, clip.UniformRRect(tr, dp(2)).Op(gtx.Ops))
		f := 0.0
		if dur > 0 {
			f = min(max(float64(elapsed)/float64(dur), 0), 1)
		}
		ex := tr.Min.X + int(float64(tr.Dx())*f+0.5)
		if ex > tr.Min.X {
			paint.FillShape(gtx.Ops, elapsedCol, clip.UniformRRect(image.Rect(tr.Min.X, tr.Min.Y, ex, tr.Max.Y), dp(2)).Op(gtx.Ops))
		}
		ts := dp(4) // the thumb at scale .5, 1 while the scrubber is hovered
		if scrubHot {
			ts = dp(8)
		}
		cx, cy := ex-dp(1), tr.Min.Y+dp(2) // :after right -3px, 8px wide: centred 1px left of the elapsed end
		paint.FillShape(gtx.Ops, elapsedCol, clip.Ellipse(image.Rect(cx-ts/2, cy-ts/2, cx-ts/2+ts, cy-ts/2+ts)).Op(gtx.Ops))
	}

	// the pill: it fades out while playing unless hovered (--playing);
	// a loading video isn't playing yet (Slack's isPlaying comes with its
	// play event), so the pill and its spinner stay
	if pillOn {
		if !hot {
			paint.FillShape(gtx.Ops, pillBg, clip.UniformRRect(pill, dp(4)).Op(gtx.Ops))
		}
		ic := image.Pt(pill.Min.X+dp(6), pill.Min.Y+(ph-iconS)/2)
		off := op.Offset(ic).Push(gtx.Ops)
		switch {
		case loading && spin != nil:
			im := paint.NewLiveImageOp(spin)
			im.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
		case playing:
			icons.Get("pause-bold")(gtx, unit.Dp(float32(iconS)/max(gtx.Metric.PxPerDp, 0.01)), white)
		default:
			icons.Get("play")(gtx, unit.Dp(float32(iconS)/max(gtx.Metric.PxPerDp, 0.01)), white) // play-filled
		}
		off.Pop()
		to := op.Offset(image.Pt(ic.X+iconS+gap, pill.Min.Y+(ph-tsz.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		to.Pop()
	}

	// mute and volume
	if hot && !noVol {
		shown := vol
		if muted {
			shown = 0
		}
		if vertical && volOver {
			box := image.Rect(z.mute.Min.X, z.mute.Max.Y-dp(118), z.mute.Max.X, z.mute.Max.Y)
			paint.FillShape(gtx.Ops, vertBg, clip.UniformRRect(box, dp(4)).Op(gtx.Ops))
			tr := image.Rect(z.slider.Min.X+(z.slider.Dx()-dp(4))/2, z.slider.Min.Y, z.slider.Min.X+(z.slider.Dx()-dp(4))/2+dp(4), z.slider.Max.Y)
			paint.FillShape(gtx.Ops, trackCol, clip.UniformRRect(tr, dp(2)).Op(gtx.Ops))
			fy := tr.Max.Y - int(float64(tr.Dy())*shown+0.5)
			if fy < tr.Max.Y {
				paint.FillShape(gtx.Ops, elapsedCol, clip.UniformRRect(image.Rect(tr.Min.X, fy, tr.Max.X, tr.Max.Y), dp(2)).Op(gtx.Ops))
			}
			if sliderHot {
				cx := tr.Min.X + tr.Dx()/2
				paint.FillShape(gtx.Ops, elapsedCol, clip.Ellipse(image.Rect(cx-dp(4), fy-dp(4), cx+dp(4), fy+dp(4))).Op(gtx.Ops))
			}
		} else if volOver {
			tr := image.Rect(z.slider.Min.X, z.slider.Min.Y+(z.slider.Dy()-dp(4))/2, z.slider.Max.X, z.slider.Min.Y+(z.slider.Dy()-dp(4))/2+dp(4))
			paint.FillShape(gtx.Ops, trackCol, clip.UniformRRect(tr, dp(2)).Op(gtx.Ops))
			fx := tr.Min.X + int(float64(tr.Dx())*shown+0.5)
			if fx > tr.Min.X {
				paint.FillShape(gtx.Ops, elapsedCol, clip.UniformRRect(image.Rect(tr.Min.X, tr.Min.Y, fx, tr.Max.Y), dp(2)).Op(gtx.Ops))
			}
			if sliderHot {
				cy := tr.Min.Y + tr.Dy()/2
				paint.FillShape(gtx.Ops, elapsedCol, clip.Ellipse(image.Rect(fx-dp(4), cy-dp(4), fx+dp(4), cy+dp(4))).Op(gtx.Ops))
			}
		}
		if muteHot && !(vertical && volOver) {
			paint.FillShape(gtx.Ops, btnHover, clip.UniformRRect(z.mute, dp(4)).Op(gtx.Ops))
		}
		name := "sound-down"
		switch {
		case muted:
			name = "mute" // sound-off
		case vol >= 0.6:
			name = "sound-up"
		case vol >= 0.3:
			name = "sound-medium"
		}
		mi := dp(20)
		off := op.Offset(image.Pt(z.mute.Min.X+(z.mute.Dx()-mi)/2, z.mute.Min.Y+(z.mute.Dy()-mi)/2)).Push(gtx.Ops)
		icons.Get(name)(gtx, unit.Dp(20), white)
		off.Pop()
	}

	// the corner, right to left: More actions, Dock to corner (PipButton:
	// clips_pip, a lone video wider than 154px), Expand; 2px apart
	// (gallery_v3's small buttons)
	bs := dp(28)
	if !mini && !inPip { // the mini player's own controls stand for the corner (PipControls)
		z.more = image.Rect(sz.X-m8-bs, m8, sz.X-m8, m8+bs)
		next := z.more
		if c.PipOn() && w > 154 {
			z.dock = next.Sub(image.Pt(bs+dp(2), 0))
			next = z.dock
		}
		if _, ok := r.h.(Expander); ok {
			z.expand = next.Sub(image.Pt(bs+dp(2), 0))
		}
	}
	ptr := image.Pt(int(ptrF.X), int(ptrF.Y))
	dark := false
	if th := r.c.theme(); th != nil {
		dark = th.Dark
	}
	for _, b := range []struct {
		r         image.Rectangle
		icon, tip string
	}{{z.expand, "expand", "Expand"}, {z.dock, "surface", "Dock to corner"}, {z.more, "kebab", "More actions"}} {
		if !hot || b.r.Empty() {
			continue
		}
		on := ptr.In(b.r)
		border, iconCol, iconHot := cornerColors(dark)
		bg := btnHover
		if on {
			bg, iconCol = cornerHot, iconHot
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(b.r, dp(4)).Op(gtx.Ops))
		ring(gtx.Ops, b.r, dp(4), max(1, dp(1)), border)
		is := dp(16)
		off := op.Offset(image.Pt(b.r.Min.X+(bs-is)/2, b.r.Min.Y+(bs-is)/2)).Push(gtx.Ops)
		icons.Get(b.icon)(gtx, unit.Dp(16), iconCol) // expand-diagonal, surface, ellipsis-vertical-filled
		off.Pop()
		c.tip(gtx, tipID{t.k, b.icon}, on, b.r, false, 0, i18n.T("stories_av", b.tip))
	}
	if hot && !z.mute.Empty() { // ToggleMute's Tooltip: top (bottom with the vertical volume), 60ms
		tip := "Mute"
		if muted {
			tip = "Unmute"
		}
		c.tip(gtx, tipID{t.k, "mute"}, ptr.In(z.mute) && !(vertical && volOver), z.mute, vertical, 60*time.Millisecond, i18n.T("stories_av", tip))
	}

	if inPip { // nothing here acts
		z = zones{}
	}
	c.mu.Lock()
	t.z = z
	c.mu.Unlock()
}

func (c *Controller) theme() *theme.Theme {
	if c.env.Theme == nil {
		return nil
	}
	return c.env.Theme()
}

// label records the pill's time in 15px bold white.
func (r *Row) label(gtx layout.Context, txt string, sp float32) (op.CallOp, image.Point) {
	th := r.c.theme()
	m := op.Record(gtx.Ops)
	var d layout.Dimensions
	if th != nil && txt != "" {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(1<<16, 1<<16)}
		d = th.Text(g, theme.LabelOpts{Font: font.Font{Weight: theme.Bold}, Size: unit.Sp(sp), MaxLines: 1, Color: white}, txt)
	}
	return m.Stop(), d.Size
}

// pillTime is the pill's time (fIJO's tp): a lone video's duration until
// it plays or was partly played, the time played after (and while
// hovered with no duration), "Video" with neither.
// pillShownLocked is whether the pill shows: fIJO's --playing rule hides
// it while the video plays and the overlay isn't active, and a loading
// tile doesn't play yet (isPlaying waits for the play event, tI).
func (t *tile) pillShownLocked() bool {
	return !t.playing || t.loading || t.hover || t.scrub || t.vol || t.menuOpen
}

func pillTime(t *tile, hover bool) string {
	d, u, m := t.durLocked(), t.elapsed, t.playing && !t.loading
	if d <= 0 || m || u > 0 {
		if u > 0 || m || hover {
			return clock(u)
		}
		return i18n.T("stories_av", "Video")
	}
	return clock(d)
}

// clock is client-boot w7xZ: m:ss, or h:mm:ss from an hour on.
func clock(d time.Duration) string {
	s := int64(d / time.Second)
	if s < 0 {
		s = 0
	}
	h, m, sec := s/3600, s%3600/60, s%60
	two := func(v int64) string {
		if v < 10 {
			return "0" + itoa(v)
		}
		return itoa(v)
	}
	if h != 0 {
		return itoa(h) + ":" + two(m) + ":" + two(sec)
	}
	return itoa(m) + ":" + two(sec)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// cornerColors are the gallery_v3 corner button's border (otl-inv-ter)
// and glyph (content-inv-sec; content-inv-pry hovered).
func cornerColors(dark bool) (border, icon, iconHot color.NRGBA) {
	if dark {
		return color.NRGBA{R: 121, G: 124, B: 129, A: 77}, color.NRGBA{R: 185, G: 186, B: 189, A: 255}, color.NRGBA{R: 248, G: 248, B: 248, A: 255}
	}
	return color.NRGBA{R: 248, G: 248, B: 248, A: 51}, color.NRGBA{R: 203, G: 204, B: 207, A: 255}, white
}

// ring strokes r's inset outline, bw wide, radius rad.
func ring(ops *op.Ops, r image.Rectangle, rad, bw int, col color.NRGBA) {
	if bw <= 0 || r.Empty() {
		return
	}
	half := float32(bw) / 2
	var p clip.Path
	p.Begin(ops)
	x0, y0, x1, y1 := float32(r.Min.X)+half, float32(r.Min.Y)+half, float32(r.Max.X)-half, float32(r.Max.Y)-half
	rd := max(float32(rad)-half, 0)
	p.MoveTo(f32.Pt(x0+rd, y0))
	p.LineTo(f32.Pt(x1-rd, y0))
	p.QuadTo(f32.Pt(x1, y0), f32.Pt(x1, y0+rd))
	p.LineTo(f32.Pt(x1, y1-rd))
	p.QuadTo(f32.Pt(x1, y1), f32.Pt(x1-rd, y1))
	p.LineTo(f32.Pt(x0+rd, y1))
	p.QuadTo(f32.Pt(x0, y1), f32.Pt(x0, y1-rd))
	p.LineTo(f32.Pt(x0, y0+rd))
	p.QuadTo(f32.Pt(x0, y0), f32.Pt(x0+rd, y0))
	p.Close()
	paint.FillShape(ops, col, clip.Stroke{Path: p.End(), Width: float32(bw)}.Op())
}

// cubicBezierY is CSS cubic-bezier(x1, y1, x2, y2) at time x.
func cubicBezierY(x1, y1, x2, y2, x float64) float64 {
	b := func(p1, p2, t float64) float64 { mt := 1 - t; return 3*mt*mt*t*p1 + 3*mt*t*t*p2 + t*t*t }
	db := func(p1, p2, t float64) float64 { mt := 1 - t; return 3*mt*mt*p1 + 6*mt*t*(p2-p1) + 3*t*t*(1-p2) }
	t := x
	for range 8 {
		e := b(x1, x2, t) - x
		if math.Abs(e) < 1e-6 {
			break
		}
		d := db(x1, x2, t)
		if d == 0 {
			break
		}
		t = min(max(t-e/d, 0), 1)
	}
	return b(y1, y2, t)
}
