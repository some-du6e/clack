package imgcache

import (
	"fmt"
	"testing"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

// The :explodes: custom emoji (Karim, v0.1.0-alpha.98: "so choppy and
// laggy… horrible") as webptest.Explodes replicates it: 13 frames of 30 to
// 70 ms, delta rectangles over a first full frame. Synthetic image only.

const explodesURL = "https://emoji.slack-edge.com/T1/explodes/0123456789abcdef.png"

func explodesDurations() []time.Duration {
	ds := make([]time.Duration, len(webptest.ExplodesDurations))
	for i, ms := range webptest.ExplodesDurations {
		ds[i] = time.Duration(ms) * time.Millisecond
	}
	return ds
}

// playing waits until url's player at px has decoded its frames (drawing
// it every 16 ms meanwhile, as a window would) and returns a map from each
// frame it may draw (the still is frame 0) to its index.
func playing(t *testing.T, c *Cache, url string, px int) map[paint.ImageOp]int {
	t.Helper()
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(16 * time.Millisecond) {
		c.Get(url, px)
		c.mu.Lock()
		el, ok := c.entries[key{url, px}]
		var idx map[paint.ImageOp]int
		if ok {
			e := el.Value.(*entry)
			if p := e.play; p != nil && p.keep && len(p.frames) == e.frames {
				idx = map[paint.ImageOp]int{e.op: 0}
				for i, op := range p.frames {
					idx[op] = i
				}
			}
		}
		c.mu.Unlock()
		if idx != nil {
			return idx
		}
	}
	t.Fatalf("%s at %d px never played", url, px)
	return nil
}

// shown is a frame the draws moved on to: its index, the draw it first
// showed at, and the gap between that draw and the one before.
type shown struct {
	i   int
	at  time.Time
	gap time.Duration
}

// checkPaced checks that the frames in seq follow each other in order, none
// skipped, each shown for its duration give or take the draw interval it
// started and ended in (one display frame at 60 Hz), for at least loops
// passes. The first frame's start isn't seen, so it isn't timed.
func checkPaced(t *testing.T, seq []shown, ds []time.Duration, loops int) {
	t.Helper()
	if len(seq) < loops*len(ds)+1 {
		t.Fatalf("%d frames shown, want %d passes of %d", len(seq)-1, loops, len(ds))
	}
	const slack = 3 * time.Millisecond // timer and sleep granularity
	for k := 1; k < len(seq); k++ {
		if want := (seq[k-1].i + 1) % len(ds); seq[k].i != want {
			t.Errorf("frame %d after frame %d at %v: frames skipped (or the still came back)", seq[k].i, seq[k-1].i, seq[k].at.Sub(seq[0].at).Round(time.Millisecond))
			continue
		}
		if k+1 == len(seq) {
			break
		}
		got, d := seq[k+1].at.Sub(seq[k].at), ds[seq[k].i]
		if got < d-seq[k].gap-slack || got > d+seq[k+1].gap+slack {
			t.Errorf("frame %d showed for %v, want %v ± a display frame (draws %v and %v apart)", seq[k].i, got.Round(time.Millisecond), d,
				seq[k].gap.Round(time.Millisecond), seq[k+1].gap.Round(time.Millisecond))
		}
	}
}

// TestExplodesPlaysOnTimeUnderLoad: drawn like a 60 Hz window, every frame
// of the replica shows, in order, for its own duration give or take one
// display frame, none skipped, over three passes, while photos load and
// fill the cache's byte budget past its limit (each one that lands evicts).
// That load used to stop the emoji's player for room (players went first,
// largest first, and it was the only one): the still came back for
// animRetry, then the animation started over. A keeping player is its
// image's frames, decoded once, and plays on (Chromium's does).
func TestExplodesPlaysOnTimeUnderLoad(t *testing.T) {
	imgs := map[string][]byte{explodesURL: webptest.Explodes()}
	photo := pngBytes(360, 360) // 518 KB decoded
	var photos []string
	for i := 0; i < 200; i++ {
		u := fmt.Sprintf("https://files.slack.com/files-tmb/T1-F%03d-x/photo_360.png", i)
		imgs[u] = photo
		photos = append(photos, u)
	}
	c, _ := gifCache(t, imgs)
	const px = 44 // 22 px on a 2x screen: the 32×32 canvas isn't scaled up
	readyEntry(t, c, explodesURL, px)
	idx := playing(t, c, explodesURL, px)
	c.SetLimit(3 << 20) // six photos fill it

	ds := explodesDurations()
	var loop time.Duration
	for _, d := range ds {
		loop += d
	}
	var seq []shown
	next, draws := 0, 0 // the next photo to ask for
	last := time.Now()
	for dl := time.Now().Add(3*loop + 2*ds[0] + 200*time.Millisecond); time.Now().Before(dl); draws++ {
		time.Sleep(time.Until(last.Add(16 * time.Millisecond)))
		now := time.Now()
		gap := now.Sub(last)
		last = now
		op, ok := c.Get(explodesURL, px)
		if !ok {
			t.Fatal("the emoji isn't ready any more")
		}
		i, known := idx[op]
		if !known {
			t.Fatal("a frame the player never decoded")
		}
		if len(seq) == 0 || seq[len(seq)-1].i != i {
			seq = append(seq, shown{i: i, at: now, gap: gap})
		}
		// the load: the two photos on screen, drawn every frame, and a
		// new one asked for every other frame (it lands a little later)
		for _, u := range photos[max(next-2, 0):next] {
			c.Get(u, 360)
		}
		if draws%2 == 0 && next < len(photos) {
			c.Get(photos[next], 360)
			next++
		}
	}
	checkPaced(t, seq, ds, 3)
	if s := c.Stats(); s.Players != 1 || s.Bytes > s.Limit || next < 50 {
		t.Errorf("stats %+v after %d photos: the emoji's player still playing, the cache within its budget", s, next)
	}
}

// TestAnimSizesShareTimeline: one image drawn at two sizes (an emoji in a
// message and as a jumbomoji, or in a reaction) shows the same frame at
// both, as one image's every <img> does in Chromium (its one animation),
// and its frames ask for one repaint each, not one per size. Each size's
// player used to keep its own timeline, from when that size started, so
// the two were out of step and their repaints interleaved (twice the
// frames for the window to draw).
func TestAnimSizesShareTimeline(t *testing.T) {
	c, inv := gifCache(t, map[string][]byte{explodesURL: webptest.Explodes()})
	readyEntry(t, c, explodesURL, 22)
	small := playing(t, c, explodesURL, 22)
	// the second size comes on screen later (a jumbomoji scrolled into view)
	for dl := time.Now().Add(170 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		c.Get(explodesURL, 22)
	}
	readyEntry(t, c, explodesURL, 32)
	var big map[paint.ImageOp]int
	for dl := time.Now().Add(5 * time.Second); big == nil; time.Sleep(4 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the second size never played")
		}
		c.Get(explodesURL, 22)
		c.Get(explodesURL, 32)
		big = playingNow(c, explodesURL, 32)
	}
	for dl := time.Now().Add(100 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		c.Get(explodesURL, 22)
		c.Get(explodesURL, 32)
	}
	inv0, moves, apart := inv.Load(), 0, 0
	prev := -1
	for dl := time.Now().Add(1500 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		a, _ := c.Get(explodesURL, 22)
		b, _ := c.Get(explodesURL, 32)
		ia, ib := small[a], big[b]
		if ia != ib {
			apart++
		}
		if ia != prev {
			moves++
			prev = ia
		}
	}
	if apart > 1 { // a frame due between the two Gets, microseconds apart
		t.Errorf("the two sizes showed different frames at %d draws of about 370", apart)
	}
	if n := int(inv.Load() - inv0); n > moves+moves/4+2 {
		t.Errorf("%d repaints for %d frames: the sizes keep separate timelines", n, moves)
	}
}

// playingNow is playing without the wait: nil until url's player at px
// has its frames.
func playingNow(c *Cache, url string, px int) map[paint.ImageOp]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key{url, px}]
	if !ok {
		return nil
	}
	e := el.Value.(*entry)
	p := e.play
	if p == nil || !p.keep || len(p.frames) != e.frames {
		return nil
	}
	idx := map[paint.ImageOp]int{e.op: 0}
	for i, op := range p.frames {
		idx[op] = i
	}
	return idx
}

// TestAnimCatchesUp: a window that draws slowly (a frame every 150 ms, as a
// long layout makes it) shows the frame the animation's timeline is at
// when it draws, skipping the ones it missed, as Chromium does
// (AdvanceAnimationState): the animation plays at its own speed. It used
// to move on one frame per draw from wherever it was, so a slow window
// played it slowly ("super laggy").
func TestAnimCatchesUp(t *testing.T) {
	c, _ := gifCache(t, map[string][]byte{explodesURL: webptest.Explodes()})
	readyEntry(t, c, explodesURL, 22)
	idx := playing(t, c, explodesURL, 22)
	ds := explodesDurations()
	var loop time.Duration
	for _, d := range ds {
		loop += d
	}
	// two passes drawn every 4 ms: on time, and past the first pass
	var at time.Time
	cur, moves := -1, 0
	for moves < 2*len(ds)+2 {
		op, _ := c.Get(explodesURL, 22)
		if i := idx[op]; i != cur {
			cur, at, moves = i, time.Now(), moves+1
		}
		time.Sleep(4 * time.Millisecond)
	}
	// cur began about at (within a draw); from there the timeline is fixed
	frameAt := func(t time.Time) (i int, edge time.Duration) {
		off := t.Sub(at) % loop
		for k := cur; ; k = (k + 1) % len(ds) {
			if off < ds[k] {
				return k, min(off, ds[k]-off)
			}
			off -= ds[k]
		}
	}
	wrong, draws, distinct := 0, 0, map[int]bool{}
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); time.Sleep(150 * time.Millisecond) {
		op, _ := c.Get(explodesURL, 22)
		now := time.Now()
		want, edge := frameAt(now)
		got := idx[op]
		draws++
		distinct[got] = true
		if got != want && edge > 8*time.Millisecond {
			wrong++
			if wrong <= 3 {
				t.Errorf("at %v drew frame %d, the timeline is at frame %d", now.Sub(at).Round(time.Millisecond), got, want)
			}
		}
	}
	if wrong > 0 {
		t.Errorf("%d of %d slow draws off the timeline", wrong, draws)
	}
	if len(distinct) < 8 {
		t.Errorf("%d different frames in %d draws 150 ms apart, of a 620 ms loop of 13", len(distinct), draws)
	}
}
