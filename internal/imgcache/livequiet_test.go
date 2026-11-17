package imgcache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gioui.org/op/paint"
)

// Synthetic images only.

func entryOf(c *Cache, url string, px int) *entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entryLocked(key{url, px})
}

// TestLiveOps: with live ops on (ui's quiet frames, Gio patch 35), Get hands
// out an animated image as one live ImageOp of its still's size, and the
// GPU's ask (live.Image) draws the frame due: it plays on with no Get at
// all, as a frame drawn again from its ops does. A size derived from
// another size's still, a pixel off the original's scaled size, plays too:
// its frames are its still's size (the ask took only frames of the still's
// size, so that copy showed its still for good). A player that's released
// leaves its live image on the still, not on the frame it gave last (held
// where neither budget counts it). An entry that's gone while a frame still
// draws it stays on its still and asks once for a frame that lays out.
func TestLiveOps(t *testing.T) {
	const u = "https://files.slack.com/files-tmb/T1-F1-x/anim.gif" // not DiskOK: a derived copy stays
	c, inv := gifCache(t, map[string][]byte{u: bigGIF(t, 100, 67, 6, 5, 0)})
	c.SetLiveOps(true)
	readyEntry(t, c, u, 64)
	op48, ok := c.Get(u, 48) // derived from the 64 px still at once
	if !ok {
		t.Fatal("no 48 px copy derived from the 64 px one")
	}
	e := entryOf(c, u, 48)
	if want := scaledSize(100, 67, 48); e.size == want {
		t.Fatalf("the derived still is %v, the original's scaled size: the test needs one a pixel off", e.size)
	}
	if op48.Size() != e.size || e.lives[0] == nil {
		t.Fatalf("Get gave %v for a %v still, live %v: want its live op", op48.Size(), e.size, e.lives[0] != nil)
	}
	var frames map[paint.ImageOp]int
	for dl := time.Now().Add(5 * time.Second); frames == nil; time.Sleep(16 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the derived size never played")
		}
		c.Get(u, 48)
		frames = playingNow(c, u, 48)
	}
	l := e.lives[0]
	seen := map[int]bool{}
	for dl := time.Now().Add(600 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		op := l.Image() // the GPU's ask, no Get
		if op.Size() != e.size {
			t.Fatalf("the live image gave %v for a %v still", op.Size(), e.size)
		}
		if i, ok := frames[op]; ok && i >= 0 {
			seen[i] = true
		}
	}
	if len(seen) < 3 {
		t.Errorf("the live image showed %d of the player's frames in 600 ms of 50 ms frames, with no Get: it doesn't play", len(seen))
	}

	c.SetAnimate(false) // releases the players
	c.mu.Lock()
	still := l.last == e.op
	c.mu.Unlock()
	if !still {
		t.Error("a released player left its live image on its last frame")
	}
	c.SetAnimate(true)
	time.Sleep(200 * time.Millisecond) // a wake armed before the release fires

	inv0 := inv.Load()
	c.mu.Lock()
	c.dropLocked(c.entries[key{u, 48}])
	c.mu.Unlock()
	for i := 0; i < 5; i++ {
		if op := l.Image(); op != e.op {
			t.Fatal("a gone entry's live image isn't its still")
		}
	}
	time.Sleep(300 * time.Millisecond)
	if n := inv.Load() - inv0; n != 1 {
		t.Errorf("a gone entry's live image asked for %d repaints, want one", n)
	}
}

// TestFrameRedrawnKeepsImagesFresh: a frame drawn again from its ops
// (FrameRedrawn, ui's quiet frames) counts as a draw of what its layout
// Got since FrameBegin: those images are used now (the idle sweep keeps
// them however long the frames stay quiet) and the loads they wait for
// stay hot; an image only an earlier frame drew isn't kept.
func TestFrameRedrawnKeepsImagesFresh(t *testing.T) {
	const a, b = "https://example.com/a.png", "https://example.com/b.png"
	imgs := map[string][]byte{a: pngBytes(16, 16), b: pngBytes(16, 16)}
	block := make(chan struct{})
	c := New(func(ctx context.Context, u string) ([]byte, error) {
		if strings.HasPrefix(u, "https://example.com/slow") {
			select {
			case <-block:
			case <-ctx.Done():
			}
			return nil, errors.New("slow")
		}
		if d, ok := imgs[u]; ok {
			return d, nil
		}
		return nil, fmt.Errorf("no %s", u)
	}, "", func() {})
	t.Cleanup(func() { close(block) })
	c.FrameBegin()
	readyEntry(t, c, a, 16) // frame 1 draws a
	c.FrameBegin()
	readyEntry(t, c, b, 16) // frame 2 draws b, and waits for slow loads:
	// the lane's workers busy with eight of them, a ninth waiting its turn
	for i := 0; i <= laneWorkers; i++ {
		c.Get(fmt.Sprintf("https://example.com/slow%d.png", i), 16)
	}
	var waitK key
	var asked0 int64
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		c.mu.Lock()
		n := len(c.waiting)
		for k, w := range c.waiting {
			waitK, asked0 = k, w.asked
		}
		c.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("%d loads wait, want the ninth", n)
		}
	}
	t0 := time.Now()
	time.Sleep(30 * time.Millisecond)
	c.FrameRedrawn() // frame 2 drawn again
	ea, eb := entryOf(c, a, 16), entryOf(c, b, 16)
	c.mu.Lock()
	usedA, usedB, asked := ea.used, eb.used, c.waiting[waitK].asked
	c.mu.Unlock()
	if usedB < t0.UnixMilli() {
		t.Error("b, which the redrawn frame draws, isn't used now")
	}
	if usedA >= t0.UnixMilli() {
		t.Error("a, which only an earlier frame drew, counts as drawn")
	}
	if asked <= asked0 {
		t.Error("the load the redrawn frame waits for didn't stay hot")
	}
	c.sweep(t0.UnixMilli()) // drops what wasn't drawn since t0
	if !c.Loaded(b) {
		t.Error("the sweep dropped b, which the redrawn frame draws")
	}
	if c.Loaded(a) {
		t.Error("the sweep kept a, which only an earlier frame drew")
	}
	if got := c.Stats(); got.Entries != 1 {
		t.Errorf("stats %+v after the sweep, want b alone", got)
	}
}
