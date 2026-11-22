package videoplayer

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/richtext"
)

// prefs is an account's prefs as users.prefs.set leaves them, at once.
type prefs struct {
	mu   sync.Mutex
	vals map[string]json.RawMessage
	sets []string
}

func (p *prefs) get(name string) (json.RawMessage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.vals[name]
	return v, ok
}

func (p *prefs) set(name string, v any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, _ := json.Marshal(v)
	p.vals[name] = b
	p.sets = append(p.sets, name+"="+string(b))
}

func (p *prefs) writes() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.sets...)
}

// fakeEnv is a controller on the fake backend, the fixture behind every
// URL, with every experiment on.
func fakeEnv(t *testing.T) (*Controller, *prefs) {
	t.Helper()
	return fakeEnvExp(t, func(string) string { return "on" })
}

// fakeEnvExp is fakeEnv with exp's experiments.
func fakeEnvExp(t *testing.T, exp func(string) string) (*Controller, *prefs) {
	t.Helper()
	_, o := mediatest.Source("FVP", mediatest.GreyGreyRed, false)
	p := &prefs{vals: map[string]json.RawMessage{}}
	c := New(Env{
		Manager: media.NewManager(media.NewFake(media.FakeOptions{Log: func(string, ...any) {}})),
		Open: func(ctx context.Context, _ string, off int64) (io.ReadCloser, media.Info, error) {
			return o.Open(ctx, off)
		},
		Pref:       p.get,
		SetPref:    p.set,
		Experiment: exp,
	})
	t.Cleanup(c.CloseAll)
	return c, p
}

// fakeEnvSlow is fakeEnv with a source of its own per file, each file's
// reads waiting slow[its id] (a network, or a runner, that slow).
func fakeEnvSlow(t *testing.T, slow map[string]time.Duration) *Controller {
	t.Helper()
	var mu sync.Mutex
	openers := map[string]*mediatest.Opener{}
	p := &prefs{vals: map[string]json.RawMessage{}}
	c := New(Env{
		Manager: media.NewManager(media.NewFake(media.FakeOptions{Log: func(string, ...any) {}})),
		Open: func(ctx context.Context, u string, off int64) (io.ReadCloser, media.Info, error) {
			mu.Lock()
			o := openers[u]
			if o == nil {
				_, o = mediatest.Source("FVP", mediatest.GreyGreyRed, false)
				for id, d := range slow {
					if strings.Contains(u, "-"+id+"/") {
						o.Delay = d
					}
				}
				openers[u] = o
			}
			mu.Unlock()
			return o.Open(ctx, off)
		},
		Pref:       p.get,
		SetPref:    p.set,
		Experiment: func(string) string { return "on" },
	})
	t.Cleanup(c.CloseAll)
	return c
}

func videoFile(id string) *richtext.File {
	return &richtext.File{ID: id, Name: "clip.mp4", Type: "mp4", Mime: "video/mp4", IsVideo: true, DurationMS: 83000,
		URL: "https://files.slack.com/files-pri/T1-" + id + "/clip.mp4"}
}

// frame is a full layout that lays the rows' tiles out (BeginFrame, the
// tiles, EndFrame).
func frame(c *Controller, tiles ...[2]any) {
	c.BeginFrame()
	c.mu.Lock()
	for _, rt := range tiles {
		r, f := rt[0].(*Row), rt[1].(*richtext.File)
		t := c.tileLocked(r.key(f), f)
		t.laid = c.frame
		c.pendingResumeLocked(t) // as the overlay's layout: a closed mini player's time
	}
	c.mu.Unlock()
	c.EndFrame()
}

func click(c *Controller, r *Row, f *richtext.File) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clickLocked(c.tileLocked(r.key(f), f))
}

func state(t *testing.T, c *Controller, r *Row, f *richtext.File) State {
	t.Helper()
	st, ok := c.TileState(r.pane, r.ch, r.ts, f.ID)
	if !ok {
		t.Fatalf("no tile for %s in %s", f.ID, r.ts)
	}
	return st
}

func waitFor(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	for dl := time.Now().Add(d); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("no %s within %v", what, d)
		}
	}
}

// strictWaits (CLACK_VP_STRICT_WAITS=1) gives waitForTile the plain wall
// time it replaced: the red side of its proof under load.
var strictWaits = os.Getenv("CLACK_VP_STRICT_WAITS") != ""

// waitForTile is waitFor for r's tile of f, on the tile's own progress
// rather than a span of wall time: d is how long it may go neither loading
// nor ok. While it loads (the spinner, or its player waiting for data) the
// wait goes on, up to a minute for a hang. A player starts once it has read
// its file's metadata, on the runner's CPU, and on a loaded runner that
// went past 5 s (train-143's TestOnePlaysAtATime, α.133's
// TestCornerDockButton). A failure says what the tile and its player were
// doing.
func waitForTile(t *testing.T, c *Controller, r *Row, f *richtext.File, what string, d time.Duration, ok func() bool) {
	t.Helper()
	start := time.Now()
	var still time.Time // since the tile last loaded
	for !ok() {
		st := state(t, c, r, f)
		switch {
		case strictWaits:
			if time.Since(start) > d {
				t.Fatalf("no %s within %v (strict): %s", what, d, tileReport(st))
			}
		case tileLoading(st):
			still = time.Time{}
		case still.IsZero():
			still = time.Now()
		case time.Since(still) > d:
			t.Fatalf("no %s: %v without it, the video not loading: %s", what, d, tileReport(st))
		}
		if time.Since(start) > time.Minute {
			t.Fatalf("no %s within a minute, the video loading all along: %s", what, tileReport(st))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tileLoading is whether st's video is still coming: the spinner, or its
// player waiting for data to play.
func tileLoading(st State) bool {
	return st.Loading || st.Player != nil && st.Player.State().Waiting
}

// tileReport is a tile's state and its player's, for a failure.
func tileReport(st State) string {
	s := fmt.Sprintf("the tile mounted %v, playing %v, loading %v, played %v, error %d, at %v of %v",
		st.Mounted, st.Playing, st.Loading, st.Played, st.Err, st.Elapsed, st.Duration)
	if p := st.Player; p != nil {
		ps, x := p.State(), p.Stats()
		s += fmt.Sprintf("; its player paused %v, waiting %v, ended %v, ready %d, error %d, at %v; %d requests, %d bytes read, %d events dropped, %d frames, %d ticks",
			ps.Paused, ps.Waiting, ps.Ended, ps.Ready, ps.Err, ps.Pos, x.Requests, x.BytesRead, x.Dropped, x.Frames, x.Ticks)
	}
	return s
}

// A lone playable video plays in place; a gallery's tiles and the "+ N"
// tile stay as they were (their click opens the media viewer in Slack).
func TestMediaTile(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	if !r.MediaTile(f, 1, false) {
		t.Error("a lone video isn't a player's")
	}
	if r.MediaTile(f, 2, false) || r.MediaTile(f, 1, true) {
		t.Error("a gallery's tile, or the + N tile, plays in place")
	}
	img := &richtext.File{ID: "F2", IsImage: true, URL: "https://files.slack.com/x.png"}
	if r.MediaTile(img, 1, false) {
		t.Error("an image is a player's")
	}
	none := New(Env{Manager: media.NewManager(nil)})
	if none.Available() && media.Available() != nil {
		t.Error("available with no backend")
	}
}

// The first click mounts a player with autoplay, the spinner showing
// until it plays; the next pauses it and the next plays on (tE).
func TestClickMountsAndToggles(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	frame(c, [2]any{r, f})
	if st := state(t, c, r, f); st.Mounted || st.Played {
		t.Fatalf("a player before the click: %+v", st)
	}
	click(c, r, f)
	if st := state(t, c, r, f); !st.Mounted || !st.Loading {
		t.Fatalf("after the click: %+v, want mounted with the spinner", st)
	}
	waitForTile(t, c, r, f, "playing", 5*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	if d := state(t, c, r, f).Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond {
		t.Errorf("duration %v, want the fixture's 3 s (not duration_ms's 1:23) once its metadata came", d)
	}
	click(c, r, f)
	waitForTile(t, c, r, f, "paused", 5*time.Second, func() bool { return !state(t, c, r, f).Playing })
	at := state(t, c, r, f).Elapsed
	click(c, r, f)
	waitForTile(t, c, r, f, "playing again", 5*time.Second, func() bool { return state(t, c, r, f).Playing })
	if st := state(t, c, r, f); st.Elapsed < at {
		t.Errorf("played on from %v, after pausing at %v", st.Elapsed, at)
	}
}

// One video plays at a time (MediaPlaybackCoordinator): the second's play
// pauses the first. The first plays once its data is in: its play event
// comes at once, and the spinner goes when it plays. Waiting only for the
// play event, the test clicked the second while the first still loaded,
// and when the first's data came its start paused the second, as Slack's
// coordinator does (an autoplay's start pauses the others): the second,
// paused while it waited, never played. A loaded runner's metadata came
// that late (train-143, 44970400272: "no the second playing within 5s");
// "late" gives that order every time.
func TestOnePlaysAtATime(t *testing.T) {
	for _, tc := range []struct {
		name         string
		first, other time.Duration // each Read of the file's body waits this
	}{
		{"at once", 0, 0},
		{"the first's data late", 300 * time.Millisecond, 600 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f1, f2 := videoFile("F1"), videoFile("F2")
			c := fakeEnvSlow(t, map[string]time.Duration{f1.ID: tc.first, f2.ID: tc.other})
			r1, r2 := c.Row("channel", "C1", "1.0", nil), c.Row("channel", "C1", "2.0", nil)
			frame(c, [2]any{r1, f1}, [2]any{r2, f2})
			click(c, r1, f1)
			waitForTile(t, c, r1, f1, "the first playing", 5*time.Second, func() bool { st := state(t, c, r1, f1); return st.Playing && !st.Loading })
			click(c, r2, f2)
			waitForTile(t, c, r2, f2, "the second playing", 5*time.Second, func() bool { st := state(t, c, r2, f2); return st.Playing && !st.Loading })
			waitForTile(t, c, r1, f1, "the first paused", 5*time.Second, func() bool { return !state(t, c, r1, f1).Playing })
		})
	}
}

// Without clips_pip (no mini player), a playing video the list shows
// under 10% of plays on (YPBt's observer isn't active); one the layout no
// longer draws pauses, then closes after unmountAfter, keeping where it
// was (React unmounting its row); the next player starts there.
func TestOffscreenPausesThenCloses(t *testing.T) {
	defer func(d time.Duration) { unmountAfter = d }(unmountAfter)
	unmountAfter = 300 * time.Millisecond
	c, _ := fakeEnvExp(t, func(name string) string {
		if name == "clips_pip" {
			return "off"
		}
		return "on"
	})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	frame(c, [2]any{r, f})
	click(c, r, f)
	waitForTile(t, c, r, f, "playing", 5*time.Second, func() bool { return state(t, c, r, f).Playing })
	r.MediaVisible(f, 0.5)
	r.MediaVisible(f, 0.05)
	time.Sleep(100 * time.Millisecond)
	if st := state(t, c, r, f); !st.Playing || st.InPip {
		t.Fatalf("under 10%% visible without clips_pip: %+v, want it playing on", st)
	}
	time.Sleep(300 * time.Millisecond)
	frame(c) // a layout that doesn't draw it: another conversation
	waitForTile(t, c, r, f, "paused off the screen", 5*time.Second, func() bool { return !state(t, c, r, f).Playing })
	at := state(t, c, r, f).Elapsed
	waitFor(t, "closed", 5*time.Second, func() bool { return c.Mounted() == 0 })
	if p, ok := c.Progress(f.ID); !ok || p < at-50*time.Millisecond || p <= 0 {
		t.Errorf("progress %v (%v), want where it stopped, %v", p, ok, at)
	}
	frame(c, [2]any{r, f}) // back: the poster with the time played
	st := state(t, c, r, f)
	if st.Mounted || !st.Played || st.Elapsed <= 0 {
		t.Errorf("laid out again after closing: %+v, want the poster with the time played", st)
	}
	if got := pillTime(c.tiles[r.key(f)], false); got != clock(st.Elapsed) {
		t.Errorf("the pill reads %q, want the time played %q", got, clock(st.Elapsed))
	}
}

// ToggleMute and the volume slider write media_muted and media_volume as
// Slack's do: the click flips the mute; a volume of 0 mutes, any other
// unmutes, and a dragged volume is written once, when the drag ends.
func TestVolumePrefs(t *testing.T) {
	c, p := fakeEnv(t)
	c.mu.Lock()
	c.syncPrefsLocked()
	if c.muted || c.vol != 1 {
		t.Errorf("no prefs: muted %v, volume %v; want unmuted at 1 (media_volume ?? 1)", c.muted, c.vol)
	}
	c.toggleMuteLocked()
	c.setVolumeLocked(0.4, true)
	c.setVolumeLocked(0.45, true)
	c.setVolumeLocked(0.45, false)
	c.setVolumeLocked(0, false)
	c.toggleMuteLocked()
	muted, vol := c.muted, c.vol
	c.mu.Unlock()
	want := []string{"media_muted=true", "media_muted=false", "media_volume=0.45", "media_muted=true", "media_muted=false"}
	got := p.writes()
	if len(got) != len(want) {
		t.Fatalf("prefs written %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prefs written %v, want %v", got, want)
		}
	}
	if muted || vol != 0.45 {
		t.Errorf("unmuted after muting at 0: muted %v, volume %v; want the pref's 0.45 back", muted, vol)
	}
	for raw, w := range map[string]float64{`0.5`: 0.5, `"0.25"`: 0.25, `null`: 1, ``: 1, `2`: 1, `"x"`: 1} {
		if got := prefVolume(json.RawMessage(raw)); got != w {
			t.Errorf("media_volume %s reads %v, want %v", raw, got, w)
		}
	}
	for raw, w := range map[string]bool{`true`: true, `false`: false, `"true"`: true, ``: false} {
		if got := prefBool(json.RawMessage(raw)); got != w {
			t.Errorf("media_muted %s reads %v, want %v", raw, got, w)
		}
	}
}

// saveMediaProgress (gnNF) keeps 0 for a video left within a second of
// its end, else where it was left.
func TestProgressAt(t *testing.T) {
	for _, c := range []struct{ pos, dur, want time.Duration }{
		{1900 * time.Millisecond, 3 * time.Second, 1900 * time.Millisecond},
		{2001 * time.Millisecond, 3 * time.Second, 0},
		{3 * time.Second, 3 * time.Second, 0},
		{5 * time.Second, 0, 5 * time.Second}, // no duration yet
	} {
		if got := progressAt(c.pos, c.dur); got != c.want {
			t.Errorf("left at %v of %v: progress %v, want %v", c.pos, c.dur, got, c.want)
		}
	}
}

// The pill's time is fIJO's: the duration at rest, the time played once
// it plays or was partly played, "Video" with neither (and the time
// played, 0:00, hovered); w7xZ's format.
func TestPillTime(t *testing.T) {
	f := videoFile("F1")
	tl := &tile{f: f}
	if got := pillTime(tl, false); got != "1:23" {
		t.Errorf("at rest: %q, want the duration 1:23", got)
	}
	tl.playing = true
	if got := pillTime(tl, false); got != "0:00" {
		t.Errorf("playing at 0: %q, want 0:00", got)
	}
	tl.playing, tl.elapsed = false, 61500*time.Millisecond
	if got := pillTime(tl, false); got != "1:01" {
		t.Errorf("paused at 61.5 s: %q, want 1:01", got)
	}
	nd := &tile{f: &richtext.File{ID: "F2", IsVideo: true}}
	if got := pillTime(nd, false); got != "Video" {
		t.Errorf("no duration: %q, want Video", got)
	}
	if got := pillTime(nd, true); got != "0:00" {
		t.Errorf("no duration, hovered: %q, want 0:00", got)
	}
	for d, w := range map[time.Duration]string{0: "0:00", 5 * time.Second: "0:05", 83 * time.Second: "1:23", 600 * time.Second: "10:00",
		3723 * time.Second: "1:02:03", 36001 * time.Second: "10:00:01", 999 * time.Millisecond: "0:00"} {
		if got := clock(d); got != w {
			t.Errorf("clock(%v) = %q, want %q", d, got, w)
		}
	}
}

// The scrubber and the volume slider read X7vT's way: the place along the
// track, clamped; the volume in hundredths, from the bottom when
// vertical.
func TestSliderValues(t *testing.T) {
	z := zones{track: image.Rect(100, 0, 300, 4), slider: image.Rect(10, 10, 80, 26)}
	for x, w := range map[int]time.Duration{50: 0, 100: 0, 150: 750 * time.Millisecond, 200: 1500 * time.Millisecond, 400: 3 * time.Second} {
		if got := z.timeAt(image.Pt(x, 2), 3*time.Second); got != w {
			t.Errorf("timeAt(%d) = %v, want %v", x, got, w)
		}
	}
	if got := z.volumeAt(image.Pt(45, 18)); got != 0.5 {
		t.Errorf("volumeAt the middle = %v, want .5", got)
	}
	z.vertical, z.slider = true, image.Rect(0, 100, 32, 170)
	if got := z.volumeAt(image.Pt(16, 100)); got != 1 {
		t.Errorf("vertical top = %v, want 1", got)
	}
	if got := z.volumeAt(image.Pt(16, 170)); got != 0 {
		t.Errorf("vertical bottom = %v, want 0", got)
	}
}

// A file whose title says "loop" loops under looping_video_option; the
// demuxer comes from the source's extension, else the filetype.
func TestLoopAndExt(t *testing.T) {
	c, _ := fakeEnv(t)
	if c.loops(videoFile("F1")) {
		t.Error("clip.mp4 loops")
	}
	if !c.loops(&richtext.File{Name: "x.mp4", Title: "Skate LOOP"}) {
		t.Error("a title saying loop doesn't loop")
	}
	for u, w := range map[string]string{"https://f/x/a.MOV": "mov", "https://f/x/a.mp4?x=1": "mp4", "https://f/x/a": "webm"} {
		if got := ext(&richtext.File{URL: u, Type: "webm"}); got != w {
			t.Errorf("ext(%s) = %s, want %s", u, got, w)
		}
	}
}
