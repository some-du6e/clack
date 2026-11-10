package composer

import (
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// textlayout.go shapes and wraps a *Doc for the editor. Every style run is
// shaped once on a single unbounded line with text.Shaper (so kerning and
// ligatures work inside a run), then the editor does its own greedy word
// wrapping across runs and atoms using per-rune caret positions. The result
// is cached by (doc version, width, metric, sizes): idle frames only replay
// two recorded macros (backgrounds and text) plus the per-frame pieces
// (selection, caret, emoji images).

// tlShapeKey identifies one shaped run.
type tlShapeKey struct {
	s    string
	font font.Font
	ppem fixed.Int26_6
	loc  system.Locale
}

// tlShape is a run shaped on one line, positions relative to the run start.
type tlShape struct {
	glyphs []text.Glyph
	// grune is, per glyph, the rune index (within the run) of the start of
	// the glyph's cluster.
	grune []int32
	// pos is the caret x before each rune; len(pos) == runes+1.
	pos []fixed.Int26_6
	// stop reports whether a caret may sit before each rune (cluster
	// boundary); len(stop) == runes+1.
	stop            []bool
	width           fixed.Int26_6
	ascent, descent fixed.Int26_6
}

// tlCache keeps shaped runs alive across relayouts: entries not used by the
// latest layout are dropped on the next one.
type tlCache struct {
	cur, old map[tlShapeKey]*tlShape
}

func (c *tlCache) begin() {
	c.old = c.cur
	c.cur = make(map[tlShapeKey]*tlShape, len(c.old))
}

func (c *tlCache) get(sh *text.Shaper, k tlShapeKey) *tlShape {
	if c.cur == nil {
		c.cur = make(map[tlShapeKey]*tlShape)
	}
	if s, ok := c.cur[k]; ok {
		return s
	}
	if s, ok := c.old[k]; ok {
		c.cur[k] = s
		return s
	}
	s := tlShapeRun(sh, k)
	c.cur[k] = s
	return s
}

// tlShapeRun shapes k.s on a single line and indexes its clusters.
func tlShapeRun(sh *text.Shaper, k tlShapeKey) *tlShape {
	n := utf8.RuneCountInString(k.s)
	r := &tlShape{pos: make([]fixed.Int26_6, n+1), stop: make([]bool, n+1)}
	r.stop[0] = true
	str := k.s
	if strings.ContainsRune(str, '\t') {
		// Tabs have no glyph (they drew as tofu); shape them as spaces so
		// rune positions stay one-to-one.
		str = strings.ReplaceAll(str, "\t", " ")
	}
	sh.LayoutString(text.Parameters{
		Font:             k.font,
		PxPerEm:          k.ppem,
		MaxWidth:         1 << 20,
		Locale:           k.loc,
		WrapPolicy:       text.WrapWords,
		DisableSpaceTrim: true,
	}, str)
	rn := 0
	first := 0
	const big = fixed.Int26_6(1 << 30)
	minX, maxX := big, -big
	end := fixed.Int26_6(0)
	for {
		g, ok := sh.NextGlyph()
		if !ok {
			break
		}
		if g.Ascent > r.ascent {
			r.ascent = g.Ascent
		}
		if g.Descent > r.descent {
			r.descent = g.Descent
		}
		if n == 0 || g.Flags&text.FlagParagraphBreak != 0 {
			continue
		}
		r.glyphs = append(r.glyphs, g)
		r.grune = append(r.grune, int32(rn))
		if g.X < minX {
			minX = g.X
		}
		if x := g.X + g.Advance; x > maxX {
			maxX = x
		}
		if maxX > end {
			end = maxX
		}
		if g.Flags&text.FlagClusterBreak == 0 {
			continue
		}
		cnt := int(g.Runes)
		for j := first; j < len(r.glyphs); j++ {
			r.grune[j] = int32(rn)
		}
		if cnt > 0 && rn+cnt <= n {
			if rn > 0 && minX > r.pos[rn] {
				r.pos[rn] = minX
			}
			w := maxX - minX
			for i := 1; i <= cnt; i++ {
				r.pos[rn+i] = minX + w*fixed.Int26_6(i)/fixed.Int26_6(cnt)
			}
			r.stop[rn+cnt] = true
			rn += cnt
		}
		first = len(r.glyphs)
		minX, maxX = big, -big
	}
	for ; rn < n; rn++ {
		r.pos[rn+1] = max(r.pos[rn], end)
		r.stop[rn+1] = true
	}
	r.width = max(r.pos[n], end)
	if n > 0 {
		r.pos[n] = r.width
	}
	return r
}

// tlMetrics are the layout sizes in pixels for one metric and style.
type tlMetrics struct {
	ppem, codePpem         fixed.Int26_6
	h1Ppem, h2Ppem, h3Ppem fixed.Int26_6
	lh, preLH              int
	h1LH, headLH           int // heading line heights (h2 and h3 share headLH)
	headMargin             int // heading bottom margin
	dividerBox             int // the divider line box height
	dividerThick           int // the divider rule thickness
	emoji                  int
	emojiGap               int
	listIndent             int
	Dp24, Dp3              int
	markerGap              int
	numberGap              int
	bulletR                int
	quoteBar               int
	quoteLeft              int
	prePad                 int
	preMargin              int
	preRadius              int
	border                 int
	codePadX               int
	codeRadius             int
	pillPadX               int
	pillRadius             int
	newlineW               int
	caretW                 int
	deco                   int // underline/strike thickness
}

func tlMakeMetrics(m unit.Metric, st *EditorStyle) tlMetrics {
	ts := st.TextSize
	if ts <= 0 {
		ts = 15
	}
	lh := st.LineHeight
	if lh <= 0 {
		lh = ts * 22 / 15
	}
	es := st.EmojiSize
	if es <= 0 {
		es = unit.Dp(float32(lh))
	}
	px1 := max(m.Dp(1), 1)
	return tlMetrics{
		ppem:     fixed.I(m.Sp(ts)),
		codePpem: fixed.I(m.Sp(ts * 0.8)),
		// Headings scale off the base text size: h1 18/15, h2 and h3 15/15.
		h1Ppem:       fixed.I(m.Sp(ts * 18 / 15)),
		h2Ppem:       fixed.I(m.Sp(ts)),
		h3Ppem:       fixed.I(m.Sp(ts)),
		lh:           max(m.Sp(lh), 1),
		preLH:        max(m.Sp(lh*18/22), 1),
		h1LH:         max(m.Sp(ts*24/15), 1),
		headLH:       max(m.Sp(ts*22/15), 1),
		headMargin:   max(m.Sp(ts*4/15), 1),
		dividerBox:   max(m.Sp(ts*12/15), 1),
		dividerThick: px1,
		emoji:        m.Dp(es),
		emojiGap:     px1,
		listIndent:   m.Dp(28),
		Dp24:         m.Dp(24),
		Dp3:          m.Dp(3),
		markerGap:    m.Dp(15),
		numberGap:    m.Dp(7),
		bulletR:      max(m.Dp(3), 2),
		quoteBar:     m.Dp(4),
		quoteLeft:    m.Dp(16),
		prePad:       m.Dp(8),
		preMargin:    m.Dp(4),
		preRadius:    m.Dp(4),
		border:       px1,
		codePadX:     m.Dp(3),
		codeRadius:   m.Dp(3),
		pillPadX:     m.Dp(2),
		pillRadius:   m.Dp(3),
		newlineW:     m.Dp(4),
		caretW:       px1,
		deco:         px1,
	}
}

// tlFont is the face for a run.
func tlFont(s Style, mono bool) font.Font {
	f := font.Font{Typeface: Lato}
	if mono {
		f.Typeface = Mono
	}
	f = theme.EditorFont(f) // unicode emoji shape as blank slots (no tofu)
	if s&Bold != 0 {
		f.Weight = font.Bold
	}
	if s&Italic != 0 {
		f.Style = font.Italic
	}
	return f
}

// tlHeadFont is the face for a heading line: Big and Medium are Black (900),
// Small is Bold (700). Inline styles are ignored (headings are plain).
func tlHeadFont(k BlockKind) font.Font {
	f := font.Font{Typeface: Lato, Weight: font.Black}
	if k == Header3 {
		f.Weight = font.Bold
	}
	return theme.EditorFont(f)
}

// headPpem is the shaped size of a heading line.
func headPpem(m *tlMetrics, k BlockKind) fixed.Int26_6 {
	switch k {
	case Header1:
		return m.h1Ppem
	case Header2:
		return m.h2Ppem
	default:
		return m.h3Ppem
	}
}

// tlItem is a style run or an atom inside a paragraph.
type tlItem struct {
	r0, r1 int           // doc rune range
	x0     fixed.Int26_6 // start in unwrapped paragraph coordinates
	w      fixed.Int26_6 // full width, padding included
	padL   fixed.Int26_6 // left padding (inline code, pills)
	icon   fixed.Int26_6 // a pill's leading icon with its gap (message slugs)
	shape  *tlShape      // text, mention label or emoji fallback; may be nil
	attr   Attr
	code   bool // inline code look
	box    int  // emoji square in px; 0 otherwise
}

// tlPara is one doc line (a paragraph) laid out.
type tlPara struct {
	start, end int // runes [start, end); end is the '\n' or Len()
	kind       BlockKind
	indent     int
	left       int             // x of the text origin
	px         []fixed.Int26_6 // caret x per position start..end, unwrapped
	stop       []bool          // caret stops per position start..end
	items      []tlItem
	l0, l1     int // visual lines
	top, bot   int
	raw        []tlRawEmoji // unicode emoji left as text (code, other editors)
	asc, desc  int          // primary font metrics
	num        int          // Ordered: item number
	// boxTop/boxBot is the code block box; set on the last line of a
	// code block group.
	boxTop, boxBot int
}

// tlLine is one visual (wrapped) line.
type tlLine struct {
	para   int
	a, b   int // runes [a, b)
	top, h int
	base   int
}

// tlRawEmoji is a unicode emoji kept as text, runes [q0, q1), that the
// font shaped as a blank slot glyph (theme.EditorFont): its image is drawn
// over the slot like an atom's. The message composer turns typed emoji
// into atoms; these are the ones in inline code and code blocks, and every
// emoji in editors without an emoji lookup (canvases, Block Kit inputs).
type tlRawEmoji struct {
	q0, q1 int
	stem   string
}

// tlEmoji is an emoji atom to draw each frame (images arrive async).
type tlEmoji struct {
	rect  image.Rectangle
	base  int
	atom  *Atom
	label *tlShape
}

// tlKey is everything the geometry depends on.
type tlKey struct {
	version uint64
	width   int
	metric  unit.Metric
	ts, lh  unit.Sp
	es      unit.Dp
	images  bool
	loc     system.Locale
	weight  font.Weight // EditorStyle.Weight
}

// tlColors is the paint state baked into the cached macros.
type tlColors struct {
	text, link, code, codeBg, codeBorder color.NRGBA
	preBg, preBorder, quoteBar           color.NRGBA
	mention, mentionBg                   color.NRGBA
	broadcast, broadcastBg               color.NRGBA
	emb                                  float32 // EditorStyle.Embolden
}

func tlColorsOf(st *EditorStyle) tlColors {
	return tlColors{
		text: st.Color, link: st.LinkColor, code: st.CodeColor, codeBg: st.CodeBg, codeBorder: st.CodeBorder,
		preBg: st.PreBg, preBorder: st.PreBorder, quoteBar: st.QuoteBar,
		mention: st.MentionColor, mentionBg: st.MentionBg,
		broadcast: st.BroadcastColor, broadcastBg: st.BroadcastBg, emb: st.Embolden,
	}
}

// tlLayout is the cached layout of the whole document.
type tlLayout struct {
	key    tlKey
	valid  bool
	gen    uint64
	m      tlMetrics
	paras  []tlPara
	lines  []tlLine
	emojis []tlEmoji
	height int
	width  int
	sh     *text.Shaper
	cache  tlCache
	emb    float32 // text stroke while painting (tlColors.emb)

	latoAsc, latoDesc int
	monoAsc, monoDesc int
	h1Asc, h1Desc     int
	h2Asc, h2Desc     int
	h3Asc, h3Desc     int

	paintOK        bool
	paintGen       uint64
	paintCol       tlColors
	bgCall, fgCall op.CallOp
	scratch        []text.Glyph
}

// build lays out d for key k.
func (l *tlLayout) build(d *Doc, st *EditorStyle, sh *text.Shaper, k tlKey) {
	l.key = k
	l.valid = true
	l.gen++
	if l.sh != sh { // a new UI font: glyph ids of the old shaper mean nothing here
		l.cache = tlCache{}
	}
	l.sh = sh
	l.width = k.width
	l.cache.begin()
	m := tlMakeMetrics(k.metric, st)
	l.m = m
	lato := l.cache.get(sh, tlShapeKey{font: tlFont(0, false), ppem: m.ppem, loc: k.loc})
	mono := l.cache.get(sh, tlShapeKey{font: tlFont(0, true), ppem: m.codePpem, loc: k.loc})
	l.latoAsc, l.latoDesc = lato.ascent.Ceil(), lato.descent.Ceil()
	l.monoAsc, l.monoDesc = mono.ascent.Ceil(), mono.descent.Ceil()
	if l.latoAsc+l.latoDesc == 0 {
		l.latoAsc, l.latoDesc = m.ppem.Ceil(), m.ppem.Ceil()/4
	}
	if l.monoAsc+l.monoDesc == 0 {
		l.monoAsc, l.monoDesc = m.codePpem.Ceil(), m.codePpem.Ceil()/4
	}
	h1 := l.cache.get(sh, tlShapeKey{font: tlHeadFont(Header1), ppem: m.h1Ppem, loc: k.loc})
	h2 := l.cache.get(sh, tlShapeKey{font: tlHeadFont(Header2), ppem: m.h2Ppem, loc: k.loc})
	h3 := l.cache.get(sh, tlShapeKey{font: tlHeadFont(Header3), ppem: m.h3Ppem, loc: k.loc})
	l.h1Asc, l.h1Desc = h1.ascent.Ceil(), h1.descent.Ceil()
	l.h2Asc, l.h2Desc = h2.ascent.Ceil(), h2.descent.Ceil()
	l.h3Asc, l.h3Desc = h3.ascent.Ceil(), h3.descent.Ceil()
	if l.h1Asc+l.h1Desc == 0 {
		l.h1Asc, l.h1Desc = m.h1Ppem.Ceil(), m.h1Ppem.Ceil()/4
	}
	if l.h2Asc+l.h2Desc == 0 {
		l.h2Asc, l.h2Desc = m.h2Ppem.Ceil(), m.h2Ppem.Ceil()/4
	}
	if l.h3Asc+l.h3Desc == 0 {
		l.h3Asc, l.h3Desc = m.h3Ppem.Ceil(), m.h3Ppem.Ceil()/4
	}
	l.paras = l.paras[:0]
	l.lines = l.lines[:0]
	l.emojis = l.emojis[:0]

	start := 0
	for i := range d.Lines {
		end := start
		for end < len(d.Runes) && d.Runes[end] != '\n' {
			end++
		}
		ln := d.Lines[i]
		p := tlPara{start: start, end: end, kind: ln.Kind, indent: ln.Indent}
		l.buildPara(d, &p, len(l.paras), k)
		l.paras = append(l.paras, p)
		start = end + 1
		if start > len(d.Runes) {
			// Malformed doc (fewer '\n' than Lines): stop at the end.
			break
		}
	}
	if len(l.paras) == 0 {
		p := tlPara{}
		l.buildPara(d, &p, 0, k)
		l.paras = append(l.paras, p)
	}
	l.numberLists()
	l.vertical()
}

// buildPara shapes the runs of p and wraps them into visual lines.
func (l *tlLayout) buildPara(d *Doc, p *tlPara, pi int, k tlKey) {
	m := &l.m
	switch p.kind {
	case Bullet, Ordered:
		p.left = tlListLeft(m, p.indent)
	case Quote:
		p.left = m.quoteLeft
	case CodeBlock:
		p.left = m.border + m.prePad
	}
	right := 0
	if p.kind == CodeBlock {
		right = m.border + m.prePad
	}
	switch p.kind {
	case CodeBlock:
		p.asc, p.desc = l.monoAsc, l.monoDesc
	case Header1:
		p.asc, p.desc = l.h1Asc, l.h1Desc
	case Header2:
		p.asc, p.desc = l.h2Asc, l.h2Desc
	case Header3:
		p.asc, p.desc = l.h3Asc, l.h3Desc
	default:
		p.asc, p.desc = l.latoAsc, l.latoDesc
	}
	n := p.end - p.start
	p.px = make([]fixed.Int26_6, n+1)
	p.stop = make([]bool, n+1)
	x := fixed.Int26_6(0)
	for q := p.start; q < p.end; {
		a := d.Attrs[q]
		if a.Atom != nil {
			it := l.atomItem(a, q, k)
			it.x0 = x
			p.px[q-p.start] = x
			p.stop[q-p.start] = true
			x += it.w
			p.items = append(p.items, it)
			q++
			continue
		}
		r := q + 1
		for r < p.end && d.Attrs[r].Atom == nil && d.Attrs[r].Style == a.Style && d.Attrs[r].Link == a.Link {
			r++
		}
		mono := p.kind == CodeBlock
		head := isHeading(p.kind)
		code := !mono && !head && a.Style&Code != 0
		ppem := m.ppem
		fnt := tlFont(a.Style, mono || code)
		if w := k.weight; w != 0 && !mono && !code && !head && fnt.Weight < w {
			fnt.Weight = w
		}
		switch {
		case head:
			ppem = headPpem(m, p.kind)
			fnt = tlHeadFont(p.kind)
		case mono || code:
			ppem = m.codePpem
		}
		s := l.cache.get(l.sh, tlShapeKey{s: string(d.Runes[q:r]), font: fnt, ppem: ppem, loc: k.loc})
		it := tlItem{r0: q, r1: r, x0: x, shape: s, attr: a, code: code}
		if code {
			it.padL = fixed.I(m.codePadX)
		}
		it.w = s.width + 2*it.padL
		p.px[q-p.start] = x
		p.stop[q-p.start] = true
		for j := q + 1; j < r; j++ {
			if j-q < len(s.pos) {
				p.px[j-p.start] = x + it.padL + s.pos[j-q]
				p.stop[j-p.start] = s.stop[j-q]
			} else {
				p.px[j-p.start] = x + it.padL + s.width
				p.stop[j-p.start] = true
			}
		}
		x += it.w
		p.items = append(p.items, it)
		p.raw = l.rawEmoji(p.raw, d.Runes[q:r], q, s)
		q = r
	}
	p.px[n] = x
	p.stop[n] = true

	// Greedy wrapping.
	avail := fixed.I(max(l.width-p.left-right, 1))
	p.l0 = len(l.lines)
	a := p.start
	for {
		b := p.end
		brk := -1
		for q := a; q < p.end; q++ {
			if q > a && l.canBreak(d, p, q) {
				brk = q
			}
			if q > a && !edSpace(d.Runes[q]) && p.px[q+1-p.start]-p.px[a-p.start] > avail {
				if brk > a {
					b = brk
				} else {
					b = q
					for b > a+1 && !p.stop[b-p.start] {
						b--
					}
				}
				break
			}
		}
		l.lines = append(l.lines, tlLine{para: pi, a: a, b: b})
		if b >= p.end {
			break
		}
		a = b
	}
	p.l1 = len(l.lines)
}

// rawEmoji appends the unicode emoji of run rs (starting at doc position
// q) that s shaped as slot glyphs.
func (l *tlLayout) rawEmoji(out []tlRawEmoji, rs []rune, q int, s *tlShape) []tlRawEmoji {
	if !emoji.MayHaveEmoji(string(rs)) {
		return out
	}
	for _, er := range theme.EmojiRuns(rs) {
		for gi, g := range s.glyphs {
			if int(s.grune[gi]) == er.Start && theme.IsEmojiSlot(l.sh, g) {
				out = append(out, tlRawEmoji{q0: q + er.Start, q1: q + er.End, stem: er.Stem})
				break
			}
		}
	}
	return out
}

// canBreak reports whether a line may break before rune q of p.
func (l *tlLayout) canBreak(d *Doc, p *tlPara, q int) bool {
	if !p.stop[q-p.start] || edSpace(d.Runes[q]) {
		return false
	}
	if edSpace(d.Runes[q-1]) {
		return true
	}
	if d.Attrs[q-1].Atom != nil || d.Attrs[q].Atom != nil {
		return true
	}
	switch d.Runes[q-1] {
	case '-', '/', '—', '–':
		return true
	}
	return false
}

// atomItem measures an atom.
func (l *tlLayout) atomItem(a Attr, q int, k tlKey) tlItem {
	m := &l.m
	at := a.Atom
	it := tlItem{r0: q, r1: q + 1, attr: a}
	lato := tlFont(0, false)
	if at.Kind == AtomEmoji {
		if (k.images && at.URL != "") || at.Uni != "" {
			it.box = m.emoji
			if at.Uni != "" {
				it.shape = l.cache.get(l.sh, tlShapeKey{s: at.Uni, font: lato, ppem: m.ppem, loc: k.loc})
			}
			it.w = fixed.I(m.emoji + 2*m.emojiGap)
			return it
		}
		label := at.Text
		if label == "" {
			label = ":" + at.ID + ":"
		}
		it.shape = l.cache.get(l.sh, tlShapeKey{s: label, font: lato, ppem: m.ppem, loc: k.loc})
		it.w = it.shape.width
		return it
	}
	label := at.Text
	if label == "" {
		label = "@" + at.ID
	}
	it.shape = l.cache.get(l.sh, tlShapeKey{s: label, font: lato, ppem: m.ppem, loc: k.loc})
	it.padL = fixed.I(m.pillPadX)
	it.w = it.shape.width + 2*it.padL
	if at.Kind == AtomMessage { // the slug's "message" icon, 4px before the label
		it.icon = m.ppem + fixed.I(max(1, m.pillPadX*2))
		it.w += it.icon
	}
	return it
}

// numberLists assigns ordered list numbers: consecutive ordered lines at
// the same indent count up; deeper lines don't interrupt the count.
func (l *tlLayout) numberLists() {
	var counters [9]int
	for i := range l.paras {
		p := &l.paras[i]
		if p.kind != Bullet && p.kind != Ordered {
			counters = [9]int{}
			continue
		}
		lvl := min(max(p.indent, 0), 8)
		for j := lvl + 1; j < len(counters); j++ {
			counters[j] = 0
		}
		if p.kind == Ordered {
			counters[lvl]++
			p.num = counters[lvl]
		} else {
			counters[lvl] = 0
		}
	}
}

// vertical assigns y positions to lines and code block boxes.
func (l *tlLayout) vertical() {
	m := &l.m
	y := 0
	groupTop := 0
	for i := range l.paras {
		p := &l.paras[i]
		prevCode := i > 0 && l.paras[i-1].kind == CodeBlock
		nextCode := i+1 < len(l.paras) && l.paras[i+1].kind == CodeBlock
		if p.kind == CodeBlock && !prevCode {
			if i > 0 {
				y += m.preMargin
			}
			groupTop = y
			y += m.border + m.prePad
		}
		p.top = y
		for li := p.l0; li < p.l1; li++ {
			ln := &l.lines[li]
			h := m.lh
			switch p.kind {
			case CodeBlock:
				h = m.preLH
			case Header1:
				h = m.h1LH
			case Header2, Header3:
				h = m.headLH
			case Divider:
				h = m.dividerBox
			}
			for _, it := range p.items {
				if it.box > 0 && it.r0 >= ln.a && it.r0 < ln.b {
					h = max(h, it.box)
				}
			}
			ln.top = y
			ln.h = h
			ln.base = y + (h+p.asc-p.desc+1)/2
			y += h
			for _, it := range p.items {
				if it.box > 0 && it.r0 >= ln.a && it.r0 < ln.b {
					x := l.vx(li, it.r0) + m.emojiGap
					ey := ln.top + (ln.h-it.box)/2
					l.emojis = append(l.emojis, tlEmoji{
						rect:  image.Rect(x, ey, x+it.box, ey+it.box),
						base:  ln.base,
						atom:  it.attr.Atom,
						label: it.shape,
					})
				}
			}
		}
		for _, r := range p.raw {
			li := p.l0
			for li < p.l1 && r.q0 >= l.lines[li].b {
				li++
			}
			if li >= p.l1 {
				continue
			}
			ln := &l.lines[li]
			x0, x1 := l.vx(li, r.q0), l.vx(li, r.q1)
			es := (x1 - x0) * 120 / 127 // the 1.2em image in its 1.27em slot
			x := x0 + (x1-x0-es)/2
			ey := ln.base - es*4/5
			l.emojis = append(l.emojis, tlEmoji{
				rect: image.Rect(x, ey, x+es, ey+es),
				base: ln.base,
				atom: &Atom{Kind: AtomEmoji, URL: l.emojiURL(r.stem)},
			})
		}
		if isHeading(p.kind) {
			y += m.headMargin // 4px bottom margin below the heading
		}
		p.bot = y
		if p.kind == CodeBlock && !nextCode {
			y += m.prePad + m.border
			p.boxTop, p.boxBot = groupTop, y
			if i+1 < len(l.paras) {
				y += m.preMargin
			}
		}
	}
	l.height = y
}

// emojiURL is Slack's image for an emoji stem in the theme's style.
func (l *tlLayout) emojiURL(stem string) string {
	if t := theme.ForShaper(l.sh); t != nil {
		return t.EmojiURL(stem)
	}
	return emoji.CDN + "apple-medium/" + stem + ".png"
}

// vx is the x of caret position q on visual line li.
func (l *tlLayout) vx(li, q int) int {
	ln := &l.lines[li]
	p := &l.paras[ln.para]
	q = min(max(q, p.start), p.end)
	return p.left + (p.px[q-p.start] - p.px[ln.a-p.start]).Round()
}

// vxf is vx with subpixel precision for glyph placement.
func (l *tlLayout) vxf(li int, x fixed.Int26_6) float32 {
	ln := &l.lines[li]
	p := &l.paras[ln.para]
	return float32(p.left) + float32(x-p.px[ln.a-p.start])/64
}

// lineOf returns the visual line showing caret position pos.
func (l *tlLayout) lineOf(pos int) int {
	if len(l.lines) == 0 {
		return 0
	}
	pi := sort.Search(len(l.paras), func(i int) bool { return l.paras[i].end >= pos })
	if pi >= len(l.paras) {
		pi = len(l.paras) - 1
	}
	p := &l.paras[pi]
	for li := p.l0; li < p.l1; li++ {
		if pos < l.lines[li].b || li == p.l1-1 {
			return li
		}
	}
	return p.l0
}

// caretInfo returns the caret x, baseline and font extent at pos.
func (l *tlLayout) caretInfo(pos int) (x, base, asc, desc, li int) {
	if len(l.lines) == 0 {
		return 0, 0, 0, 0, 0
	}
	li = l.lineOf(pos)
	ln := &l.lines[li]
	p := &l.paras[ln.para]
	return l.vx(li, pos), ln.base, p.asc, p.desc, li
}

// lineAtY returns the visual line at content y (nearest if between lines).
func (l *tlLayout) lineAtY(y int) int {
	best, bestD := 0, int(^uint(0)>>1)
	for i := range l.lines {
		ln := &l.lines[i]
		if y >= ln.top && y < ln.top+ln.h {
			return i
		}
		d := ln.top - y
		if y >= ln.top+ln.h {
			d = y - (ln.top + ln.h) + 1
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// lineMax is the last caret position that displays on line li.
func (l *tlLayout) lineMax(li int) int {
	ln := &l.lines[li]
	p := &l.paras[ln.para]
	if ln.b < p.end && ln.b > ln.a {
		return ln.b - 1
	}
	return ln.b
}

// posAt returns the caret position on line li nearest to x.
func (l *tlLayout) posAt(li, x int) int {
	ln := &l.lines[li]
	p := &l.paras[ln.para]
	best, bestD := ln.a, int(^uint(0)>>1)
	for q := ln.a; q <= l.lineMax(li); q++ {
		if !p.stop[q-p.start] {
			continue
		}
		d := l.vx(li, q) - x
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = q, d
		}
	}
	return best
}

// hit maps a point in content coordinates to a caret position.
func (l *tlLayout) hit(pt image.Point) int {
	if len(l.lines) == 0 {
		return 0
	}
	return l.posAt(l.lineAtY(pt.Y), pt.X)
}

// lineEnd is where End puts the caret on line li: before a hanging space
// of a wrapped line.
func (l *tlLayout) lineEnd(d *Doc, li int) int {
	ln := &l.lines[li]
	p := &l.paras[ln.para]
	if ln.b < p.end && ln.b > ln.a && edSpace(d.Runes[ln.b-1]) {
		return ln.b - 1
	}
	return ln.b
}

// forRange calls fn with the horizontal extent of [lo, hi) on every visual
// line it touches; nl reports that the range covers the line's newline.
func (l *tlLayout) forRange(lo, hi int, fn func(li, x0, x1 int, nl bool)) {
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo == hi || len(l.lines) == 0 {
		return
	}
	for li := l.lineOf(lo); li < len(l.lines); li++ {
		ln := &l.lines[li]
		p := &l.paras[ln.para]
		if ln.a >= hi {
			break
		}
		a, b := max(lo, ln.a), min(hi, ln.b)
		nl := li == p.l1-1 && hi > p.end
		if a > b || (a == b && !nl) {
			continue
		}
		fn(li, l.vx(li, a), l.vx(li, b), nl)
	}
}

// paintStatic records the background and text macros if the layout or the
// colors changed since the last recording.
func (l *tlLayout) paintStatic(gtx layout.Context, c tlColors) {
	l.emb = c.emb
	if l.paintOK && l.paintGen == l.gen && l.paintCol == c {
		return
	}
	g := gtx
	g.Ops = new(op.Ops)
	rec := op.Record(g.Ops)
	l.paintBG(g, c)
	l.bgCall = rec.Stop()
	rec = op.Record(g.Ops)
	l.paintFG(g, c)
	l.fgCall = rec.Stop()
	l.paintOK, l.paintGen, l.paintCol = true, l.gen, c
}

// segment returns the part of item it on line li, or ok false.
func (l *tlLayout) segment(li int, it *tlItem) (a, b int, ok bool) {
	ln := &l.lines[li]
	a, b = max(it.r0, ln.a), min(it.r1, ln.b)
	return a, b, a < b
}

func (l *tlLayout) paintBG(gtx layout.Context, c tlColors) {
	m := &l.m
	for pi := range l.paras {
		p := &l.paras[pi]
		if p.kind == CodeBlock && p.boxBot > p.boxTop {
			r := image.Rect(0, p.boxTop, l.width, p.boxBot)
			fillRRect(gtx, r, m.preRadius, c.preBg)
			strokeRRect(gtx, r, m.preRadius, m.border, c.preBorder)
		}
		if p.kind == Quote {
			fillRect(gtx, image.Rect(0, p.top, m.quoteBar, p.bot), c.quoteBar)
		}
		if p.kind == Divider {
			y := p.top + (p.bot-p.top-m.dividerThick)/2
			fillRect(gtx, image.Rect(0, y, l.width, y+m.dividerThick), c.quoteBar)
		}
		for li := p.l0; li < p.l1; li++ {
			ln := &l.lines[li]
			for ii := range p.items {
				it := &p.items[ii]
				a, b, ok := l.segment(li, it)
				if !ok {
					continue
				}
				switch {
				case it.code:
					x0 := l.vx(li, a)
					if a > it.r0 {
						x0 -= m.codePadX
					}
					x1 := l.vx(li, b)
					if b < it.r1 {
						x1 += m.codePadX
					}
					top := max(ln.top, ln.base-p.asc+m.border)
					bot := min(ln.top+ln.h, ln.base+p.desc+m.border)
					r := image.Rect(x0, top, x1, bot)
					fillRRect(gtx, r, m.codeRadius, c.codeBg)
					strokeRRect(gtx, r, m.codeRadius, m.border, c.codeBorder)
				case it.attr.Atom != nil && it.attr.Atom.Kind != AtomEmoji:
					bg := c.mentionBg
					if it.attr.Atom.Kind == AtomBroadcast {
						bg = c.broadcastBg
					}
					top := max(ln.top, ln.base-p.asc-m.border)
					bot := min(ln.top+ln.h, ln.base+p.desc+m.border)
					fillRRect(gtx, image.Rect(l.vx(li, a), top, l.vx(li, b), bot), m.pillRadius, bg)
				}
			}
		}
	}
}

func (l *tlLayout) paintFG(gtx layout.Context, c tlColors) {
	m := &l.m
	for pi := range l.paras {
		p := &l.paras[pi]
		if p.l1 > p.l0 && (p.kind == Bullet || p.kind == Ordered) {
			l.paintMarker(gtx, p, &l.lines[p.l0], c.text)
		}
		for li := p.l0; li < p.l1; li++ {
			ln := &l.lines[li]
			for ii := range p.items {
				it := &p.items[ii]
				a, b, ok := l.segment(li, it)
				if !ok || it.shape == nil || it.box > 0 {
					continue
				}
				col := c.text
				switch {
				case it.attr.Atom != nil:
					col = c.mention
					switch it.attr.Atom.Kind {
					case AtomBroadcast:
						col = c.broadcast
					case AtomEmoji:
						col = c.text
					}
				case it.attr.Link != "":
					col = c.link
				case it.code:
					col = c.code
				}
				l.paintRun(gtx, li, it, a, b, col)
				if it.attr.Atom != nil {
					if it.icon > 0 {
						l.paintSlugIcon(gtx, li, it, col)
					}
					continue
				}
				st := it.attr.Style
				if st&(Underline|Strike) == 0 {
					continue
				}
				x0, x1 := l.vx(li, a), l.vx(li, b)
				if st&Underline != 0 {
					y := ln.base + max(m.ppem.Round()/10, 1)
					fillRect(gtx, image.Rect(x0, y, x1, y+m.deco), col)
				}
				if st&Strike != 0 {
					y := ln.base - (m.ppem.Round()*3+5)/10
					if it.code || p.kind == CodeBlock {
						y = ln.base - (m.codePpem.Round()*3+5)/10
					}
					fillRect(gtx, image.Rect(x0, y, x1, y+m.deco), col)
				}
			}
		}
	}
}

// paintRun paints the glyphs of runes [a, b) of item it on line li.
func (l *tlLayout) paintRun(gtx layout.Context, li int, it *tlItem, a, b int, col color.NRGBA) {
	s := it.shape
	gs := l.scratch[:0]
	if it.attr.Atom != nil {
		gs = append(gs, s.glyphs...)
	} else {
		sa, sb := int32(a-it.r0), int32(b-it.r0)
		for gi := range s.glyphs {
			if r := s.grune[gi]; r >= sa && r < sb {
				gs = append(gs, s.glyphs[gi])
			}
		}
	}
	l.scratch = gs
	if len(gs) == 0 {
		return
	}
	x := l.vxf(li, it.x0+it.padL+it.icon+gs[0].X)
	tlPaintGlyphs(gtx, l.sh, gs, f32.Pt(x, float32(l.lines[li].base)), col, l.emb)
}

// tlPaintGlyphs paints glyphs of one line with their first dot at origin.
// emb > 0 grows the ink by an emb-px stroke, like richview's message text.
func tlPaintGlyphs(gtx layout.Context, sh *text.Shaper, gs []text.Glyph, origin f32.Point, col color.NRGBA, emb ...float32) {
	if len(gs) == 0 {
		return
	}
	for _, g := range gs {
		theme.ScanGlyph(sh, g) // a running tofu scan (tests); one atomic load otherwise
	}
	t := op.Affine(f32.AffineId().Offset(origin)).Push(gtx.Ops)
	if len(emb) > 0 && emb[0] > 0 {
		if path, ok := theme.EmboldenedShape(sh, gs, emb[0]); ok {
			paint.FillShape(gtx.Ops, col, clip.Outline{Path: path}.Op())
		} else {
			path := sh.Shape(gs)
			paint.FillShape(gtx.Ops, col, clip.Outline{Path: path}.Op())
			paint.FillShape(gtx.Ops, col, clip.Stroke{Path: path, Width: emb[0]}.Op())
		}
		if call := sh.Bitmaps(gs); call != (op.CallOp{}) {
			call.Add(gtx.Ops)
		}
		t.Pop()
		return
	}
	path := sh.Shape(gs)
	outline := clip.Outline{Path: path}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	outline.Pop()
	if call := sh.Bitmaps(gs); call != (op.CallOp{}) {
		call.Add(gtx.Ops)
	}
	t.Pop()
}

// paintMarker draws a bullet or an ordered list number for p.
func (l *tlLayout) paintMarker(gtx layout.Context, p *tlPara, ln *tlLine, col color.NRGBA) {
	m := &l.m
	if p.kind == Bullet {
		cx := p.left - m.markerGap
		cy := ln.base - (m.ppem.Round()*3+5)/10
		r := m.bulletR
		switch p.indent % 3 {
		case 0:
			fillCircle(gtx, image.Pt(cx, cy), r, col)
		case 1:
			strokeRRect(gtx, image.Rect(cx-r, cy-r, cx+r, cy+r), r, m.border, col)
		default:
			fillRect(gtx, image.Rect(cx-r+1, cy-r+1, cx+r-1, cy+r-1), col)
		}
		return
	}
	s := l.cache.get(l.sh, tlShapeKey{s: tlListNumber(p.num, p.indent), font: tlFont(0, false), ppem: m.ppem, loc: l.key.loc})
	if len(s.glyphs) == 0 {
		return
	}
	// Slack: a 24px marker box ending 3px before the text; decimal and
	// alpha markers are left-aligned in it, roman ones right-aligned.
	x := float32(p.left-m.Dp24-m.Dp3) + float32(s.glyphs[0].X)/64
	if p.indent%3 == 2 {
		x = float32(p.left-m.numberGap) - float32(s.width)/64 + float32(s.glyphs[0].X)/64
	}
	tlPaintGlyphs(gtx, l.sh, s.glyphs, f32.Pt(x, float32(ln.base)), col)
}

// tlListNumber formats an ordered list marker: 1. a. i. by nesting level.
func tlListNumber(n, indent int) string {
	if n < 1 {
		n = 1
	}
	switch indent % 3 {
	case 1:
		var b []byte
		for v := n; v > 0; v = (v - 1) / 26 {
			b = append([]byte{byte('a' + (v-1)%26)}, b...)
		}
		return string(b) + "."
	case 2:
		return tlRoman(n) + "."
	}
	return strconv.Itoa(n) + "."
}

func tlRoman(n int) string {
	if n <= 0 || n >= 4000 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// paintEmoji draws emoji atoms visible in content rows [y0, y1).
func (l *tlLayout) paintEmoji(gtx layout.Context, images func(string, int) (paint.ImageOp, bool), col color.NRGBA, y0, y1 int) {
	for i := range l.emojis {
		em := &l.emojis[i]
		if em.rect.Max.Y < y0 || em.rect.Min.Y > y1 {
			continue
		}
		size := em.rect.Dx()
		if images != nil && em.atom.URL != "" {
			if img, ok := images(em.atom.URL, size); ok {
				tlPaintImage(gtx, img, em.rect)
				continue
			}
		}
		if em.label != nil && len(em.label.glyphs) > 0 {
			x := float32(em.rect.Min.X) + (float32(size)-float32(em.label.width)/64)/2 + float32(em.label.glyphs[0].X)/64
			tlPaintGlyphs(gtx, l.sh, em.label.glyphs, f32.Pt(x, float32(em.base)), col)
		}
	}
}

// tlPaintImage draws img scaled to fit r, centred.
func tlPaintImage(gtx layout.Context, img paint.ImageOp, r image.Rectangle) {
	sz := img.Size()
	if sz.X <= 0 || sz.Y <= 0 || r.Empty() {
		return
	}
	s := min(float32(r.Dx())/float32(sz.X), float32(r.Dy())/float32(sz.Y))
	w, h := float32(sz.X)*s, float32(sz.Y)*s
	off := f32.Pt(float32(r.Min.X)+(float32(r.Dx())-w)/2, float32(r.Min.Y)+(float32(r.Dy())-h)/2)
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(off)).Push(gtx.Ops)
	cl := clip.Rect{Max: sz}.Push(gtx.Ops)
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	t.Pop()
}

// paintPlaceholder draws txt where the first line's text would start.
func (l *tlLayout) paintPlaceholder(gtx layout.Context, txt string, col color.NRGBA) {
	if txt == "" || len(l.lines) == 0 {
		return
	}
	p := &l.paras[0]
	ppem := l.m.ppem
	mono := p.kind == CodeBlock
	if mono {
		ppem = l.m.codePpem
	}
	s := l.cache.get(l.sh, tlShapeKey{s: txt, font: tlFont(0, mono), ppem: ppem, loc: l.key.loc})
	if len(s.glyphs) == 0 {
		return
	}
	x := float32(p.left) + float32(s.glyphs[0].X)/64
	tlPaintGlyphs(gtx, l.sh, s.glyphs, f32.Pt(x, float32(l.lines[0].base)), col)
}

// tlListLeft is the text x of a list item: Slack indents 26, 56, 84, 112,
// then 28 per level.
func tlListLeft(m *tlMetrics, indent int) int {
	indent = min(max(indent, 0), 8)
	switch indent {
	case 0:
		return m.Dp24 + m.Dp3*2/3
	case 1:
		return m.listIndent * 2
	}
	return m.listIndent * (indent + 1)
}
