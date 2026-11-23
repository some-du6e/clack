package richview

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// giantLines is n lines the giant-wall fixture's shape (one word each),
// every 97th a sentence long enough to wrap at 400px, every 13th two
// words, so lazy lines, shifted words and wrapped lines all occur.
func giantLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch {
		case i%97 == 50:
			b.WriteString(strings.Repeat(fmt.Sprintf("sentence%d words ", i), 8))
		case i%13 == 5:
			fmt.Fprintf(&b, "two%d wordsMMW", i)
		default:
			fmt.Fprintf(&b, "w%dqx-%d", i, i%11)
		}
	}
	return b.String()
}

func giantDocs(text string) (raw, expanded rt.Doc) {
	sp := rt.Span{Kind: rt.Text, Text: text, Br: rt.BrRawFirst}
	raw = doc(para(sp))
	expanded = doc(para(rt.Expand([]rt.Span{sp})...))
	return
}

// TestGiantViewLaysOutAsExpanded: a raw span lays out as the spans it
// stands for: the same lines at the same heights, the same text, the
// same words, and once shaped (materialize) each in the same place; lazy
// words are only the lines that can't wrap.
func TestGiantViewLaysOutAsExpanded(t *testing.T) {
	st := newTestStyle()
	rawDoc, expDoc := giantDocs(giantLines(3000))
	for _, w := range []int{400, 900} {
		a := layoutAt(st, rawDoc, Options{}, w)
		b := layoutAt(st, expDoc, Options{}, w)
		if a.d.lazyN == 0 {
			t.Fatalf("%dpx: no lazy words", w)
		}
		if b.d.lazyN != 0 {
			t.Fatalf("%dpx: lazy words without a raw span", w)
		}
		if a.d.size.Y != b.d.size.Y || len(a.d.lines) != len(b.d.lines) || string(a.d.plain) != string(b.d.plain) || len(a.d.frags) != len(b.d.frags) {
			t.Fatalf("%dpx: %d px, %d lines, %d frags; expanded %d px, %d lines, %d frags", w, a.d.size.Y, len(a.d.lines), len(a.d.frags), b.d.size.Y, len(b.d.lines), len(b.d.frags))
		}
		for i := range a.d.lines {
			if la, lb := a.d.lines[i], b.d.lines[i]; la != lb {
				t.Fatalf("%dpx: line %d %+v, expanded %+v", w, i, la, lb)
			}
		}
		a.setClip()
		a.materialize(st)
		for i := range a.d.frags {
			fa, fb := &a.d.frags[i], &b.d.frags[i]
			if fa.sh == nil || fa.sh.str != fb.sh.str || fa.p0 != fb.p0 || fa.p1 != fb.p1 || fa.y != fb.y || fa.top != fb.top || fa.bot != fb.bot ||
				math.Abs(float64(fa.x-fb.x)) > 0.05 || math.Abs(float64(fa.w-fb.w)) > 0.001 {
				t.Fatalf("%dpx: frag %d (%q) at %.3f w %.3f y %d, expanded (%q) at %.3f w %.3f y %d", w, i, string(a.d.plain[fa.p0:fa.p1]), fa.x, fa.w, fa.y, fb.sh.str, fb.x, fb.w, fb.y)
			}
		}
	}
}

// TestGiantViewRecordsNearTheScreen: a View taller than three screens,
// told where it is on screen, records and shapes only the lines within a
// screen of the visible part, records again as that part moves (from its
// top or its bottom edge), and gives the words it left back.
func TestGiantViewRecordsNearTheScreen(t *testing.T) {
	st := newTestStyle()
	rawDoc, _ := giantDocs(giantLines(5000))
	v := New(rawDoc, Options{})
	var ops op.Ops
	const screen = 900
	shaped := func() (n, outside int) {
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.sh != nil && f.lazy != 0 {
				n++
				if f.bot <= v.clipA || f.top >= v.clipB {
					outside++
				}
			}
		}
		return
	}
	for _, c := range []struct {
		name string
		win  Window
		a, b int // the part on screen, in the View
	}{
		{"top", Window{Edge: 0, Height: screen}, 0, screen},
		{"middle", Window{Edge: -40000, Height: screen}, 40000, 40000 + screen},
		{"bottom", Window{Edge: screen, Bottom: true, Height: screen}, -1, -1},
	} {
		ops.Reset()
		v.SetWindow(c.win)
		v.Layout(testGtx(&ops, 600), st, nil)
		if c.a < 0 {
			c.a, c.b = v.d.size.Y-screen, v.d.size.Y
		}
		if !v.clipOn || v.clipA > c.a || v.clipB < c.b || v.clipB-v.clipA > 3*screen+1 {
			t.Fatalf("%s: recorded [%d, %d) of %d px (clip %v), on screen [%d, %d)", c.name, v.clipA, v.clipB, v.d.size.Y, v.clipOn, c.a, c.b)
		}
		n, outside := shaped()
		if lines := 3 * screen / 22; n == 0 || n > 2*lines || outside > 0 {
			t.Errorf("%s: %d lazy words shaped, %d of them away from the screen; about %d lines are near it", c.name, n, outside, lines)
		}
	}
	// a move within the recorded range records nothing again
	v.SetWindow(Window{Edge: -40000, Height: screen})
	ops.Reset()
	v.Layout(testGtx(&ops, 600), st, nil)
	a := v.clipA
	v.SetWindow(Window{Edge: -40000 - screen/2, Height: screen})
	ops.Reset()
	v.Layout(testGtx(&ops, 600), st, nil)
	if v.clipA != a {
		t.Errorf("half a screen moved the recording: [%d…) after [%d…)", v.clipA, a)
	}
	// no window: all of it, as before
	v.SetWindow(Window{})
	ops.Reset()
	v.Layout(testGtx(&ops, 600), st, nil)
	if n, _ := shaped(); v.clipOn || n != v.d.lazyN {
		t.Errorf("without a window: clip %v, %d of %d lazy words shaped", v.clipOn, n, v.d.lazyN)
	}
}
