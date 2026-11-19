// Package videoplayer is Slack's inline video player in a message: the
// VideoPlayer (fIJO, build 2c0d02ca130f) that shows its
// VideoControlsOverlay from the start and mounts the <video>
// (BaseMediaVideo, 1ERX) on the first click, drawn in Gio over the
// message's video tile. richview hands it the tile (richview.Media);
// internal/media plays the file with the OS's decoder, whose frames the
// window draws as its own (Gio patch 51), so a playing video costs a
// quiet frame for each of its frames and no layout.
package videoplayer

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"log"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Env is what a window gives its players.
type Env struct {
	// Manager makes the players (the OS's backend, and Slack's
	// MediaPlaybackCoordinator: one plays at a time). nil, or one with no
	// backend here, leaves every tile as it was: its click opens the
	// file's page.
	Manager *media.Manager
	// Open streams a file's URL from off (core.App.OpenRange: the
	// session's TLS, memory only).
	Open func(ctx context.Context, rawURL string, off int64) (io.ReadCloser, media.Info, error)
	// FrameReady is a new video frame's repaint: the App's animReady, so
	// the frame can be a quiet one.
	FrameReady func()
	// Repaint asks for a layout (the App's Invalidate).
	Repaint func()
	// Pref is an account pref as Slack sent it; SetPref writes one
	// (users.prefs.set, Slack's setPref).
	Pref    func(name string) (json.RawMessage, bool)
	SetPref func(name string, value any)
	// Experiment is the viewer's group of a Slack experiment.
	Experiment func(name string) string
	// Theme is the window's: its shaper draws the overlay's text.
	Theme func() *theme.Theme
}

// RowHost is what a message gives its tiles: the file actions (the
// corner's More actions is richview.FileMore, btn in View coordinates).
type RowHost interface {
	OpenFile(f *richtext.File)
	FileAction(a richview.FileAction, f *richtext.File, btn image.Rectangle)
}

// unmountAfter is how long a player paused and not laid out lives: then
// it closes, as React unmounting its row does (Clack's stand-in).
var unmountAfter = 10 * time.Second

// dockBelow is useScrollDockTrigger's (YPBt): a playing video scrolled
// under 10% visible docks into the mini player. Until Clack has one, it
// pauses there.
const dockBelow = 0.1

// key names a tile: a file in a message, in one list (the channel's and
// the thread pane's root are two players, as in Slack).
type key struct{ pane, ch, ts, file string }

// Controller is a window's inline players.
type Controller struct {
	env Env

	mu    sync.Mutex
	tiles map[key]*tile
	// progress is where the viewer left each file (memory only): a tile
	// whose player closed shows it, and a new player starts there
	progress map[string]time.Duration
	frame    uint64 // the full layouts so far (BeginFrame)
	gen      uint64 // the players made so far
	// media_muted and media_volume as the players have them: the
	// slider's value (vol, ToggleMute's local volume) and the pref's
	// (prefVol); what the viewer sets shows at once, the prefs follow
	muted       bool
	vol         float64
	prefVol     float64
	prefRaw     [2]string
	prefsLoaded bool
}

// New is a window's players.
func New(env Env) *Controller {
	return &Controller{env: env, tiles: map[key]*tile{}, progress: map[string]time.Duration{}, vol: 1, prefVol: 1}
}

// Available reports whether videos play in place here.
func (c *Controller) Available() bool {
	return c != nil && c.env.Manager != nil && c.env.Manager.Available() == nil
}

// Row is a message's tiles (richview.Media): message ts in conversation
// ch, as list pane draws it.
func (c *Controller) Row(pane, ch, ts string, h RowHost) *Row {
	return &Row{c: c, pane: pane, ch: ch, ts: ts, h: h}
}

// Row is one message's richview.Media.
type Row struct {
	c            *Controller
	pane, ch, ts string
	h            RowHost
}

// MediaTile is richview.Media's: a lone playable video (the gallery's
// only tile) plays in place, fIJO's tE; a gallery's tiles open the media
// viewer, which doesn't play videos yet, so they stay as they were.
func (r *Row) MediaTile(f *richtext.File, files int, overflow bool) bool {
	return r.c.Available() && f != nil && f.IsVideo && f.URL != "" && f.ID != "" && files == 1 && !overflow
}

func (r *Row) key(f *richtext.File) key { return key{r.pane, r.ch, r.ts, f.ID} }

// MediaVisible is how much of f's tile the list showed as it laid out
// (0..1): a playing video under dockBelow pauses (Slack docks it).
func (r *Row) MediaVisible(f *richtext.File, frac float64) {
	if f == nil || frac >= dockBelow {
		return
	}
	c := r.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.tiles[r.key(f)]; t != nil && t.h != nil && t.playing {
		t.h.Pause()
	}
}

// tile is a video tile's player and overlay.
type tile struct {
	k key
	f *richtext.File
	// h is the player once the tile mounted one (Slack's showVideo), nil
	// before and after; gen is its, so a closed player's events drop
	h   *media.Handle
	gen uint64
	// as the player's events left them
	playing bool // isPlaying: since play, until pause or the end
	loading bool // isLoading: the spinner, from the click that mounted it until it plays
	played  bool // playback started once: the pill shows the time played
	ended   bool
	errCode int // the last media_error_code (no error UI: the spinner goes on)
	elapsed time.Duration
	dur     time.Duration // the player's duration, 0 until its metadata
	// the overlay's (input, and the zones the last layout drew)
	hover   bool
	ptr     f32.Point
	press   zone
	scrub   bool // the scrubber is dragged
	resume  bool // the video played when the scrub began
	vol     bool // the volume slider is dragged
	volOver bool // the pointer is on the volume control: its slider shows
	z       zones
	// the frame it was last laid out in, and the close waiting for it
	laid    uint64
	unmount *time.Timer
	size    image.Point // the drawn size the player was given
	applied bool
	appMute bool
	appVol  float64
}

// tileLocked is the tile for k, made with f's progress if new; f is the
// latest copy of the file.
func (c *Controller) tileLocked(k key, f *richtext.File) *tile {
	t := c.tiles[k]
	if t == nil {
		t = &tile{k: k, f: f}
		if p, ok := c.progress[f.ID]; ok && p > 0 {
			t.elapsed, t.played = p, true
		}
		c.tiles[k] = t
	}
	t.f = f
	return t
}

// durLocked is the tile's duration: the player's once known, else the
// file's duration_ms.
func (t *tile) durLocked() time.Duration {
	if t.dur > 0 {
		return t.dur
	}
	if t.f != nil && t.f.DurationMS > 0 {
		return time.Duration(t.f.DurationMS) * time.Millisecond
	}
	return 0
}

// mountLocked makes the tile's player (Slack mounts the <video> with
// autoPlay on the first click, without it when a scrub starts). It starts
// where the viewer left the file: the local progress, else
// media_progress.offset_ms unless media_watched (xnwb).
func (c *Controller) mountLocked(t *tile, autoplay bool) {
	if t.h != nil || c.env.Manager == nil || c.env.Open == nil || t.f == nil {
		return
	}
	f := t.f
	u := f.URL
	src := media.Source{FileID: f.ID, Mime: f.Mime, Ext: ext(f), Open: func(ctx context.Context, off int64) (io.ReadCloser, media.Info, error) {
		return c.env.Open(ctx, u, off)
	}}
	c.gen++
	gen := c.gen
	h, err := c.env.Manager.Open(src, c.env.FrameReady, func(ev media.Event) { c.onEvent(t, gen, ev) })
	if err != nil {
		log.Printf("videoplayer: %s: %v", f.ID, err)
		return
	}
	t.h, t.gen = h, gen
	t.loading, t.ended, t.errCode, t.applied = autoplay, false, 0, false
	start := c.startLocked(f)
	t.elapsed = start
	c.applyLocked(t)
	if c.loops(f) {
		h.SetLoop(true)
	}
	if t.size.X > 0 && t.size.Y > 0 {
		h.SetDrawSize(t.size)
	}
	if start > 0 {
		h.Seek(start)
	}
	if autoplay {
		h.Play()
	}
}

// progressAt is the progress kept for a video left at pos of dur:
// saveMediaProgress (gnNF) keeps 0 within a second of the end.
func progressAt(pos, dur time.Duration) time.Duration {
	if dur > 0 && dur-pos < time.Second {
		return 0
	}
	return pos
}

// startLocked is where a new player of f starts.
func (c *Controller) startLocked(f *richtext.File) time.Duration {
	if p, ok := c.progress[f.ID]; ok {
		return p
	}
	if f.MediaWatched || f.MediaOffsetMS <= 0 {
		return 0
	}
	return time.Duration(f.MediaOffsetMS) * time.Millisecond
}

// loops is fIJO's: with looping_video_option on, a file whose title says
// "loop" loops.
func (c *Controller) loops(f *richtext.File) bool {
	if c.env.Experiment == nil || c.env.Experiment("looping_video_option") != "on" {
		return false
	}
	title := f.Title
	if title == "" {
		title = f.Name
	}
	return strings.Contains(strings.ToLower(title), "loop")
}

// ext picks the demuxer: the source URL's extension (the mp4 Slack made
// for a mov is an .mp4), else the filetype.
func ext(f *richtext.File) string {
	if u, err := url.Parse(f.URL); err == nil {
		switch e := strings.ToLower(strings.TrimPrefix(path.Ext(u.Path), ".")); e {
		case "mp4", "m4v", "mov", "webm":
			return e
		}
	}
	switch t := strings.ToLower(f.Type); t {
	case "mp4", "m4v", "mov", "webm":
		return t
	}
	return "mp4"
}

// onEvent is a player's event, on its handle's goroutine.
func (c *Controller) onEvent(t *tile, gen uint64, ev media.Event) {
	c.mu.Lock()
	if t.gen != gen || t.h == nil {
		c.mu.Unlock()
		return
	}
	st := ev.State
	repaint := t.hover // the overlay shows the time
	switch ev.Kind {
	case media.Play: // onPlay (tI)
		t.playing, t.played, t.ended, repaint = true, true, false, true
	case media.Playing: // the spinner shows until it plays
		t.playing, t.loading, repaint = true, false, true
	case media.Pause:
		t.playing, repaint = false, true
		if !t.ended {
			c.progress[t.f.ID] = progressAt(st.Pos, st.Duration)
		}
	case media.Ended:
		t.playing, t.ended, repaint = false, true, true
		c.progress[t.f.ID] = 0
	case media.Error: // 1ERX: no error UI; a spinner showing keeps spinning
		t.errCode, repaint = st.Err, true
	case media.LoadedMetadata, media.DurationChange, media.Seeked:
		repaint = true
	}
	if !t.scrub {
		t.elapsed = st.Pos
	}
	if st.Duration > 0 {
		t.dur = st.Duration
	}
	c.mu.Unlock()
	if repaint && c.env.Repaint != nil {
		c.env.Repaint()
	}
}

// closeLocked closes the tile's player; the tile keeps showing where it
// was (the poster, the time played).
func (c *Controller) closeLocked(t *tile) {
	if t.unmount != nil {
		t.unmount.Stop()
		t.unmount = nil
	}
	if t.h == nil {
		return
	}
	if !t.ended && t.f != nil {
		c.progress[t.f.ID] = progressAt(t.elapsed, t.durLocked())
	}
	t.h.Close()
	t.h, t.gen = nil, 0
	t.playing, t.loading, t.scrub, t.vol, t.applied = false, false, false, false, false
}

// BeginFrame starts a full layout of the window (not a quiet frame).
func (c *Controller) BeginFrame() {
	c.mu.Lock()
	c.frame++
	c.mu.Unlock()
}

// EndFrame ends it: a player the layout didn't lay out (its
// conversation left, its pane closed, its row out of the list) pauses,
// and closes once it has been away unmountAfter; a tile with no player
// that wasn't laid out is forgotten.
func (c *Controller) EndFrame() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, t := range c.tiles {
		if t.laid == c.frame {
			if t.unmount != nil {
				t.unmount.Stop()
				t.unmount = nil
			}
			continue
		}
		if t.h == nil {
			delete(c.tiles, k)
			continue
		}
		if t.playing {
			t.h.Pause()
		}
		if t.unmount == nil {
			k, t, laid := k, t, t.laid
			t.unmount = time.AfterFunc(unmountAfter, func() { c.unmountIdle(k, t, laid) })
		}
	}
}

// unmountIdle closes t's player if it wasn't laid out since laid.
func (c *Controller) unmountIdle(k key, t *tile, laid uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tiles[k] != t || t.laid != laid || t.h == nil {
		return
	}
	t.unmount = nil
	c.closeLocked(t)
	delete(c.tiles, k)
}

// CloseAll closes every player and forgets the progress (sign-out).
func (c *Controller) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, t := range c.tiles {
		c.closeLocked(t)
		delete(c.tiles, k)
	}
	c.progress = map[string]time.Duration{}
}

// syncPrefsLocked reads media_muted and media_volume (?? 1) when they
// changed: ToggleMute follows the prefs, and its slider takes the pref's
// volume unless it's being dragged.
func (c *Controller) syncPrefsLocked() {
	if c.env.Pref == nil {
		return
	}
	m, _ := c.env.Pref("media_muted")
	v, _ := c.env.Pref("media_volume")
	raw := [2]string{string(m), string(v)}
	if c.prefsLoaded && raw == c.prefRaw {
		return
	}
	first := !c.prefsLoaded
	if first || raw[0] != c.prefRaw[0] {
		c.muted = prefBool(m)
	}
	if first || raw[1] != c.prefRaw[1] { // useEffect([C]): the slider takes a new pref volume
		c.prefVol = prefVolume(v)
		if !c.dragging() {
			c.vol = c.prefVol
		}
	}
	c.prefRaw, c.prefsLoaded = raw, true
}

func (c *Controller) dragging() bool {
	for _, t := range c.tiles {
		if t.vol {
			return true
		}
	}
	return false
}

func prefBool(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		b, _ = strconv.ParseBool(s)
	}
	return b
}

// prefVolume is media_volume ?? 1, a number or the string users.prefs.get
// may give.
func prefVolume(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 1
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 1
		}
		var err error
		if f, err = strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
			return 1
		}
	}
	return min(max(f, 0), 1)
}

// applyLocked gives t's player the mute and volume (ToggleMute's
// effects: setMuted(media_muted), setVolume(its slider's volume)).
func (c *Controller) applyLocked(t *tile) {
	if t.h == nil {
		return
	}
	if !t.applied || t.appMute != c.muted {
		t.h.SetMuted(c.muted)
		t.appMute = c.muted
	}
	if !t.applied || t.appVol != c.vol {
		t.h.SetVolume(c.vol)
		t.appVol = c.vol
	}
	t.applied = true
}

func (c *Controller) setPref(name string, v any) {
	if c.env.SetPref != nil {
		c.env.SetPref(name, v)
	}
}

// toggleMuteLocked is ToggleMute's click: media_muted flips, and
// unmuting puts the slider back at the pref's volume.
func (c *Controller) toggleMuteLocked() {
	was := c.muted
	c.muted = !was
	if was {
		c.vol = c.prefVol
	}
	c.setPref("media_muted", c.muted)
}

// setVolumeLocked is the volume slider's change (j, then T): 0 mutes; any
// other volume unmutes, and is written as media_volume unless the slider
// is still being dragged (its mouseup writes it).
func (c *Controller) setVolumeLocked(v float64, dragging bool) {
	if v < 0 {
		return
	}
	c.vol = v
	if v == 0 {
		if !c.muted {
			c.muted = true
			c.setPref("media_muted", true)
		}
		return
	}
	if c.muted {
		c.muted = false
		c.setPref("media_muted", false)
	}
	if !dragging && v != c.prefVol {
		c.prefVol = v
		c.setPref("media_volume", v)
	}
}

// clickLocked is tE for a lone video: the first click mounts the player
// with autoplay (the spinner until it plays), later ones toggle it.
func (c *Controller) clickLocked(t *tile) {
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

// State is a tile's, for tests and the inspect tool.
type State struct {
	Mounted, Playing, Loading, Played bool
	Err                               int
	Elapsed, Duration                 time.Duration
	// Player is the mounted player (nil before the first click and once
	// closed).
	Player media.Player
	// Mute, Slider, Scrub and More are the overlay's controls as the last
	// layout drew them, in the tile's coordinates (the slider shows while
	// the pointer is on the volume control).
	Mute, Slider, Scrub, More image.Rectangle
	Hover                     bool
}

// Click is the tile's click (fIJO's tE), as a press and release on it
// give: the real-window test's, which gives the window no input.
func (c *Controller) Click(pane, ch, ts, file string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tiles[key{pane, ch, ts, file}]
	if t == nil {
		return false
	}
	c.clickLocked(t)
	return true
}

// TileState is the tile of file in message ts of ch in pane.
func (c *Controller) TileState(pane, ch, ts, file string) (State, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tiles[key{pane, ch, ts, file}]
	if t == nil {
		return State{}, false
	}
	st := State{Mounted: t.h != nil, Playing: t.playing, Loading: t.loading, Played: t.played, Err: t.errCode,
		Elapsed: t.elapsed, Duration: t.durLocked(), Mute: t.z.mute, Slider: t.z.slider, Scrub: t.z.scrub, More: t.z.more,
		Hover: t.hover}
	if t.h != nil {
		st.Player = t.h
	}
	return st, true
}

// Mounted is the players open now.
func (c *Controller) Mounted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.tiles {
		if t.h != nil {
			n++
		}
	}
	return n
}

// Progress is where the viewer left file (memory only), ok when known.
func (c *Controller) Progress(file string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.progress[file]
	return p, ok
}

// Volume is the mute and the slider's volume the players have.
func (c *Controller) Volume() (muted bool, vol float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.muted, c.vol
}
