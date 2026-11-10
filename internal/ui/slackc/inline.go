package slackc

import (
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// The inline formatting context: text runs from any number of nested
// inline elements, atomic inline boxes (inline-block, images, svg,
// components, form controls) and forced breaks, broken into line boxes.

type itemKind uint8

const (
	iWord itemKind = iota
	iSpace
	iBreak
	iOpen
	iClose
	iAtomic
	iEmoji
	iAbs
)

type item struct {
	kind  itemKind
	b     *box // style element (text), the inline box (open/close), the atomic/abs box
	sh    *shaped
	fk    faceKey
	w     float32
	url   string
	brk   bool // break opportunity after this item
	hang  bool // collapsible space: hangs at line end, dropped at line start
	deco  css.TextDecoration
	dcol  color.NRGBA
	edge  float32 // open/close: margin+border+padding on that side
	medge float32 // open/close: the margin part
}

type frag struct {
	it     int32
	x, y   float32 // left, baseline (IFC content coordinates)
	w      float32
	sh     *shaped // may be a truncated head of the item's word
	ell    bool    // an ellipsis
	hidden bool    // cut by text-overflow / line-clamp
}

type lineBox struct {
	top, bot, base float32
	f0, f1         int32
	w              float32
	phantom        bool // only out-of-flow placeholders: no height
}

type span struct {
	b           *box
	x0, x1      float32
	top, bot    float32
	line        int32
	first, last bool
}

type ifc struct {
	items    []item
	frags    []frag
	lines    []lineBox
	spans    []span
	gen      uint32 // styleGen the items were built for
	nbuilt   int    // items from buildItems (ellipses are appended after)
	w, h     float32
	base     float32
	lastBase float32
}

// run is a sequence of inline-level boxes in a block container, laid
// out as one anonymous block.
type run struct {
	start, end int // b.lk range
	f          ifc
	y          float32 // top of the anonymous block within the container's content box
}

type tok struct {
	kind itemKind
	sh   *shaped
	w    float32
	url  string
	brk  bool
	hang bool
}

type textCache struct {
	src  string
	ws   css.WhiteSpace
	tt   css.TextTransform
	fk   faceKey
	wsp  float32
	esz  float32
	toks []tok
}

func collapses(ws css.WhiteSpace) bool {
	return ws == css.WSNormal || ws == css.WSNoWrap || ws == css.WSPreLine
}

func wraps(ws css.WhiteSpace) bool { return ws != css.WSNoWrap && ws != css.WSPre }

func isCJK(r rune) bool {
	return r >= 0x2E80 && (r <= 0x9FFF || (r >= 0xAC00 && r <= 0xD7AF) || (r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFF00 && r <= 0xFFEF) || (r >= 0x20000 && r <= 0x2FA1F) || (r >= 0x3000 && r <= 0x30FF))
}

func transformText(s string, tt css.TextTransform) string {
	switch tt {
	case css.TTUppercase:
		return strings.ToUpper(s)
	case css.TTLowercase:
		return strings.ToLower(s)
	case css.TTCapitalize:
		var b strings.Builder
		prevLetter := false
		for _, r := range s {
			if !prevLetter && unicode.IsLetter(r) {
				r = unicode.ToTitle(r)
			}
			prevLetter = unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\''
			b.WriteRune(r)
		}
		return b.String()
	}
	return s
}

// textToks returns the shaped tokens of a text box under style st.
func (v *View) textToks(t *box, st *css.Style) []tok {
	fk := v.face(st)
	wsp := st.WordSpacing * v.dp
	c := t.tc
	if c != nil && c.src == t.text && c.ws == st.WhiteSpace && c.tt == st.TextTransform && c.fk == fk && c.wsp == wsp {
		return c.toks
	}
	if c == nil {
		c = &textCache{}
		t.tc = c
	}
	c.src, c.ws, c.tt, c.fk, c.wsp = t.text, st.WhiteSpace, st.TextTransform, fk, wsp
	c.toks = c.toks[:0]
	c.esz = f26(fk.size) * 1.2
	s := transformText(t.text, st.TextTransform)
	ws := st.WhiteSpace
	coll := collapses(ws)
	wrap := wraps(ws)
	keepNL := !coll || ws == css.WSPreLine
	sp := spaceOf(fk)
	var wb strings.Builder
	flushWord := func(brk bool) {
		if wb.Len() == 0 {
			return
		}
		w := wb.String()
		wb.Reset()
		if segs := splitEmoji(w); segs != nil {
			for i, sg := range segs {
				last := i == len(segs)-1
				if sg.stem != "" {
					c.toks = append(c.toks, tok{kind: iEmoji, url: emojiURL(sg.stem), w: c.esz + v.dp, brk: last && brk})
					continue
				}
				sh := word(fk, sg.text)
				c.toks = append(c.toks, tok{kind: iWord, sh: sh, w: sh.adv, brk: last && brk})
			}
			return
		}
		sh := word(fk, w)
		c.toks = append(c.toks, tok{kind: iWord, sh: sh, w: sh.adv, brk: brk})
	}
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch {
		case r == '\n' || r == '\r':
			if r == '\r' && i < len(s) && s[i] == '\n' {
				i++
			}
			flushWord(false)
			if keepNL {
				c.toks = append(c.toks, tok{kind: iBreak})
			} else {
				c.toks = append(c.toks, tok{kind: iSpace, w: sp.adv + wsp, brk: wrap, hang: true})
			}
		case r == ' ' || r == '\t' || r == '\f' || r == 0x200B:
			flushWord(false)
			if r == 0x200B {
				c.toks = append(c.toks, tok{kind: iSpace, brk: wrap, hang: true})
				continue
			}
			w := sp.adv + wsp
			if r == '\t' && !coll {
				w = sp.adv * 8
			}
			c.toks = append(c.toks, tok{kind: iSpace, w: w, brk: wrap, hang: coll || ws == css.WSPreWrap})
		case r == 0xA0: // nbsp: a non-breaking space glued into the word
			wb.WriteRune(r)
		case isCJK(r) && wrap:
			flushWord(false)
			wb.WriteRune(r)
			flushWord(true)
		default:
			wb.WriteRune(r)
			if r == '-' && wrap && i < len(s) {
				nr, _ := utf8.DecodeRuneInString(s[i:])
				if unicode.IsLetter(nr) || unicode.IsDigit(nr) {
					flushWord(true)
				}
			}
		}
	}
	flushWord(false)
	return c.toks
}

// buildItems flattens a run's boxes into inline items.
func (v *View) buildItems(f *ifc, owner *box, boxes []*box) {
	f.items = f.items[:0]
	prevSpace := true // collapse leading whitespace
	var walk func(list []*box, sb *box, deco css.TextDecoration, dcol color.NRGBA)
	walk = func(list []*box, sb *box, deco css.TextDecoration, dcol color.NRGBA) {
		for _, k := range list {
			if k.kind == kText {
				st := &sb.st
				coll := collapses(st.WhiteSpace)
				fk := v.face(st)
				for _, t := range v.textToks(k, st) {
					if t.kind == iSpace && coll {
						if prevSpace {
							continue
						}
						prevSpace = true
					} else if t.kind != iSpace {
						prevSpace = t.kind == iBreak
					} else {
						prevSpace = false
					}
					f.items = append(f.items, item{kind: t.kind, b: sb, sh: t.sh, fk: fk, w: t.w, url: t.url, brk: t.brk, hang: t.hang, deco: deco, dcol: dcol})
				}
				continue
			}
			if k.st.Display == css.DisplayNone {
				continue
			}
			if outOfFlow(&k.st) {
				f.items = append(f.items, item{kind: iAbs, b: k})
				continue
			}
			if k.repl == rBr {
				f.items = append(f.items, item{kind: iBreak, b: k})
				prevSpace = true
				continue
			}
			if v.isInlineBox(k) {
				d, dc := deco, dcol
				if k.st.TextDecoration.Lines() != 0 {
					d |= k.st.TextDecoration
					dc = k.st.DecoColor
					if dc.A == 0 {
						dc = k.st.Color
					}
				}
				f.items = append(f.items, item{kind: iOpen, b: k})
				walk(k.lk, k, d, dc)
				f.items = append(f.items, item{kind: iClose, b: k})
				continue
			}
			wrap := wraps(sb.st.WhiteSpace)
			if n := len(f.items); n > 0 && wrap {
				f.items[n-1].brk = true
			}
			f.items = append(f.items, item{kind: iAtomic, b: k, brk: wrap})
			prevSpace = false
		}
	}
	d, dc := owner.st.TextDecoration, owner.st.DecoColor
	if dc.A == 0 {
		dc = owner.st.Color
	}
	walk(boxes, owner, d, dc)
	f.gen = v.styleGen
	f.nbuilt = len(f.items)
}

// isInlineBox reports a non-atomic inline element (its content flows in
// the parent's lines).
func (v *View) isInlineBox(k *box) bool {
	if k.st.Display != css.DisplayInline {
		return false
	}
	if k.kind == kPseudo {
		return true
	}
	return k.kind == kElem && k.repl == rNone && k.n.T != "button" && k.n.T != "input"
}

// lineHeight returns the used line height in px for st.
func (v *View) lineHeight(st *css.Style) float32 {
	fs := st.FontSize * v.sp
	lh := st.LineHeight
	if lh.Kind != css.LenFixed {
		return fs * 1.2
	}
	return lh.Px*v.dp + lh.Pct*fs/100
}

// metrics returns the half-leading-adjusted ascent/descent of st's font.
func (v *View) metrics(st *css.Style) (asc, desc, fasc, fdesc float32) {
	fk := v.face(st)
	m := spaceOf(fk)
	lh := v.lineHeight(st)
	hl := (lh - (m.asc + m.desc)) / 2
	return m.asc + hl, m.desc + hl, m.asc, m.desc
}

const inf = float32(1e7)

// layoutIFC breaks a run into lines at width avail (content box). It
// returns the used width (longest line) and height.
func (v *View) layoutIFC(f *ifc, owner *box, boxes []*box, avail, cbH float32, measure bool) (float32, float32) {
	if f.gen != v.styleGen || f.items == nil {
		v.buildItems(f, owner, boxes)
	}
	f.items = f.items[:f.nbuilt]
	ost := &owner.st
	f.frags = f.frags[:0]
	f.lines = f.lines[:0]
	f.spans = f.spans[:0]
	f.w, f.h, f.base, f.lastBase = 0, 0, -1, -1

	// Edges of inline boxes and atomic sizes.
	for i := range f.items {
		it := &f.items[i]
		switch it.kind {
		case iOpen, iClose:
			k := it.b
			v.edges(k, avail)
			if it.kind == iOpen {
				it.medge = k.m[3]
				it.edge = k.m[3] + k.bd[3] + k.pad[3]
			} else {
				it.medge = k.m[1]
				it.edge = k.m[1] + k.bd[1] + k.pad[1]
			}
			it.w = it.edge
		case iAtomic:
			k := it.b
			v.layout(k, lc{availW: avail, cbW: avail, cbH: cbH, shrink: true, measure: measure, bfc: true})
			it.w = k.w + k.m[1] + k.m[3]
		}
	}

	type openSpan struct {
		b  *box
		x0 float32
		fi bool
	}
	var opens [16]openSpan
	nOpen := 0

	x := float32(0)
	lineStart := int32(0)
	content := false
	hasBoxes := false // inline box edges on the line (they give it height)
	var y float32
	nItems := len(f.items)

	closeLine := func() {
		// Hanging trailing spaces don't count.
		w := x
		for j := len(f.frags) - 1; j >= int(lineStart); j-- {
			fr := &f.frags[j]
			it := &f.items[fr.it]
			if it.kind == iSpace && it.hang {
				w = fr.x
				continue
			}
			break
		}
		// Spans still open continue on the next line.
		li := int32(len(f.lines))
		for j := 0; j < nOpen; j++ {
			f.spans = append(f.spans, span{b: opens[j].b, x0: opens[j].x0, x1: x, first: opens[j].fi, line: li})
			opens[j].x0 = 0
			opens[j].fi = false
		}
		phantom := !content && !hasBoxes
		f.lines = append(f.lines, lineBox{f0: lineStart, f1: int32(len(f.frags)), w: w, phantom: phantom})
		if w > f.w {
			f.w = w
		}
		lineStart = int32(len(f.frags))
		x = 0
		content = false
		hasBoxes = nOpen > 0
	}

	place := func(i int) {
		it := &f.items[i]
		switch it.kind {
		case iOpen:
			if nOpen < len(opens) {
				opens[nOpen] = openSpan{b: it.b, x0: x + it.medge, fi: true}
				nOpen++
			}
			x += it.w
			hasBoxes = true
			f.frags = append(f.frags, frag{it: int32(i), x: x - it.w, w: it.w})
		case iClose:
			x += it.w
			for j := nOpen - 1; j >= 0; j-- {
				if opens[j].b == it.b {
					f.spans = append(f.spans, span{b: it.b, x0: opens[j].x0, x1: x - it.medge, first: opens[j].fi, last: true, line: int32(len(f.lines))})
					copy(opens[j:], opens[j+1:nOpen])
					nOpen--
					break
				}
			}
			f.frags = append(f.frags, frag{it: int32(i), x: x - it.w, w: it.w})
		case iAbs:
			f.frags = append(f.frags, frag{it: int32(i), x: x})
		default:
			f.frags = append(f.frags, frag{it: int32(i), x: x, w: it.w, sh: it.sh})
			x += it.w
			if it.kind != iSpace {
				content = true
			}
		}
	}

	breakAll := func(it *item) bool { return it.b != nil && it.b.st.WordBreakAll }

	for i := 0; i < nItems; {
		it := &f.items[i]
		if it.kind == iBreak {
			if !content {
				hasBoxes = true // a <br> on an empty line still makes a line
			}
			closeLine()
			i++
			continue
		}
		// Leading collapsible spaces at a line start vanish.
		if it.kind == iSpace && it.hang && !content && x == 0 {
			i++
			continue
		}
		// Segment: items up to the next break opportunity.
		j := i
		segW := float32(0)
		trail := float32(0)
		for j < nItems {
			sj := &f.items[j]
			if sj.kind == iBreak {
				break
			}
			if sj.kind == iSpace && sj.hang {
				trail += sj.w
			} else {
				segW += trail + sj.w
				trail = 0
			}
			j++
			if sj.brk {
				break
			}
		}
		if (content || x > 0) && x+segW > avail+0.01 && segHasBreakableStart(f, i) {
			closeLine()
			continue // re-evaluate (drops leading spaces)
		}
		// A single word wider than the line: break it when allowed.
		if segW > avail+0.01 && j-i == 1 && it.kind == iWord && it.sh != nil && (breakAll(it) || owner.st.WordBreakAll) && avail > 0 {
			room := avail - x
			head, tail := splitAt(it.fk, it.sh, room, x > 0)
			if head == nil {
				closeLine()
				continue
			}
			f.frags = append(f.frags, frag{it: int32(i), x: x, w: head.adv, sh: head})
			x += head.adv
			content = true
			if tail != nil {
				closeLine()
				// Replace the item's word by its tail and loop on it.
				f.items[i].sh = tail
				f.items[i].w = tail.adv
				continue
			}
			i++
			continue
		}
		for ; i < j; i++ {
			place(i)
		}
	}
	if len(f.frags) > int(lineStart) {
		closeLine()
	}
	// Restore words split by break-all for the next layout.
	if owner.st.WordBreakAll {
		f.gen = 0
	}

	// Vertical metrics and placement.
	sasc, sdesc, xasc, _ := v.metrics(ost)
	xh := xasc * 0.52
	for li := range f.lines {
		ln := &f.lines[li]
		if ln.phantom {
			ln.top, ln.base, ln.bot = y, y, y
			continue
		}
		asc, desc := float32(0), float32(0)
		var tb float32 // top/bottom aligned heights
		for fi := ln.f0; fi < ln.f1; fi++ {
			fr := &f.frags[fi]
			it := &f.items[fr.it]
			switch it.kind {
			case iWord, iSpace, iEmoji:
				a, d, _, _ := v.metrics(&it.b.st)
				sh := v.vshift(&it.b.st, ost)
				a -= sh
				d += sh
				if it.kind == iEmoji {
					ea := it.w * 0.8
					if ea > a {
						a = ea
					}
				}
				asc, desc = max(asc, a), max(desc, d)
			case iOpen, iClose:
				a, d, _, _ := v.metrics(&it.b.st)
				asc, desc = max(asc, a), max(desc, d)
			case iAtomic:
				k := it.b
				mh := k.h + k.m[0] + k.m[2]
				above := atomicAbove(k, mh)
				switch k.st.VerticalAlign {
				case css.VAMiddle:
					above = mh/2 + xh/2
				case css.VATop, css.VABottom:
					tb = max(tb, mh)
					continue
				case css.VATextTop:
					above = xasc
				case css.VASub:
					above -= ost.FontSize * v.sp * 0.2
				case css.VASuper:
					above += ost.FontSize * v.sp * 0.33
				}
				asc, desc = max(asc, above), max(desc, mh-above)
			}
		}
		asc, desc = max(asc, sasc), max(desc, sdesc)
		if tb > asc+desc {
			desc = tb - asc
		}
		ln.top = y
		ln.base = y + asc
		ln.bot = y + asc + desc
		y = ln.bot
	}

	// Line clamp and text-overflow.
	clampLines := ost.LineClamp
	if clampLines > 0 && len(f.lines) > clampLines {
		cut := f.lines[clampLines-1]
		for fi := cut.f1; fi < int32(len(f.frags)); fi++ {
			f.frags[fi].hidden = true
		}
		f.lines = f.lines[:clampLines]
		v.ellipsize(f, owner, clampLines-1, avail, true)
		y = f.lines[clampLines-1].bot
	}
	if ost.TextOverflowEllipsis && ost.OverflowX != css.OverflowVisible {
		for li := range f.lines {
			if f.lines[li].w > avail+0.5 {
				v.ellipsize(f, owner, li, avail, false)
			}
		}
	}

	// Alignment.
	width := avail
	if width >= inf/2 {
		width = f.w
	}
	v.alignLines(f, ost, width)

	// Vertical positions of frags, spans and atomics.
	for li := range f.lines {
		ln := &f.lines[li]
		for fi := ln.f0; fi < ln.f1; fi++ {
			fr := &f.frags[fi]
			it := &f.items[fr.it]
			fr.y = ln.base
			switch it.kind {
			case iWord, iSpace, iEmoji:
				fr.y = ln.base + v.vshift(&it.b.st, ost)
			case iAtomic:
				k := it.b
				mh := k.h + k.m[0] + k.m[2]
				top := ln.base - atomicAbove(k, mh)
				switch k.st.VerticalAlign {
				case css.VAMiddle:
					top = ln.base - xh/2 - mh/2
				case css.VATop:
					top = ln.top
				case css.VABottom:
					top = ln.bot - mh
				case css.VATextTop:
					top = ln.base - xasc
				case css.VASub:
					top += ost.FontSize * v.sp * 0.2
				case css.VASuper:
					top -= ost.FontSize * v.sp * 0.33
				}
				k.x = fr.x + k.m[3]
				k.y = top + k.m[0]
				k.lp = owner
			case iAbs:
				k := it.b
				k.staticX, k.staticY = fr.x, ln.top
			}
		}
	}
	for si := range f.spans {
		sp := &f.spans[si]
		ln := &f.lines[min(int(sp.line), len(f.lines)-1)]
		_, _, fa, fd := v.metrics(&sp.b.st)
		k := sp.b
		sp.top = ln.base - fa - k.pad[0] - k.bd[0]
		sp.bot = ln.base + fd + k.pad[2] + k.bd[2]
	}
	if len(f.lines) > 0 {
		f.base = f.lines[0].base
		f.lastBase = f.lines[len(f.lines)-1].base
	}
	f.h = y
	return f.w, f.h
}

// segHasBreakableStart reports whether the line may break before item i
// (the previous item is a break opportunity).
func segHasBreakableStart(f *ifc, i int) bool {
	for p := i - 1; p >= 0; p-- {
		it := &f.items[p]
		if it.kind == iOpen || it.kind == iClose || it.kind == iAbs {
			if it.brk {
				return true
			}
			continue
		}
		return it.brk
	}
	return false
}

// vshift is the baseline shift of sub/super text (positive = down).
func (v *View) vshift(st, ost *css.Style) float32 {
	switch st.VerticalAlign {
	case css.VASub:
		return ost.FontSize * v.sp * 0.2
	case css.VASuper:
		return -ost.FontSize * v.sp * 0.33
	}
	return 0
}

// atomicAbove is the height of an atomic inline's margin box above
// its baseline: the last line box of an inline-block, the bottom margin
// edge otherwise.
func atomicAbove(k *box, mh float32) float32 {
	if k.lastBase >= 0 && k.st.OverflowY == css.OverflowVisible {
		return k.m[0] + k.lastBase
	}
	return mh
}

func (v *View) alignLines(f *ifc, st *css.Style, width float32) {
	ta := st.TextAlign
	if ta == css.TextStart || ta == css.TextLeft {
		return
	}
	for li := range f.lines {
		ln := &f.lines[li]
		extra := width - ln.w
		if extra <= 0 {
			continue
		}
		var dx float32
		switch ta {
		case css.TextRight, css.TextEnd:
			dx = extra
		case css.TextCenter:
			dx = extra / 2
		case css.TextJustify:
			if li == len(f.lines)-1 {
				continue
			}
			n := 0
			for fi := ln.f0; fi < ln.f1; fi++ {
				if f.items[f.frags[fi].it].kind == iSpace {
					n++
				}
			}
			// don't count trailing spaces
			for fi := ln.f1 - 1; fi >= ln.f0 && f.items[f.frags[fi].it].kind == iSpace; fi-- {
				n--
			}
			if n <= 0 {
				continue
			}
			per := extra / float32(n)
			acc := float32(0)
			for fi := ln.f0; fi < ln.f1; fi++ {
				fr := &f.frags[fi]
				fr.x += acc
				it := &f.items[fr.it]
				if it.kind == iSpace && acc < extra-per/2 {
					acc += per
				}
			}
			continue
		}
		for fi := ln.f0; fi < ln.f1; fi++ {
			f.frags[fi].x += dx
		}
		for si := range f.spans {
			if f.spans[si].line == int32(li) {
				f.spans[si].x0 += dx
				f.spans[si].x1 += dx
			}
		}
	}
}

// ellipsize truncates line li to fit avail with a trailing "…". force
// adds the ellipsis even when the line fits (line-clamp).
func (v *View) ellipsize(f *ifc, owner *box, li int, avail float32, force bool) {
	ln := &f.lines[li]
	// style of the last text on the line
	sb := owner
	for fi := ln.f1 - 1; fi >= ln.f0; fi-- {
		it := &f.items[f.frags[fi].it]
		if it.kind == iWord || it.kind == iEmoji {
			sb = it.b
			break
		}
	}
	fk := v.face(&sb.st)
	ell := word(fk, "…")
	limit := avail - ell.adv
	if limit < 0 {
		limit = 0
	}
	end := ln.f1
	x := float32(0)
	cut := false
	for fi := ln.f0; fi < ln.f1; fi++ {
		fr := &f.frags[fi]
		if fr.hidden {
			continue
		}
		it := &f.items[fr.it]
		if fr.x+fr.w <= limit+0.01 || it.kind == iOpen || it.kind == iClose || it.kind == iAbs {
			if !(it.kind == iSpace) {
				x = max(x, fr.x+fr.w)
			}
			continue
		}
		cut = true
		if it.kind == iWord && fr.sh != nil {
			head, _ := splitAt(it.fk, fr.sh, limit-fr.x, true)
			if head != nil {
				fr.sh = head
				fr.w = head.adv
				x = fr.x + fr.w
				end = fi + 1
				break
			}
		}
		end = fi
		break
	}
	if !cut && !force {
		return
	}
	for fi := end; fi < ln.f1; fi++ {
		it := &f.items[f.frags[fi].it]
		if it.kind == iClose || it.kind == iOpen || it.kind == iAbs {
			continue
		}
		f.frags[fi].hidden = true
	}
	// drop trailing spaces before the ellipsis
	for fi := end - 1; fi >= ln.f0; fi-- {
		fr := &f.frags[fi]
		if f.items[fr.it].kind == iSpace && !fr.hidden {
			fr.hidden = true
			x = fr.x
			continue
		}
		break
	}
	// The ellipsis frag goes at the end of the frag list for this line:
	// insert it at ln.f1 and shift later lines.
	f.items = append(f.items, item{kind: iWord, b: sb, sh: ell, fk: fk, w: ell.adv})
	efr := frag{it: int32(len(f.items) - 1), x: x, w: ell.adv, sh: ell, ell: true}
	f.frags = append(f.frags, frag{})
	copy(f.frags[ln.f1+1:], f.frags[ln.f1:])
	f.frags[ln.f1] = efr
	for l := li + 1; l < len(f.lines); l++ {
		f.lines[l].f0++
		f.lines[l].f1++
	}
	ln.f1++
	ln.w = x + ell.adv
	if ln.w > f.w {
		f.w = ln.w
	}
	if f.w > avail && avail < inf/2 {
		f.w = avail
	}
}
