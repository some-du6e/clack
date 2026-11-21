package videoplayer

import (
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/richtext"
)

// The mini player: Slack's MediaPlayerPipContainer and VideoPipContent
// (yF7D), with their triggers, YPBt's usePipController (build
// 2c0d02ca130f). One video at a time docks into the window's corner,
// where a player of its own plays it on from its time (openPip mounts a
// new VideoPlayer in the mini player's context, autoplaying):
//   - with clips_pip on and clips_pip_auto_dock (?? true): a playing video
//     whose tile crosses under 10% visible (useScrollDockTrigger: an
//     IntersectionObserver at threshold .1, whose first callback is
//     skipped), or whose message goes while it plays
//     (useNavigateDockTrigger: React unmounting its row);
//   - the media viewer's Dock to corner (MediaViewerPipButton, any
//     playable video: trigger "manual").
//
// The inline tile pauses (onOpen) and shows only its pill while the mini
// player shows its video (isThisPlayerInPip). Closing the mini player
// gives the inline player its time (useResumeAfterPipClose, with
// clips_pip on), playing on only after Jump to message from a playing
// video that can play inline; an inline player not laid out then gets it
// when it is. Without clips_pip no tile docks, and a playing video
// scrolled away plays on (YPBt isn't active), as it does with
// clips_pip_auto_dock off.

// PipMedia is the docked video's message.
type PipMedia struct {
	File        *richtext.File
	Channel, TS string
	// Gallery is a gallery's video (docked from the viewer): it can't
	// play inline (cannotPlayInline), so Jump to message doesn't resume it
	Gallery bool
}

type pipState struct {
	k       key // the mini player's tile (pane "pip")
	row     *Row
	media   PipMedia
	trigger string // "scroll", "navigate" or "manual"
}

// pipResume is a closed mini player's time, for the inline player of its
// message when it's laid out next.
type pipResume struct {
	ch, ts, file string
	at           time.Duration
	play         bool
}

// PipOn is clips_pip (W4bO's ZP): inline videos dock, and the mini player
// moves, resizes and jumps to its message.
func (c *Controller) PipOn() bool {
	return c.env.Experiment != nil && c.env.Experiment("clips_pip") == "on"
}

// autoDockLocked is clips_pip_auto_dock: on unless the pref is false.
func (c *Controller) autoDockLocked() bool {
	if c.env.Pref == nil {
		return true
	}
	raw, ok := c.env.Pref("clips_pip_auto_dock")
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return true
	}
	return prefBool(raw)
}

// docksLocked is whether t's video docks on scroll or navigation.
func (c *Controller) docksLocked(t *tile) bool {
	return t.h != nil && t.playing && t.inline() && !t.inPip && c.PipOn() && c.autoDockLocked()
}

// dockLocked docks t's video (openPip from an inline player), playing
// there if autoplay (the scroll and navigate triggers always: isPlaying
// !0; the manual one if t played).
func (c *Controller) dockLocked(t *tile, trigger string, autoplay bool) {
	if t.f == nil {
		return
	}
	if t.h != nil && t.playing {
		t.h.Pause() // onOpen
	}
	c.openPipLocked(PipMedia{File: t.f, Channel: t.k.ch, TS: t.k.ts}, t.elapsed, autoplay, trigger)
}

// openPipLocked shows m in the mini player, a new player from at, playing
// unless !autoplay; the video it showed, if any, makes way.
func (c *Controller) openPipLocked(m PipMedia, at time.Duration, autoplay bool, trigger string) {
	c.closePipLocked(false)
	k := key{pane: "pip", ch: m.Channel, ts: m.TS, file: m.File.ID}
	t := c.tileLocked(k, m.File)
	t.mini, t.laid = true, c.frame
	t.startAt = at
	t.elapsed, t.played = at, at > 0
	c.mountLocked(t, autoplay)
	c.pip = &pipState{k: k, row: &Row{c: c, pane: "pip", ch: m.Channel, ts: m.TS}, media: m, trigger: trigger}
	for _, o := range c.tiles {
		if o.inlineOf(m) {
			o.inPip = true
		}
	}
}

// inlineOf is whether t is an inline tile of m's video (a list's: the
// channel, a thread pane, a view; not the mini player's or the viewer's).
func (t *tile) inlineOf(m PipMedia) bool {
	return t.inline() && t.f != nil && m.File != nil && t.f.ID == m.File.ID && t.k.ch == m.Channel && t.k.ts == m.TS
}

// inline is whether t is a list's tile, by its key (a new tile's flags
// aren't set yet).
func (t *tile) inline() bool { return t.k.pane != "pip" && t.k.pane != "viewer" }

// closePipLocked closes the mini player (usePipUndock's onClose and
// onUndock): its player goes, and with clips_pip on the inline player of
// its message takes its time, playing on if resume and it was playing
// (and it can play inline).
func (c *Controller) closePipLocked(resume bool) {
	p := c.pip
	if p == nil {
		return
	}
	c.pip = nil
	var at time.Duration
	playing := false
	if t := c.tiles[p.k]; t != nil {
		at, playing = t.elapsed, t.playing
		c.closeLocked(t)
		delete(c.tiles, p.k)
	}
	play := resume && playing && !p.media.Gallery
	found := false
	for _, t := range c.tiles {
		if !t.inlineOf(p.media) {
			continue
		}
		t.inPip = false
		// a player laid out now takes it at once; one that isn't (Jump to
		// message hasn't drawn its conversation yet) when it is, as the
		// effect runs once the VideoPlayer mounts again
		if c.PipOn() && t.laid == c.frame {
			c.resumeLocked(t, at, play)
			found = true
		}
	}
	if !found && c.PipOn() && p.media.File != nil {
		c.pipResume = &pipResume{ch: p.media.Channel, ts: p.media.TS, file: p.media.File.ID, at: at, play: play}
	}
}

// resumeLocked is useResumeAfterPipClose's onResume: the inline player at
// at, playing if play (setCurrentTime, or mounting it again).
func (c *Controller) resumeLocked(t *tile, at time.Duration, play bool) {
	t.elapsed = at
	if at > 0 {
		t.played = true
	}
	if t.h != nil {
		t.h.Seek(at)
		if play {
			t.h.Play()
		}
		return
	}
	t.startAt = at
	if play {
		c.mountLocked(t, true)
	}
}

// pendingResumeLocked gives t a closed mini player's time, once, when t is
// the inline tile of its message.
func (c *Controller) pendingResumeLocked(t *tile) {
	r := c.pipResume
	if r == nil || !t.inline() || t.f == nil || t.f.ID != r.file || t.k.ch != r.ch || t.k.ts != r.ts {
		return
	}
	c.pipResume = nil
	c.resumeLocked(t, r.at, r.play)
}

// Pip is the mini player now: the row whose LayoutMedia draws its player
// and overlay, and its video's message; false when nothing is docked.
func (c *Controller) Pip() (*Row, PipMedia, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pip == nil {
		return nil, PipMedia{}, false
	}
	return c.pip.row, c.pip.media, true
}

// PipPlayback is the mini player's player: where it is, its duration and
// whether it plays (Expand opens the viewer there).
func (c *Controller) PipPlayback() (at, dur time.Duration, playing bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pip == nil {
		return 0, 0, false
	}
	if t := c.tiles[c.pip.k]; t != nil {
		return t.elapsed, t.durLocked(), t.playing
	}
	return 0, 0, false
}

// PipTrigger is how the shown video docked: "scroll", "navigate" or
// "manual" ("" for none).
func (c *Controller) PipTrigger() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pip == nil {
		return ""
	}
	return c.pip.trigger
}

// ClosePip closes the mini player: Close (resume false), Expand (false:
// the viewer plays it), or Jump to message (true: the inline player plays
// on if it played).
func (c *Controller) ClosePip(resume bool) {
	c.mu.Lock()
	c.closePipLocked(resume)
	c.mu.Unlock()
	if c.env.Repaint != nil {
		c.env.Repaint()
	}
}

// DockViewer is the media viewer's Dock to corner: m's video in the mini
// player from at, playing if playing (the viewer closes).
func (c *Controller) DockViewer(m PipMedia, at time.Duration, playing bool) {
	if m.File == nil || !c.Available() {
		return
	}
	c.mu.Lock()
	c.openPipLocked(m, at, playing, "manual")
	c.mu.Unlock()
	if c.env.Repaint != nil {
		c.env.Repaint()
	}
}

// PipFreeAt is whether p, in the mini player's video, is on the overlay's
// own button (the video itself), not on its controls: a press there may
// drag the mini player.
func (c *Controller) PipFreeAt(p image.Point) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pip == nil {
		return false
	}
	t := c.tiles[c.pip.k]
	return t != nil && t.z.at(p, t.volOver) == zTile
}

// PipWidth is the mini player's width the viewer dragged it to (dp; 0:
// the default), kept for the session (VideoPipContent's resized width).
func (c *Controller) PipWidth() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pipW
}

// SetPipWidth sets it (0 resets it: a resize handle's double click).
func (c *Controller) SetPipWidth(dp int) {
	c.mu.Lock()
	c.pipW = max(dp, 0)
	c.mu.Unlock()
}

// tipID keys a control's tooltip state.
type tipID struct {
	k    key
	what string
}

// tip draws a control's tooltip through the host.
func (c *Controller) tip(gtx layout.Context, id tipID, hovered bool, r image.Rectangle, below bool, delay time.Duration, label string) {
	if c.env.Tooltip != nil {
		c.env.Tooltip(gtx, id, hovered, r, below, delay, label)
	}
}
