package ui

// Round 4, step 2: the mini player (Slack's MediaPlayerPipContainer,
// yF7D, and YPBt's triggers) in the App, on the fake backend with
// clips_pip on as the test account has it. Its reads go through
// video_pip_hooks_test.go; files-shots runs this file on ui with
// testdata/video_pip_hooks_ui.go.txt in that file's place, where nothing
// docks: a video scrolled away or left behind pauses there.

import (
	"fmt"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// pipOther is a conversation other than #files-gallery.
func pipOther(e *perfEnv) string {
	for _, id := range e.fs.ChannelIDs() {
		if id != fakeslack.FilesChannelID {
			return id
		}
	}
	return ""
}

// pipDock plays #files-gallery's lone video, then opens another
// conversation while it plays: it docks (useNavigateDockTrigger).
func pipDock(t *testing.T, e *perfEnv, clk *time.Duration) (ts string, p pipView) {
	t.Helper()
	m := e.filesMsg("video")
	tile := vvTile(t, e, m.TS)
	*clk += time.Second
	e.clickAt(vvMid(tile), *clk)
	e.until("the inline video playing", 10*time.Second, func() bool { p, _, ok := inlineState(e, m.TS); return ok && p })
	e.settle(400 * time.Millisecond)
	e.openChannel(pipOther(e))
	e.until("the mini player playing", 10*time.Second, func() bool { v, ok := pipShown(e); return ok && v.playing && !v.rect.Empty() })
	p, _ = pipShown(e)
	return m.TS, p
}

// tileShare is how much of message ts's video tile the channel list shows
// (its rects aren't clipped to it): what the scroll dock trigger judges.
// -1: no tile.
func tileShare(e *perfEnv, ts string) float64 {
	var l channelList
	if c := e.a.convos[fakeslack.FilesChannelID+"/"]; c != nil {
		l, _ = c.list.(channelList)
	}
	if l.ChannelView == nil {
		return -1
	}
	list := l.ListRect()
	for _, r := range l.FileRects() {
		if r.TS == ts && r.File.IsVideo && !r.R.Empty() {
			in := r.R.Intersect(list)
			return float64(in.Dx()*in.Dy()) / float64(r.R.Dx()*r.R.Dy())
		}
	}
	return -1
}

// rowTop is message ts's row top under the channel list's top, and
// whether the list shows its newest row (at its end, where Gio's List, as
// Slack's bracketScrollTop, holds a jump's message lower than its anchor).
func rowTop(e *perfEnv, ts string) (off int, atEnd, ok bool) {
	var l channelList
	if c := e.a.convos[fakeslack.FilesChannelID+"/"]; c != nil {
		l, _ = c.list.(channelList)
	}
	if l.ChannelView == nil {
		return 0, false, false
	}
	list := l.ListRect()
	for _, r := range l.RowRects() {
		if r.TS == ts {
			off, ok = r.R.Min.Y-list.Min.Y, true
		}
	}
	_, atEnd = l.NewestRowBottom()
	return off, atEnd, ok
}

func pipPress(e *perfEnv, p image.Point, k pointer.Kind, at time.Duration) {
	var btn pointer.Buttons
	if k != pointer.Release {
		btn = pointer.ButtonPrimary
	}
	e.r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Buttons: btn, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: at})
	e.frame()
	e.frame()
}

// A video playing when its conversation goes docks into the window's top
// right, 360 wide (the window under 1680) and 16:9 as the file is, with
// Expand, Jump to message and Close; the inline player shows only its
// pill and pauses.
func TestVideoPipDocks(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	ts, p := pipDock(t, e, &clk)
	if p.trigger != "navigate" || p.corner != "top-right" {
		t.Errorf("docked by %q at %q, want navigate, top-right", p.trigger, p.corner)
	}
	if p.rect.Dx() != 360 || p.rect.Max.X != 1440-20 || p.rect.Min.Y != 56 || p.rect.Dy() < 150 {
		t.Errorf("the shell is %v, want 360 wide at 20px from the right, 56 down (top-right)", p.rect)
	}
	if h := p.rect.Dy(); h < 200 || h > 206 {
		t.Errorf("the shell is %d tall, want the 16:9 video's 202-203", h)
	}
	if len(p.controls) != 3 {
		t.Errorf("%d controls, want Expand, Jump to message and Close", len(p.controls))
	}
	if playing, _, _ := inlineState(e, ts); playing {
		t.Error("the inline player plays on behind the mini player")
	}
	if !inlineInPip(e, ts) {
		t.Error("the inline tile doesn't show the mini player has its video")
	}
}

// Close: the mini player goes, and the inline player takes its time,
// paused, once it's laid out again (useResumeAfterPipClose runs as the
// VideoPlayer mounts).
func TestVideoPipClose(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	ts, p := pipDock(t, e, &clk)
	e.settle(300 * time.Millisecond)
	p, _ = pipShown(e)
	at := p.elapsed
	clk += time.Second
	e.clickAt(vvMid(p.controls[len(p.controls)-1]), clk)
	e.until("the mini player closed", 5*time.Second, func() bool { _, ok := pipShown(e); return !ok })
	e.openChannel(fakeslack.FilesChannelID)
	vvTile(t, e, ts)
	e.settle(300 * time.Millisecond)
	playing, el, ok := inlineState(e, ts)
	if !ok || playing || el < at-100*time.Millisecond {
		t.Errorf("after Close the inline player: playing %v at %v (%v), want paused at the mini player's %v", playing, el, ok, at)
	}
	if inlineInPip(e, ts) {
		t.Error("the inline tile still shows the mini player")
	}
}

// Jump to message: the conversation opens at the message and its player
// plays on from the mini player's time.
func TestVideoPipJump(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	ts, p := pipDock(t, e, &clk)
	at := p.elapsed
	clk += time.Second
	e.clickAt(vvMid(p.controls[1]), clk)
	// the tile's share of the list in the frames after the jump, as it
	// changed (a crossing under 10% after it was seen docks it again)
	var shares []string
	share := func() {
		if v := fmt.Sprintf("%.2f", tileShare(e, ts)); len(shares) == 0 || shares[len(shares)-1] != v {
			shares = append(shares, v)
		}
	}
	for dl := time.Now().Add(10 * time.Second); ; {
		share()
		if pl, _, ok := inlineState(e, ts); ok && pl {
			break
		}
		if time.Now().After(dl) {
			pl, el, ok := inlineState(e, ts)
			v, pip := pipShown(e)
			t.Fatalf("after Jump to message the inline player: playing %v at %v (%v); route %q; mini player %v %+v; the tile's share of the list %v",
				pl, el, ok, e.a.router.Current(), pip, v, shares)
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	// it plays on there: not docked again as the list settles
	for i := 0; i < 20; i++ {
		e.frame()
		share()
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("the tile's share of the list in the frames after the jump: %v", shares)
	// it shows the message as Slack's jump does: its top ANCHOR_OFFSET
	// (64dp) under the list's top, or lower at the list's end, the video
	// wholly on screen (two rows above it left 4% of the tile there)
	sc := e.scale
	if sc == 0 {
		sc = 1
	}
	want := int(64*sc + 0.5)
	if off, atEnd, ok := rowTop(e, ts); !ok || off < want-1 || (off > want+1 && !atEnd) {
		t.Errorf("after Jump to message the message's top is %dpx under the list's top (%v; the list at its end %v), want %d", off, ok, atEnd, want)
	}
	if sh := tileShare(e, ts); sh < 1 {
		t.Errorf("after Jump to message %.2f of the video is on screen, want all of it; its share in the frames after the jump %v", sh, shares)
	}
	if v, ok := pipShown(e); ok {
		t.Errorf("the mini player stayed after Jump to message, or docked again (%q); the tile's share of the list %v", v.trigger, shares)
	}
	if _, el, _ := inlineState(e, ts); el < at-100*time.Millisecond {
		t.Errorf("the inline player plays from %v, the mini player was at %v", el, at)
	}
	if r := string(e.a.router.Current()); !strings.HasPrefix(r, "c/"+fakeslack.FilesChannelID) {
		t.Errorf("Jump to message went to %q, want #files-gallery", r)
	}
}

// Expand: the media viewer at the mini player's time, playing; the mini
// player closes.
func TestVideoPipExpand(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	_, p := pipDock(t, e, &clk)
	at := p.elapsed
	clk += time.Second
	e.clickAt(vvMid(p.controls[0]), clk)
	e.until("the viewer playing the video", 10*time.Second, func() bool {
		st, ok := viewerState(e)
		return ok && st.video && st.file == vvVideoID && st.playing
	})
	if _, ok := pipShown(e); ok {
		t.Error("the mini player stayed after Expand")
	}
	if st, _ := viewerState(e); st.elapsed < at-200*time.Millisecond {
		t.Errorf("the viewer started at %v, the mini player was at %v", st.elapsed, at)
	}
}

// A drag past 5px moves the shell, the release snaps it to the corner of
// the window's quarter the pointer is in, and the press's click doesn't
// reach the video; with the focus the arrow keys move it between corners.
func TestVideoPipMoves(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	_, p := pipDock(t, e, &clk)
	from := vvMid(p.rect)
	clk += time.Second
	pipPress(e, from, pointer.Move, clk)
	pipPress(e, from, pointer.Press, clk+10*time.Millisecond)
	for i, to := range []image.Point{from.Add(image.Pt(-3, 2)), from.Add(image.Pt(-200, 150)), image.Pt(300, 700), image.Pt(200, 760)} {
		pipPress(e, to, pointer.Move, clk+time.Duration(20+20*i)*time.Millisecond) // the button held: a drag
	}
	pipPress(e, image.Pt(200, 760), pointer.Release, clk+200*time.Millisecond)
	e.settle(400 * time.Millisecond) // the .2s snap
	q, _ := pipShown(e)
	if q.corner != "bottom-left" || q.rect.Min.X != 20 || q.rect.Max.Y != 900-20 {
		t.Errorf("dropped in the bottom left quarter: %q at %v, want bottom-left, 20px in", q.corner, q.rect)
	}
	if !q.playing && q.elapsed < 2900*time.Millisecond {
		t.Error("the drag's press paused the video: Slack swallows the click after a drag")
	}
	e.key(key.NameRightArrow, 0)
	e.frame()
	if q, _ := pipShown(e); q.corner != "bottom-right" || q.rect.Max.X != 1440-20 {
		t.Errorf("→ with the focus: %q at %v, want bottom-right", q.corner, q.rect)
	}
	e.key(key.NameUpArrow, 0)
	e.frame()
	if q, _ := pipShown(e); q.corner != "top-right" {
		t.Errorf("↑: %q, want top-right", q.corner)
	}
}

// The handles on the sides away from its corner resize it, the aspect
// kept and the corner's sides held; a double click resets it.
func TestVideoPipResize(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	_, p := pipDock(t, e, &clk)
	w := image.Pt(p.rect.Min.X+3, p.rect.Min.Y+p.rect.Dy()/2) // the left side's handle (top-right: s, w, sw)
	clk += time.Second
	pipPress(e, w, pointer.Move, clk)
	pipPress(e, w, pointer.Press, clk+10*time.Millisecond)
	pipPress(e, w.Sub(image.Pt(60, 0)), pointer.Move, clk+30*time.Millisecond)
	pipPress(e, w.Sub(image.Pt(100, 0)), pointer.Move, clk+50*time.Millisecond)
	pipPress(e, w.Sub(image.Pt(100, 0)), pointer.Release, clk+70*time.Millisecond)
	q, _ := pipShown(e)
	if q.rect.Dx() != 460 || q.rect.Max.X != 1440-20 || q.rect.Min.Y != 56 {
		t.Errorf("the left side dragged 100px out: %v, want 460 wide, its top and right where they were", q.rect)
	}
	if h := q.rect.Dy(); h < 256 || h > 261 {
		t.Errorf("%d tall at 460 wide, want 16:9's 259", h)
	}
	time.Sleep(600 * time.Millisecond)
	w = image.Pt(q.rect.Min.X+3, q.rect.Min.Y+q.rect.Dy()/2)
	for i := 0; i < 2; i++ { // a double click
		pipPress(e, w, pointer.Press, clk+time.Second+time.Duration(i)*80*time.Millisecond)
		pipPress(e, w, pointer.Release, clk+time.Second+time.Duration(i)*80*time.Millisecond+20*time.Millisecond)
	}
	if q, _ := pipShown(e); q.rect.Dx() != 360 {
		t.Errorf("after a double click on a handle it's %d wide, want 360 again", q.rect.Dx())
	}
}

// The media viewer's Dock to corner (MediaViewerPipButton) docks the
// video it plays, from its time: the viewer closes.
func TestVideoViewerDock(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	m := e.filesMsg("video")
	tile := vvTile(t, e, m.TS)
	e.clickAt(vvMid(tile), time.Second)
	e.until("the inline video playing", 10*time.Second, func() bool { p, _, ok := inlineState(e, m.TS); return ok && p })
	e.settle(400 * time.Millisecond)
	vvHover(e, vvMid(tile), 1500*time.Millisecond)
	e.frame()
	ex, ok := inlineExpand(e, m.TS)
	if !ok || ex.Empty() {
		t.Fatal("no Expand in the player's corner")
	}
	e.clickAt(vvMid(ex), 2*time.Second)
	e.until("the viewer playing the video", 10*time.Second, func() bool { st, ok := viewerState(e); return ok && st.video && st.playing })
	vvHover(e, image.Pt(e.size.X/2, e.size.Y/2), 2500*time.Millisecond)
	e.frame()
	d := viewerDock(e)
	if d.Empty() {
		t.Fatal("no Dock to corner in the viewer's footer")
	}
	st, _ := viewerState(e)
	e.clickAt(vvMid(d), 3*time.Second)
	e.until("docked from the viewer", 10*time.Second, func() bool { v, ok := pipShown(e); return ok && v.playing })
	if _, ok := viewerState(e); ok {
		t.Error("the viewer stayed open")
	}
	if v, _ := pipShown(e); v.trigger != "manual" || v.elapsed < st.elapsed-200*time.Millisecond {
		t.Errorf("docked by %q at %v, want manual from the viewer's %v", v.trigger, v.elapsed, st.elapsed)
	}
}

// files-shots' screenshots of the mini player, light and dark.
func TestVideoPipShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	tag := os.Getenv("VIDEO_VIEWER_SHOTS_TAG")
	if tag == "" {
		tag = "after"
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			viewerVideos(t)
			e := filesEnv(t, image.Pt(1440, 900), dark)
			var clk time.Duration
			m := e.filesMsg("video")
			tile := vvTile(t, e, m.TS)
			clk += time.Second
			e.clickAt(vvMid(tile), clk)
			e.settle(time.Second)
			e.openChannel(pipOther(e))
			e.settle(800 * time.Millisecond)
			e.filesShot("video-pip-docked-" + mode + "-" + tag)
			if p, ok := pipShown(e); ok {
				vvHover(e, vvMid(p.controls[len(p.controls)-1]), clk+time.Second)
				e.settle(700 * time.Millisecond) // the tooltip's delay
				e.filesShot("video-pip-close-tip-" + mode + "-" + tag)
			}
		})
	}
}
