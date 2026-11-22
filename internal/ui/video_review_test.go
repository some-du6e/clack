package ui

// Round 3's review (the inline player at 0cbe7e63), the fixes' tests, on
// the fake backend. Their calls into what the fixes added go through
// video_review_hooks_test.go; the red run puts
// testdata/video_review_hooks_red.go.txt in its place on 0cbe7e63.

import (
	"image"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/media"
)

// A video's frames repaint the main window alone, never the huddle call
// window or the screen share popouts (MAJOR 2: they drew a frame each,
// and the main window's next frame wasn't quiet).
func TestVideoFramesSkipHuddleWindows(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	e.a.huddles() // the huddle controller is live
	fr := playerFrameReady(e)
	p0 := hudPings(e)
	for i := 0; i < 30; i++ {
		fr()
	}
	if d := hudPings(e) - p0; d != 0 {
		t.Errorf("%d of 30 video frames asked the huddle windows to draw", d)
	}
}

// The session ending (auth_failed) shows the sign-in screen and closes
// every player: nothing plays on, with its sound, behind it (MAJOR 3).
func TestVideoSessionEndClosesPlayers(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	e.clickAt(mid(tile.R), time.Second)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing })
	st, _ := e.videoState(m.TS)
	pl := st.Player
	e.a.sessionEnded()
	e.frame()
	e.until("every player closed behind the sign-in screen", 5*time.Second, func() bool {
		return e.a.vids == nil || e.a.vids.Mounted() == 0
	})
	// the player itself closed: its goroutine gone, its clock stopped
	if h, ok := pl.(*media.Handle); ok {
		select {
		case <-h.Done():
		case <-time.After(2 * time.Second):
			t.Error("the player wasn't closed: its handle's goroutine runs on")
		}
	}
	time.Sleep(200 * time.Millisecond)
	if p := pl.State(); !p.Paused && p.Pos > 0 {
		before := p.Pos
		time.Sleep(300 * time.Millisecond)
		if pl.State().Pos > before {
			t.Errorf("the player plays on behind the sign-in screen (%v, then %v)", before, pl.State().Pos)
		}
	}
}

// The api.accounts sign-in screen (switchAccount with no credentials)
// closes them too.
func TestVideoAccountSwitchClosesPlayers(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	e.clickAt(mid(tile.R), time.Second)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing })
	e.a.showLogin()
	e.frame()
	e.until("every player closed", 5*time.Second, func() bool { return e.a.vids == nil || e.a.vids.Mounted() == 0 })
}

// Where the viewer is in a video goes to Slack with media.progress.update
// (gnNF's saveMediaProgress, as gBWG times it): a pause sends it at once,
// with the file, offset_ms and duration_ms (MAJOR 5: it was only kept in
// memory).
func TestVideoProgressSentToSlack(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	var mu sync.Mutex
	var sent []url.Values
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "media.progress.update" {
			mu.Lock()
			sent = append(sent, f)
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	e.clickAt(mid(tile.R), time.Second)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })
	e.settle(1200 * time.Millisecond)
	e.clickAt(mid(tile.R), 2*time.Second)
	e.until("paused", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	e.until("media.progress.update after the pause", 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sent) > 0
	})
	mu.Lock()
	f := sent[len(sent)-1]
	mu.Unlock()
	off, _ := strconv.Atoi(f.Get("offset_ms"))
	dur, _ := strconv.Atoi(f.Get("duration_ms"))
	if f.Get("file") != "F0FILESVID01" || off < 500 || off > 2900 || dur < 2900 || dur > 3100 {
		t.Errorf("media.progress.update %v, want the video at about 1.2 s of its 3 s", f)
	}
}

// Paused within a second of its end, the progress kept is 0 (gnNF), so
// the next player starts over (MAJOR 5's port: before it, the pause kept
// the time as it was).
func TestVideoProgressNearEndIsZero(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	e.clickAt(mid(tile.R), time.Second)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })
	e.until("2.1 s in", 10*time.Second, func() bool {
		st, _ := e.videoState(m.TS)
		return st.Player != nil && st.Player.State().Pos >= 2100*time.Millisecond
	})
	e.clickAt(mid(tile.R), 5*time.Second)
	e.until("paused", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	st, _ := e.videoState(m.TS)
	if ps := st.Player.State(); ps.Ended || ps.Pos >= 3*time.Second {
		t.Skipf("the fixture ended before the pause (%v)", ps.Pos)
	}
	if p, ok := e.a.vids.Progress(fakeslack.FilesVideoID); !ok || p != 0 {
		t.Errorf("paused at %v, within a second of the end: progress %v (%v), want 0", st.Elapsed, p, ok)
	}
}

// filesList is the channel list of #files-gallery.
func (e *perfEnv) filesList(t *testing.T) channelList {
	t.Helper()
	var l channelList
	if c := e.a.convos[fakeslack.FilesChannelID+"/"]; c != nil {
		l, _ = c.list.(channelList)
	}
	if l.ChannelView == nil {
		t.Fatal("no channel list")
	}
	return l
}

// A playing video scrolled until about 5% of it shows in the list, its row
// still laid out, docks into the mini player (YPBt's 10% rule, with
// clips_pip as the test account has it): the inline player pauses; at
// about half it plays on. Scrolling the row out of the list
// (TestVideoVisibilityPause) docks it whether or not the share is
// measured; this needs the share.
func TestVideoTenPercentPause(t *testing.T) {
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	ch := fakeslack.FilesChannelID
	m := e.filesMsg("video")
	e.a.jumpToMessage(ch, m.TS, "", false)
	e.settle(800 * time.Millisecond)
	tile := e.videoTileInView(t, m.TS)
	l := e.filesList(t)
	// the tile and the share of it in the list, false once its row isn't
	// laid out
	at := func() (image.Rectangle, float64, bool) {
		for _, r := range rectsOf(l.FileRects(), m.TS) {
			if r.File.IsVideo && r.R.Dy() > 0 {
				in := r.R.Intersect(l.ListRect())
				return r.R, float64(in.Dx()*in.Dy()) / float64(r.R.Dx()*r.R.Dy()), true
			}
		}
		return image.Rectangle{}, 0, false
	}
	// scrollTo moves the tile up across the list's top edge until about
	// share of it shows
	scrollTo := func(share float64) float64 {
		for i := 0; ; i++ {
			r, got, ok := at()
			if !ok {
				t.Fatalf("the video's row left the list on the way to %.0f%%", share*100)
			}
			if got > share-0.025 && got < share+0.025 {
				return got
			}
			if i == 20 {
				t.Fatalf("the tile at %v shows %.1f%%, want about %.0f%%", r, got*100, share*100)
			}
			want := int(share * float64(r.Dy())) // the rows of it to show
			l.ScrollPx(r.Min.Y - l.ListRect().Min.Y + r.Dy() - want)
			e.frame()
			e.frame()
		}
	}
	e.clickAt(mid(tile.R), time.Second)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })
	half := scrollTo(0.5)
	e.settle(250 * time.Millisecond)
	if st, _ := e.videoState(m.TS); !st.Playing {
		t.Fatalf("paused with %.0f%% of it showing: above 10%% it plays on", half*100)
	}
	five := scrollTo(0.05)
	e.until("paused", 3*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	st, _ := e.videoState(m.TS)
	if !st.Mounted {
		t.Fatal("the player closed")
	}
	if ps := st.Player.State(); ps.Ended {
		t.Fatalf("the fixture ended at %.0f%% showing, not paused by the rule", five*100)
	}
	if _, _, ok := at(); !ok {
		t.Error("the row left the list: the pause may be EndFrame's, not the 10% rule's")
	}
	if p, ok := pipShown(e); !ok || p.trigger != "scroll" {
		t.Errorf("under 10%% visible while playing: the mini player %+v (%v), want it docked by the scroll", p, ok)
	}
}

// filesEnvWith is filesEnv with before run on the fake before the client
// starts (a hook from boot on).
func filesEnvWith(t *testing.T, size image.Point, before func(*fakeslack.Server)) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	before(fs)
	e := startEnv(t, fs, t.TempDir())
	e.size = size
	e.waitReady()
	e.openChannel(fakeslack.FilesChannelID)
	n := len(fs.FilesCases())
	e.until("the #files-gallery history", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.FilesChannelID) >= n })
	e.settle(1500 * time.Millisecond)
	return e
}

// No media_muted or media_volume write without the viewer's press, from
// boot on: through a hover, the click that mounts and plays the video, a
// pause, scrolling it away and back, leaving the conversation and coming
// back, and a media_volume of 0.3 set elsewhere (pref_change), which the
// slider and the player take with nothing written. Then the mute's click
// and the slider's give exactly media_muted=true, media_muted=false,
// media_volume=0.5, and the mounted player follows each.
func TestVideoPrefsNoUnpromptedWrites(t *testing.T) {
	keepIdlePlayers(t) // paused a while: the player checked is the one mounted
	fakeVideos(t)
	var mu sync.Mutex
	var sets []string
	e := filesEnvWith(t, image.Pt(1440, 900), func(fs *fakeslack.Server) {
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			if method == "users.prefs.set" && strings.HasPrefix(f.Get("name"), "media_") {
				mu.Lock()
				sets = append(sets, f.Get("name")+"="+f.Get("value"))
				mu.Unlock()
			}
			return nil, false
		})
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	written := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sets...)
	}
	none := func(step string) {
		t.Helper()
		if w := written(); len(w) != 0 {
			t.Fatalf("%s: prefs written without a mute or volume press: %v", step, w)
		}
	}
	ch := fakeslack.FilesChannelID
	m := e.filesMsg("video")
	player := func(step string, muted bool, vol float64) {
		t.Helper()
		st, _ := e.videoState(m.TS)
		if st.Player == nil {
			t.Fatalf("%s: no player", step)
		}
		if ps := st.Player.State(); ps.Muted != muted || ps.Volume != vol {
			t.Errorf("%s: the player is muted %v at %v, want muted %v at %v", step, ps.Muted, ps.Volume, muted, vol)
		}
	}
	e.a.jumpToMessage(ch, m.TS, "", false)
	e.settle(800 * time.Millisecond)
	tile := e.videoTileInView(t, m.TS)
	clk := time.Second
	e.videoHover(mid(tile.R), clk)
	e.frame()
	e.frame()
	none("hovered")
	clk += 300 * time.Millisecond
	e.clickAt(mid(tile.R), clk)
	e.until("playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })
	e.settle(300 * time.Millisecond)
	none("mounted and playing")
	player("playing", false, 1)
	clk += time.Second
	e.clickAt(mid(tile.R), clk)
	e.until("paused", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })
	none("paused")
	for i := 0; i < 12; i++ { // far up the channel: the video's row leaves the list
		e.scroll(e.listPoint(), -600)
		e.frame()
	}
	e.a.jumpToMessage(ch, m.TS, "", false)
	e.settle(800 * time.Millisecond)
	none("scrolled away and back")
	other := ""
	for _, id := range e.fs.ChannelIDs() {
		if id != ch {
			other = id
			break
		}
	}
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	e.openChannel(ch)
	e.a.jumpToMessage(ch, m.TS, "", false)
	e.settle(800 * time.Millisecond)
	none("left and back")
	player("back", false, 1)

	// media_volume set elsewhere: the slider and the player take it
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected && e.fs.Sockets() > 0 })
	e.fs.SetPrefs(map[string]any{"media_volume": 0.3})
	e.fs.Push(map[string]any{"type": "pref_change", "name": "media_volume", "value": 0.3})
	e.until("the slider at the account's 0.3", 15*time.Second, func() bool { _, v := e.a.vids.Volume(); return v == 0.3 })
	e.frame()
	none("media_volume changed elsewhere")
	player("media_volume changed elsewhere", false, 0.3)

	tile = e.videoTileInView(t, m.TS)
	clk += time.Second
	e.videoHover(mid(tile.R), clk)
	e.frame()
	e.frame()
	st, _ := e.videoState(m.TS)
	if st.Mute.Empty() {
		t.Fatalf("hovered: %+v, want the controls", st)
	}
	mute := tile.R.Min.Add(mid(st.Mute))
	clk += 300 * time.Millisecond
	e.clickAt(mute, clk)
	e.until("media_muted written", 10*time.Second, func() bool { return len(written()) >= 1 })
	e.frame()
	player("muted", true, 0.3)
	clk += 300 * time.Millisecond
	e.videoHover(mute, clk) // resting on the mute: the slider opens
	e.until("the slider open", 3*time.Second, func() bool { st, _ := e.videoState(m.TS); return volumeOpen(st) })
	st, _ = e.videoState(m.TS)
	clk += 300 * time.Millisecond
	e.clickAt(tile.R.Min.Add(mid(st.Slider)), clk)
	e.until("three writes", 10*time.Second, func() bool { return len(written()) >= 3 })
	e.settle(300 * time.Millisecond)
	want := []string{"media_muted=true", "media_muted=false", "media_volume=0.5"}
	if w := written(); !slices.Equal(w, want) {
		t.Errorf("the mute's click and the slider's wrote %v, want %v", w, want)
	}
	player("the slider's click", false, 0.5)
}
