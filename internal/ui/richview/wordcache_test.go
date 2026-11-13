package richview

import (
	"strconv"
	"testing"

	"gioui.org/font"
)

// The shaped-word cache is bounded by the bytes its words' glyphs and
// outlines hold, not only by their count (shape.go: at the Hack Club
// account's scale 16,000 words held 120 MB): drawing a stream of distinct
// words keeps the two generations under twice maxCachedWordBytes, and a
// word drawn again after a rotation is the same word, outline and all.
func TestWordCacheByteBudget(t *testing.T) {
	st := newTestStyle()
	k := faceKey{f: font.Font{Typeface: st.Face}, size: fixedPx(15)}
	first := st.word(k, "firstword")
	p0, _ := st.pathOf(first, true)
	retained := func() (n int) {
		for _, w := range st.words {
			n += w.bytes
		}
		for wk, w := range st.prev {
			if _, dup := st.words[wk]; !dup {
				n += w.bytes
			}
		}
		return n
	}
	most := 0
	for i := 0; i < 40000; i++ {
		w := st.word(k, "wordnumber"+strconv.Itoa(i)+"withsomelength")
		st.pathOf(w, true)
		if i%997 == 0 {
			most = max(most, retained())
		}
	}
	most = max(most, retained())
	if most > 2*maxCachedWordBytes+(1<<20) {
		t.Fatalf("the word cache held %d bytes, budget 2 × %d", most, maxCachedWordBytes)
	}
	if most < maxCachedWordBytes/2 {
		t.Fatalf("the word cache held only %d bytes: the byte budget isn't what rotates it", most)
	}
	again := st.word(k, "firstword")
	if again.str != "firstword" || again.adv != first.adv || len(again.glyphs) != len(first.glyphs) {
		t.Fatalf("firstword reshaped wrong: %q adv %v (was %v)", again.str, again.adv, first.adv)
	}
	if p, _ := st.pathOf(again, true); pathBytes(p, len(again.glyphs)) == 0 || pathBytes(p0, len(first.glyphs)) == 0 {
		t.Fatal("firstword has no outline")
	}
}
