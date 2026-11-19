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
)

// zones are the overlay's parts as the last layout drew them (tile
// coordinates).
type zones struct {
	scrub, track         image.Rectangle
	mute, volume, slider image.Rectangle // volume: the control, its slider included, as :hover sees it
	vertical             bool
	more                 image.Rectangle
}

func (z *zones) at(p image.Point, volOpen bool) zone {
	switch {
	case p.In(z.more):
		return zMore
	case p.In(z.mute):
		return zMute
	case volOpen && p.In(z.slider):
		return zSlider
	case p.In(z.scrub):
		return zScrub
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
	c.syncPrefsLocked()
	if ps := poster.Size(); ps != t.size {
		t.size = ps
		if t.h != nil {
			t.h.SetDrawSize(ps)
		}
	}
	c.applyLocked(t)
	c.mu.Unlock()
	r.input(gtx, t, s)
	r.draw(gtx, t, s, sz, poster)
	// the input area: the overlay's button covers the tile, above the
	// doc's own areas (richview skips presses on it: inSlot)
	area := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()
	return layout.Dimensions{Size: sz}
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
		more := false
		c.mu.Lock()
		switch e.Kind {
		case pointer.Enter, pointer.Move:
			t.hover, t.ptr = true, e.Position
			t.volOver = t.vol || p.In(t.z.volume)
		case pointer.Leave:
			t.hover = t.scrub || t.vol
			t.volOver = t.vol
		case pointer.Cancel:
			c.endDragsLocked(t, p)
			t.hover, t.volOver, t.press = false, false, zNone
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary {
				break
			}
			t.hover, t.ptr = true, e.Position
			switch z := t.z.at(p, t.volOver); z {
			case zScrub: // X7vT's mousedown: the time there at once
				t.scrub, t.resume = true, t.playing
				if t.h == nil {
					c.mountLocked(t, false) // tF: a scrub mounts the video, without autoplay
				} else if t.playing {
					t.h.Pause()
				}
				c.scrubToLocked(t, p)
			case zSlider:
				t.vol = true
				c.setVolumeLocked(t.z.volumeAt(p), true)
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
			if z := t.z.at(p, t.volOver); !drag && t.press != zNone && z == t.press {
				switch z {
				case zTile:
					c.clickLocked(t)
				case zMute:
					c.toggleMuteLocked()
					c.applyLocked(t)
				case zMore:
					more = true
				}
			}
			t.press = zNone
		}
		btn := t.z.more.Add(s.At)
		c.mu.Unlock()
		if more && r.h != nil {
			r.h.FileAction(richview.FileMore, s.File, btn) // VideoActionsButton's menu
		}
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
			c.progress[t.f.ID] = t.elapsed
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
	c.mu.Lock()
	h := t.h
	hot := t.hover || t.scrub || t.vol
	playing, loading := t.playing, t.loading
	label := pillTime(t, hot)
	elapsed, dur := t.elapsed, t.durLocked()
	volOver := t.volOver || t.vol
	scrubHot := t.scrub || hot && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.scrub)
	muteHot := hot && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.mute)
	sliderHot := t.vol || volOver && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.slider)
	moreHot := hot && image.Pt(int(t.ptr.X), int(t.ptr.Y)).In(t.z.more)
	muted, vol := c.muted, c.vol
	c.mu.Unlock()

	if h != nil {
		ps := poster.Size()
		im := h.Op(paint.ImageOp{}, ps) // the poster underneath shows until a frame does
		cl := clip.RRect{Rect: poster, NW: s.Radii[0], NE: s.Radii[1], SE: s.Radii[2], SW: s.Radii[3]}.Push(gtx.Ops)
		off := op.Offset(poster.Min).Push(gtx.Ops)
		im.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		off.Pop()
		cl.Pop()
		// the tile's inset outline, which Slack draws over the video too
		if bw := s.BorderW; bw > 0 && s.Border.A > 0 {
			ring(gtx.Ops, image.Rectangle{Max: sz}, s.Radius, bw, s.Border)
		}
	}

	w := float32(sz.X) / max(gtx.Metric.PxPerDp, 0.01) // the player's width in CSS px (its breakpoints)
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
	iconS, textH := dp(20), dp(22)
	call, tsz := r.label(gtx, label)
	pw := dp(6) + iconS + dp(8) + tsz.X + dp(8)
	ph := dp(4) + max(iconS, textH) + dp(4)
	pill := image.Rect(row.Min.X, row.Min.Y+(rowH-ph)/2, row.Min.X+pw, row.Min.Y+(rowH-ph)/2+ph)

	// mute and volume: their place, drawn while hovered
	ml := p4
	if ultra {
		ml = 0
	}
	z.mute = image.Rect(pill.Max.X+ml, row.Min.Y, pill.Max.X+ml+rowH, row.Max.Y)
	z.vertical = vertical
	if vertical {
		box := image.Rect(z.mute.Min.X, z.mute.Max.Y-dp(118), z.mute.Max.X, z.mute.Max.Y)
		z.slider = image.Rect(box.Min.X, box.Min.Y+dp(12), box.Max.X, box.Min.Y+dp(12)+dp(70))
		z.volume = z.mute.Union(box)
	} else {
		z.slider = image.Rect(z.mute.Min.X+dp(40), z.mute.Min.Y+m8, z.mute.Min.X+dp(40)+dp(70), z.mute.Min.Y+m8+dp(16))
		z.volume = z.mute.Union(image.Rect(z.mute.Min.X, z.slider.Min.Y, z.slider.Max.X+m8, z.slider.Max.Y))
	}

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
		cx, cy := ex+dp(1), tr.Min.Y+dp(2)
		paint.FillShape(gtx.Ops, elapsedCol, clip.Ellipse(image.Rect(cx-ts/2, cy-ts/2, cx-ts/2+ts, cy-ts/2+ts)).Op(gtx.Ops))
	}

	// the pill: it fades out while playing unless hovered (--playing)
	if !playing || hot {
		if !hot {
			paint.FillShape(gtx.Ops, pillBg, clip.UniformRRect(pill, dp(4)).Op(gtx.Ops))
		}
		ic := image.Pt(pill.Min.X+dp(6), pill.Min.Y+(ph-iconS)/2)
		off := op.Offset(ic).Push(gtx.Ops)
		switch {
		case loading:
			spinner(gtx, iconS)
		case playing:
			icons.Get("pause-bold")(gtx, unit.Dp(20), white)
		default:
			icons.Get("play")(gtx, unit.Dp(20), white) // play-filled
		}
		off.Pop()
		to := op.Offset(image.Pt(ic.X+iconS+dp(8), pill.Min.Y+(ph-tsz.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		to.Pop()
	}

	// mute and volume
	if hot {
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
		off := op.Offset(image.Pt(z.mute.Min.X+(z.mute.Dx()-iconS)/2, z.mute.Min.Y+(z.mute.Dy()-iconS)/2)).Push(gtx.Ops)
		icons.Get(name)(gtx, unit.Dp(20), white)
		off.Pop()
	}

	// the corner: More actions
	bs := dp(28)
	z.more = image.Rect(sz.X-m8-bs, m8, sz.X-m8, m8+bs)
	if hot {
		dark := false
		if th := r.c.theme(); th != nil {
			dark = th.Dark
		}
		border, iconCol, iconHot := cornerColors(dark)
		bg := btnHover
		if moreHot {
			bg, iconCol = cornerHot, iconHot
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(z.more, dp(4)).Op(gtx.Ops))
		ring(gtx.Ops, z.more, dp(4), max(1, dp(1)), border)
		is := dp(16)
		off := op.Offset(image.Pt(z.more.Min.X+(bs-is)/2, z.more.Min.Y+(bs-is)/2)).Push(gtx.Ops)
		icons.Get("kebab")(gtx, unit.Dp(16), iconCol) // ellipsis-vertical-filled
		off.Pop()
	}

	c.mu.Lock()
	t.z = z
	c.mu.Unlock()
	if loading && (!playing || hot) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)}) // the spinner turns
	}
}

func (c *Controller) theme() *theme.Theme {
	if c.env.Theme == nil {
		return nil
	}
	return c.env.Theme()
}

// label records the pill's time in 15px bold white.
func (r *Row) label(gtx layout.Context, txt string) (op.CallOp, image.Point) {
	th := r.c.theme()
	m := op.Record(gtx.Ops)
	var d layout.Dimensions
	if th != nil && txt != "" {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(1<<16, 1<<16)}
		d = th.Text(g, theme.LabelOpts{Font: font.Font{Weight: theme.Bold}, Size: unit.Sp(15), MaxLines: 1, Color: white}, txt)
	}
	return m.Stop(), d.Size
}

// pillTime is the pill's time (fIJO's tp): a lone video's duration until
// it plays or was partly played, the time played after (and while
// hovered with no duration), "Video" with neither.
func pillTime(t *tile, hover bool) string {
	d, u, m := t.durLocked(), t.elapsed, t.playing
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

// spinner is the pill's (s6So, medium, white, noMargin): Slack Kit's
// InfiniteSpinner in a d box on a 78-unit viewBox, the track circle (r
// 35, stroke 8) at .25 and the arc's two quarter turns, each once per
// 1.2 s on its own easing.
func spinner(gtx layout.Context, d int) {
	box := float32(78)
	sw := float32(d) * 8 / box
	rr := float32(d) * 35 / box
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	track := white
	track.A = 64
	arc := func(a0 float64, col color.NRGBA, sweep float64) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+rr*float32(math.Cos(a0)), ctr.Y+rr*float32(math.Sin(a0))))
		p.ArcTo(ctr, ctr, float32(sweep))
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: sw}.Op())
	}
	arc(0, track, math.Pi)
	arc(math.Pi, track, math.Pi)
	q := 1.5 * math.Pi
	arc(q+2*math.Pi*cubicBezierY(.25, .29, .54, .86, t), white, math.Pi/2)
	arc(q+2*math.Pi*cubicBezierY(.41, .24, .64, .69, t), white, math.Pi/2)
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
