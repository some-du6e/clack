//go:build windows || (darwin && cgo)

package ui

import (
	"image"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestRealWindowVideoFrames is the inline player on Clack's real window
// (App.Run's own, as TestRealWindowQuietFrames): the OS's decoder (Media
// Foundation on ci's windows-idle, AVFoundation on macos) plays
// #files-gallery's video, which fakeslack serves from mediatest's fixture
// with ranges, through core's OpenRange, as on Karim's machines. 8 s in,
// once boot loads settled, the tile is clicked and its loop turned on, and
// for 5 s: the video's frames come, at least 90% of the window's frames
// are quiet (the last frame's ops drawn again, no layout), and its partial
// frames' damage stays inside the tile. Then paused for 3 s: no frame
// from the player. Synthetic data only.
func TestRealWindowVideoFrames(t *testing.T) {
	if on, why := realWindowGate(); !on {
		t.Skip(why)
	}
	if err := videoBackendAvailable(); err != nil {
		if os.Getenv("CLACK_MEDIA_TESTS") != "" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	start := time.Now()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	cache := filepath.Join(home, "cache")

	// what the window drew while measuring: its FrameEvents, and the GPU's
	// stats for each frame (partial, damage); and every frame's asks for
	// external images (Gio patch 51), counted from the click
	var mu sync.Mutex
	var measuring, clicked bool
	var frames, gpuAll, externals, wraps int
	var gpuFrames []caretGPUFrame
	stopHook := caretGPUHook(func(f caretGPUFrame) {
		mu.Lock()
		if measuring {
			gpuFrames = append(gpuFrames, f)
		}
		if clicked {
			gpuAll++
			externals += f.externals
			wraps += f.wraps
		}
		mu.Unlock()
	})
	t.Cleanup(stopHook)
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		realWindowEvent(e)
		if _, ok := e.(app.FrameEvent); ok {
			mu.Lock()
			if measuring {
				frames++
			}
			mu.Unlock()
		}
	}}, images)
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{Width: 800, Height: 600}, Bounds: &winRect{X: 60, Y: 60, Width: 800, Height: 600}}
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("Quit didn't end Run")
		}
	})
	until := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(d); !ok(); time.Sleep(50 * time.Millisecond) {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	onUI := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(30 * time.Second):
			t.Fatal("work posted to the UI goroutine didn't run")
		}
	}
	until("core ready", 90*time.Second, func() bool {
		booted := false
		onUI(func() { booted = a.data != nil && !a.data.booted.IsZero() })
		return booted
	})
	realWindowShow(t)
	ch := fakeslack.FilesChannelID
	ts := ""
	for _, c := range fs.FilesCases() {
		if c.Name == "video" {
			ts = c.TS
		}
	}
	onUI(func() { a.Navigate("c/" + ch) })
	until("the channel's history", 60*time.Second, func() bool {
		found := false
		onUI(func() {
			if tl := a.data.core.S.Timeline(ch); tl != nil {
				for _, m := range tl.Msgs {
					found = found || m.TS == ts
				}
			}
		})
		return found
	})
	onUI(func() { a.jumpToMessage(ch, ts, "") })
	if d := 8*time.Second - time.Since(start); d > 0 {
		time.Sleep(d) // boot's loads settle (idle-tests-wait-boot-loads)
	}
	// the tile wholly in the list: a jump puts the two rows above the
	// message at the list's top, and #files-gallery's are tall, so in this
	// window the tile starts under the list or off the screen (a video
	// under 10% visible pauses, and the GPU never draws an image off the
	// screen)
	var tile, list image.Rectangle
	for i := 0; ; i++ {
		in := false
		onUI(func() {
			tile, list = image.Rectangle{}, image.Rectangle{}
			if l, ok := a.convoFor(ch, "").list.(channelList); ok {
				tile, list, in = videoScrollStep(l, ts)
			}
		})
		if in {
			break
		}
		if i == 100 {
			t.Fatalf("the video's tile %v isn't wholly in the list %v", tile, list)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("the video's tile %v in the list %v", tile, list)
	until("the video's tile laid out as a player's", 30*time.Second, func() bool {
		ok := false
		onUI(func() { _, ok = videoTileState(a, ch, ts) })
		return ok
	})
	ok := false
	mu.Lock()
	clicked = true
	mu.Unlock()
	onUI(func() { ok = videoClick(a, ch, ts) })
	if !ok {
		t.Fatal("the video's tile has no player to click")
	}
	at := time.Now()
	var last videoTileInfo
	for dl := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		onUI(func() { last, _ = videoTileState(a, ch, ts) })
		if last.playing && !last.loading && last.frames > 0 {
			break
		}
		if time.Now().After(dl) {
			mu.Lock()
			g, x, w := gpuAll, externals, wraps
			mu.Unlock()
			t.Fatalf("no frame of the video 30 s after the click: tile %+v; the GPU drew %d frames since, asked external images %d times, wrapped %d textures", last, g, x, w)
		}
	}
	t.Logf("the first frame %v after the click", time.Since(at).Round(time.Millisecond))
	onUI(func() { videoLoop(a, ch, ts) })
	time.Sleep(time.Second)

	var st0, st1 videoTileInfo
	onUI(func() { st0, _ = videoTileState(a, ch, ts) })
	q0, l0 := a.QuietFrames(), a.FrameSeq()
	mu.Lock()
	measuring, frames, gpuFrames = true, 0, nil
	mu.Unlock()
	time.Sleep(5 * time.Second)
	mu.Lock()
	measuring = false
	n, gf := frames, append([]caretGPUFrame(nil), gpuFrames...)
	mu.Unlock()
	quiet, laid := int(a.QuietFrames()-q0), int(a.FrameSeq()-l0)
	onUI(func() { st1, _ = videoTileState(a, ch, ts) })
	vf := st1.frames - st0.frames
	partial, over := 0, 0
	var dmgSum, dmgMax int64
	area := int64(tile.Dx() * tile.Dy())
	for _, f := range gf {
		if !f.partial {
			continue
		}
		partial++
		dmgSum += f.damagePx
		dmgMax = max(dmgMax, f.damagePx)
		if area > 0 && f.damagePx > area {
			over++
		}
	}
	avg := int64(0)
	if partial > 0 {
		avg = dmgSum / int64(partial)
	}
	t.Logf("5 s playing: %d video frames published; the window drew %d frames, %d quiet, %d laid out; GPU: %d frames, %d partial, damage %d px a frame on average, %d at most (the tile %v is %d px)",
		vf, n, quiet, laid, len(gf), partial, avg, dmgMax, tile, area)
	if vf < 30 {
		t.Errorf("%d video frames in 5 s of playing", vf)
	}
	if n == 0 || quiet*10 < n*9 {
		t.Errorf("%d of the window's %d frames were quiet (%d laid out): a video frame lays the window out", quiet, n, laid)
	}
	if partial == 0 {
		t.Error("no partial frame while the video played: its frames redraw the whole window")
	}
	if over > 0 {
		t.Errorf("%d partial frames damaged more than the tile's %d px (at most %d)", over, area, dmgMax)
	}

	onUI(func() { videoClick(a, ch, ts) })
	until("paused", 10*time.Second, func() bool {
		var st videoTileInfo
		onUI(func() { st, _ = videoTileState(a, ch, ts) })
		return !st.playing
	})
	time.Sleep(500 * time.Millisecond)
	var st2, st3 videoTileInfo
	onUI(func() { st2, _ = videoTileState(a, ch, ts) })
	mu.Lock()
	measuring, frames = true, 0
	mu.Unlock()
	time.Sleep(3 * time.Second)
	mu.Lock()
	measuring = false
	idle := frames
	mu.Unlock()
	onUI(func() { st3, _ = videoTileState(a, ch, ts) })
	t.Logf("3 s paused: %d frames from the player, the window drew %d", st3.frames-st2.frames, idle)
	if st3.frames != st2.frames {
		t.Errorf("paused, the player published %d frames in 3 s", st3.frames-st2.frames)
	}
}
