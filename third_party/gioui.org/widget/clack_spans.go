// SPDX-License-Identifier: Unlicense OR MIT

package widget

// Clack patch 6 (CLACK_PATCHES.md): styled spans in an Editor's text, for
// inline tokens drawn as pills (Slack's search box filter tokens: a
// regular modifier, a bold value, padding around the pill).

import (
	"io"
	"math"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

// TextSpan styles runes [Start, End) of an Editor's text. Font, if not
// nil, shapes them in that font instead of the Editor's. Before and After
// are empty space, in pixels, before the span's first rune and after its
// last. Carets, selections, Regions and pointer hits follow the styled
// layout.
//
// Spans apply to a single-line, unmasked Editor whose text has no line
// break; otherwise the text is shaped as usual. Spans are in order and
// don't overlap; parts outside the text, or overlapping an earlier span,
// are dropped.
type TextSpan struct {
	Start, End    int
	Font          *font.Font
	Before, After int
}

// SetSpans replaces the editor's spans. The text is shaped again only when
// they change.
func (e *Editor) SetSpans(spans []TextSpan) {
	if spansEqual(e.text.spans, spans) {
		return
	}
	e.text.spans = append(e.text.spans[:0], spans...)
	e.text.invalidate()
}

// Spans are the editor's spans (SetSpans).
func (e *Editor) Spans() []TextSpan { return e.text.spans }

func spansEqual(a, b []TextSpan) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Start != y.Start || x.End != y.End || x.Before != y.Before || x.After != y.After {
			return false
		}
		if (x.Font == nil) != (y.Font == nil) || x.Font != nil && *x.Font != *y.Font {
			return false
		}
	}
	return true
}

// spanSegment is a run of the text shaped in one font, with the space
// around it.
type spanSegment struct {
	start, end    int // runes
	font          font.Font
	before, after int
}

// spanScratch holds layoutSpans' buffers between layouts.
type spanScratch struct {
	text   []byte
	offs   []int // byte offset of each rune, and of the end
	segs   []spanSegment
	glyphs []text.Glyph
}

// layoutSpans is layoutText for a text with spans: the text shaped in
// segments split at the spans' edges, each in its font, and every glyph
// moved right by the width of what comes before it (the earlier segments
// and the spans' space). It returns false, having shaped nothing and left
// the text's read position at the start, when the spans don't apply.
func (e *textView) layoutSpans(lt *text.Shaper, it *textIterator) bool {
	if len(e.spans) == 0 || !e.SingleLine || e.Mask != 0 {
		return false
	}
	sc := &e.span
	sc.text = e.Text(sc.text[:0])
	e.Seek(0, io.SeekStart)
	txt := sc.text
	if len(txt) == 0 {
		return false
	}
	sc.offs = sc.offs[:0]
	for i := 0; i < len(txt); {
		r, n := utf8.DecodeRune(txt[i:])
		switch r {
		case '\n', '\r', ' ', ' ', '\u0085', '\u000b', '\u000c':
			return false
		}
		sc.offs = append(sc.offs, i)
		i += n
	}
	nr := len(sc.offs)
	sc.offs = append(sc.offs, len(txt))

	base := e.params.Font
	segs := sc.segs[:0]
	pos := 0
	for _, sp := range e.spans {
		s, t := min(max(sp.Start, pos), nr), min(max(sp.End, 0), nr)
		if t < s {
			continue
		}
		if s > pos {
			segs = append(segs, spanSegment{start: pos, end: s, font: base})
		}
		f := base
		if sp.Font != nil {
			f = *sp.Font
		}
		segs = append(segs, spanSegment{start: s, end: t, font: f, before: sp.Before, after: sp.After})
		pos = t
	}
	if pos < nr {
		segs = append(segs, spanSegment{start: pos, end: nr, font: base})
	}
	sc.segs = segs

	p := e.params
	p.MaxWidth, p.MinWidth, p.Alignment, p.MaxLines = math.MaxInt, 0, text.Start, 0
	glyphs := sc.glyphs[:0]
	var x fixed.Int26_6
	for _, sg := range segs {
		x += fixed.I(sg.before)
		if sg.start < sg.end {
			p.Font = sg.font
			lt.LayoutString(p, string(txt[sc.offs[sg.start]:sc.offs[sg.end]]))
			end := x
			for g, ok := lt.NextGlyph(); ok; g, ok = lt.NextGlyph() {
				g.X += x
				end = max(end, g.X+g.Advance)
				glyphs = append(glyphs, g)
			}
			x = end
		}
		x += fixed.I(sg.after)
	}
	sc.glyphs = glyphs
	// one line: only the last glyph ends it, only the first starts the
	// paragraph
	for i := range glyphs {
		if i < len(glyphs)-1 {
			glyphs[i].Flags &^= text.FlagLineBreak
		}
		if i > 0 {
			glyphs[i].Flags &^= text.FlagParagraphStart
		}
	}
	for _, g := range glyphs {
		if !it.processGlyph(g, true) {
			break
		}
		e.index.Glyph(g)
	}
	// the space before the first span and after the last is the text's
	if len(glyphs) > 0 {
		it.bounds.Min.X = min(it.bounds.Min.X, 0)
		it.bounds.Max.X = max(it.bounds.Max.X, x.Ceil())
	}
	return true
}
