package videoplayer

import (
	"image"
	"math"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
)

// The media viewer's video: Slack's MediaViewerContentVideo (Ivom:20-66,
// build 2c0d02ca130f) under the viewer's MediaVideoCoordinatorContext
// (2CjY). The viewer makes one player for the video item it shows, with
// autoplay on unless the caller said otherwise (Expand passes whether the
// inline video was playing) and from the caller's time, else from where
// the viewer left the file (2CjY: initialTimeElapsed || xnwb's start).
// It's one of the controller's players, so one plays at a time with the
// inline ones (bxCZ registers the viewer's too), and sign-out closes it.
// It closes when the viewer moves to another item or closes, as the
// item's <video> unmounts; the local progress keeps where it was. It
// never loops (looping_video_option is the inline player's, fIJO:957).

// Viewer is the player of the video item the media viewer shows.
type Viewer struct {
	c *Controller
	k key
}

// OpenViewer makes the viewer's player of f, nil where videos don't play
// in place (no backend: the viewer shows the poster).
func (c *Controller) OpenViewer(f *richtext.File, start time.Duration, autoplay bool) *Viewer {
	if !c.Available() || f == nil || !f.IsVideo || f.ID == "" || f.URL == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.viewers++
	k := key{pane: "viewer", ts: strconv.FormatUint(c.viewers, 10), file: f.ID}
	t := c.tileLocked(k, f)
	t.viewer, t.laid = true, c.frame
	if start > 0 {
		t.startAt = start
	}
	c.mountLocked(t, autoplay)
	return &Viewer{c: c, k: k}
}

// Close closes the player (the item's <video> unmounting).
func (v *Viewer) Close() {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.tiles[v.k]; t != nil {
		c.closeLocked(t)
		delete(c.tiles, v.k)
	}
}

// tileLocked is the viewer's tile, nil once closed.
func (v *Viewer) tileLocked() *tile {
	if v == nil {
		return nil
	}
	return v.c.tiles[v.k]
}

// LayoutVideo draws the video's frame in area as the viewer's <video>
// shows it (.p-media_viewer__video: width and height 100%, so the frame
// is contained in the area, scaled up or down; nothing drawn until the
// first frame, so the caller's poster shows), and returns where. It
// keeps the player laid out (EndFrame) and tells it the drawn size.
func (v *Viewer) LayoutVideo(gtx layout.Context, area image.Rectangle) image.Rectangle {
	if v == nil {
		return image.Rectangle{}
	}
	c := v.c
	c.mu.Lock()
	t := v.tileLocked()
	if t == nil {
		c.mu.Unlock()
		return image.Rectangle{}
	}
	t.laid = c.frame
	if t.unmount != nil {
		t.unmount.Stop()
		t.unmount = nil
	}
	c.syncPrefsLocked()
	c.applyLocked(t)
	h := t.h
	nat := image.Point{}
	if h != nil {
		nat = h.State().Size
	}
	if (nat.X <= 0 || nat.Y <= 0) && t.f != nil {
		nat = image.Pt(t.f.OrigW, t.f.OrigH) // the file's size until the player knows the video's
	}
	r := Contain(area, nat)
	if sz := r.Size(); sz != t.size && sz.X > 0 && sz.Y > 0 {
		t.size = sz
		if h != nil {
			h.SetDrawSize(sz)
		}
	}
	c.mu.Unlock()
	if h == nil || r.Empty() {
		return r
	}
	im := h.Op(paint.ImageOp{}, r.Size())
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: r.Size()}.Push(gtx.Ops).Pop()
	im.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return r
}

// Contain is a box of size nat contained in area, centered (object-fit
// contain), empty when either is.
func Contain(area image.Rectangle, nat image.Point) image.Rectangle {
	if area.Empty() || nat.X <= 0 || nat.Y <= 0 {
		return image.Rectangle{}
	}
	s := min(float64(area.Dx())/float64(nat.X), float64(area.Dy())/float64(nat.Y))
	w, h := int(math.Round(float64(nat.X)*s)), int(math.Round(float64(nat.Y)*s))
	x, y := area.Min.X+(area.Dx()-w)/2, area.Min.Y+(area.Dy()-h)/2
	return image.Rect(x, y, x+w, y+h)
}

// ViewerState is the viewer's player as its footer draws it: 2CjY's
// elapsed, duration (0 until the metadata, which the clock shows as
// 0:00) and isPlaying.
type ViewerState struct {
	Mounted, Playing, Loading, Ended bool
	Err                              int
	Elapsed, Duration                time.Duration
	// Size is the video's own size, once known.
	Size image.Point
	// Muted and Volume are ToggleMute's (the media_muted and media_volume
	// prefs, as the inline players have them).
	Muted  bool
	Volume float64
	// Player is the player, for tests (nil once closed).
	Player media.Player
}

// State is the player now.
func (v *Viewer) State() ViewerState {
	if v == nil {
		return ViewerState{}
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	t := v.tileLocked()
	if t == nil {
		return ViewerState{Muted: c.muted, Volume: c.vol}
	}
	st := ViewerState{Mounted: t.h != nil, Playing: t.playing, Loading: t.loading, Ended: t.ended, Err: t.errCode,
		Elapsed: t.elapsed, Duration: t.dur, Muted: c.muted, Volume: c.vol}
	if t.h != nil {
		st.Player = t.h
		st.Size = t.h.State().Size
	}
	return st
}

// TogglePlay is TogglePlayButton's click and Space (VG1W, DBSG):
// videoRef's togglePlayPause.
func (v *Viewer) TogglePlay() {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	t := v.tileLocked()
	if t == nil {
		return
	}
	if t.h == nil {
		c.mountLocked(t, true)
		return
	}
	if st := t.h.State(); st.Paused || st.Ended {
		t.h.Play()
	} else {
		t.h.Pause()
	}
}

// Play and Pause are the player's (MediaProgressBar's scrub: paused while
// pressed, played on after if it played before).
func (v *Viewer) Play()  { v.do(func(h *media.Handle) { h.Play() }) }
func (v *Viewer) Pause() { v.do(func(h *media.Handle) { h.Pause() }) }

// Seek is the progress bar's change (setCurrentTime).
func (v *Viewer) Seek(d time.Duration) {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := v.tileLocked(); t != nil && t.h != nil {
		t.h.Seek(max(d, 0))
		t.elapsed, t.ended = max(d, 0), false
	}
}

func (v *Viewer) do(f func(h *media.Handle)) {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := v.tileLocked(); t != nil && t.h != nil {
		f(t.h)
	}
}

// ToggleMute is ToggleMute's click and m (lGYe; DBSG writes media_muted
// the same way).
func (v *Viewer) ToggleMute() {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toggleMuteLocked()
	if t := v.tileLocked(); t != nil {
		c.applyLocked(t)
	}
}

// SetVolume is the volume slider's change (lGYe's j then T): while
// dragging the pref waits for the press to end.
func (v *Viewer) SetVolume(vol float64, dragging bool) {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	t := v.tileLocked()
	if t != nil {
		t.vol = dragging
	}
	c.setVolumeLocked(vol, dragging)
	if t != nil {
		c.applyLocked(t)
	}
}

// SetShown tells the player whether the viewer's footer shows: while it
// does, each time update repaints (its clock and progress bar).
func (v *Viewer) SetShown(on bool) {
	if v == nil {
		return
	}
	c := v.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := v.tileLocked(); t != nil {
		t.shown = on
	}
}

// ViewerClock is the viewer's PlaybackTimestamp half (v8k6, g8Kq's
// formatTime): minutes and two-digit seconds, the minutes past 59 as
// they are, "0:00" for an unknown time.
func ViewerClock(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	s := int64(d / time.Second)
	sec := s % 60
	out := strconv.FormatInt(s/60, 10) + ":"
	if sec < 10 {
		out += "0"
	}
	return out + strconv.FormatInt(sec, 10)
}
