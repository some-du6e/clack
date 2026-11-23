package composer

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"

	"gioui.org/gesture"
)

// The links and slugs Texty's handlers see. TextyInput's onContextMenu
// (boot-shared LPC/, build 83aff18a1847) acts when the right-click's
// e.target is an <a> or a <ts-slug> (bHl3: "A", "TS-SLUG"), and its
// link click (maybeHandleLinkClick) when the click's target is in an <a>
// (closest("a")). Quill nests its inline formats outer to inner as
// Inline.order has them (underline, strike, italic, bold, script, link,
// code), so an <a> holds its text directly unless the text is inline
// code: a bold link is <strong><a>…</a></strong>, its target the <a>,
// and a code link <a><code>…</code></a>, whose right-click targets the
// <code> (Slack's handler lets that go to the desktop menu). An <a> ends
// where its link or one of the formats outside it changes. A slug is
// Quill's slackslug format on its label, whose text node it holds, so it
// is the target itself (Clack's slug is an atom: one rune).

// edElem is the element a pointer event targets.
type edElem struct {
	slug   bool  // a <ts-slug> (a link or message slug atom)
	atom   *Atom // the slug's atom
	inner  bool  // the target is the <code> inside the <a>
	lo, hi int   // the element's runes (the whole <a>, or the slug)
	url    string
	text   string          // its textContent
	rect   image.Rectangle // getBoundingClientRect, editor-local (viewport) px
}

// linkOuter is the part of a rune's style Quill draws outside its <a>.
func linkOuter(s Style) Style { return s &^ Code }

// linkElemRange is the <a> rune i is in (Attrs[i].Link set).
func (d *Doc) linkElemRange(i int) (lo, hi int) {
	l, s := d.Attrs[i].Link, linkOuter(d.Attrs[i].Style)
	same := func(j int) bool {
		a := d.Attrs[j]
		return a.Atom == nil && d.Runes[j] != '\n' && a.Link == l && linkOuter(a.Style) == s
	}
	lo, hi = i, i+1
	for lo > 0 && same(lo-1) {
		lo--
	}
	for hi < d.Len() && same(hi) {
		hi++
	}
	return lo, hi
}

// codeRunIn is the <code> inside an <a> that rune i is in.
func (d *Doc) codeRunIn(i, lo, hi int) (int, int) {
	a, b := i, i+1
	for a > lo && d.Attrs[a-1].Style&Code != 0 {
		a--
	}
	for b < hi && d.Attrs[b].Style&Code != 0 {
		b++
	}
	return a, b
}

// deltaOpAt is 6ZFk over the document's delta: the op holding index t
// (start <= t < end), a run of runes with the same inline formats; an
// embed is an op of its own. ok is false past the end.
func (d *Doc) deltaOpAt(t int) (lo, hi int, ok bool) {
	if t < 0 || t >= d.Len() {
		return 0, 0, false
	}
	if d.Attrs[t].Atom != nil {
		return t, t + 1, true
	}
	a := d.Attrs[t]
	same := func(j int) bool {
		b := d.Attrs[j]
		return b.Atom == nil && b.Link == a.Link && b.Style == a.Style
	}
	lo, hi = t, t+1
	for lo > 0 && same(lo-1) {
		lo--
	}
	for hi < d.Len() && same(hi) {
		hi++
	}
	return lo, hi, true
}

// isLinkOp is vmJG isOpLink for the op [lo, hi): text with a link.
func (d *Doc) isLinkOp(lo, hi int, ok bool) bool {
	return ok && hi > lo && d.Attrs[lo].Atom == nil && d.Attrs[lo].Link != ""
}

// coverLink is lQ9i adjustSelectionToCoverLink for a collapsed range at
// index l whose format has a link (hasLink): the link op at l (or, when
// the op at l is no link, the one before it), to the end of the op at l
// when that is a link.
func (d *Doc) coverLink(l int, hasLink bool) (lo, hi int) {
	lo, hi = l, l
	h0, h1, hok := d.deltaOpAt(l)
	if !d.isLinkOp(h0, h1, hok) && l > 0 && hasLink {
		h0, h1, hok = d.deltaOpAt(l - 1)
	}
	if d.isLinkOp(h0, h1, hok) {
		lo = h0
		hi = l // length o + (l - index), o = 0
	}
	if u0, u1, uok := d.deltaOpAt(l); d.isLinkOp(u0, u1, uok) {
		hi = u1
	}
	return lo, hi
}

// formatLinkAt is quill.getFormat({index, length: 0}).link: a collapsed
// caret reads the leaf before it (Block.path is inclusive), the first one
// at a line's start.
func (d *Doc) formatLinkAt(pos int) string {
	pos = d.clamp(pos)
	if pos > 0 && d.Runes[pos-1] != '\n' {
		return d.Attrs[pos-1].Link
	}
	if pos < d.Len() && d.Runes[pos] != '\n' {
		return d.Attrs[pos].Link
	}
	return ""
}

// runeBox is rune i's inline box on the visual line under pt (layout
// coordinates): its advance across, the font's ascent and descent down,
// as Blink hit-tests a text fragment; an atom's pill box.
func (l *tlLayout) runeBox(d *Doc, i int, pt image.Point) (image.Rectangle, bool) {
	if d.Attrs[i].Atom != nil {
		return l.atomBox(i, pt)
	}
	var hit image.Rectangle
	ok := false
	l.forRange(i, i+1, func(li, x0, x1 int, _ bool) {
		ln := &l.lines[li]
		p := &l.paras[ln.para]
		r := image.Rect(min(x0, x1), max(ln.top, ln.base-p.asc), max(x0, x1), min(ln.top+ln.h, ln.base+p.desc))
		if !ok && pt.In(r) {
			hit, ok = r, true
		}
	})
	return hit, ok
}

// runeUnder is the rune whose box holds pt (editor-local px).
func (e *Editor) runeUnder(pt image.Point) (int, bool) {
	if !e.ensureLayout() {
		return 0, false
	}
	p := pt.Add(image.Pt(0, e.scrollY))
	d := e.doc
	pos := d.clamp(e.lay.hit(p))
	for _, i := range [...]int{pos - 1, pos} {
		if i < 0 || i >= d.Len() || d.Runes[i] == '\n' {
			continue
		}
		if _, ok := e.lay.runeBox(d, i, p); ok {
			return i, true
		}
	}
	return 0, false
}

// rangeBox is the bounding box of [lo, hi)'s inline boxes, editor-local
// px: an element's getBoundingClientRect.
func (e *Editor) rangeBox(lo, hi int) image.Rectangle {
	if !e.ensureLayout() || hi <= lo {
		return image.Rectangle{}
	}
	l := &e.lay
	var r image.Rectangle
	l.forRange(lo, hi, func(li, x0, x1 int, _ bool) {
		ln := &l.lines[li]
		p := &l.paras[ln.para]
		b := image.Rect(min(x0, x1), max(ln.top, ln.base-p.asc), max(x0, x1), min(ln.top+ln.h, ln.base+p.desc))
		if r.Empty() {
			r = b
		} else {
			r = r.Union(b)
		}
	})
	if d := e.doc; hi-lo == 1 && d.Attrs[lo].Atom != nil {
		// a slug: its pill, which stands a border out of the text
		l.forRange(lo, hi, func(li, x0, _ int, _ bool) {
			if b, ok := l.atomBox(lo, image.Pt(x0, l.lines[li].base)); ok {
				r = r.Union(b)
			}
		})
	}
	return r.Sub(image.Pt(0, e.scrollY))
}

// elemAt is the link or slug targeted at pt (editor-local px).
func (e *Editor) elemAt(pt image.Point) (edElem, bool) {
	i, ok := e.runeUnder(pt)
	if !ok {
		return edElem{}, false
	}
	d := e.doc
	a := d.Attrs[i]
	if at := a.Atom; at != nil {
		if at.Kind != AtomLink && at.Kind != AtomMessage {
			return edElem{}, false
		}
		return edElem{slug: true, atom: at, lo: i, hi: i + 1, url: at.URL, text: at.Text, rect: e.rangeBox(i, i+1)}, true
	}
	if a.Link == "" {
		return edElem{}, false
	}
	lo, hi := d.linkElemRange(i)
	el := edElem{inner: a.Style&Code != 0, lo: lo, hi: hi, url: a.Link, text: d.String(lo, hi), rect: e.rangeBox(lo, hi)}
	return el, true
}

// linkClick is what TextyInput's onLinkClick gets from a click in an <a>
// (maybeHandleLinkClick): the <a>'s box, its link, and the range the
// popover edits.
type linkClick struct {
	el             edElem
	lo, hi         int // adjustSelectionToCoverLink's range
	initLo, initHi int // the selection the click left (initialRange)
}

// noteLinkPress remembers what a primary press targeted, for the click
// its release makes (Chromium's click targets the press's and the
// release's common ancestor).
func (e *Editor) noteLinkPress(ev gesture.ClickEvent) {
	e.linkPress, e.linkPressOK = edElem{}, false
	if ev.Source == pointer.Mouse && e.OnLinkClick != nil {
		e.linkPress, e.linkPressOK = e.elemAt(ev.Position)
		e.linkPressAt = ev.Position
	}
}

// onLinkRelease is maybeHandleLinkClick: a click whose press and release
// were in the same <a>, not within 5px of either end of one at least
// 20px wide, selects the link op it's in and hands it to OnLinkClick
// (TextyInput.onLinkClick, which ComposerEditLinkPopover answers). The
// markup pref (isPlainTextModeEnabledPref) has no links to click.
func (e *Editor) onLinkRelease(gtx layout.Context, ev gesture.ClickEvent) {
	pr, ok := e.linkPress, e.linkPressOK
	e.linkPressOK = false
	if !ok || e.OnLinkClick == nil || e.plainText || ev.Source != pointer.Mouse || pr.slug {
		return
	}
	el, ok := e.elemAt(ev.Position)
	if !ok || el.slug || el.lo != pr.lo || el.hi != pr.hi || el.url != pr.url {
		return
	}
	r := el.rect
	x := ev.Position.X
	if r.Dx() >= gtx.Dp(20) && (x-r.Min.X < gtx.Dp(5) || r.Max.X-x < gtx.Dp(5)) {
		return
	}
	// r: the target's offset (the <a>, or the <code> both ends were in),
	// then the link op at r+1
	d := e.doc
	start := el.lo
	pi, ok1 := e.runeUnder(e.linkPressAt)
	ri, ok2 := e.runeUnder(ev.Position)
	if ok1 && ok2 && d.Attrs[pi].Style&Code != 0 && d.Attrs[ri].Style&Code != 0 {
		if a, b := d.codeRunIn(pi, el.lo, el.hi); ri >= a && ri < b {
			start = a
		}
	}
	lo, hi := d.coverLink(start+1, d.formatLinkAt(start+1) != "")
	il, ih := e.sel()
	// the click event comes after mouseup: the release's caret is placed
	// already, and the selection the handler sets stands
	e.dragging = false
	e.OnLinkClick(gtx, linkClick{el: el, lo: lo, hi: hi, initLo: il, initHi: ih})
}
