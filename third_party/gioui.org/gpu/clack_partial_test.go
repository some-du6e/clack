// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"fmt"
	"image"
	"math/rand"
	"testing"
)

// Clack patch 46: the damage between two frames, without a GPU.

// randSigs is n signatures from an alphabet of k (repeats on purpose).
func randSigs(rng *rand.Rand, n, k int) []opSig {
	s := make([]opSig, n)
	for i := range s {
		v := rng.Intn(k)
		s[i] = opSig{h: [2]uint64{uint64(v), uint64(v) * 7}, clip: image.Rect(v, v, v+3, v+2)}
	}
	return s
}

func lcsLen(a, b []opSig) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp[0][0]
}

// The edit script turns the last frame's ops into this frame's (what it
// keeps of each is the same sequence) and is a shortest one (as many
// edits as the two lengths less twice their longest common subsequence),
// for random sequences with many repeats; more edits than allowed is
// refused.
func TestClackDamageEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(45))
	var sc diffScratch
	for iter := 0; iter < 3000; iter++ {
		a := randSigs(rng, rng.Intn(28), 1+rng.Intn(6))
		b := randSigs(rng, rng.Intn(28), 1+rng.Intn(6))
		if iter%3 == 0 { // b a few edits from a
			b = append([]opSig(nil), a...)
			for e := rng.Intn(4); e > 0 && len(b) > 0; e-- {
				i := rng.Intn(len(b))
				if rng.Intn(2) == 0 {
					b = append(b[:i], b[i+1:]...)
				} else {
					b = append(b[:i], append([]opSig{randSigs(rng, 1, 9)[0]}, b[i:]...)...)
				}
			}
		}
		dropped := map[int]bool{}
		added := map[int]bool{}
		if !sc.edits(a, b, 64, func(fromA bool, i int) {
			if fromA {
				if dropped[i] {
					t.Fatalf("a[%d] dropped twice", i)
				}
				dropped[i] = true
			} else {
				if added[i] {
					t.Fatalf("b[%d] added twice", i)
				}
				added[i] = true
			}
		}) {
			t.Fatalf("%d and %d ops: refused within 64 edits", len(a), len(b))
		}
		var keptA, keptB []opSig
		for i, s := range a {
			if !dropped[i] {
				keptA = append(keptA, s)
			}
		}
		for i, s := range b {
			if !added[i] {
				keptB = append(keptB, s)
			}
		}
		if len(keptA) != len(keptB) {
			t.Fatalf("keeps %d of a's %d and %d of b's %d", len(keptA), len(a), len(keptB), len(b))
		}
		for i := range keptA {
			if keptA[i] != keptB[i] {
				t.Fatalf("what's kept differs at %d", i)
			}
		}
		if want := len(a) + len(b) - 2*lcsLen(a, b); len(dropped)+len(added) != want {
			t.Fatalf("%d edits, the shortest script has %d", len(dropped)+len(added), want)
		}
	}
	a := randSigs(rng, 40, 1)
	b := make([]opSig, 40)
	for i := range b {
		b[i] = opSig{h: [2]uint64{99, 1}}
	}
	if sc.edits(a, b, 64, func(bool, int) {}) {
		t.Error("80 edits allowed within 64")
	}
}

// The damage rectangles are at most partialMaxRects, disjoint (a pixel
// is drawn again once), and cover every rectangle added.
func TestClackDamageRects(t *testing.T) {
	rng := rand.New(rand.NewSource(45))
	const w, h = 200, 150
	for iter := 0; iter < 400; iter++ {
		var s rectSet
		var added []image.Rectangle
		for k := rng.Intn(30); k > 0; k-- {
			x, y := rng.Intn(w), rng.Intn(h)
			r := image.Rect(x, y, x+1+rng.Intn(40), y+1+rng.Intn(30))
			s.add(r)
			added = append(added, r)
		}
		if len(s.r) > partialMaxRects {
			t.Fatalf("%d rects", len(s.r))
		}
		for i := range s.r {
			for j := i + 1; j < len(s.r); j++ {
				if s.r[i].Overlaps(s.r[j]) {
					t.Fatalf("%v and %v overlap", s.r[i], s.r[j])
				}
			}
		}
		for _, r := range added {
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					in := false
					for _, d := range s.r {
						in = in || image.Pt(x, y).In(d)
					}
					if !in {
						t.Fatalf("%v of %v isn't in the damage %v", image.Pt(x, y), r, s.r)
					}
				}
			}
		}
	}
}

// damage: the ops one frame has and the other doesn't, past their common
// start and end; nothing for the same frame; too much is refused.
func TestClackDamage(t *testing.T) {
	vp := image.Pt(400, 300)
	var sc diffScratch
	sig := func(v int, r image.Rectangle) opSig { return opSig{h: [2]uint64{uint64(v), 1}, clip: r} }
	var a []opSig
	for i := 0; i < 50; i++ {
		a = append(a, sig(i, image.Rect(i*7, i*5, i*7+6, i*5+4)))
	}
	if d, ok := damage(a, a, vp, &sc); !ok || len(d) != 0 {
		t.Errorf("the same frame: damage %v, %v; want none", d, ok)
	}
	caret := image.Rect(100, 100, 102, 118)
	b := append(append(append([]opSig(nil), a[:20]...), sig(1000, caret)), a[20:]...)
	if d, ok := damage(a, b, vp, &sc); !ok || len(d) != 1 || d[0] != caret {
		t.Errorf("an op added: damage %v, %v; want %v", d, ok, caret)
	}
	if d, ok := damage(b, a, vp, &sc); !ok || len(d) != 1 || d[0] != caret {
		t.Errorf("an op dropped: damage %v, %v; want %v", d, ok, caret)
	}
	c := append([]opSig(nil), a...)
	c[3] = sig(2000, c[3].clip)
	c[40] = sig(2001, c[40].clip)
	if d, ok := damage(a, c, vp, &sc); !ok || len(d) != 2 {
		t.Errorf("two ops changed far apart: damage %v, %v; want their two clips", d, ok)
	}
	big := append(append([]opSig(nil), a...), sig(3000, image.Rect(0, 0, 390, 290)))
	if d, ok := damage(a, big, vp, &sc); ok {
		t.Errorf("an op over most of the window: damage %v; want the whole frame", d)
	}
}

// stickyFrame is a frame's regions for stickyAtlas.pack: one pathOp per
// region (id: its geometry), placed by pack.
func stickyFrame(regs []stickyTestRegion) []*pathOp {
	ops := make([]*pathOp, len(regs))
	for i, r := range regs {
		ops[i] = &pathOp{clip: r.clip, hash: [2]uint64{r.id, 1}, path: true}
	}
	return ops
}

type stickyTestRegion struct {
	id   uint64
	clip image.Rectangle
}

// stickyOverlaps reports two regions of a frame whose atlas places
// overlap, or one outside its page.
func stickyOverlaps(pk *packer, ops []*pathOp) string {
	type placed struct {
		idx int
		r   image.Rectangle
	}
	var ps []placed
	for _, p := range ops {
		r := image.Rectangle{Min: p.place.Pos, Max: p.place.Pos.Add(p.clip.Size())}
		if p.place.Idx < 0 || p.place.Idx >= len(pk.sizes) || !r.In(image.Rectangle{Max: pk.sizes[p.place.Idx]}) {
			return fmt.Sprintf("%v of page %d is outside its page %v", r, p.place.Idx, pk.sizes)
		}
		ps = append(ps, placed{p.place.Idx, r})
	}
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			if ps[i].idx == ps[j].idx && ps[i].r.Overlaps(ps[j].r) {
				return fmt.Sprintf("%v and %v overlap on page %d", ps[i].r, ps[j].r, ps[i].idx)
			}
		}
	}
	return ""
}

// Clack patch 46: sticky places never overlap and stay in their pages,
// whatever regions come and go and change size from frame to frame; a
// region the last frame had keeps its place.
func TestClackStickyNoOverlap(t *testing.T) {
	rng := rand.New(rand.NewSource(46))
	pk := &packer{maxDims: image.Pt(4096, 4096)}
	var s stickyAtlas
	var regs []stickyTestRegion
	next := uint64(1)
	add := func() {
		x, y := rng.Intn(1800), rng.Intn(1000)
		regs = append(regs, stickyTestRegion{next, image.Rect(x, y, x+4+rng.Intn(300), y+4+rng.Intn(40))})
		next++
	}
	for range 300 {
		add()
	}
	prev := map[uint64]placement{}
	for f := 0; f < 600; f++ {
		for e := rng.Intn(12); e > 0; e-- {
			switch k := rng.Intn(len(regs)); rng.Intn(3) {
			case 0: // dropped
				regs = append(regs[:k], regs[k+1:]...)
			case 1: // changed: a new geometry and size
				regs[k].id = next
				next++
				regs[k].clip.Max = regs[k].clip.Min.Add(image.Pt(4+rng.Intn(300), 4+rng.Intn(40)))
			default:
				add()
			}
		}
		ops := stickyFrame(regs)
		repacked := s.repacked
		s.pack(pk, ops)
		if bad := stickyOverlaps(pk, ops); bad != "" {
			t.Fatalf("frame %d: %s", f, bad)
		}
		for i, p := range ops {
			if was, ok := prev[regs[i].id]; ok && was != p.place && s.repacked == repacked {
				t.Fatalf("frame %d: region %d moved from %v to %v without a repack", f, regs[i].id, was, p.place)
			}
		}
		clear(prev)
		for i, p := range ops {
			prev[regs[i].id] = p.place
		}
	}
	t.Logf("600 frames: %d slots at the end (%d holes), %d repacks", len(s.slots), s.holeSlots, s.repacked)
}

// Clack patch 46: a path that changes every frame in the middle of the
// sequence (codeSpinner's two arcs, every 33 ms while an agent works) takes
// back the slots its last frames left: over 3,000 frames (100 s) the
// sequence stays within a constant of the live regions, the packer's work
// a frame stays within a small factor of the first frame's, and the page
// keeps its size. Before, every frame left two holes the replay packed
// ever after: 4,302 slots and 55x the packing after 1,800 frames.
func TestClackStickySpinnerBounded(t *testing.T) {
	rng := rand.New(rand.NewSource(45))
	var before, after []stickyTestRegion
	id := uint64(1)
	for range 350 {
		x, y := rng.Intn(1700), rng.Intn(1000)
		before = append(before, stickyTestRegion{id, image.Rect(x, y, x+14+rng.Intn(400), y+13+rng.Intn(8))})
		id++
	}
	for range 350 {
		x, y := rng.Intn(1700), rng.Intn(1000)
		after = append(after, stickyTestRegion{id, image.Rect(x, y, x+14+rng.Intn(400), y+13+rng.Intn(8))})
		id++
	}
	// the arcs' boxes over a turn, as codeSpinner draws them at 16 px
	boxes := []image.Point{{5, 11}, {6, 10}, {6, 11}, {7, 10}, {8, 9}, {9, 8}, {10, 5}, {10, 6}, {10, 7}, {11, 5}, {11, 6}}
	pk := &packer{maxDims: image.Pt(8192, 8192)}
	var s stickyAtlas
	var work0, maxWork int64
	var size0 image.Point
	maxSlots, grown := 0, -1
	for f := 0; f < 3000; f++ {
		regs := append([]stickyTestRegion(nil), before...)
		for a := 0; a < 2; a++ {
			b := boxes[(f*3+a*5)%len(boxes)]
			regs = append(regs, stickyTestRegion{id, image.Rect(30, 320, 30+b.X, 320+b.Y)})
			id++
		}
		regs = append(regs, after...)
		ops := stickyFrame(regs)
		pk.work = 0
		s.pack(pk, ops)
		if f == 0 {
			work0, size0 = pk.work, pk.sizes[0]
		}
		if f%500 == 0 || f == 2999 {
			t.Logf("frame %d: %d slots (%d holes), packer work %d (%.1fx the first frame's), page %v, %d repacks", f, len(s.slots), s.holeSlots, pk.work, float64(pk.work)/float64(work0), pk.sizes, s.repacked)
		}
		maxSlots, maxWork = max(maxSlots, len(s.slots)-len(regs)), max(maxWork, pk.work)
		if grown < 0 && (len(pk.sizes) != 1 || pk.sizes[0] != size0) {
			grown = f
		}
		if bad := stickyOverlaps(pk, ops); bad != "" {
			t.Fatalf("frame %d: %s", f, bad)
		}
	}
	if maxSlots > 64 {
		t.Errorf("up to %d slots more than the frame's regions (holes the replay packs every frame); want 64 at most", maxSlots)
	}
	if maxWork > 3*work0 {
		t.Errorf("the packer's work reached %d a frame, %.1fx the first frame's %d; want 3x at most", maxWork, float64(maxWork)/float64(work0), work0)
	}
	if grown >= 0 {
		t.Errorf("the page grew at frame %d (%v, the first frame's %v)", grown, pk.sizes, size0)
	}
	if s.repacked > 0 {
		t.Errorf("%d repacks over 3,000 spinner frames (each a whole frame); want none", s.repacked)
	}
}
