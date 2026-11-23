package richview

import (
	"image"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
)

// Messages of hundreds of thousands of characters (a channel Heliodex
// reported, 2026-11-20: 92 of its latest 100 messages each about 908,000
// characters in 92,000 lines). Slack draws all of such a message: its
// message pane cuts nothing (richtext/rawtext.go), and Chromium lays the
// whole text out but paints only what intersects the viewport, its glyphs
// out of one cache. Laid out and recorded whole, a View held every word's
// shaped glyphs and outline, about a gigabyte a message.
//
//   - A long rich_text element is one raw span (richtext RawLines):
//     addRaw lays out the spans it stands for one at a time.
//   - Lazy words: a line of a raw span that starts a line and ends at a
//     break, in the paragraph's own face (no style but bold and italic,
//     no link, pill or CSS part) and in printable ASCII, is laid out
//     unshaped when even its widest glyphs fit the flow's width
//     (advBound): it can't wrap, so it is one line box whatever its exact
//     width. Its frags are shaped when their line comes near the screen
//     (materialize) and give their shaped words back once it's gone.
//   - A View taller than three screens whose owner says where it is on
//     screen (SetWindow: the message list's rows) records only the
//     primitives within a screen of the visible part, and records again
//     when the visible part leaves that range.

// Window says where a View is on screen (SetWindow).
type Window struct {
	// Edge is the View's top edge in screen coordinates (negative above
	// the screen's top), or its bottom edge when Bottom is set.
	Edge   int
	Bottom bool
	// Height is the screen's height; 0 is no window: all of the View is
	// recorded.
	Height int
}

// SetWindow tells the View where it is on screen before Layout. A View
// taller than three screens then records, and shapes, only what is within
// a screen of the visible part.
func (v *View) SetWindow(w Window) { v.win = w }

// clipped reports whether v records a window of itself: it's taller than
// three screens of its Window.
func (v *View) clipped() bool {
	return v.win.Height > 0 && v.d.size.Y > 3*v.win.Height
}

// visible is the part of v on screen, in v's coordinates.
func (v *View) visible() (a, b int) {
	top := v.win.Edge
	if v.win.Bottom {
		top -= v.d.size.Y
	}
	return -top, -top + v.win.Height
}

// windowStale reports whether the recording no longer covers the visible
// part (it was made for another part of the View, or for all of it).
func (v *View) windowStale() bool {
	if !v.clipped() {
		return v.clipOn // recorded clipped, now drawn whole
	}
	if !v.clipOn {
		return true
	}
	a, b := v.visible()
	return a < v.clipA || b > v.clipB
}

// setClip picks the range a clipped recording covers: a screen above and
// below the visible part. Unclipped, it's everything.
func (v *View) setClip() {
	if !v.clipped() {
		v.clipOn = false
		return
	}
	a, b := v.visible()
	m := v.win.Height
	v.clipOn, v.clipA, v.clipB = true, a-m, b+m
}

// inClip reports whether the rows [top, bot) are in the recording's range.
func (v *View) inClip(top, bot int) bool {
	return !v.clipOn || bot > v.clipA && top < v.clipB
}

func (v *View) rectInClip(r image.Rectangle) bool { return v.inClip(r.Min.Y, r.Max.Y) }

// faceIndex is k's lazy face number (frag.lazy).
func (d *display) faceIndex(k faceKey) uint16 {
	for i := range d.faces {
		if d.faces[i] == k {
			return uint16(i + 1)
		}
	}
	d.faces = append(d.faces, k)
	return uint16(len(d.faces))
}

// advBound is a width no printable ASCII rune in face k exceeds, kerning
// included: the widest such glyph's advance, a quarter more and a pixel.
func (s *Style) advBound(k faceKey) float32 {
	if b, ok := s.bound[k]; ok {
		return b
	}
	var ascii [95]byte
	for i := range ascii {
		ascii[i] = byte(' ' + i)
	}
	w := s.shapeNow(k, string(ascii[:]), true)
	var m float32
	for _, g := range w.glyphs {
		if a := f2f(g.Advance); a > m {
			m = a
		}
	}
	if m <= 0 {
		m = f2f(k.size)
	}
	b := m*1.25 + 1
	if s.bound == nil {
		s.bound = map[faceKey]float32{}
	}
	s.bound[k] = b
	return b
}

// lazyText reports whether t is all printable ASCII without tabs: text
// whose every rune Lato has and whose width advBound bounds.
func lazyText(t string) bool {
	for i := 0; i < len(t); i++ {
		if c := t[i]; c < ' ' || c > '~' {
			return false
		}
	}
	return t != ""
}

// addRaw lays out raw span sp: the spans it stands for one by one
// (richtext.Pieces), the lines that can't wrap as lazy words.
func (l *lay) addRaw(sp *richtext.Span, fs *flowStyle) {
	lazyOK := l.lazyW > 0 && l.css == nil && !l.noPlain && fs.maxLines == 0 && !fs.ellipsis &&
		sp.Style&^(richtext.Bold|richtext.Italic) == 0
	var k faceKey
	var bound, space float32
	if lazyOK {
		k = l.faceFor(fs.face, fs.size, sp.Style)
		bound, space = l.st.advBound(k), l.st.spaceOf(k).adv
	}
	// fresh: the next piece starts a line (the flow's first, or after a
	// break that ends one); held: a text piece kept until the next piece
	// says whether a break ends its line
	fresh := len(l.items) == 0 && l.lead == 0
	// a clamped flow (fs.maxLines: an Activity row's preview) draws its
	// first lines only: past a line and a break more than it can hold,
	// the rest of a raw span is never laid out
	ends := 0
	var held richtext.Span
	holding, heldFresh := false, false
	flush := func(endsLine bool) {
		if !holding {
			return
		}
		holding = false
		if lazyOK && heldFresh && endsLine && lazyText(held.Text) {
			w := float32(0)
			for i := 0; i < len(held.Text); i++ {
				if held.Text[i] == ' ' {
					w += space
				} else {
					w += bound
				}
			}
			if w <= l.lazyW {
				l.addTextRunAs(held.Text, fs, k, fs.col, held.Style, -1, -1, bound)
				return
			}
		}
		l.addSpan(&held, fs)
	}
	// a break as addSpan adds one (a Break has no style or part), without
	// its per-span setup: a raw span is tens of thousands of them
	kb := l.faceFor(fs.face, fs.size, 0)
	// room for what it adds, at once rather than by doublings: an item a
	// word and a break, a frag and a line box a line, its text
	if fs.maxLines == 0 {
		n := strings.Count(sp.Text, "\n") + 1
		l.items = slices.Grow(l.items, 2*n)
		l.d.frags = slices.Grow(l.d.frags, n)
		l.d.lines = slices.Grow(l.d.lines, n)
		l.d.plain = slices.Grow(l.d.plain, len(sp.Text))
	}
	richtext.Pieces(*sp, func(p richtext.Span) bool {
		if p.Kind == richtext.Break {
			end := p.Br != richtext.BrNone
			flush(end)
			if l.css != nil {
				l.addSpan(&p, fs)
			} else {
				p0, p1 := l.plainAdd("\n")
				l.items = append(l.items, item{kind: itBreak, face: kb, deco: -1, target: -1, p0: p0, p1: p1, br: p.Br})
			}
			if end {
				fresh = true
				ends++
			}
			return fs.maxLines == 0 || ends <= fs.maxLines+1
		}
		flush(false)
		held, holding, heldFresh = p, true, fresh
		fresh = false
		return true
	})
	flush(false)
}

// materialize shapes the lazy words of the lines within the recording's
// range and lets those outside it give theirs back (the shaped-word cache
// keeps the recent ones). A line's words shaped, they move to their
// exact places: each word's x moves by what the words before it on its
// line came out narrower than their bounds.
func (v *View) materialize(st *Style) {
	d := &v.d
	if d.lazyN == 0 {
		return
	}
	for li := range d.lines {
		ln := &d.lines[li]
		if ln.f1 <= ln.f0 || d.frags[ln.f0].lazy == 0 {
			continue
		}
		in := v.inClip(ln.top, ln.bot)
		var shift float32
		for fi := ln.f0; fi < ln.f1; fi++ {
			f := &d.frags[fi]
			if f.lazy == 0 {
				continue
			}
			f.x += shift
			switch {
			case in && f.sh == nil:
				sh := st.word(d.faces[f.lazy-1], string(d.plain[f.p0:f.p1]))
				shift += sh.adv - f.w
				f.sh, f.w = sh, sh.adv
			case !in && f.sh != nil:
				f.sh = nil // its width stays the exact one
			}
		}
	}
}
