// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"runtime"
	"strings"
	"testing"
	"weak"

	"gioui.org/font/opentype"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

// releaseOwner owns a text the Shaper reads, as an editor owns the
// textView it shapes through.
type releaseOwner struct {
	r *strings.Reader
	_ [64 << 10]byte
}

type releaseReader struct{ o *releaseOwner }

func (r releaseReader) Read(p []byte) (int, error) { return r.o.r.Read(p) }

// Clack patch 19: once shaped, a text's source is no longer reachable
// from the Shaper.
func TestShaperReleasesSource(t *testing.T) {
	face, _ := opentype.Parse(goregular.TTF)
	sh := NewShaper(NoSystemFonts(), WithCollection([]FontFace{{Face: face}}))
	params := Parameters{PxPerEm: fixed.I(10), MaxWidth: 200, Locale: english}
	o := &releaseOwner{r: strings.NewReader("a field's text\nand a second line")}
	w := weak.Make(o)
	sh.Layout(params, releaseReader{o})
	glyphs := 0
	for {
		if _, ok := sh.NextGlyph(); !ok {
			break
		}
		glyphs++
	}
	if glyphs == 0 {
		t.Fatal("nothing shaped")
	}
	o = nil
	runtime.GC()
	if w.Value() != nil {
		t.Error("the Shaper still reaches the source of the text it shaped")
	}
	sh.LayoutString(params, "a label")
	if sh.iterString != "" {
		t.Error("the Shaper keeps the last string it shaped")
	}
}
