package slackc

import (
	"image"
	"math"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// lc is a layout request for one box.
type lc struct {
	availW   float32 // width available to the margin box (inf: unconstrained)
	cbW, cbH float32 // percentage bases (containing block content box); cbH < 0: indefinite
	shrink   bool    // auto width shrinks to fit the content
	measure  bool    // measuring pass: descendants' positions don't matter
	bfc      bool    // the box establishes a formatting context (no margin collapsing through it)
	fixedW   bool    // fixW is the border-box width (flex/grid/stretch/abs)
	fixedH   bool
	fixW     float32
	fixH     float32
	autoMinW bool // flex: an automatic minimum width is being computed
}

// lres is what a layout produces for the box itself.
type lres struct {
	w, h, base, lastBase, mtc, mbc, contentW, contentH float32
	m, pad, bd                                         [4]float32
}

type measEntry struct {
	gen uint32
	c   lc
	r   lres
}

func (b *box) res() lres {
	return lres{b.w, b.h, b.base, b.lastBase, b.mtc, b.mbc, b.contentW, b.contentH, b.m, b.pad, b.bd}
}

func (b *box) setRes(r lres) {
	b.w, b.h, b.base, b.lastBase, b.mtc, b.mbc, b.contentW, b.contentH = r.w, r.h, r.base, r.lastBase, r.mtc, r.mbc, r.contentW, r.contentH
	b.m, b.pad, b.bd = r.m, r.pad, r.bd
}

// length resolves l in device px; percentages against base (device px).
func (v *View) length(l css.Length, base float32) float32 {
	if l.Kind != css.LenFixed {
		return 0
	}
	if base < 0 {
		base = 0
	}
	if l.Pct == 0 && l.Vw == 0 && l.Vh == 0 {
		return l.Resolve(0, 0, 0, 0) * v.dp
	}
	// Resolve works in CSS px (and evaluates min()/max()/clamp()).
	return l.Resolve(base/v.dp, v.vw/v.dp, v.vh/v.dp, 0) * v.dp
}

// lenDef reports whether l is a definite length against base (base < 0:
// percentages are indefinite).
func lenDef(l css.Length, base float32) bool {
	if l.Kind != css.LenFixed {
		return false
	}
	return l.Pct == 0 || base >= 0
}

func snapBorder(px float32) float32 {
	if px <= 0 {
		return 0
	}
	if px < 1 {
		return 1
	}
	return float32(math.Floor(float64(px)))
}

// edges resolves margins, padding and borders against cbW.
func (v *View) edges(b *box, cbW float32) {
	st := &b.st
	for i := 0; i < 4; i++ {
		b.m[i] = v.length(st.Margin[i], cbW)
		p := v.length(st.Padding[i], cbW)
		if p < 0 {
			p = 0
		}
		b.pad[i] = p
		if st.BorderStyle[i] == css.BorderNone || st.BorderStyle[i] == css.BorderHidden {
			b.bd[i] = 0
		} else {
			b.bd[i] = snapBorder(st.BorderWidth[i] * v.dp)
		}
	}
}

func (b *box) hpad() float32 { return b.pad[1] + b.pad[3] + b.bd[1] + b.bd[3] }
func (b *box) vpad() float32 { return b.pad[0] + b.pad[2] + b.bd[0] + b.bd[2] }

// specW is the specified content width, or -1 for auto.
func (v *View) specW(b *box, cbW float32) float32 {
	st := &b.st
	if !lenDef(st.Width, cbW) {
		return -1
	}
	w := v.length(st.Width, cbW)
	if st.BoxSizing == css.BorderBox {
		w -= b.hpad()
	}
	return max(w, 0)
}

func (v *View) specH(b *box, cbH float32) float32 {
	st := &b.st
	if !lenDef(st.Height, cbH) {
		return -1
	}
	h := v.length(st.Height, cbH)
	if st.BoxSizing == css.BorderBox {
		h -= b.vpad()
	}
	return max(h, 0)
}

// minMaxW returns the content-box min and max widths.
func (v *View) minMaxW(b *box, cbW float32) (float32, float32) {
	st := &b.st
	lo, hi := float32(0), inf
	if lenDef(st.MinWidth, cbW) {
		lo = v.length(st.MinWidth, cbW)
		if st.BoxSizing == css.BorderBox {
			lo -= b.hpad()
		}
	}
	if lenDef(st.MaxWidth, cbW) {
		hi = v.length(st.MaxWidth, cbW)
		if st.BoxSizing == css.BorderBox {
			hi -= b.hpad()
		}
	}
	return max(lo, 0), max(hi, 0)
}

func (v *View) minMaxH(b *box, cbH float32) (float32, float32) {
	st := &b.st
	lo, hi := float32(0), inf
	if lenDef(st.MinHeight, cbH) {
		lo = v.length(st.MinHeight, cbH)
		if st.BoxSizing == css.BorderBox {
			lo -= b.vpad()
		}
	}
	if lenDef(st.MaxHeight, cbH) {
		hi = v.length(st.MaxHeight, cbH)
		if st.BoxSizing == css.BorderBox {
			hi -= b.vpad()
		}
	}
	return max(lo, 0), max(hi, 0)
}

func clampF(x, lo, hi float32) float32 {
	if x > hi {
		x = hi
	}
	if x < lo {
		x = lo
	}
	return x
}

// layout lays out b (and its in-flow subtree) under c. Results land in
// b.w/b.h…; children positions are valid after the last non-measure call.
//
// Measuring and final passes compute the same thing (positions, native
// constraints), so the measure flag isn't part of the cache key: the last
// computation's key says whose positions the subtree holds.
func (v *View) layout(b *box, c lc) {
	measure := c.measure
	c.measure = false
	if b.lastOK && b.lastGen == v.layGen && b.last == c {
		b.setRes(b.lastRes)
		return
	}
	if measure {
		// sizes only: positions stay those of b.last
		for i := range b.mcache {
			e := &b.mcache[i]
			if e.gen == v.layGen && e.c == c {
				b.setRes(e.r)
				return
			}
		}
	}
	c.measure = measure
	v.doLayout(b, c)
	c.measure = false
	b.last, b.lastGen, b.lastOK = c, v.layGen, true
	b.lastRes = b.res()
	e := &b.mcache[b.mnext%uint8(len(b.mcache))]
	b.mnext++
	e.gen, e.c, e.r = v.layGen, c, b.lastRes
}

func (v *View) doLayout(b *box, c lc) {
	st := &b.st
	v.edges(b, c.cbW)
	b.base, b.lastBase = -1, -1
	b.contentW, b.contentH = 0, 0
	b.runs = b.runs[:0]
	hp, vp := b.hpad(), b.vpad()
	lo, hi := v.minMaxW(b, c.cbW)
	hlo, hhi := v.minMaxH(b, c.cbH)

	cw := float32(-1)
	if c.fixedW {
		cw = max(c.fixW-hp, 0)
	} else if sw := v.specW(b, c.cbW); sw >= 0 && !c.autoMinW {
		cw = clampF(sw, lo, hi)
	} else if !c.shrink {
		cw = clampF(c.availW-b.m[1]-b.m[3]-hp, lo, hi)
	}
	ch := float32(-1)
	if c.fixedH {
		ch = max(c.fixH-vp, 0)
	} else if sh := v.specH(b, c.cbH); sh >= 0 {
		ch = clampF(sh, hlo, hhi)
	}
	if st.AspectRatio > 0 && b.repl == rNone {
		switch {
		case cw >= 0 && ch < 0:
			ch = clampF(cw/st.AspectRatio, hlo, hhi)
		case ch >= 0 && cw < 0:
			cw = clampF(ch*st.AspectRatio, lo, hi)
		}
	}
	// the width the content may use when shrinking
	avail := c.availW - b.m[1] - b.m[3] - hp
	if avail > hi {
		avail = hi
	}
	if avail < 0 {
		avail = 0
	}

	switch {
	case b.repl != rNone && b.repl != rBr:
		cw, ch = v.layoutReplaced(b, cw, ch, avail, c)
		cw = clampF(cw, lo, hi)
	case b.native:
		cw, ch = v.layoutNative(b, cw, ch, avail, lo, hi, hlo, hhi, c)
	case isFlex(st.Display) && st.LineClamp == 0:
		// display:-webkit-box with -webkit-line-clamp lays out as a block
		// flow with clamped lines, like Chrome.
		cw, ch = v.layoutFlex(b, cw, ch, avail, lo, hi, hlo, hhi, c)
	case isGrid(st.Display):
		cw, ch = v.layoutGrid(b, cw, ch, avail, lo, hi, c)
	case st.Display == css.DisplayTableRow:
		cw, ch = v.layoutFlex(b, cw, ch, avail, lo, hi, hlo, hhi, c)
	default:
		bfc := c.bfc || st.OverflowX != css.OverflowVisible || st.OverflowY != css.OverflowVisible ||
			st.Display == css.DisplayInlineBlock || st.Display == css.DisplayTableCell || st.Display == css.DisplayTable
		h := float32(0)
		used := float32(0)
		if cw < 0 {
			u, h1 := v.layoutFlow(b, avail, ch, bfc, ch < 0, true, true, c)
			cw = clampF(u, lo, hi)
			used = u
			// Inline-only, start-aligned content is already laid out
			// exactly as a second pass at the used width would.
			if cw == u && v.inlineOnly(b) {
				h = h1
			} else {
				used, h = v.layoutFlow(b, cw, ch, bfc, ch < 0, c.measure, false, c)
			}
		} else {
			used, h = v.layoutFlow(b, cw, ch, bfc, ch < 0, c.measure, false, c)
		}
		if ch < 0 {
			ch = clampF(h, hlo, hhi)
		}
		// scrollable overflow (padding box)
		b.contentH = h + b.pad[0] + b.pad[2]
		b.contentW = used + b.pad[1] + b.pad[3]
	}
	if ch < 0 {
		ch = 0
	}
	switch {
	case b.repl == rInput || b.repl == rSelect:
		// Like Chrome, a text input or select sits on the baseline of the
		// text it draws (vertically centred in the content box; see
		// painter.input / selectCtl), not on its bottom edge.
		lh := v.lineHeight(st)
		a, _, _, _ := v.metrics(st)
		oy := (ch - lh) / 2
		if b.repl == rInput && oy < 0 {
			oy = 0
		}
		b.base = b.bd[0] + b.pad[0] + oy + a
		b.lastBase = b.base
	case b.n != nil && b.n.T == "button" && b.base >= 0:
		// A button's baseline is its first line's (HTML rendering), not
		// the last line box an inline-block would use.
		b.lastBase = b.base
	}
	b.w = cw + hp
	b.h = ch + vp
	b.contentW = max(b.contentW, cw+b.pad[1]+b.pad[3])
	b.contentH = max(b.contentH, ch+b.pad[0]+b.pad[2])
}

// mcoll accumulates collapsing margins.
type mcoll struct{ pos, neg float32 }

func (m *mcoll) add(x float32) {
	if x > m.pos {
		m.pos = x
	}
	if x < m.neg {
		m.neg = x
	}
}
func (m mcoll) val() float32 { return m.pos + m.neg }

// layoutFlow lays out a block container's children (blocks and inline
// runs) in a content box of width cw. It returns the widest child (for
// shrink-to-fit) and the content height.
// shrink lays block children out at their preferred widths (a
// shrink-to-fit measuring pass).
func (v *View) layoutFlow(b *box, cw, ch float32, bfc, autoH, measure, shrink bool, c lc) (float32, float32) {
	st := &b.st
	cx := b.bd[3] + b.pad[3]
	cy := b.bd[0] + b.pad[0]
	collTop := !bfc && b.pad[0] == 0 && b.bd[0] == 0
	collBot := !bfc && b.pad[2] == 0 && b.bd[2] == 0 && autoH
	chDef := ch
	if ch < 0 && c.fixedH {
		chDef = max(c.fixH-b.vpad(), 0)
	}
	used := float32(0)
	y := float32(0)
	var acc, top mcoll
	first := true
	absorbed := false
	lk := b.lk
	b.runs = b.runs[:0]
	nrun := 0
	for i := 0; i < len(lk); {
		k := lk[i]
		if k.kind != kText && k.st.Display == css.DisplayNone {
			i++
			continue
		}
		if v.inRun(k) {
			j := i + 1
			for j < len(lk) && v.inRun(lk[j]) {
				j++
			}
			r := v.runAt(b, nrun)
			nrun++
			r.start, r.end = i, j
			w, h := v.layoutIFC(&r.f, b, lk[i:j], cw, chDef, measure)
			if w > used {
				used = w
			}
			if h > 0 {
				y += acc.val()
				acc = mcoll{}
				first = false
			}
			r.y = y
			y += h
			{
				for fi := range r.f.items {
					it := &r.f.items[fi]
					switch it.kind {
					case iAtomic:
						it.b.x += cx
						it.b.y += cy + r.y
					case iAbs:
						it.b.staticX += cx
						it.b.staticY += cy + r.y
						it.b.staticLP = b
					}
				}
				if b.base < 0 && r.f.base >= 0 {
					b.base = cy + r.y + r.f.base
				}
				if r.f.lastBase >= 0 {
					b.lastBase = cy + r.y + r.f.lastBase
				}
			}
			i = j
			continue
		}
		i++
		if outOfFlow(&k.st) {
			k.staticX = cx
			k.staticY = cy + y + acc.val()
			k.staticLP = b
			continue
		}
		v.layout(k, lc{availW: cw, cbW: cw, cbH: chDef, measure: measure, shrink: shrink})
		ml, mr := k.m[3], k.m[1]
		if k.st.Margin[3].IsAuto() || k.st.Margin[1].IsAuto() {
			free := cw - k.w - ml - mr
			if free > 0 {
				switch {
				case k.st.Margin[3].IsAuto() && k.st.Margin[1].IsAuto():
					ml += free / 2
				case k.st.Margin[3].IsAuto():
					ml += free
				}
			}
		}
		if k.w+k.m[1]+k.m[3] > used {
			used = k.w + k.m[1] + k.m[3]
		}
		k.x = cx + ml
		k.lp = b
		acc.add(k.mtc)
		if k.h == 0 && k.collapseThrough() {
			acc.add(k.mbc)
			k.y = cy + y + acc.val()
			continue
		}
		if first && collTop {
			top = acc
			absorbed = true
		} else {
			y += acc.val()
		}
		k.y = cy + y
		if b.base < 0 && k.base >= 0 {
			b.base = k.y + k.base
		}
		if k.lastBase >= 0 {
			b.lastBase = k.y + k.lastBase
		}
		y += k.h
		acc = mcoll{}
		acc.add(k.mbc)
		first = false
	}
	b.runs = b.runs[:nrun]
	b.mtc, b.mbc = b.m[0], b.m[2]
	if collBot {
		var m mcoll
		m.add(b.m[2])
		m.pos, m.neg = max(m.pos, acc.pos), min(m.neg, acc.neg)
		b.mbc = m.val()
	} else {
		y += acc.val()
	}
	if absorbed {
		var m mcoll
		m.add(b.m[0])
		m.pos, m.neg = max(m.pos, top.pos), min(m.neg, top.neg)
		b.mtc = m.val()
	}
	if first && collTop && collBot && y == 0 {
		// empty box: its own margins collapse through
		var m mcoll
		m.add(b.m[0])
		m.add(b.m[2])
		m.pos, m.neg = max(m.pos, acc.pos), min(m.neg, acc.neg)
		b.mtc, b.mbc = m.val(), 0
	}
	_ = st
	return used, y
}

// inlineOnly reports a block container whose in-flow content is all
// inline runs with start-aligned text (one shrink-to-fit pass suffices).
func (v *View) inlineOnly(b *box) bool {
	switch b.st.TextAlign {
	case css.TextStart, css.TextLeft:
	default:
		return false
	}
	for _, k := range b.lk {
		if k.kind != kText && k.st.Display != css.DisplayNone && !v.inRun(k) && !outOfFlow(&k.st) {
			return false
		}
	}
	return true
}

// collapseThrough reports an empty block whose margins collapse
// through it.
func (b *box) collapseThrough() bool {
	return b.pad[0] == 0 && b.pad[2] == 0 && b.bd[0] == 0 && b.bd[2] == 0 && b.repl == rNone && !b.native &&
		!isFlex(b.st.Display) && !isGrid(b.st.Display) && b.st.Height.IsAuto() && len(b.runs) == 0
}

func (v *View) runAt(b *box, i int) *run {
	for len(b.runPool) <= i {
		b.runPool = append(b.runPool, &run{})
	}
	r := b.runPool[i]
	b.runs = append(b.runs, r)
	return r
}

// inRun reports a box that flows in an inline run of its container.
func (v *View) inRun(k *box) bool {
	if k.kind == kText {
		return true
	}
	if outOfFlow(&k.st) {
		return isInlineLevel(k.st.Display) && k.kind != kComp
	}
	return isInlineLevel(k.st.Display) || k.repl == rBr
}

// --- replaced elements ---

func attrNum(n *Node, name string) (float32, bool) {
	if n == nil {
		return 0, false
	}
	switch x := n.P[name].(type) {
	case float64:
		return float32(x), true
	case string:
		x = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(x), "px"))
		if f, ok := toFloat(x); ok {
			return float32(f), true
		}
	}
	return 0, false
}

func (v *View) layoutReplaced(b *box, cw, ch, avail float32, c lc) (float32, float32) {
	n := b.n
	st := &b.st
	em := st.FontSize * v.sp
	natW, natH := float32(-1), float32(-1) // CSS px * dp
	switch b.repl {
	case rImg:
		if w, ok := attrNum(n, "width"); ok {
			natW = w * v.dp
		}
		if h, ok := attrNum(n, "height"); ok {
			natH = h * v.dp
		}
		src, _ := n.AttrString("src")
		if natW < 0 || natH < 0 {
			if im := env().Images; im != nil && src != "" {
				im.Prefetch(src, 0)
				if sz, ok := im.Size(src, 0); ok && sz.X > 0 && sz.Y > 0 {
					iw, ih := float32(sz.X)*v.dp, float32(sz.Y)*v.dp
					switch {
					case natW < 0 && natH < 0:
						natW, natH = iw, ih
					case natW < 0:
						natW = natH * iw / ih
					case natH < 0:
						natH = natW * ih / iw
					}
				} else {
					v.pendingImg = true
				}
			}
		}
		if natW < 0 && natH < 0 {
			if alt, _ := n.AttrString("alt"); alt != "" && cw < 0 {
				sh := word(v.face(st), alt)
				natW, natH = sh.adv, v.lineHeight(st)
			}
		}
	case rSVG:
		if b.svg == nil {
			b.svg = compileSVG(n)
		}
		w, h, wok, hok, vb, hasVB := b.svg.intrinsic()
		if wok {
			natW = w * v.dp
		}
		if hok {
			natH = h * v.dp
		}
		if hasVB && vb[2] > 0 && vb[3] > 0 {
			r := vb[2] / vb[3]
			switch {
			case natW >= 0 && natH < 0:
				natH = natW / r
			case natH >= 0 && natW < 0:
				natW = natH * r
			case natW < 0 && natH < 0 && cw >= 0:
				natH = cw / r
			case natW < 0 && natH < 0 && ch >= 0:
				natW = ch * r
			}
		}
		if natW < 0 {
			natW = 300 * v.dp
		}
		if natH < 0 {
			natH = 150 * v.dp
		}
	case rInput:
		natH = v.lineHeight(st)
		size := float32(20)
		if s, ok := attrNum(n, "size"); ok && s > 0 {
			size = s
		}
		natW = size * em * 0.5
	case rTextarea:
		cols, rows := float32(20), float32(2)
		if s, ok := attrNum(n, "cols"); ok && s > 0 {
			cols = s
		}
		if s, ok := attrNum(n, "rows"); ok && s > 0 {
			rows = s
		}
		natW = cols * em * 0.55
		natH = rows * v.lineHeight(st)
	case rCheck, rRadio:
		natW, natH = 13*v.dp, 13*v.dp
		if b.st.Margin[0].Kind == css.LenFixed && b.st.Margin == [4]css.Length{} && b.set == 0 {
			b.m = [4]float32{3 * v.dp, 3 * v.dp, 3 * v.dp, 4 * v.dp}
		}
	case rRange:
		natW, natH = 129*v.dp, 16*v.dp
	case rProgress:
		natW, natH = 10*em, em
	case rSelect:
		w := float32(0)
		for _, o := range selectOptions(n) {
			if sw := word(v.face(st), o.label).adv; sw > w {
				w = sw
			}
		}
		natW = w + 24*v.dp
		natH = v.lineHeight(st)
	case rEmbed:
		natW, natH = 300*v.dp, 150*v.dp
		if w, ok := attrNum(n, "width"); ok {
			natW = w * v.dp
		}
		if h, ok := attrNum(n, "height"); ok {
			natH = h * v.dp
		}
	}
	if natW < 0 {
		natW = 0
	}
	if natH < 0 {
		natH = 0
	}
	switch {
	case cw >= 0 && ch >= 0:
	case cw >= 0:
		if natW > 0 && (b.repl == rImg || b.repl == rSVG || b.repl == rEmbed) {
			ch = cw * natH / natW
		} else {
			ch = natH
		}
	case ch >= 0:
		if natH > 0 && (b.repl == rImg || b.repl == rSVG || b.repl == rEmbed) {
			cw = ch * natW / natH
		} else {
			cw = natW
		}
	default:
		cw, ch = natW, natH
		if b.repl == rImg && st.AspectRatio > 0 {
			ch = cw / st.AspectRatio
		}
	}
	if b.repl == rInput || b.repl == rTextarea || b.repl == rSelect {
		// form controls don't shrink below their content, but fit the line
		if cw > avail && avail > 0 && b.st.Width.IsAuto() {
			cw = avail
		}
	}
	return cw, ch
}

// --- natives ("@" components drawn by Clack widgets) ---

type natMeas struct {
	ok bool
	ck consKey
	d  dimsF
}

type consKey struct{ minW, minH, maxW, maxH int }

type dimsF struct{ w, h, base float32 }

func ck(cs layout.Constraints) consKey {
	return consKey{cs.Min.X, cs.Min.Y, cs.Max.X, cs.Max.Y}
}

func (k consKey) cons() layout.Constraints {
	return layout.Constraints{Min: image.Pt(k.minW, k.minH), Max: image.Pt(k.maxW, k.maxH)}
}

func ceilI(x float32) int { return int(math.Ceil(float64(x) - 0.01)) }

func (v *View) layoutNative(b *box, cw, ch, avail, lo, hi, hlo, hhi float32, c lc) (float32, float32) {
	var cs layout.Constraints
	if cw >= 0 {
		cs.Min.X, cs.Max.X = ceilI(cw), ceilI(cw)
	} else {
		cs.Min.X = ceilI(lo)
		cs.Max.X = ceilI(clampF(avail, lo, hi))
	}
	if ch >= 0 {
		cs.Min.Y, cs.Max.Y = ceilI(ch), ceilI(ch)
	} else {
		cs.Min.Y = ceilI(hlo)
		mh := v.maxH
		if c.cbH >= 0 && b.st.Height.Kind == css.LenFixed {
			mh = ceilI(c.cbH)
		}
		if hhi < inf/2 {
			mh = min(mh, ceilI(hhi))
		}
		cs.Max.Y = max(mh, cs.Min.Y)
	}
	if cs.Max.X < cs.Min.X {
		cs.Max.X = cs.Min.X
	}
	d := b.scope.measureNative(b, cs)
	b.natCons = ck(cs)
	b.natLaid = d
	b.base = d.base
	b.lastBase = d.base
	if b.base >= 0 {
		b.base += b.pad[0] + b.bd[0]
		b.lastBase = b.base
	}
	return d.w, d.h
}

// --- out-of-flow boxes, relative offsets and stacking (after the flow) ---

// containing returns the containing block of an absolute/fixed box: the
// nearest positioned (or transformed) block ancestor, nil for the scope
// root (absolute) or the viewport (fixed).
func (s *scope) containing(k *box) *box {
	if k.topLayer {
		return nil // the window, whatever its ancestors are
	}
	fixed := k.st.Position == css.PosFixed
	for p := k.parent; p != nil && p != s.host; p = p.parent {
		if p.kind == kText {
			continue
		}
		if len(p.st.Transform) > 0 || !p.st.Filter.IsZero() {
			return s.blockOf(p)
		}
		if !fixed && p.st.Position != css.PosStatic {
			return s.blockOf(p)
		}
	}
	return nil
}

// blockOf maps an inline box to the block that lays it out.
func (s *scope) blockOf(p *box) *box {
	for p != nil && p != s.host && p.inIFCBox() {
		p = p.parent
	}
	if p == s.host {
		return nil
	}
	return p
}

// inIFCBox reports a non-atomic inline box (no box of its own).
func (b *box) inIFCBox() bool {
	return b.kind != kText && b.st.Display == css.DisplayInline && !outOfFlow(&b.st) &&
		(b.kind == kPseudo || b.kind == kElem && b.repl == rNone && !b.native)
}

// offsetTo is the position of b's border box relative to anc's border box
// along layout parents (ok=false when anc isn't a layout ancestor).
func offsetTo(b, anc *box) (float32, float32, bool) {
	var x, y float32
	for p := b; p != nil; p = p.lp {
		if p == anc {
			return x, y, true
		}
		x += p.x
		y += p.y
	}
	return x, y, anc == nil
}

// postLayout places out-of-flow boxes, computes relative offsets and
// collects stacking-context layers. Final layouts only.
func (s *scope) postLayout() {
	root := &s.root
	s.postWalk(root)
	s.stack(root)
}

func (s *scope) postWalk(b *box) {
	v := s.v
	for _, k := range b.lk {
		if k.kind == kText || k.st.Display == css.DisplayNone {
			continue
		}
		if outOfFlow(&k.st) {
			s.placeAbs(k)
		} else if k.st.Position == css.PosRelative || k.st.Position == css.PosSticky {
			cbw, cbh := s.cbSize(k.lp)
			l, r, t, bt := k.st.Left, k.st.Right, k.st.Top, k.st.Bottom
			k.relX, k.relY = 0, 0
			if l.Kind == css.LenFixed {
				k.relX = v.length(l, cbw)
			} else if r.Kind == css.LenFixed {
				k.relX = -v.length(r, cbw)
			}
			if t.Kind == css.LenFixed && (t.Pct == 0 || cbh >= 0) {
				k.relY = v.length(t, cbh)
			} else if bt.Kind == css.LenFixed && (bt.Pct == 0 || cbh >= 0) {
				k.relY = -v.length(bt, cbh)
			}
		} else {
			k.relX, k.relY = 0, 0
		}
		if k.native {
			s.addNative(k)
			continue // its children belong to the host's sub scopes
		}
		s.postWalk(k)
	}
}

func (s *scope) cbSize(cb *box) (float32, float32) {
	if cb == nil || cb == &s.root {
		r := &s.root
		return r.w, r.h
	}
	return max(cb.w-cb.bd[1]-cb.bd[3], 0), max(cb.h-cb.bd[0]-cb.bd[2], 0)
}

func (s *scope) placeAbs(k *box) {
	v := s.v
	fixed := k.st.Position == css.PosFixed
	cb := s.containing(k)
	var cbW, cbH, cbX, cbY float32
	k.fixedVP = false
	switch {
	case cb != nil:
		cbW, cbH = s.cbSize(cb)
		cbX, cbY = cb.bd[3], cb.bd[0]
		k.lp = cb
	case fixed:
		cbW, cbH = v.vw, v.vh
		k.lp = &s.root
		k.fixedVP = true
	default:
		cb = &s.root
		cbW, cbH = s.cbSize(cb)
		k.lp = cb
	}
	st := &k.st
	l, r, t, bt := st.Left, st.Right, st.Top, st.Bottom
	hasL, hasR := l.Kind == css.LenFixed, r.Kind == css.LenFixed
	hasT, hasB := t.Kind == css.LenFixed, bt.Kind == css.LenFixed
	lv, rv, tv, bv := v.length(l, cbW), v.length(r, cbW), v.length(t, cbH), v.length(bt, cbH)
	v.edges(k, cbW)
	c := lc{availW: cbW, cbW: cbW, cbH: cbH, shrink: true, bfc: true}
	if hasL {
		c.availW -= lv
	}
	if hasR {
		c.availW -= rv
	}
	if st.Width.IsAuto() && hasL && hasR && !k.native && k.repl == rNone {
		c.fixedW = true
		c.fixW = max(cbW-lv-rv-k.m[1]-k.m[3], 0)
	}
	if st.Height.IsAuto() && hasT && hasB && k.repl == rNone {
		c.fixedH = true
		c.fixH = max(cbH-tv-bv-k.m[0]-k.m[2], 0)
	}
	v.layout(k, c)
	// static position relative to the containing block's border box
	sx, sy := cbX, cbY
	if k.staticLP != nil {
		if ox, oy, ok := offsetTo(k.staticLP, k.lp); ok && !k.fixedVP {
			sx, sy = ox+k.staticX, oy+k.staticY
		}
	}
	ml, mr, mt, mb := k.m[3], k.m[1], k.m[0], k.m[2]
	var x, y float32
	switch {
	case hasL && hasR && !st.Width.IsAuto():
		free := cbW - lv - rv - k.w - ml - mr
		if st.Margin[3].IsAuto() && st.Margin[1].IsAuto() {
			ml += free / 2
		} else if st.Margin[3].IsAuto() {
			ml += free
		}
		x = cbX + lv + ml
	case hasL:
		x = cbX + lv + ml
	case hasR:
		x = cbX + cbW - rv - mr - k.w
	default:
		x = sx + ml
	}
	switch {
	case hasT && hasB && !st.Height.IsAuto():
		free := cbH - tv - bv - k.h - mt - mb
		if st.Margin[0].IsAuto() && st.Margin[2].IsAuto() {
			mt += free / 2
		} else if st.Margin[0].IsAuto() {
			mt += free
		}
		y = cbY + tv + mt
	case hasT:
		y = cbY + tv + mt
	case hasB:
		y = cbY + cbH - bv - mb - k.h
	default:
		y = sy + mt
	}
	k.x, k.y = x, y
	k.relX, k.relY = 0, 0
}

// stack marks stacking contexts and collects their layers.
func (s *scope) stack(root *box) {
	root.ctxRoot = true
	markCtx(root, false)
	root.neg, root.pos = root.neg[:0], root.pos[:0]
	collectCtx(root, root, root)
}

func markCtx(b *box, parentFlex bool) {
	for _, k := range b.lk {
		if k.kind == kText || k.st.Display == css.DisplayNone {
			continue
		}
		st := &k.st
		pos := st.Position != css.PosStatic && !k.inIFCBox()
		flexItem := parentFlex && !st.ZAuto
		k.layered = pos || flexItem
		k.ctxRoot = (pos && !st.ZAuto) || st.Position == css.PosFixed || st.Position == css.PosSticky ||
			st.Opacity < 1 || len(st.Transform) > 0 || !st.Filter.IsZero() || flexItem
		if !k.native {
			markCtx(k, isFlex(st.Display) || isGrid(st.Display))
		}
	}
}

func collectCtx(root, ctx, b *box) {
	for _, k := range b.lk {
		if k.kind == kText || k.st.Display == css.DisplayNone {
			continue
		}
		if k.topLayer {
			// the top layer: above everything in the tree, in the order
			// the dialogs appear
			root.pos = insertZ(root.pos, k)
		} else if k.layered {
			if k.st.ZIndex < 0 && !k.st.ZAuto {
				ctx.neg = insertZ(ctx.neg, k)
			} else {
				ctx.pos = insertZ(ctx.pos, k)
			}
		}
		if k.native {
			continue
		}
		if k.ctxRoot {
			k.neg, k.pos = k.neg[:0], k.pos[:0]
			collectCtx(root, k, k)
			continue
		}
		collectCtx(root, ctx, k)
	}
}

func zOf(b *box) int32 {
	if b.topLayer {
		return math.MaxInt32
	}
	if b.st.ZAuto {
		return 0
	}
	return b.st.ZIndex
}

// insertZ inserts k keeping z order stable (tree order within a z).
func insertZ(list []*box, k *box) []*box {
	z := zOf(k)
	i := len(list)
	for i > 0 && zOf(list[i-1]) > z {
		i--
	}
	list = append(list, nil)
	copy(list[i+1:], list[i:])
	list[i] = k
	return list
}
