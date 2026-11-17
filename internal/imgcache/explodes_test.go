package imgcache

import (
	"context"
	"fmt"
	"sync/atomic"
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
// frame it may draw to its index; the still is -1. Until its timeline
// first moves a player draws the still, its first frame (curOp).
func playing(t *testing.T, c *Cache, url string, px int) map[paint.ImageOp]int {
	t.Helper()
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(16 * time.Millisecond) {
		c.Get(url, px)
		if idx := playingNow(c, url, px); idx != nil {
			return idx
		}
	}
	t.Fatalf("%s at %d px never played", url, px)
	return nil
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
	idx := map[paint.ImageOp]int{e.op: -1}
	for i, op := range p.frames {
		idx[op] = i
	}
	return idx
}

// anchor waits (drawing url at px, and at the other sizes also, every 4
// ms) until its schedule is past its first two passes, after which nothing
// restarts it (Chromium's first-pass restart needs repetitions_completed
// 1; the resync, 5 minutes behind), and returns where its timeline is: the
// frame showing and when it began. From there frameAt says which frame the
// timeline is at.
func anchor(t *testing.T, c *Cache, url string, px int, also ...int) (cur int, shown time.Time) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		c.Get(url, px)
		for _, o := range also {
			c.Get(url, o)
		}
		c.mu.Lock()
		p := playerOfLocked(c, url, px)
		ok := p != nil && p.keep && len(p.frames) == len(p.ix.frames) && p.s.reps >= 2 && !p.s.ended
		if ok {
			cur, shown = p.s.cur, p.s.shown
		}
		c.mu.Unlock()
		if ok {
			return cur, shown
		}
	}
	t.Fatalf("%s at %d px never played two passes", url, px)
	return 0, time.Time{}
}

func playerOfLocked(c *Cache, url string, px int) *player {
	if el, ok := c.entries[key{url, px}]; ok {
		return el.Value.(*entry).play
	}
	return nil
}

// frameAt is the frame a timeline that showed frame cur from shown is at
// at t (t not before shown), on frames of durations ds.
func frameAt(ds []time.Duration, cur int, shown, t time.Time) int {
	off := t.Sub(shown)
	k := cur
	for off >= ds[k] {
		off -= ds[k]
		k = (k + 1) % len(ds)
	}
	return k
}

// onTimeline reports whether frame got is one the timeline (cur from
// shown) showed at some time in [tb, ta]: the Get between them read the
// clock in there.
func onTimeline(ds []time.Duration, cur int, shown, tb, ta time.Time, got int) bool {
	k, end := frameAt(ds, cur, shown, tb), frameAt(ds, cur, shown, ta)
	for n := 0; n <= len(ds); n++ {
		if k == got {
			return true
		}
		if k == end {
			return false
		}
		k = (k + 1) % len(ds)
	}
	return false
}

// shown is a frame the draws moved on to: its index (-1: the still), the
// draw it first showed at, and the gap between that draw and the one
// before.
type shown struct {
	i   int
	at  time.Time
	gap time.Duration
}

// checkPaced checks the frames the draws moved on to (seq) against the
// timeline's rules, none of which a slow machine can break: each frame
// comes after the one before in the animation's order, and never the
// still; a frame passed over is one whose whole time fell between two
// draws (the gap before the draw that moved on is at least the skipped
// frames' durations); each frame shows for its duration, with those of the
// frames passed over after it, give or take the two draw intervals it
// began and ended in; and at least loops passes play. The first frame's
// start isn't seen, so it isn't timed.
func checkPaced(t *testing.T, seq []shown, ds []time.Duration, loops int) {
	t.Helper()
	const slack = 3 * time.Millisecond // timer and sleep granularity
	n, passes := len(ds), 0
	for k, f := range seq {
		if f.i < 0 {
			t.Fatalf("draw %d (%v in) drew the still: the player went", k, f.at.Sub(seq[0].at).Round(time.Millisecond))
		}
		if k == 0 {
			continue
		}
		prev := seq[k-1]
		var skipped time.Duration
		for j := (prev.i + 1) % n; j != f.i; j = (j + 1) % n {
			skipped += ds[j]
		}
		if skipped > f.gap+slack {
			t.Errorf("frame %d after frame %d at %v: frames of %v passed over between draws %v apart", f.i, prev.i,
				f.at.Sub(seq[0].at).Round(time.Millisecond), skipped, f.gap.Round(time.Millisecond))
		}
		if f.i <= prev.i {
			passes++
		}
		if k+1 < len(seq) {
			next := seq[k+1]
			var d time.Duration
			for j := f.i; j != next.i; j = (j + 1) % n {
				d += ds[j]
			}
			if got := next.at.Sub(f.at); got < d-f.gap-slack || got > d+next.gap+slack {
				t.Errorf("frame %d (and what it passed over) showed for %v, want %v ± a draw interval (draws %v and %v apart)", f.i,
					got.Round(time.Millisecond), d, f.gap.Round(time.Millisecond), next.gap.Round(time.Millisecond))
			}
		}
	}
	if passes < loops {
		t.Errorf("%d passes played, want %d", passes, loops)
	}
}

// TestExplodesPlaysOnTimeUnderLoad: drawn like a 60 Hz window while photos
// load and push the cache past its byte budget (each one that lands
// evicts), the replica keeps its player (the same one throughout, never the
// still) and keeps to its timeline (checkPaced) for three passes. That
// load used to stop the emoji's player for room (players went first,
// largest first, and it was the only one): the still came back for
// animRetry, then the animation started over. A keeping player is its
// image's frames, decoded once, and plays on (Chromium's does). It runs
// until three passes have played and 24 photos have loaded, however slow
// the machine.
func TestExplodesPlaysOnTimeUnderLoad(t *testing.T) {
	imgs := map[string][]byte{explodesURL: webptest.Explodes()}
	photo := pngBytes(360, 360) // 518 KB decoded
	var photos []string
	for i := 0; i < 400; i++ {
		u := fmt.Sprintf("https://files.slack.com/files-tmb/T1-F%03d-x/photo_360.png", i)
		imgs[u] = photo
		photos = append(photos, u)
	}
	var fetched atomic.Int32
	c := New(func(_ context.Context, u string) ([]byte, error) {
		b, ok := imgs[u]
		if !ok {
			return nil, fmt.Errorf("no %s", u)
		}
		if u != explodesURL {
			fetched.Add(1)
		}
		return b, nil
	}, "", func() {})
	const px = 44 // 22 px on a 2x screen: the 32×32 canvas isn't scaled up
	readyEntry(t, c, explodesURL, px)
	idx := playing(t, c, explodesURL, px)
	before := playerOf(c, explodesURL, px)
	c.SetLimit(3 << 20) // six photos fill it

	ds := explodesDurations()
	var seq []shown
	passes := func() int {
		n := 0
		for k := 1; k < len(seq); k++ {
			if seq[k].i <= seq[k-1].i {
				n++
			}
		}
		return n
	}
	next, draws := 0, 0 // the next photo to ask for
	moved := false      // a frame of the player's drawn: the still is gone for good
	last := time.Now()
	for dl := time.Now().Add(60 * time.Second); passes() < 3 || fetched.Load() < 24; draws++ {
		if time.Now().After(dl) {
			t.Fatalf("after %d draws: %d passes, %d photos loaded", draws, passes(), fetched.Load())
		}
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
		if i >= 0 {
			moved = true
		} else if !moved {
			i = 0 // the still, the first frame, until the timeline first moves
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
	s := c.Stats()
	if p := playerOf(c, explodesURL, px); p != before || s.Players != 1 {
		t.Errorf("the emoji's player went or started over (%p, was %p): %+v", p, before, s)
	}
	if s.Bytes > s.Limit || s.Entries > 8 {
		t.Errorf("%d photos loaded, and the cache holds %+v: past its budget, or nothing evicted", fetched.Load(), s)
	}
}

// TestAnimSizesShareTimeline: one image drawn at two sizes (an emoji in a
// message and as a jumbomoji, or in a reaction) shows the same frame at
// both, as one image's every <img> does in Chromium (its one animation),
// and its frames ask for one repaint each, not one per size. Each size's
// player used to keep its own timeline, from when that size started, so
// the two were out of step and their repaints interleaved (twice the
// frames for the window to draw). Each draw is checked against the shared
// timeline at the time it read the clock, so a slow machine can't fail it.
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
	if a, b := playerOf(c, explodesURL, 22), playerOf(c, explodesURL, 32); a.s != b.s {
		t.Fatal("the two sizes play on separate timelines")
	}
	ds := explodesDurations()
	cur, shown := anchor(t, c, explodesURL, 22, 32)
	inv0, moves := inv.Load(), 0
	prev := -2
	for dl := time.Now().Add(1500 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		tb := time.Now()
		a, _ := c.Get(explodesURL, 22)
		tm := time.Now()
		b, _ := c.Get(explodesURL, 32)
		ta := time.Now()
		ia, ib := small[a], big[b]
		if !onTimeline(ds, cur, shown, tb, tm, ia) || !onTimeline(ds, cur, shown, tm, ta, ib) {
			t.Fatalf("at %v the sizes drew frames %d and %d, off their shared timeline", tb.Sub(shown).Round(time.Millisecond), ia, ib)
		}
		if ia != prev {
			moves++
			prev = ia
		}
	}
	if n := int(inv.Load() - inv0); n > moves+moves/4+2 {
		t.Errorf("%d repaints for %d frames: the sizes keep separate timelines", n, moves)
	}
}

// TestAnimCatchesUp: a window that draws slowly (a frame every 150 ms, as a
// long layout makes it) shows the frame the animation's timeline is at
// when it draws, skipping the ones it missed, as Chromium does
// (AdvanceAnimationState): the animation plays at its own speed. It used
// to move on one frame per draw from wherever it was, so a slow window
// played it slowly ("super laggy"). The timeline is the schedule's own
// (anchor), each draw is checked at the time it read the clock, and a draw
// 150 ms after the last always passes over a frame (none is longer than
// 70 ms), so a slow machine can't fail it.
func TestAnimCatchesUp(t *testing.T) {
	c, _ := gifCache(t, map[string][]byte{explodesURL: webptest.Explodes()})
	readyEntry(t, c, explodesURL, 22)
	idx := playing(t, c, explodesURL, 22)
	ds := explodesDurations()
	cur, shown := anchor(t, c, explodesURL, 22)
	skipped0 := c.Stats().Skipped
	draws := 0
	time.Sleep(150 * time.Millisecond) // every draw 150 ms after the one before
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); time.Sleep(150 * time.Millisecond) {
		tb := time.Now()
		op, _ := c.Get(explodesURL, 22)
		ta := time.Now()
		got := idx[op]
		draws++
		if !onTimeline(ds, cur, shown, tb, ta, got) {
			t.Errorf("at %v drew frame %d, the timeline is at frame %d", tb.Sub(shown).Round(time.Millisecond), got, frameAt(ds, cur, shown, tb))
		}
	}
	if s := c.Stats(); s.Skipped-skipped0 < uint64(draws) {
		t.Errorf("%d frames passed over in %d draws 150 ms apart (frames of 30 to 70 ms): each should pass over one at least", s.Skipped-skipped0, draws)
	}
}

// TestKeepingPlayerSurvivesStall: a window that draws nothing at all for
// 2.5 s (a frame stuck in a plugin or a long collection) hasn't scrolled
// the emoji away: its player stays, and once frames come again it shows
// the frame its timeline is at, with no still and no start over. Players
// used to go once nothing drew them for animGrace, whatever else drew, so
// after every long frame the emoji showed its still, settled, decoded its
// frames again and played from the first. A streaming player (a large GIF)
// still goes: it holds a canvas and three frames.
func TestKeepingPlayerSurvivesStall(t *testing.T) {
	const big = "https://example.com/big.gif"
	c, _ := gifCache(t, map[string][]byte{explodesURL: webptest.Explodes(), big: bigGIF(t, 400, 300, 10, 5, 0)})
	readyEntry(t, c, big, 300)
	readyEntry(t, c, explodesURL, 22)
	idx := playing(t, c, explodesURL, 22)
	drawFor(c, big, 300, 300*time.Millisecond)
	before := playerOf(c, explodesURL, 22)
	time.Sleep(2500 * time.Millisecond) // nothing drawn at all
	if p := playerOf(c, explodesURL, 22); p == nil || p != before {
		t.Fatalf("the emoji's player went during the stall (now %p, was %p)", p, before)
	}
	if p := playerOf(c, big, 300); p != nil {
		t.Errorf("the streaming player stayed through the stall")
	}
	c.mu.Lock()
	still := c.entries[key{explodesURL, 22}].Value.(*entry).op
	c.mu.Unlock()
	if op, _ := c.Get(explodesURL, 22); op == still {
		t.Error("after the stall the emoji drew its still")
	} else if _, ok := idx[op]; !ok {
		t.Error("after the stall the emoji drew a frame its player never decoded")
	}
	if p := playerOf(c, explodesURL, 22); p == nil || p != before {
		t.Error("the emoji's player started over after the stall")
	}
}
