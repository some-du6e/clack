// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"image"
	"math/rand"
	"testing"
)

// Clack patch 45: the damage between two frames, without a GPU.

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
