package richview

import (
	"image"
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// ---------- display list ----------

type fragKind uint8

const (
	fText fragKind = iota
	fImage
)

// frag is one positioned inline thing: a shaped word or an inline image.
type frag struct {
	kind   fragKind
	x      float32 // text: pen x; image: left
	y      int     // text: baseline; image: top
	w      float32
	size   int // image size px / text px size (for decorations)
	top    int // line box top/bottom, for hit tests and selection
	bot    int
	line   int32
	sh     *shaped
	col    color.NRGBA
	style  richtext.Style
	target int16
	url    string // image
	p0, p1 int32  // plain text byte range
	alt    bool   // image frag has text fallback in plain (":name:")
}

type boxKind uint8

const (
	bFill   boxKind = iota // filled (rounded) rect
	bBorder                // fill + 1px border
	bCircle
	bRing   // circle outline
	bSquare // small filled square
	bPlay   // play triangle
	bChevron
)

type box struct {
	kind   boxKind
	r      image.Rectangle
	radius int
	fill   color.NRGBA
	stroke color.NRGBA
	bw     int
	layer  uint8 // 0 = under text, 1 = above images (overlays)
	target int16 // hover-highlight target (-1 none)
	hover  color.NRGBA
	// perCorner uses corners (NW NE SE SW) instead of radius.
	perCorner bool
	corners   [4]int
}

type imgPrim struct {
	r      image.Rectangle
	url    string
	radii  [4]int // NW NE SE SW
	target int16
	px     int // requested decode size
}

type targetKind uint8

const (
	tLink targetKind = iota
	tUser
	tChannel
	tUsergroup
	tImage
	tFile
	tButton
	tCard // whole-card click (opens URL)
)

type target struct {
	kind targetKind
	url  string
	id   string
	img  *richtext.Image
	file *richtext.File
	btn  *richtext.Button
	// underline on hover (links)
	underline bool
}

type hit struct {
	r image.Rectangle
	t int16
}

type lineInfo struct {
	top, bot int
	f0, f1   int // frag range
}

// display is the laid-out result of a Doc at one width.
type display struct {
	frags   []frag
	boxes   []box
	imgs    []imgPrim
	targets []target
	hits    []hit
	lines   []lineInfo
	plain   []byte
	size    image.Point
}

func (d *display) reset() {
	d.frags = d.frags[:0]
	d.boxes = d.boxes[:0]
	d.imgs = d.imgs[:0]
	d.targets = d.targets[:0]
	d.hits = d.hits[:0]
	d.lines = d.lines[:0]
	d.plain = d.plain[:0]
	d.size = image.Point{}
}

func (d *display) addTarget(t target) int16 {
	d.targets = append(d.targets, t)
	return int16(len(d.targets) - 1)
}

// ---------- inline flow ----------

type itemKind uint8

const (
	itWord itemKind = iota
	itSpace
	itImage
	itBreak
	itPad
)

type deco struct {
	kind   uint8 // 1 pill, 2 code, 3 highlight
	bg     color.NRGBA
	border color.NRGBA
	padL   float32
	padR   float32
	padT   float32
	padB   float32
	radius int
	target int16
	face   faceKey
}

type item struct {
	kind   itemKind
	sh     *shaped
	face   faceKey
	w      float32
	col    color.NRGBA
	style  richtext.Style
	deco   int16
	target int16
	url    string
	size   int
	p0, p1 int32
	brk    bool // break opportunity after this item (CJK, images)
	alt    bool
}

// flowStyle configures one inline flow.
type flowStyle struct {
	face     font.Font
	size     int // px
	lineH    int // px
	col      color.NRGBA
	emoji    int  // emoji px
	pre      bool // preserve spaces and newlines, mono
	maxLines int
	ellipsis bool
	noSelect bool
}

// lay holds per-layout state.
type lay struct {
	st    *Style
	d     *display
	scale float32 // px per dp
	spx   float32 // px per sp
	jumbo bool
	items []item
	decos []deco
	opts  Options
}

func (l *lay) dp(v float32) int      { return int(v*l.scale + 0.5) }
func (l *lay) sp(v float32) int      { return int(v*l.spx + 0.5) }
func (l *lay) dpf(v float32) float32 { return v * l.scale }

func (l *lay) faceFor(base font.Font, sizePx int, s richtext.Style) faceKey {
	f := base
	if s&richtext.Bold != 0 && f.Weight < font.Bold {
		f.Weight = font.Bold
	}
	if s&richtext.Italic != 0 {
		f.Style = font.Italic
	}
	return faceKey{f: f, size: fixed.I(sizePx)}
}

func (l *lay) plainAdd(s string) (int32, int32) {
	p0 := int32(len(l.d.plain))
	l.d.plain = append(l.d.plain, s...)
	return p0, int32(len(l.d.plain))
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// addText splits s into word/space/break items.
func (l *lay) addText(s string, fs *flowStyle, k faceKey, col color.NRGBA, st richtext.Style, dc, tg int16) {
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		w := s[start:end]
		sh := l.st.word(k, w)
		p0, p1 := l.plainAdd(w)
		l.items = append(l.items, item{kind: itWord, sh: sh, face: k, w: sh.adv, col: col, style: st, deco: dc, target: tg, p0: p0, p1: p1})
		start = -1
	}
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\n':
			flush(i)
			p0, p1 := l.plainAdd("\n")
			l.items = append(l.items, item{kind: itBreak, face: k, deco: dc, p0: p0, p1: p1})
		case r == ' ' || r == '\t' || r == '\r' || (r != ' ' && unicode.IsSpace(r)):
			flush(i)
			n := 1
			if r == '\t' {
				n = 4
				if !fs.pre {
					n = 1
				}
			}
			if r == '\r' {
				break
			}
			sp := l.st.spaceOf(k)
			// Collapse runs of spaces outside pre/rich text? Slack uses
			// pre-wrap everywhere, so spaces are preserved.
			p0, p1 := l.plainAdd(s[i : i+sz])
			l.items = append(l.items, item{kind: itSpace, face: k, w: sp.adv * float32(n), col: col, style: st, deco: dc, target: tg, p0: p0, p1: p1})
		case isCJK(r):
			flush(i)
			start = i
			flush(i + sz)
			l.items[len(l.items)-1].brk = true
		default:
			if start < 0 {
				start = i
			}
		}
		i += sz
	}
	flush(len(s))
}

func (l *lay) addPad(w float32, dc int16, k faceKey) {
	l.items = append(l.items, item{kind: itPad, w: w, deco: dc, face: k, target: -1})
}

// addSpans converts spans into items.
func (l *lay) addSpans(spans []richtext.Span, fs *flowStyle) {
	p := &l.st.Palette
	for i := range spans {
		sp := &spans[i]
		st := sp.Style
		col := fs.col
		size := fs.size
		base := fs.face
		var dc int16 = -1
		var tg int16 = -1
		if st&richtext.Code != 0 && !fs.pre {
			base = font.Font{Typeface: l.st.Mono}
			size = l.sp(12)
			col = p.CodeText
		}
		k := l.faceFor(base, size, st)
		switch sp.Kind {
		case richtext.Break:
			p0, p1 := l.plainAdd("\n")
			l.items = append(l.items, item{kind: itBreak, face: k, deco: -1, target: -1, p0: p0, p1: p1})
			continue
		case richtext.Emoji:
			if sp.URL == "" {
				txt := sp.Text
				if txt == "" {
					txt = ":" + sp.ID + ":"
				}
				l.addText(txt, fs, k, col, st, -1, -1)
				continue
			}
			alt := sp.Text
			if alt == "" {
				alt = ":" + sp.ID + ":"
			}
			p0, p1 := l.plainAdd(alt)
			ep := fs.emoji
			l.items = append(l.items, item{kind: itImage, face: k, w: float32(ep + 2*l.dp(1)), url: sp.URL, size: ep, deco: -1, target: -1, p0: p0, p1: p1, brk: true, alt: true})
			continue
		case richtext.InlineImage:
			if sp.URL == "" {
				break
			}
			p0, p1 := l.plainAdd(sp.Text)
			ip := max(fs.emoji, l.dp(16))
			l.items = append(l.items, item{kind: itImage, face: k, w: float32(ip + 2*l.dp(1)), url: sp.URL, size: ip, deco: -1, target: -1, p0: p0, p1: p1, brk: true})
			continue
		case richtext.ColorSwatch:
			if c := parseColor(sp.Text, color.NRGBA{}); c.A > 0 {
				sw := l.dp(12)
				l.decos = append(l.decos, deco{kind: 4, bg: c, border: rgba(0, 0, 0, 0.15), face: k, target: -1, radius: l.dp(3)})
				l.addPad(float32(sw), int16(len(l.decos)-1), k)
				l.addPad(l.dpf(4), -1, k)
			}
		case richtext.Link:
			tg = l.d.addTarget(target{kind: tLink, url: sp.URL, underline: true})
			if st&richtext.Code == 0 {
				col = p.Link
			}
		case richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast:
			kind := tUser
			switch sp.Kind {
			case richtext.Channel:
				kind = tChannel
			case richtext.Usergroup:
				kind = tUsergroup
			}
			if sp.Kind != richtext.Broadcast {
				tg = l.d.addTarget(target{kind: kind, id: sp.ID})
			}
			bg, fg := p.MentionBg, p.MentionText
			if sp.Self || sp.Kind == richtext.Broadcast {
				bg, fg = p.SelfMentionBg, p.SelfMentionText
			}
			col = fg
			l.decos = append(l.decos, deco{kind: 1, bg: bg, padL: l.dpf(2), padR: l.dpf(2), padB: l.dpf(1), radius: l.dp(3), target: tg, face: k})
			dc = int16(len(l.decos) - 1)
		case richtext.Date:
			if sp.URL != "" {
				tg = l.d.addTarget(target{kind: tLink, url: sp.URL, underline: true})
				col = p.Link
			}
		}
		if st&richtext.Code != 0 && !fs.pre && dc < 0 {
			l.decos = append(l.decos, deco{kind: 2, bg: p.CodeBg, border: p.CodeBorder, padL: l.dpf(3), padR: l.dpf(3), padT: l.dpf(2), padB: l.dpf(1), radius: l.dp(3), target: tg, face: k})
			dc = int16(len(l.decos) - 1)
		} else if st&richtext.Highlight != 0 && dc < 0 {
			l.decos = append(l.decos, deco{kind: 3, bg: p.Highlight, face: k, target: -1})
			dc = int16(len(l.decos) - 1)
		}
		if dc >= 0 && l.decos[dc].padL > 0 {
			l.addPad(l.decos[dc].padL, dc, k)
		}
		txt := sp.Text
		if !fs.pre && st&richtext.Code != 0 {
			// Inline code wraps normally.
			txt = strings.ReplaceAll(txt, "\n", " ")
		}
		l.addText(txt, fs, k, col, st, dc, tg)
		if dc >= 0 && l.decos[dc].padR > 0 {
			l.addPad(l.decos[dc].padR, dc, k)
		}
	}
}

// flow lays out l.items at (x0, y0) within maxW px. Returns the height and
// the widest line width.
func (l *lay) flow(x0, y0, maxW int, fs *flowStyle) (h, wmax int) {
	items := l.items
	// Drop one trailing hard break (pre-wrap does not render it).
	if n := len(items); n > 0 && items[n-1].kind == itBreak {
		items = items[:n-1]
	}
	if len(items) == 0 {
		return 0, 0
	}
	base := l.faceFor(fs.face, fs.size, 0)
	bm := l.st.spaceOf(base)
	lineH := fs.lineH
	// Baseline offset in a CSS line box: half-leading + ascent.
	baseOff := (float32(lineH)-(bm.asc+bm.desc))/2 + bm.asc

	d := l.d
	y := y0
	lines := 0
	x := float32(0)
	lineStart := len(d.frags)
	lineH0 := lineH
	maxWf := float32(maxW)
	truncated := false

	finishLine := func() {
		// Compute this line's height: emoji may be taller.
		lh := lineH0
		for i := lineStart; i < len(d.frags); i++ {
			if f := &d.frags[i]; f.kind == fImage {
				if h := f.size + l.dp(2); h > lh {
					lh = h
				}
			}
		}
		off := baseOff + float32(lh-lineH0)/2
		for i := lineStart; i < len(d.frags); i++ {
			f := &d.frags[i]
			f.top, f.bot = y, y+lh
			f.line = int32(len(d.lines))
			if f.kind == fText {
				f.y = y + int(off+0.5)
			} else {
				f.y = y + (lh-f.size)/2 - l.dp(1)
				if lh == lineH0 && f.size <= lineH0 {
					f.y = y + (lineH0-f.size)/2 - l.dp(1)
					if f.y < y-l.dp(1) {
						f.y = y - l.dp(1)
					}
				}
			}
		}
		d.lines = append(d.lines, lineInfo{top: y, bot: y + lh, f0: lineStart, f1: len(d.frags)})
		if w := int(x + 0.999); w > wmax {
			wmax = w
		}
		y += lh
		lines++
		x = 0
		lineStart = len(d.frags)
	}

	emit := func(it *item, sh *shaped, w float32, p0, p1 int32) {
		if it.kind == itImage {
			d.frags = append(d.frags, frag{kind: fImage, x: float32(x0) + x + l.dpf(1), w: float32(it.size), size: it.size, url: it.url, target: it.target, p0: p0, p1: p1, alt: it.alt})
		} else if it.kind == itWord {
			d.frags = append(d.frags, frag{kind: fText, x: float32(x0) + x, w: w, size: int(it.face.size >> 6), sh: sh, col: it.col, style: it.style, target: it.target, p0: p0, p1: p1})
		}
		x += w
	}

	// Pass: chunk items into unbreakable runs separated by spaces/breaks.
	type placed struct {
		item  int
		frag  int // frag index or -1 for pads/spaces
		x     float32
		w     float32
		line  int
		decoI int16
	}
	var decoRuns []placed // only for items with deco >= 0

	i := 0
	for i < len(items) && !truncated {
		it := &items[i]
		switch it.kind {
		case itBreak:
			finishLine()
			if fs.maxLines > 0 && lines >= fs.maxLines {
				truncated = i < len(items)-1
			}
			i++
			continue
		case itSpace:
			if x > 0 || fs.pre {
				if x+it.w <= maxWf || fs.pre {
					if it.deco >= 0 {
						decoRuns = append(decoRuns, placed{item: i, frag: -1, x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.deco})
					}
					x += it.w
				} // else: a space that does not fit is swallowed by the wrap

			}
			i++
			continue
		}
		// Gather a chunk: consecutive words/pads/images until a break opportunity.
		j := i
		cw := float32(0)
		for j < len(items) {
			k := items[j].kind
			if k == itSpace || k == itBreak {
				break
			}
			cw += items[j].w
			j++
			if items[j-1].brk {
				break
			}
			if j < len(items) && items[j].kind == itImage {
				break
			}
		}
		if x > 0 && x+cw > maxWf {
			finishLine()
			if fs.maxLines > 0 && lines >= fs.maxLines {
				truncated = true
				break
			}
		}
		for q := i; q < j && !truncated; q++ {
			it := &items[q]
			if it.kind == itPad {
				if it.deco >= 0 {
					decoRuns = append(decoRuns, placed{item: q, frag: -1, x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.deco})
				}
				x += it.w
				continue
			}
			if it.kind == itWord && x+it.w > maxWf && it.w > 0 {
				// Forced break inside a word that cannot fit.
				sh := it.sh
				p0 := it.p0
				for sh != nil {
					room := maxWf - x
					if sh.adv <= room {
						if it.deco >= 0 {
							decoRuns = append(decoRuns, placed{item: q, frag: len(d.frags), x: float32(x0) + x, w: sh.adv, line: len(d.lines), decoI: it.deco})
						}
						emit(it, sh, sh.adv, p0, it.p1)
						break
					}
					if x > 0 {
						finishLine()
						if fs.maxLines > 0 && lines >= fs.maxLines {
							truncated = true
							break
						}
						continue
					}
					head, tail, nb := l.st.split(it.face, sh, room)
					if it.deco >= 0 {
						decoRuns = append(decoRuns, placed{item: q, frag: len(d.frags), x: float32(x0) + x, w: head.adv, line: len(d.lines), decoI: it.deco})
					}
					emit(it, head, head.adv, p0, p0+int32(nb))
					p0 += int32(nb)
					sh = tail
					if sh != nil {
						finishLine()
						if fs.maxLines > 0 && lines >= fs.maxLines {
							truncated = true
							break
						}
					}
				}
				continue
			}
			if it.deco >= 0 {
				decoRuns = append(decoRuns, placed{item: q, frag: len(d.frags), x: float32(x0) + x, w: it.w, line: len(d.lines), decoI: it.deco})
			}
			emit(it, it.sh, it.w, it.p0, it.p1)
		}
		i = j
	}
	if !truncated && (x > 0 || lineStart < len(d.frags)) {
		finishLine()
	} else if truncated && lineStart < len(d.frags) {
		finishLine()
	}
	if truncated && fs.ellipsis && len(d.lines) > 0 {
		l.ellipsize(&d.lines[len(d.lines)-1], x0, maxWf, fs)
	}

	// Decoration rects: merge consecutive runs of the same deco on a line.
	for a := 0; a < len(decoRuns); {
		r := decoRuns[a]
		b := a + 1
		x1 := r.x + r.w
		for b < len(decoRuns) && decoRuns[b].decoI == r.decoI && decoRuns[b].line == r.line {
			x1 = decoRuns[b].x + decoRuns[b].w
			b++
		}
		if r.line < len(d.lines) {
			dc := &l.decos[r.decoI]
			ln := d.lines[r.line]
			fm := l.st.spaceOf(dc.face)
			// content area of the decoration's font, centered on the baseline
			bl := float32(ln.top) + baseOff + float32((ln.bot-ln.top)-lineH0)/2
			top := bl - fm.asc - dc.padT
			bot := bl + fm.desc + dc.padB
			rr := image.Rect(int(r.x+0.5), int(top+0.5), int(x1+0.5), int(bot+0.5))
			switch dc.kind {
			case 4:
				sw := l.dp(12)
				cy := int(bl+0.5) - sw + l.dp(1)
				rr = image.Rect(int(r.x+0.5), cy, int(r.x+0.5)+sw, cy+sw)
				d.boxes = append(d.boxes, box{kind: bBorder, r: rr, radius: dc.radius, fill: dc.bg, stroke: dc.border, bw: l.dp(1), target: -1})
			case 2:
				d.boxes = append(d.boxes, box{kind: bBorder, r: rr, radius: dc.radius, fill: dc.bg, stroke: dc.border, bw: l.dp(1), target: -1})
			default:
				bx := box{kind: bFill, r: rr, radius: dc.radius, fill: dc.bg, target: dc.target}
				if dc.kind == 1 {
					bx.hover = l.st.Palette.MentionBg
				}
				d.boxes = append(d.boxes, bx)
			}
			if dc.target >= 0 {
				d.hits = append(d.hits, hit{r: rr, t: dc.target})
			}
		}
		a = b
	}
	l.items = l.items[:0]
	l.decos = l.decos[:0]
	return y - y0, wmax
}

// ellipsize trims the last line so that "…" fits.
func (l *lay) ellipsize(ln *lineInfo, x0 int, maxW float32, fs *flowStyle) {
	d := l.d
	k := l.faceFor(fs.face, fs.size, 0)
	ell := l.st.word(k, "…")
	for ln.f1 > ln.f0 {
		last := &d.frags[ln.f1-1]
		if last.x+last.w+ell.adv <= float32(x0)+maxW {
			break
		}
		ln.f1--
		d.frags = d.frags[:ln.f1]
	}
	ex := float32(x0)
	ty := ln.top + fs.lineH/2
	if ln.f1 > ln.f0 {
		last := d.frags[ln.f1-1]
		ex = last.x + last.w
		ty = last.y
	}
	d.frags = append(d.frags, frag{kind: fText, x: ex, w: ell.adv, size: fs.size, sh: ell, col: fs.col, target: -1, top: ln.top, bot: ln.bot, line: int32(len(d.lines) - 1), y: ty, p0: int32(len(d.plain)), p1: int32(len(d.plain))})
	ln.f1 = len(d.frags)
}

// textFlow is the common entry: spans laid out at (x, y) with width w.
func (l *lay) textFlow(spans []richtext.Span, x, y, w int, fs *flowStyle) (int, int) {
	l.addSpans(spans, fs)
	return l.flow(x, y, w, fs)
}

// plainFlow lays out a plain string in one style.
func (l *lay) plainFlow(s string, x, y, w int, fs *flowStyle, tg int16) (int, int) {
	k := l.faceFor(fs.face, fs.size, 0)
	l.addText(s, fs, k, fs.col, 0, -1, tg)
	return l.flow(x, y, w, fs)
}

// measure returns the width of s in face k (single line).
func (l *lay) measure(s string, k faceKey) float32 {
	var w float32
	for _, part := range strings.SplitAfter(s, " ") {
		trim := strings.TrimSuffix(part, " ")
		if trim != "" {
			w += l.st.word(k, trim).adv
		}
		if len(trim) != len(part) {
			w += l.st.spaceOf(k).adv
		}
	}
	return w
}
