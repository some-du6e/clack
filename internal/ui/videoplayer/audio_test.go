package videoplayer

// The audio player (audio.go: Slack's FileAudio → AudioPlayer) on Go
// audio, keeping time on its clock (no sound card), every URL serving one
// of mediatest's audio fixtures (3 s of tones): which files it takes, the
// click that mounts and plays (the aac rendition first, url_private when
// it doesn't decode here), the pause and the end, the media_playback_speed
// pref and who it applies to, the scrubber's seek, the time shown, and the
// geometry of both styles.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	clipAAC = "https://files.slack.com/files-tmb/T1-F0CLIP-aac/audio_message.m4a"
	clipURL = "https://files.slack.com/files-pri/T1-F0CLIP/audio_message.webm"
	mp3URL  = "https://files.slack.com/files-pri/T1-F0MP3/standup.mp3"
)

func clipFile() *richtext.File {
	return &richtext.File{ID: "F0CLIP", Name: "audio_message.webm", Title: "Audio clip", Type: "webm", Mime: "audio/webm", Subtype: "slack_audio",
		IsAudio: true, DurationMS: 12000, URL: clipURL, MediaURL: clipAAC, Waveform: []int{4, 9, 18, 30, 44, 61, 72, 80, 66, 52, 47, 58, 70, 84, 95, 88}}
}

func mp3File() *richtext.File {
	return &richtext.File{ID: "F0MP3", Name: "standup.mp3", Title: "standup.mp3", Type: "mp3", Mime: "audio/mpeg", IsAudio: true,
		DurationMS: 154000, Size: 900 << 10, URL: mp3URL}
}

var (
	audioThemeOnce sync.Once
	audioTheme     *theme.Theme
)

// audioEnv is a controller on Go audio with no sound card, each URL in
// files serving its bytes; opened lists the URLs the players read.
type audioRig struct {
	c        *Controller
	p        *prefs
	mu       sync.Mutex
	opened   []string
	repaints atomic.Int64
}

func audioEnv(t *testing.T, exp func(string) string, files map[string][]byte) *audioRig {
	t.Helper()
	audioThemeOnce.Do(func() { audioTheme = theme.New(false) })
	if exp == nil {
		exp = func(string) string { return "off" }
	}
	r := &audioRig{p: &prefs{vals: map[string]json.RawMessage{}}}
	openers := map[string]*mediatest.Opener{}
	for u, b := range files {
		_, o := mediatest.Source("FAUD", b, false)
		openers[u] = o
	}
	noCard := func(func([]int16)) (io.Closer, error) { return nil, errors.New("no sound card (the test's)") }
	r.c = New(Env{
		Manager: media.NewManager(media.NewChain(nil, &goaudio.Backend{Output: noCard, Logf: t.Logf})),
		Open: func(ctx context.Context, u string, off int64) (io.ReadCloser, media.Info, error) {
			r.mu.Lock()
			r.opened = append(r.opened, u)
			r.mu.Unlock()
			o := openers[u]
			if o == nil {
				return nil, media.Info{Total: -1}, fmt.Errorf("no file at %s", u)
			}
			return o.Open(ctx, off)
		},
		Repaint:    func() { r.repaints.Add(1) },
		Pref:       r.p.get,
		SetPref:    r.p.set,
		Experiment: exp,
		Theme:      func() *theme.Theme { return audioTheme },
	})
	t.Cleanup(r.c.CloseAll)
	return r
}

func (r *audioRig) urls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, u := range r.opened {
		if len(out) == 0 || out[len(out)-1] != u {
			out = append(out, u)
		}
	}
	return out
}

func audioClick(c *Controller, r *Row, f *richtext.File) {
	c.Click(r.pane, r.ch, r.ts, f.ID)
}

// Audio files are the player's with Go audio alone, videos aren't (it
// plays no video), and an audio file with no URL stays a plain card.
func TestAudioTiles(t *testing.T) {
	a := audioEnv(t, nil, nil)
	if a.c.Available() {
		t.Error("videos play in place with Go audio alone")
	}
	if !a.c.AudioAvailable() {
		t.Fatal("no audio player with Go audio")
	}
	r := a.c.Row("channel", "C1", "1.0", nil)
	if !r.MediaTile(clipFile(), 1, false) || !r.MediaTile(mp3File(), 1, false) {
		t.Error("an audio file isn't the player's")
	}
	if r.MediaTile(videoFile("F3"), 1, false) {
		t.Error("a video is a player's with Go audio alone")
	}
	none := mp3File()
	none.URL = ""
	if r.MediaTile(none, 1, false) {
		t.Error("an audio file with no URL is a player's")
	}
}

// The clip's first click mounts its player playing the aac rendition;
// Go audio has no AAC, so it plays url_private (its own webm) instead:
// the file's 3 s once its metadata came, not duration_ms's 12 s. The next
// click pauses it, the next plays on, and it plays to its end.
func TestAudioClipPlaysAndFallsBack(t *testing.T) {
	a := audioEnv(t, nil, map[string][]byte{clipAAC: mediatest.TonesAAC, clipURL: mediatest.TonesWebM})
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	f := clipFile()
	frame(c, [2]any{r, f})
	if st := state(t, c, r, f); st.Mounted || st.Duration != 12*time.Second {
		t.Fatalf("before the click: %+v (want no player and duration_ms's 12 s)", st)
	}
	audioClick(c, r, f)
	waitFor(t, "playing", 10*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	st := state(t, c, r, f)
	switch got := a.urls(); {
	case goaudio.DecodesAAC(): // Media Foundation's decoder: the aac rendition plays
		if st.Alt || len(got) != 1 || got[0] != clipAAC {
			t.Errorf("read %v (url_private: %v), want the aac rendition alone", got, st.Alt)
		}
	case !st.Alt:
		t.Error("playing without having fallen back to url_private")
	case len(got) < 2 || got[0] != clipAAC || got[len(got)-1] != clipURL:
		t.Errorf("read %v, want the aac rendition, then url_private", got)
	}
	if st.Duration < 2950*time.Millisecond || st.Duration > 3050*time.Millisecond {
		t.Errorf("duration %v, want the file's 3 s", st.Duration)
	}
	if st.Err != 0 {
		t.Errorf("error %d after the fallback", st.Err)
	}
	audioClick(c, r, f)
	waitFor(t, "paused", 5*time.Second, func() bool { return !state(t, c, r, f).Playing })
	at := state(t, c, r, f).Player.State().Pos
	audioClick(c, r, f)
	waitFor(t, "playing again", 5*time.Second, func() bool { return state(t, c, r, f).Playing })
	if p := state(t, c, r, f).Player.State().Pos; p < at {
		t.Errorf("played on from %v, paused at %v", p, at)
	}
	waitFor(t, "the end", 8*time.Second, func() bool {
		st := state(t, c, r, f)
		return !st.Playing && st.Player != nil && st.Player.State().Ended
	})
}

// An mp3 with no aac rendition plays its url_private.
func TestAudioMP3Plays(t *testing.T) {
	a := audioEnv(t, nil, map[string][]byte{mp3URL: mediatest.TonesMP3})
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	f := mp3File()
	frame(c, [2]any{r, f})
	audioClick(c, r, f)
	waitFor(t, "playing", 10*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	if st := state(t, c, r, f); st.Alt || st.Duration < 2950*time.Millisecond || st.Duration > 3100*time.Millisecond {
		t.Errorf("playing: %+v", st)
	}
}

// media_playback_speed (Cv's .25 to 2) is the clip's playbackRate (an
// audio message), not the mp3 card's unless transcribe_opt_in_ui_v2 is on;
// a value outside Cv is 1. At 2x the clip plays in half its time.
func TestAudioSpeedPref(t *testing.T) {
	files := map[string][]byte{clipAAC: mediatest.TonesAAC, clipURL: mediatest.TonesWebM, mp3URL: mediatest.TonesMP3}
	a := audioEnv(t, nil, files)
	a.p.set("media_playback_speed", 2)
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	clip, mp3 := clipFile(), mp3File()
	frame(c, [2]any{r, clip}, [2]any{r, mp3})
	audioClick(c, r, clip)
	waitFor(t, "the clip playing", 10*time.Second, func() bool { st := state(t, c, r, clip); return st.Playing && !st.Loading })
	t0 := time.Now()
	if st := state(t, c, r, clip); st.Rate != 2 {
		t.Errorf("the clip's rate %v, want the pref's 2 (also after the fallback to url_private: %v)", st.Rate, st.Alt)
	}
	waitFor(t, "the clip's end", 8*time.Second, func() bool {
		st := state(t, c, r, clip)
		return st.Player != nil && st.Player.State().Ended
	})
	if took := time.Since(t0); took > 2800*time.Millisecond {
		t.Errorf("3 s at 2x took %v", took)
	}
	audioClick(c, r, mp3)
	waitFor(t, "the mp3 mounted", 5*time.Second, func() bool { return state(t, c, r, mp3).Mounted })
	if st := state(t, c, r, mp3); st.Rate != 1 {
		t.Errorf("the mp3 card's rate %v, want 1 (the pref is an audio message's)", st.Rate)
	}

	b := audioEnv(t, func(n string) string {
		if n == "transcribe_opt_in_ui_v2" {
			return "on"
		}
		return "off"
	}, files)
	b.p.set("media_playback_speed", "1.25")
	r2 := b.c.Row("channel", "C1", "2.0", nil)
	frame(b.c, [2]any{r2, mp3})
	audioClick(b.c, r2, mp3)
	waitFor(t, "the mp3 mounted", 5*time.Second, func() bool { return state(t, b.c, r2, mp3).Mounted })
	if st := state(t, b.c, r2, mp3); st.Rate != 1.25 {
		t.Errorf("with transcribe_opt_in_ui_v2 the card's rate is %v, want the pref's 1.25", st.Rate)
	}
	b.p.set("media_playback_speed", 3)
	b.c.mu.Lock()
	got := b.c.prefSpeedLocked()
	b.c.mu.Unlock()
	if got != 1 {
		t.Errorf("media_playback_speed 3 (not one of Cv) reads %v, want 1", got)
	}
}

// The scrubber's mousedown (setTime) on a player not mounted yet mounts
// one there, playing: the last second plays and the clip ends. The track
// maps the pointer to the time: its middle is half the duration.
func TestAudioScrubPlaysFromThere(t *testing.T) {
	a := audioEnv(t, nil, map[string][]byte{mp3URL: mediatest.TonesMP3})
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	f := mp3File()
	layoutAt(c, r, f, image.Pt(426, 92))
	st := state(t, c, r, f)
	if st.AudioScrub.Empty() {
		t.Fatal("no scrubber laid out")
	}
	c.mu.Lock()
	tl := c.tiles[r.key(f)]
	z := tl.au.z
	mid := image.Pt((z.track.Min.X+z.track.Max.X)/2, (z.track.Min.Y+z.track.Max.Y)/2)
	if got := z.timeAt(mid, 3*time.Second); got < 1450*time.Millisecond || got > 1550*time.Millisecond {
		t.Errorf("the track's middle is %v of 3 s", got)
	}
	c.audioSeekLocked(tl, 2*time.Second)
	c.mu.Unlock()
	waitFor(t, "playing", 10*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	t0 := time.Now()
	if p := state(t, c, r, f).Player.State().Pos; p < 1950*time.Millisecond {
		t.Errorf("playing from %v, want the scrubber's 2 s", p)
	}
	waitFor(t, "the end", 6*time.Second, func() bool {
		st := state(t, c, r, f)
		return st.Player != nil && st.Player.State().Ended
	})
	if took := time.Since(t0); took > 2500*time.Millisecond {
		t.Errorf("the last second took %v", took)
	}
}

// The time shown (eW) is the duration until the player was used, then
// the position.
func TestAudioShownTime(t *testing.T) {
	a := audioEnv(t, nil, map[string][]byte{clipAAC: mediatest.TonesAAC, clipURL: mediatest.TonesWebM})
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	f := clipFile()
	frame(c, [2]any{r, f})
	c.mu.Lock()
	tl := c.tiles[r.key(f)]
	if tl.au == nil {
		tl.au = &audioState{}
	}
	before := tl.shownTime()
	c.mu.Unlock()
	if before != 12*time.Second {
		t.Errorf("before a click the time shows %v, want duration_ms's 12 s", before)
	}
	audioClick(c, r, f)
	c.mu.Lock()
	after := tl.shownTime()
	c.mu.Unlock()
	if after >= time.Second {
		t.Errorf("after the click the time shows %v, want the position (about 0), not the duration", after)
	}
}

// Both styles as Slack lays them out (1 px a dp): the clip's flat row
// (426×60: the 34px play button in the 12px padding and 1px border, the
// waveform scrubber 12px after it, the 32px More at the right) and the
// card (426×92: the 36px play button, then the 18px scrubber row 12px
// below it).
func TestAudioLayout(t *testing.T) {
	a := audioEnv(t, nil, nil)
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	clip, mp3 := clipFile(), mp3File()
	layoutAt(c, r, clip, image.Pt(426, 60))
	c.mu.Lock()
	z := c.tiles[r.key(clip)].au.z
	c.mu.Unlock()
	if z.play != image.Rect(13, 13, 47, 47) {
		t.Errorf("the clip's play button %v, want (13,13)-(47,47)", z.play)
	}
	if z.scrub.Min.X != 59 || z.scrub.Dy() != 34 || z.scrub.Dx() < 200 {
		t.Errorf("the clip's scrubber %v", z.scrub)
	}
	if z.more.Dx() != 32 || z.more.Max.X != 413 {
		t.Errorf("the clip's More %v, want 32px at the right", z.more)
	}
	if z.speed.Empty() || z.speed.Max.X > z.more.Min.X-8 || z.speed.Dy() != 28 {
		t.Errorf("the clip's speed control %v (More %v)", z.speed, z.more)
	}
	if !z.transcript.Empty() {
		t.Errorf("a transcript button %v with no team pref", z.transcript)
	}
	layoutAt(c, r, mp3, image.Pt(426, 92))
	c.mu.Lock()
	z = c.tiles[r.key(mp3)].au.z
	c.mu.Unlock()
	if z.play != image.Rect(13, 13, 49, 49) {
		t.Errorf("the card's play button %v, want (13,13)-(49,49)", z.play)
	}
	if z.scrub.Min != image.Pt(13, 61) || z.scrub.Dy() != 18 || z.scrub.Dx() < 300 {
		t.Errorf("the card's scrubber %v, want from (13,61), 18px tall", z.scrub)
	}
	if !z.speed.Empty() || !z.more.Empty() {
		t.Errorf("the card has a speed control %v or More %v without transcribe_opt_in_ui_v2", z.speed, z.more)
	}
}

// The player's source: the demuxer from the URL's extension (the aac
// rendition is an m4a), else the filetype.
func TestAudioSource(t *testing.T) {
	f := clipFile()
	for _, tc := range []struct {
		u         string
		ext, mime string
	}{
		{clipAAC, "m4a", "audio/mp4"},
		{clipURL, "webm", "audio/webm"},
		{mp3URL, "mp3", "audio/mpeg"},
		{"https://files.slack.com/files-tmb/T1-F0CLIP-aac/download", "m4a", "audio/mp4"}, // the aac rendition with no extension
		{"https://files.slack.com/files-pri/T1-F0CLIP/download", "webm", "audio/webm"},   // url_private: the filetype
	} {
		u := tc.u
		g := *f
		switch u {
		case "https://files.slack.com/files-tmb/T1-F0CLIP-aac/download":
			g.MediaURL = u
		case "https://files.slack.com/files-pri/T1-F0CLIP/download":
			g.URL = u
		}
		src := audioSource(&g, u)
		if src.Ext != tc.ext || src.Mime != tc.mime || src.FileID != f.ID {
			t.Errorf("%s: %+v, want %s %s", u, src, tc.ext, tc.mime)
		}
	}
}

// A playing clip repaints with every time update, the pointer elsewhere:
// its time and its waveform's fill move on (the review of c38b91b8: they
// froze once the pointer left, the repaint being the video overlay's,
// which shows only while hovered).
func TestAudioRepaintsWhilePlaying(t *testing.T) {
	a := audioEnv(t, nil, map[string][]byte{clipAAC: mediatest.TonesAAC, clipURL: mediatest.TonesWebM})
	c := a.c
	r := c.Row("channel", "C1", "1.0", nil)
	f := clipFile()
	frame(c, [2]any{r, f})
	audioClick(c, r, f)
	waitFor(t, "playing", 10*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	if st := state(t, c, r, f); st.Hover {
		t.Fatal("hovered: the test needs the pointer elsewhere")
	}
	before := a.repaints.Load()
	time.Sleep(900 * time.Millisecond) // three of the 250 ms time updates at least
	if n := a.repaints.Load() - before; n < 2 {
		t.Errorf("%d repaints in 900 ms of playing, want one a time update", n)
	}
}

// A playing clip never docks into the mini player (Slack's message
// lists give FileAudio no onOpenMiniPlayer): scrolled under 10% it plays
// on; its message gone, it stops.
func TestAudioDoesNotDock(t *testing.T) {
	a := audioEnv(t, func(string) string { return "on" }, map[string][]byte{clipAAC: mediatest.TonesAAC, clipURL: mediatest.TonesWebM})
	c := a.c
	if !c.PipOn() {
		t.Fatal("clips_pip is off: the test needs it on")
	}
	r := c.Row("channel", "C1", "1.0", nil)
	f := clipFile()
	frame(c, [2]any{r, f})
	audioClick(c, r, f)
	waitFor(t, "playing", 10*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	r.MediaVisible(f, 0.05) // the observer's first report: skipped
	r.MediaVisible(f, 0.5)
	r.MediaVisible(f, 0.05)
	if _, m, ok := c.Pip(); ok {
		t.Fatalf("scrolled away while playing, the clip docked: %+v", m)
	}
	time.Sleep(100 * time.Millisecond)
	if !state(t, c, r, f).Playing {
		t.Error("scrolled away, the clip stopped (it plays on)")
	}
	frame(c) // another conversation
	if _, m, ok := c.Pip(); ok {
		t.Fatalf("its message gone while playing, the clip docked: %+v", m)
	}
	waitFor(t, "the clip paused with its message gone", 5*time.Second, func() bool { return !state(t, c, r, f).Playing })
}
