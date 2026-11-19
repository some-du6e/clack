package ui

// Round 3's review (the inline player at 0cbe7e63), the fixes' tests, on
// the fake backend. Their calls into what the fixes added go through
// video_review_hooks_test.go; the red run puts
// testdata/video_review_hooks_red.go.txt in its place on 0cbe7e63.

import (
	"image"
	"testing"
	"time"
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
