package ui

// The inline video player (internal/ui/videoplayer) in the App, driven
// through Gio's router over fakeslack's #files-gallery: its "video"
// message (F0FILESVID01, which fakeslack serves from mediatest's fixture
// with ranges) on the fake backend (media.NewFake). Slack's VideoPlayer
// (fIJO): the click, the pause when it scrolls away, the release once
// its conversation is gone, and the mute and volume prefs.

import (
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// fakeVideos plays videos on the fake backend for the test.
func fakeVideos(t *testing.T) {
	t.Helper()
	prev := videoBackend
	videoBackend = media.NewFake(media.FakeOptions{Log: func(string, ...any) {}})
	t.Cleanup(func() { videoBackend = prev })
}

// videoTile waits for message ts's video tile on screen (window
// coordinates).
func (e *perfEnv) videoTile(t *testing.T, ts string) messages.FileRect {
	t.Helper()
	var out messages.FileRect
	e.until("the video's tile", 15*time.Second, func() bool {
		for _, r := range rectsOf(e.channelFiles(fakeslack.FilesChannelID), ts) {
			if r.File.IsVideo {
				out = r
				return true
			}
		}
		return false
	})
	return out
}

// videoTileInView scrolls message ts's video tile wholly into the
// channel's list (videoScrollStep) and returns it.
func (e *perfEnv) videoTileInView(t *testing.T, ts string) messages.FileRect {
	t.Helper()
	for i := 0; ; i++ {
		var l channelList
		if c := e.a.convos[fakeslack.FilesChannelID+"/"]; c != nil {
			l, _ = c.list.(channelList)
		}
		if l.ChannelView == nil {
			t.Fatal("no channel list")
		}
		tile, list, in := videoScrollStep(l, ts)
		if in {
			for _, r := range rectsOf(l.FileRects(), ts) {
				if r.File.IsVideo {
					return r
				}
			}
		}
		if i == 60 {
			t.Fatalf("the video's tile %v isn't wholly in the list %v", tile, list)
		}
		e.frame()
		e.frame()
	}
}

// videoState is message ts's player tile in the channel.
func (e *perfEnv) videoState(ts string) (videoplayer.State, bool) {
	if e.a.vids == nil {
		return videoplayer.State{}, false
	}
	return e.a.vids.TileState("channel", fakeslack.FilesChannelID, ts, fakeslack.FilesVideoID)
}

func (e *perfEnv) videoHover(p image.Point, at time.Duration) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: at})
}

func mid(r image.Rectangle) image.Point { return r.Min.Add(r.Max).Div(2) }

// A lone video in a message plays in place (fIJO's tE): there's no
// player until the click, which mounts one with autoplay and the spinner
// until it plays; it plays the file (the fixture's 3 s, its time
// running); the next click pauses it and the next plays on from there. A
// gallery's video stays as it was (Slack opens the media viewer there).
func TestVideoInlinePlay(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	if e.a.vids == nil || !e.a.vids.Available() {
		t.Fatal("no inline players: the list didn't ask, or the fake backend isn't in use")
	}
	if n := e.a.vids.Mounted(); n != 0 {
		t.Fatalf("%d players before any click (Slack mounts no <video> until then)", n)
	}
	if st, ok := e.videoState(m.TS); !ok || st.Mounted {
		t.Fatalf("the tile isn't the player's, or mounted early: %+v %v", st, ok)
	}
	clk := time.Second
	e.clickAt(mid(tile.R), clk)
	e.until("a player", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Mounted })
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })
	st, _ := e.videoState(m.TS)
	if st.Duration < 2950*time.Millisecond || st.Duration > 3050*time.Millisecond {
		t.Errorf("duration %v, want the file's own 3 s once its metadata came", st.Duration)
	}
	e.settle(600 * time.Millisecond)
	if p := st.Player.State().Pos; p < 300*time.Millisecond {
		t.Errorf("at %v after 600 ms of playing", p)
	}
	clk += 500 * time.Millisecond
	e.clickAt(mid(tile.R), clk)
	e.until("paused", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	paused := st.Player.State().Pos
	clk += 500 * time.Millisecond
	e.clickAt(mid(tile.R), clk)
	e.until("playing again", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing })
	if p := st.Player.State().Pos; p < paused {
		t.Errorf("played on from %v, paused at %v", p, paused)
	}
	if n := e.a.vids.Mounted(); n != 1 {
		t.Errorf("%d players for one video", n)
	}
	g := e.filesMsg("image and video")
	if _, ok := e.a.vids.TileState("channel", fakeslack.FilesChannelID, g.TS, fakeslack.FilesVideoID); ok {
		t.Error("the image-and-video gallery's video is a player's: Slack opens the media viewer from a gallery")
	}
}

// A playing video scrolled out of the list docks into the mini player
// (clips_pip, as the test account has it: YPBt), the inline player
// paused, and doesn't play on by itself when it's back.
func TestVideoVisibilityPause(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	ch := fakeslack.FilesChannelID
	m := e.filesMsg("video")
	e.a.jumpToMessage(ch, m.TS, "", false)
	e.settle(800 * time.Millisecond)
	tile := e.videoTileInView(t, m.TS)
	clk := time.Second
	e.clickAt(mid(tile.R), clk)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing })
	for i := 0; i < 12; i++ { // far up the channel: the video's row leaves the list
		e.scroll(e.listPoint(), -600)
		e.frame()
	}
	e.until("paused off the screen", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	if _, ok := pipShown(e); !ok {
		t.Error("scrolled away while playing, it didn't dock")
	}
	if rs := rectsOf(e.channelFiles(ch), m.TS); len(rs) > 0 {
		view := image.Rectangle{Min: e.a.primaryOrigin, Max: e.size}
		if in := rs[0].R.Intersect(view); in.Dx()*in.Dy()*10 >= rs[0].R.Dx()*rs[0].R.Dy() {
			t.Fatalf("the tile is still on screen at %v", rs[0].R)
		}
	}
	e.a.jumpToMessage(ch, m.TS, "", false)
	e.settle(800 * time.Millisecond)
	if st, _ := e.videoState(m.TS); st.Playing {
		t.Error("back on screen, it played on by itself")
	}
}

// A paused video whose conversation the window leaves lets its player go
// after 10 s, as React unmounting its row does (a playing one docks into
// the mini player: TestVideoPipDocks); the tile then shows the poster with
// the time played, and a new player starts there.
func TestVideoUnmountReleases(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	ch := fakeslack.FilesChannelID
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	e.clickAt(mid(tile.R), time.Second)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing })
	e.settle(500 * time.Millisecond)
	e.clickAt(mid(tile.R), 2*time.Second) // paused: it doesn't dock
	e.until("paused", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	other := ""
	for _, id := range e.fs.ChannelIDs() {
		if id != ch {
			other = id
			break
		}
	}
	if other == "" {
		t.Fatal("no other channel to open")
	}
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	e.until("paused once its conversation is gone", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	if e.a.vids.Mounted() != 1 {
		t.Fatalf("%d players right after leaving, want the paused one kept a while", e.a.vids.Mounted())
	}
	time.Sleep(10500 * time.Millisecond) // unmountAfter
	e.frame()
	if n := e.a.vids.Mounted(); n != 0 {
		t.Fatalf("%d players 10.5 s after the conversation went", n)
	}
	p, ok := e.a.vids.Progress(fakeslack.FilesVideoID)
	if !ok || p <= 0 {
		t.Fatalf("no progress kept (%v, %v)", p, ok)
	}
	e.openChannel(ch)
	tile = e.videoTile(t, m.TS)
	e.settle(300 * time.Millisecond)
	st, ok := e.videoState(m.TS)
	if !ok || st.Mounted || !st.Played || st.Elapsed != p {
		t.Errorf("back in the conversation: %+v %v, want the poster showing the time played, %v", st, ok, p)
	}
	// the next click's player starts there (mountLocked's Seek)
	e.clickAt(mid(tile.R), 20*time.Second)
	e.until("a new player playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })
	st, _ = e.videoState(m.TS)
	if pos := st.Player.State().Pos; pos < p-100*time.Millisecond || pos > p+900*time.Millisecond {
		t.Errorf("the new player starts at %v, the video was left at %v", pos, p)
	}
}

// Mute and volume are Slack's media_muted and media_volume prefs (lGYe):
// the mute button's click writes media_muted, and the volume slider's,
// unmuting, writes media_muted false and then media_volume, at the press
// (X7vT's onChange comes before its onMouseDown). The slider opens once
// the pointer has rested on the mute .16 s.
func TestVideoPrefs(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	var mu sync.Mutex
	var sets []string
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.prefs.set" && strings.HasPrefix(f.Get("name"), "media_") {
			mu.Lock()
			sets = append(sets, f.Get("name")+"="+f.Get("value"))
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	sent := func(want string) func() bool {
		return func() bool {
			mu.Lock()
			defer mu.Unlock()
			for _, s := range sets {
				if s == want {
					return true
				}
			}
			return false
		}
	}
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	clk := time.Second
	e.videoHover(mid(tile.R), clk)
	e.frame()
	e.frame()
	st, _ := e.videoState(m.TS)
	if !st.Hover || st.Mute.Empty() {
		t.Fatalf("hovered: %+v, want the controls shown", st)
	}
	mute := tile.R.Min.Add(mid(st.Mute))
	clk += 300 * time.Millisecond
	e.clickAt(mute, clk)
	e.until("users.prefs.set media_muted=true", 10*time.Second, sent("media_muted=true"))
	clk += 300 * time.Millisecond
	e.videoHover(mute, clk) // over the control: its slider shows
	e.until("the volume slider", 3*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.VolumeOpen })
	st, _ = e.videoState(m.TS)
	if st.Slider.Empty() {
		t.Fatalf("no volume slider: %+v", st)
	}
	clk += 300 * time.Millisecond
	e.clickAt(tile.R.Min.Add(mid(st.Slider)), clk)
	e.until("users.prefs.set media_muted=false", 10*time.Second, sent("media_muted=false"))
	e.until("users.prefs.set media_volume=0.5", 10*time.Second, sent("media_volume=0.5"))
	e.until("unmuted at .5 once the prefs are in", 10*time.Second, func() bool {
		muted, vol := e.a.vids.Volume()
		return !muted && vol == 0.5
	})
}
