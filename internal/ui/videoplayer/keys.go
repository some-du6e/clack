package videoplayer

import (
	"image"
	"math"
	"time"

	"gioui.org/io/event"
	gkey "gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// The controls' keyboard (round 4 step 3). Each control the overlay draws
// takes the focus (Kit's buttons; X7vT's track, tabIndex 0), so Tab
// reaches them. While one has it the overlay stays active (fIJO's
// onFocus), the control draws the focus ring, and it takes its keys:
// Space and Enter press a button; the scrubber and the volume take
// X7vT's (→ ↑ a tick, ← ↓ one back, PageUp and PageDown a jump, Home the
// start, End the end; with Shift a tenth of a tick or a jump). The
// scrubber's tick is a second and its jump a tenth of the duration
// (VideoControlsScrubber passes neither); the volume's are 1 and 10 of
// MediaVolumeSlider's 0..100.
const (
	fPill = iota
	fScrub
	fMute
	fSlider
	fExpand
	fDock
	fMore
	fN
)

// focusRect is control i's place as the last layout drew it.
func (z *zones) focusRect(i int, volOpen bool) image.Rectangle {
	switch i {
	case fPill:
		return z.pill
	case fScrub:
		return z.scrub
	case fMute:
		return z.mute
	case fSlider:
		if volOpen {
			return z.slider
		}
	case fExpand:
		return z.expand
	case fDock:
		return z.dock
	case fMore:
		return z.more
	}
	return image.Rectangle{}
}

// focusAreas registers the controls' focus tags over their places: key
// focus only, the pointer passing on to the overlay's button.
func (r *Row) focusAreas(gtx layout.Context, t *tile) {
	c := r.c
	c.mu.Lock()
	z, volOpen, focus := t.z, t.volOver || t.vol || t.volFocused(), t.focus
	c.mu.Unlock()
	pass := pointer.PassOp{}.Push(gtx.Ops)
	for i := 0; i < fN; i++ {
		fr := z.focusRect(i, volOpen)
		if fr.Empty() {
			continue
		}
		a := clip.Rect(fr).Push(gtx.Ops)
		event.Op(gtx.Ops, &t.ftags[i])
		a.Pop()
		if focus == i && c.env.FocusRing != nil {
			rad := gtx.Dp(4)
			if i == fScrub || i == fSlider {
				rad = gtx.Dp(2)
			}
			c.env.FocusRing(gtx, fr, rad)
		}
	}
	pass.Pop()
}

var sliderKeys = []gkey.Name{gkey.NameRightArrow, gkey.NameUpArrow, gkey.NameLeftArrow, gkey.NameDownArrow, gkey.NamePageUp, gkey.NamePageDown, gkey.NameHome, gkey.NameEnd}

// keyInput reads the controls' focus and keys; it reports what it pressed
// that the caller acts on outside the lock (More actions, Expand).
func (r *Row) keyInput(gtx layout.Context, t *tile) (more, expand bool) {
	c := r.c
	for i := 0; i < fN; i++ {
		filters := []event.Filter{gkey.FocusFilter{Target: &t.ftags[i]}}
		if i == fScrub || i == fSlider {
			for _, k := range sliderKeys {
				filters = append(filters, gkey.Filter{Focus: &t.ftags[i], Name: k, Optional: gkey.ModShift})
			}
		} else {
			filters = append(filters, gkey.Filter{Focus: &t.ftags[i], Name: gkey.NameSpace}, gkey.Filter{Focus: &t.ftags[i], Name: gkey.NameReturn})
		}
		for {
			ev, ok := gtx.Event(filters...)
			if !ok {
				break
			}
			c.mu.Lock()
			switch e := ev.(type) {
			case gkey.FocusEvent:
				switch {
				case e.Focus:
					t.focus = i
				case t.focus == i:
					t.focus = -1
				}
			case gkey.Event:
				if e.State != gkey.Press || t.inPip {
					break
				}
				switch i {
				case fScrub:
					c.keySeekLocked(t, e)
				case fSlider:
					c.keyVolumeLocked(t, e)
				case fPill:
					c.clickLocked(t)
				case fMute:
					c.toggleMuteLocked()
					c.applyLocked(t)
				case fDock:
					c.dockLocked(t, "manual", t.playing)
				case fMore:
					more = true
				case fExpand:
					expand = true
				}
			}
			c.mu.Unlock()
		}
	}
	return more, expand
}

// sliderStep is X7vT's change for key k on a value in [0, max]: tick and
// jump (a tenth of each with Shift), Home and End to the ends; ok false
// for another key.
func sliderStep(k gkey.Name, shift bool, v, max, tick, jump float64) (float64, bool) {
	if shift {
		tick, jump = tick/10, jump/10
	}
	switch k {
	case gkey.NameRightArrow, gkey.NameUpArrow:
		v += tick
	case gkey.NameLeftArrow, gkey.NameDownArrow:
		v -= tick
	case gkey.NamePageUp:
		v += jump
	case gkey.NamePageDown:
		v -= jump
	case gkey.NameHome:
		v = 0
	case gkey.NameEnd:
		v = max
	default:
		return v, false
	}
	return math.Min(math.Max(v, 0), max), true
}

// keySeekLocked is the scrubber's key: its onChange (tP), the time there
// (mounting the player without playing it first, as a scrub does).
func (c *Controller) keySeekLocked(t *tile, e gkey.Event) {
	d := t.durLocked()
	if d <= 0 {
		return
	}
	v, ok := sliderStep(e.Name, e.Modifiers.Contain(gkey.ModShift), t.elapsed.Seconds(), d.Seconds(), 1, 0.1*d.Seconds())
	if !ok {
		return
	}
	at := time.Duration(v * float64(time.Second))
	t.elapsed, t.played = at, true
	if t.h == nil {
		t.startAt = at
		c.mountLocked(t, false)
		return
	}
	t.h.Seek(at)
}

// keyVolumeLocked is the volume slider's key: ToggleMute's change (j,
// then T) on MediaVolumeSlider's 0..100.
func (c *Controller) keyVolumeLocked(t *tile, e gkey.Event) {
	cur := c.vol * 100
	if c.muted {
		cur = 0
	}
	v, ok := sliderStep(e.Name, e.Modifiers.Contain(gkey.ModShift), cur, 100, 1, 10)
	if !ok {
		return
	}
	c.setVolumeLocked(math.Round(v)/100, false)
	c.applyLocked(t)
}

// volFocused is whether the keyboard focus is on the volume control (the
// mute or its slider): its slider shows (p-media_volume_slider--
// keyboard_focus:focus-within).
func (t *tile) volFocused() bool { return t.focus == fMute || t.focus == fSlider }
