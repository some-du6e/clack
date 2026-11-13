package richview

import (
	"unicode/utf8"
	"unsafe"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// faceKey identifies a font at a pixel size.
type faceKey struct {
	f    font.Font
	size fixed.Int26_6
}

type wordKey struct {
	faceKey
	s string
}

// shaped is one word (or word fragment) shaped once and reused by every
// message and every width. Its path is built lazily and kept, so painting a
// word never re-shapes or re-hashes glyphs.
type shaped struct {
	glyphs    []text.Glyph // X relative to the word origin, Y zero
	adv       float32      // advance in px
	asc, desc float32      // font ascent/descent in px
	runes     int
	str       string

	pathDone bool
	bmDone   bool
	path     clip.PathSpec
	// emboldened outline (theme.EmboldenedShape) at width embW
	embW    float32
	embPath clip.PathSpec
	embOK   bool
	bitmaps op.CallOp
	color   bool // has bitmap (color emoji) glyphs
	// bytes is roughly the memory the word holds: its glyphs, then its
	// outlines once built (the shaped-word cache's budget, word)
	bytes int
}

type spaceMetrics struct {
	adv, asc, desc float32
}

// A generation of the shaped-word cache ends at maxCachedWords words or
// maxCachedWordBytes of glyphs and outlines, whichever comes first (word).
// Each word carries its outline, about 1 KB a glyph: at the Hack Club
// account's scale two generations of 8,000 words held 120 MB, most of it
// outlines of words no message on screen used. Views keep the words they
// show, so the cache only has to cover words seen recently; one screen of
// messages is a few hundred words.
const (
	maxCachedWords     = 8000
	maxCachedWordBytes = 8 << 20
)

const glyphSize = int(unsafe.Sizeof(text.Glyph{}))

func f2f(v fixed.Int26_6) float32 { return float32(v) / 64 }

// word shapes s with face k, using the Style's cache.
func (s *Style) word(k faceKey, str string) *shaped {
	wk := wordKey{k, str}
	if w, ok := s.words[wk]; ok {
		return w
	}
	if len(s.words) >= maxCachedWords || s.wordBytes >= maxCachedWordBytes {
		// Two generations: the current one becomes the previous, the older
		// one is dropped. Words used since the last rotation survive (they
		// are promoted on lookup). Views keep pointers to the words they use.
		s.prev = s.words
		s.words = make(map[wordKey]*shaped, min(len(s.prev), maxCachedWords)/2)
		s.wordBytes = 0
	}
	w, ok := s.prev[wk]
	if !ok {
		w = s.shapeNow(k, str, false)
	}
	s.words[wk] = w
	s.wordBytes += w.bytes
	return w
}

func (s *Style) shapeNow(k faceKey, str string, keepSpace bool) *shaped {
	s.Shaper.LayoutString(text.Parameters{
		Font:             k.f,
		PxPerEm:          k.size,
		MaxWidth:         1 << 24,
		WrapPolicy:       text.WrapGraphemes,
		DisableSpaceTrim: keepSpace,
	}, str)
	w := &shaped{str: str, runes: utf8.RuneCountInString(str)}
	var first = true
	var x0 fixed.Int26_6
	var adv fixed.Int26_6
	for g, ok := s.Shaper.NextGlyph(); ok; g, ok = s.Shaper.NextGlyph() {
		theme.ScanGlyph(s.Shaper, g)
		if first {
			first = false
			x0 = g.X
			w.asc, w.desc = f2f(g.Ascent), f2f(g.Descent)
		}
		if g.Flags&text.FlagParagraphBreak != 0 && g.Runes == 0 {
			continue
		}
		g.X -= x0
		g.Y = 0
		adv += g.Advance
		w.glyphs = append(w.glyphs, g)
	}
	w.adv = f2f(adv)
	w.bytes = cap(w.glyphs)*glyphSize + len(str) + int(unsafe.Sizeof(shaped{}))
	if first {
		// Nothing shaped (empty string): take metrics from a space.
		sp := s.spaceOf(k)
		w.asc, w.desc = sp.asc, sp.desc
	}
	return w
}

// spaceOf returns the metrics of a space in face k.
func (s *Style) spaceOf(k faceKey) spaceMetrics {
	if m, ok := s.space[k]; ok {
		return m
	}
	s.Shaper.LayoutString(text.Parameters{
		Font:             k.f,
		PxPerEm:          k.size,
		MaxWidth:         1 << 24,
		DisableSpaceTrim: true,
	}, " ")
	var m spaceMetrics
	var adv fixed.Int26_6
	got := false
	for g, ok := s.Shaper.NextGlyph(); ok; g, ok = s.Shaper.NextGlyph() {
		if !got {
			got = true
			m.asc, m.desc = f2f(g.Ascent), f2f(g.Descent)
		}
		adv += g.Advance
	}
	m.adv = f2f(adv)
	if m.adv <= 0 {
		m.adv = f2f(k.size) / 4
	}
	if !got {
		m.asc, m.desc = f2f(k.size)*0.987, f2f(k.size)*0.213
	}
	s.space[k] = m
	return m
}

// pathOf returns the cached outline (shaped only when needPath) and the
// bitmap ops for w.
func (s *Style) pathOf(w *shaped, needPath bool) (clip.PathSpec, op.CallOp) {
	if !w.bmDone {
		w.bmDone = true
		if len(w.glyphs) > 0 && needsBitmaps(w.str) {
			w.bitmaps = s.Shaper.Bitmaps(w.glyphs)
			w.color = w.bitmaps != (op.CallOp{})
		}
	}
	if needPath && !w.pathDone {
		w.pathDone = true
		if len(w.glyphs) > 0 {
			w.path = s.Shaper.Shape(w.glyphs)
			n := theme.PathBytes(w.path, len(w.glyphs))
			w.bytes += n
			s.wordBytes += n
		}
	}
	return w.path, w.bitmaps
}

// emboldenedOf returns w's outline grown by emb px (one fill instead of the
// outline plus a clip.Stroke), or false for runs in fallback fonts.
func (s *Style) emboldenedOf(w *shaped, emb float32) (clip.PathSpec, bool) {
	if w.embW != emb {
		w.embW = emb
		w.embPath, w.embOK = theme.EmboldenedShape(s.Shaper, w.glyphs, emb)
		if w.embOK {
			n := theme.PathBytes(w.embPath, len(w.glyphs))
			w.bytes += n
			s.wordBytes += n
		}
	}
	return w.embPath, w.embOK
}

// needsBitmaps reports whether s may contain color (emoji) glyphs.
func needsBitmaps(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xE2 {
			return true
		}
	}
	return false
}

// split cuts w into a prefix that fits in maxW px (at least one cluster) and
// the remainder, by glyph clusters. Returns byte lengths too.
func (s *Style) split(k faceKey, w *shaped, maxW float32) (head, tail *shaped, headBytes int) {
	if len(w.glyphs) <= 1 {
		return w, nil, len(w.str)
	}
	cut := -1 // glyph index after which we cut
	for i, g := range w.glyphs {
		if g.Runes == 0 {
			continue
		}
		if f2f(g.X+g.Advance) > maxW && cut >= 0 {
			break
		}
		cut = i
	}
	if cut < 0 || cut >= len(w.glyphs)-1 {
		return w, nil, len(w.str)
	}
	// Count runes up to and including glyph cut.
	nr := 0
	for i := 0; i <= cut; i++ {
		nr += int(w.glyphs[i].Runes)
	}
	b := 0
	for i := 0; i < nr && b < len(w.str); i++ {
		_, sz := utf8.DecodeRuneInString(w.str[b:])
		b += sz
	}
	if b <= 0 || b >= len(w.str) {
		return w, nil, len(w.str)
	}
	return s.word(k, w.str[:b]), s.word(k, w.str[b:]), b
}

// caretX returns the x offset (px) of the boundary before rune index r.
func (w *shaped) caretX(r int) float32 {
	if r <= 0 {
		return 0
	}
	n := 0
	for _, g := range w.glyphs {
		if n >= r {
			return f2f(g.X)
		}
		n += int(g.Runes)
	}
	return w.adv
}

// runeAt returns the rune boundary nearest to x.
func (w *shaped) runeAt(x float32) int {
	if x <= 0 {
		return 0
	}
	n := 0
	for _, g := range w.glyphs {
		mid := f2f(g.X) + f2f(g.Advance)/2
		if x < mid {
			return n
		}
		n += int(g.Runes)
	}
	return w.runes
}
