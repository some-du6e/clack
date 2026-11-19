package ui

// Round 3's review, a test gap: the #files-gallery thread had no video,
// and every App test read the channel's tiles. A video replied in a
// thread plays in the thread pane, as in Slack: the pane's rows take
// their players under "thread"; its More actions has no "Open in thread"
// (frPc, not in a thread pane); one video plays at a time across the
// channel and the pane (bxCZ); closing the pane while its video plays
// docks it into the mini player (useNavigateDockTrigger), its own player
// paused at once and let go 10 s later (React unmounting the pane).

import (
	"image"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

func TestVideoThreadPane(t *testing.T) {
	keepIdlePlayers(t)
	fakeVideos(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	ch := fakeslack.FilesChannelID
	root := ""
	for _, c := range e.fs.FilesCases() {
		if c.Name == fakeslack.FilesThread {
			root = c.TS
		}
	}
	reply := ""
	for _, r := range e.fs.FilesReplies(root) {
		for _, f := range r.Files {
			if f.ID == fakeslack.FilesVideoID && r.TS != root {
				reply = r.TS
			}
		}
	}
	if reply == "" {
		t.Fatal("the thread fixture has no video reply")
	}
	inThread := func() (videoplayer.State, bool) {
		return e.a.vids.TileState("thread", ch, reply, fakeslack.FilesVideoID)
	}
	// the channel's video plays first
	m := e.filesMsg("video")
	tile := e.videoTile(t, m.TS)
	clk := time.Second
	e.clickAt(mid(tile.R), clk)
	e.until("the channel's video playing", 15*time.Second, func() bool { st, _ := e.videoState(m.TS); return st.Playing && !st.Loading })

	e.a.OpenThread(ch, root)
	var tr image.Rectangle
	e.until("the reply's video tile in the pane", 15*time.Second, func() bool {
		for _, r := range rectsOf(e.threadFiles(ch, root), reply) {
			if r.File.IsVideo {
				tr = r.R
				return true
			}
		}
		return false
	})
	// the gallery's images load and the pane's rows move: wait for the
	// tile to stay put
	videoIn := func() image.Rectangle {
		for _, r := range rectsOf(e.threadFiles(ch, root), reply) {
			if r.File.IsVideo {
				return r.R
			}
		}
		return image.Rectangle{}
	}
	for dl := time.Now().Add(10 * time.Second); time.Now().Before(dl); {
		e.settle(300 * time.Millisecond)
		if r := videoIn(); r == tr && !r.Empty() {
			break
		} else {
			tr = r
		}
	}
	if _, ok := inThread(); !ok {
		t.Fatal("the reply's video isn't the thread pane's player tile")
	}
	clk += time.Second
	e.clickAt(mid(tr), clk)
	e.until("the pane's video playing", 15*time.Second, func() bool { st, _ := inThread(); return st.Playing && !st.Loading })
	e.until("the channel's video paused (one at a time)", 5*time.Second, func() bool { st, _ := e.videoState(m.TS); return !st.Playing })

	// More actions, from the pane: no "Open in thread"
	for _, r := range rectsOf(e.threadFiles(ch, root), reply) {
		if r.File.IsVideo {
			tr = r.R
		}
	}
	clk += 300 * time.Millisecond
	e.hoverAt(mid(tr), clk)
	e.frame()
	e.frame()
	st, _ := inThread()
	if st.More.Empty() {
		t.Fatalf("hovered in the pane: %+v, want the corner's More actions", st)
	}
	clk += 300 * time.Millisecond
	e.clickAt(tr.Min.Add(mid(st.More)), clk)
	e.until("the video's menu", 5*time.Second, func() bool { return e.topMenu() != nil })
	labels := e.topMenu().Labels()
	if slices.Contains(labels, "Open in") || slices.Contains(labels, "Open in thread") {
		t.Errorf("the pane's video menu %q offers Open in thread", labels)
	}
	if !slices.Contains(labels, "Download") {
		t.Errorf("the pane's video menu %q, want VideoActionsButton's", labels)
	}
	e.a.dismissTop()
	e.frame()

	// the pane closes: its video pauses at once, its player goes 10 s on
	e.a.CloseSecondary()
	e.settle(300 * time.Millisecond)
	e.until("the pane's video paused", 5*time.Second, func() bool { st, _ := inThread(); return !st.Playing })
	time.Sleep(10500 * time.Millisecond) // unmountAfter
	e.frame()
	if st, ok := inThread(); ok && st.Mounted {
		t.Errorf("the pane closed 10.5 s ago and its player is still mounted: %+v", st)
	}
}
