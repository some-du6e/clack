// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"fmt"
	"image"
	"math/rand"
	"slices"
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
	// each repack is a frame drawn whole: up to 12 changes a frame among
	// 300 regions near a page's capacity repack now and then, never most
	// frames (217 of 600 when every replay needing a second page repacked)
	if s.repacked > 30 {
		t.Errorf("%d repacks in 600 frames, want 30 (5%%) at most", s.repacked)
	}
}

// Clack patch 46: a path that changes every frame in the middle of the
// sequence (codeSpinner's two arcs, every 33 ms while an agent works) takes
// back the slots its last frames left: over 3,000 frames (100 s) the
// sequence stays within a constant of the live regions, the packer's work
// a frame stays within a small factor of the first frame's, and the page
// keeps its size. Before, every frame left two holes the replay packed
// ever after: 4,302 slots and 55x the packing after 1,800 frames.
func TestClackStickySpinnerBounded(t *testing.T) {
	// one page (8192) and, as at 4K, two (2048: Gio's own packing of the
	// scene needs a second page)
	for _, maxDim := range []int{8192, 2048} {
		t.Run(fmt.Sprintf("page %d", maxDim), func(t *testing.T) { stickySpinnerBounded(t, maxDim) })
	}
}

func stickySpinnerBounded(t *testing.T, maxDim int) {
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
	pk := &packer{maxDims: image.Pt(maxDim, maxDim)}
	var s stickyAtlas
	var work0, maxWork int64
	var sizes0 []image.Point
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
			work0, sizes0 = pk.work, append([]image.Point(nil), pk.sizes...)
		}
		if f%500 == 0 || f == 2999 {
			t.Logf("frame %d: %d slots (%d holes), packer work %d (%.1fx the first frame's), pages %v, %d repacks", f, len(s.slots), s.holeSlots, pk.work, float64(pk.work)/float64(work0), pk.sizes, s.repacked)
		}
		maxSlots, maxWork = max(maxSlots, len(s.slots)-len(regs)), max(maxWork, pk.work)
		if grown < 0 && !slices.Equal(pk.sizes, sizes0) {
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
		t.Errorf("the pages grew at frame %d (%v, the first frame's %v)", grown, pk.sizes, sizes0)
	}
	if s.repacked > 0 {
		t.Errorf("%d repacks over 3,000 spinner frames (each a whole frame); want none", s.repacked)
	}
}

// stickyFresh is Gio's own packing of ops' regions in their order: the
// packer's work and its pages' area.
func stickyFresh(maxDims image.Point, ops []*pathOp) (work, pagesArea int64) {
	pk := &packer{maxDims: maxDims}
	pk.clear()
	for _, p := range ops {
		for i := -1; i < len(p.more); i++ {
			stickyAdd(pk, stickyRegion{p, i}.clip().Size())
		}
	}
	for _, sz := range pk.sizes {
		pagesArea += int64(sz.X * sz.Y)
	}
	return pk.work, pagesArea
}

func pagesArea(pk *packer) (a int64) {
	for _, sz := range pk.sizes {
		a += int64(sz.X * sz.Y)
	}
	return a
}

// Clack patch 46: the same window's regions at a lower scale (a 2x display
// to a 1x one, or a window made smaller): every path is new and smaller,
// so each fits an old, bigger slot. The slack counts as waste, so the
// sequence is packed afresh and the pages come back to Gio's own size,
// instead of keeping the old scale's for as long as the window shows.
func TestClackStickyShrink(t *testing.T) {
	rng := rand.New(rand.NewSource(47))
	var regs2x []stickyTestRegion
	for id := uint64(1); id <= 600; id++ {
		x, y := rng.Intn(3400), rng.Intn(2000)
		regs2x = append(regs2x, stickyTestRegion{id, image.Rect(x, y, x+28+rng.Intn(800), y+26+rng.Intn(16))})
	}
	maxDims := image.Pt(8192, 8192)
	pk := &packer{maxDims: maxDims}
	var s stickyAtlas
	for range 30 {
		s.pack(pk, stickyFrame(regs2x))
	}
	before := pagesArea(pk)
	for _, k := range []float64{0.5, 0.6, 0.75} {
		var regs []stickyTestRegion
		for i, r := range regs2x {
			c := image.Rect(int(float64(r.clip.Min.X)*k), int(float64(r.clip.Min.Y)*k), int(float64(r.clip.Max.X)*k), int(float64(r.clip.Max.Y)*k))
			regs = append(regs, stickyTestRegion{uint64(10000*(k*100) + float64(i)), c})
		}
		// back at 2x, then the lower scale for a while
		for range 10 {
			s.pack(pk, stickyFrame(regs2x))
		}
		r0 := s.repacked
		var ops []*pathOp
		for range 60 {
			ops = stickyFrame(regs)
			s.pack(pk, ops)
		}
		_, fresh := stickyFresh(maxDims, ops)
		got := pagesArea(pk)
		t.Logf("2x, then x%.2f: pages %.2f Mpx (2x %.2f), Gio's own %.2f; %d repacks", k, float64(got)/1e6, float64(before)/1e6, float64(fresh)/1e6, s.repacked-r0)
		if got*4 > fresh*5 {
			t.Errorf("at x%.2f the pages are %.2f Mpx, Gio's own packing %.2f: want 1.25x at most", k, float64(got)/1e6, float64(fresh)/1e6)
		}
	}
}

// Clack patch 46: a scroll moves every region of a list (its clip is in
// its key), so each is new every frame, and rows enter and leave at the
// list's edges. A busy list (more new regions than a partial frame can
// have) is packed afresh at once, at Gio's own cost; a sparse one goes
// through the sticky places, its rows taking holes of their own size.
// Either way the packing's work (the holes looked through included) and
// the pages stay within a small factor of Gio's own packing. The busy
// list's end is where it turns sparse: the rows that leave are holes until
// they outweigh the regions (a frame that replays and then packs afresh,
// 2.84x in the port, pages up to 2.19x for a dozen frames before).
func TestClackStickyScroll(t *testing.T) {
	for _, c := range []struct {
		name            string
		rows            int // rows on screen
		maxWork, maxPgs float64
	}{
		{"a busy list", 90, 3, 2.5}, // 270 regions a frame
		{"a sparse list", 18, 1.5, 1.5},
	} {
		t.Run(c.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(48))
			// the window's other regions, still
			var still []stickyTestRegion
			for id := uint64(1); id <= 300; id++ {
				x, y := rng.Intn(1700), rng.Intn(1000)
				still = append(still, stickyTestRegion{id, image.Rect(x, y, x+14+rng.Intn(300), y+13+rng.Intn(8))})
			}
			// a message row: an avatar, a name, a line of text
			type row struct{ w, h [3]int }
			rows := make([]row, 400)
			for i := range rows {
				rows[i] = row{w: [3]int{36, 50 + rng.Intn(130), 80 + rng.Intn(800)}, h: [3]int{36, 17, 18}}
			}
			const top, bottom = 60, 1000
			maxDims := image.Pt(8192, 8192)
			pk := &packer{maxDims: maxDims}
			var s stickyAtlas
			var maxRatio, maxPages float64
			for f := 0; f < 300; f++ {
				off := f * 14 // a wheel's 14 px a frame
				regs := append([]stickyTestRegion(nil), still...)
				pitch := (bottom - top) / c.rows
				for i, r := range rows {
					y := top + i*pitch - off
					for k := range 3 {
						c := image.Rect(300+k*60, y+k*2, 300+k*60+r.w[k], y+k*2+r.h[k]).Intersect(image.Rect(0, top, 1900, bottom))
						if c.Empty() {
							continue
						}
						// the key: the row's own geometry, its offset (y) moving
						regs = append(regs, stickyTestRegion{uint64(1_000_000+i*10+k)<<20 | uint64(y&0xfffff), c})
					}
				}
				ops := stickyFrame(regs)
				pk.work = 0
				s.pack(pk, ops)
				work, pages := stickyFresh(maxDims, ops)
				if bad := stickyOverlaps(pk, ops); bad != "" {
					t.Fatalf("frame %d: %s", f, bad)
				}
				if f < 10 {
					continue // the first frames pack afresh
				}
				maxRatio = max(maxRatio, float64(pk.work)/float64(work))
				maxPages = max(maxPages, float64(pagesArea(pk))/float64(pages))
				if f%60 == 0 {
					t.Logf("frame %d: %d regions, %d slots (%d holes), work %d (Gio's own %d), pages %v, %d repacks", f, len(regs), len(s.slots), s.holeSlots, pk.work, work, pk.sizes, s.repacked)
				}
			}
			t.Logf("over 300 frames: the work up to %.2fx Gio's own packing's, the pages up to %.2fx", maxRatio, maxPages)
			if maxRatio > c.maxWork {
				t.Errorf("the sticky packing's work reached %.2fx Gio's own; want %.1fx at most", maxRatio, c.maxWork)
			}
			if maxPages > c.maxPgs {
				t.Errorf("the sticky pages reached %.2fx Gio's own; want %.1fx at most", maxPages, c.maxPgs)
			}
		})
	}
}
