package richview

import (
	"strings"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A clamp's "…" that a component card owns is the card's: the card draws
// it (paintAbove with its tag), so it goes when a plugin hides the card and
// isn't painted a second time over everything by paintEllipses, which
// draws only the static ones. Round 3 painted every "…" there: twice on
// every clamped card under any plugin CSS, and left on top of the next
// block when the card was hidden.
func TestOwnedEllipsesStayWithTheirCard(t *testing.T) {
	// a link preview (MessageAttachmentGeneric v3) clamps its title and
	// text to two lines with a "…"
	long := strings.Repeat("a long link preview title that has to be clamped ", 12)
	d := embedDoc(t, "text only")
	att := attBlock(t, d).Att
	att.Title = []rt.Span{txt(long)}
	att.Text = []rt.Block{para(txt(long))}
	st := newTestStyle()

	plain := layoutAt(st, d, Options{}, 320)
	static := 0
	for i := range plain.d.frags {
		if staticEllipsis(&plain.d.frags[i]) {
			static++
		}
	}
	if static == 0 {
		t.Fatal("the unpatched link preview clamps nothing: no \"…\" to check")
	}

	patchAll(t)
	v := layoutAt(st, d, Options{}, 320)
	owned := 0
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if !f.ell {
			continue
		}
		if f.comp == 0 && f.hov == 0 {
			t.Errorf("frag %d: the patched card's \"…\" has no owner", i)
			continue
		}
		if staticEllipsis(f) {
			t.Errorf("frag %d: a component's \"…\" counts as static, so paintEllipses paints it again", i)
		}
		owned++
	}
	if owned == 0 {
		t.Fatal("the patched card has no \"…\"")
	}

	// a hover layer's "…" is its layer's too
	if staticEllipsis(&frag{ell: true, hov: 1}) {
		t.Error("a hover layer's \"…\" counts as static: drawn while its card isn't hovered")
	}
	if !staticEllipsis(&frag{ell: true}) || staticEllipsis(&frag{}) {
		t.Error("staticEllipsis is wrong for a plain \"…\" or a plain frag")
	}
}
