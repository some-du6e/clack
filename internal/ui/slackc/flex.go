package slackc

import (
	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// fitem is one flex item's working state.
type fitem struct {
	b              *box
	base, hyp      float32 // content-box flex base and hypothetical main size
	lo, hi         float32 // content-box min/max main size
	mainExtra      float32 // main-axis padding+border
	mm0, mm1       float32 // main-axis margins (start, end)
	target         float32 // resolved content main size
	frozen         bool
	cross          float32 // border-box cross size
	above          float32 // baseline from the margin-box cross start
	grow, shrinkF  float32
	autoM0, autoM1 bool // auto main margins
	line           int
	none           bool // a native that drew nothing: no item (see layoutFlex)
}

type fline struct {
	i0, i1     int
	cross      float32
	pos        float32
	maxAbove   float32
	usedMain   float32
	hasBaseAln bool
}

func (v *View) alignSelf(parent, k *box) css.Align {
	a := k.st.AlignSelf
	if a == css.AlignAuto {
		a = parent.st.AlignItems
	}
	if a == css.AlignAuto {
		a = css.AlignStretch
	}
	return a
}

func (v *View) layoutFlex(b *box, cw, ch, avail, lo, hi, hlo, hhi float32, c lc) (float32, float32) {
	st := &b.st
	tableRow := st.Display == css.DisplayTableRow
	row := tableRow || st.FlexDirection == css.Row || st.FlexDirection == css.RowReverse
	rev := !tableRow && (st.FlexDirection == css.RowReverse || st.FlexDirection == css.ColumnReverse)
	wrap := !tableRow && st.FlexWrap != css.NoWrap
	wrapRev := st.FlexWrap == css.WrapReverse
	cx := b.bd[3] + b.pad[3]
	cy := b.bd[0] + b.pad[0]
	measure := c.measure

	chDef := ch
	if chDef < 0 && c.fixedH {
		chDef = max(c.fixH-b.vpad(), 0)
	}
	cwBase := cw
	if cwBase < 0 {
		cwBase = avail
	}
	rowGap := v.length(st.RowGap, max(chDef, 0))
	colGap := v.length(st.ColGap, cwBase)
	gapMain, gapCross := colGap, rowGap
	if !row {
		gapMain, gapCross = rowGap, colGap
	}

	// main size available
	var mainAvail float32
	mainDef := false
	if row {
		if cw >= 0 {
			mainAvail, mainDef = cw, true
		} else {
			mainAvail = avail
		}
	} else {
		if ch >= 0 {
			mainAvail, mainDef = ch, true
		} else {
			mainAvail = hhi
		}
	}

	items := b.fitems[:0]
	for _, k := range b.lk {
		if k.kind == kText {
			continue // whitespace-only text between items
		}
		if k.st.Display == css.DisplayNone {
			continue
		}
		if outOfFlow(&k.st) {
			k.staticX, k.staticY, k.staticLP = cx, cy, b
			continue
		}
		items = append(items, fitem{b: k})
	}
	// order (stable)
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].b.st.Order < items[j-1].b.st.Order; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	pctBase := cwBase
	dropped := false
	for i := range items {
		it := &items[i]
		k := it.b
		v.edges(k, pctBase)
		ks := &k.st
		it.grow, it.shrinkF = ks.FlexGrow, ks.FlexShrink
		basis := ks.FlexBasis
		if tableRow && ks.Width.IsAuto() {
			it.grow, it.shrinkF = 1, 1
			basis = css.Px(0)
		}
		if row {
			it.mm0, it.mm1 = k.m[3], k.m[1]
			it.autoM0, it.autoM1 = ks.Margin[3].IsAuto(), ks.Margin[1].IsAuto()
			it.mainExtra = k.hpad()
			it.lo, it.hi = v.minMaxW(k, pctBase)
		} else {
			it.mm0, it.mm1 = k.m[0], k.m[2]
			it.autoM0, it.autoM1 = ks.Margin[0].IsAuto(), ks.Margin[2].IsAuto()
			it.mainExtra = k.vpad()
			it.lo, it.hi = v.minMaxH(k, chDef)
		}
		mainBase := mainAvail
		if !mainDef {
			mainBase = -1
		}
		// flex base size (content box)
		fb := float32(-1)
		if basis.Kind == css.LenFixed && (basis.Pct == 0 || mainBase >= 0) {
			fb = v.length(basis, mainBase)
			if ks.BoxSizing == css.BorderBox {
				fb -= it.mainExtra
			}
			fb = max(fb, 0)
		} else if basis.Kind == css.LenAuto || basis.Kind == css.LenFixed {
			if row {
				fb = v.specW(k, pctBase)
			} else {
				fb = v.specH(k, chDef)
			}
		}
		crossFix := !row && v.alignSelf(b, k) == css.AlignStretch && ks.Width.IsAuto() && !ks.Margin[1].IsAuto() && !ks.Margin[3].IsAuto() && cw >= 0
		if fb < 0 {
			if row {
				v.layout(k, lc{availW: inf, cbW: pctBase, cbH: chDef, shrink: true, measure: true, bfc: true})
				fb = k.w - it.mainExtra
			} else {
				kc := lc{availW: cwBase, cbW: cwBase, cbH: -1, shrink: !crossFix, measure: true, bfc: true}
				if crossFix {
					kc.fixedW, kc.fixW = true, max(cw-k.m[1]-k.m[3], 0)
				}
				v.layout(k, kc)
				fb = k.h - it.mainExtra
			}
			// A native that drew nothing stands for a Slack component
			// that rendered null (ThreadSearchButton with find_in_thread
			// off, beside ThreadJump's button): the browser has no box
			// there, so it's no item and takes no gap.
			if k.native && k.w == 0 && k.h == 0 && it.mainExtra == 0 && it.mm0 == 0 && it.mm1 == 0 {
				it.none, dropped = true, true
			}
		}
		it.base = fb
		// automatic minimum size
		autoMin := false
		if row {
			autoMin = ks.MinWidth.IsAuto() && ks.OverflowX == css.OverflowVisible
		} else {
			autoMin = ks.MinHeight.IsAuto() && ks.OverflowY == css.OverflowVisible
		}
		if autoMin && !tableRow {
			var mc float32
			if row {
				v.layout(k, lc{availW: 0, cbW: pctBase, cbH: chDef, shrink: true, measure: true, bfc: true, autoMinW: true})
				mc = k.w - it.mainExtra
				if sw := v.specW(k, pctBase); sw >= 0 && sw < mc {
					mc = sw
				}
			} else {
				mc = fb
				if sh := v.specH(k, chDef); sh >= 0 && sh < mc {
					mc = sh
				}
			}
			if mc > it.hi {
				mc = it.hi
			}
			if mc > it.lo {
				it.lo = mc
			}
		}
		it.hyp = clampF(fb, it.lo, it.hi)
	}
	if dropped {
		n := 0
		for _, it := range items {
			if it.none {
				k := it.b
				k.x, k.y, k.w, k.h = cx, cy, 0, 0
				continue
			}
			items[n] = it
			n++
		}
		items = items[:n]
	}

	// container main size when not definite
	outerHyp := func(it *fitem) float32 { return it.hyp + it.mainExtra + it.mm0 + it.mm1 }
	if !mainDef {
		sum := float32(0)
		if wrap {
			// the widest line when wrapping at avail
			lineSum := float32(0)
			n := 0
			for i := range items {
				o := outerHyp(&items[i])
				if n > 0 && lineSum+gapMain+o > mainAvail {
					sum = max(sum, lineSum)
					lineSum, n = 0, 0
				}
				if n > 0 {
					lineSum += gapMain
				}
				lineSum += o
				n++
			}
			sum = max(sum, lineSum)
		} else {
			for i := range items {
				if i > 0 {
					sum += gapMain
				}
				sum += outerHyp(&items[i])
			}
		}
		if row {
			mainAvail = clampF(min(sum, avail), lo, hi)
		} else {
			mainAvail = clampF(sum, hlo, hhi)
		}
	}

	// lines
	lines := b.flines[:0]
	start := 0
	lineSum := float32(0)
	for i := range items {
		o := outerHyp(&items[i])
		if wrap && i > start && lineSum+gapMain+o > mainAvail+0.01 {
			lines = append(lines, fline{i0: start, i1: i})
			start, lineSum = i, 0
		}
		if i > start {
			lineSum += gapMain
		}
		lineSum += o
	}
	if len(items) > 0 {
		lines = append(lines, fline{i0: start, i1: len(items)})
	}

	// resolve flexible lengths per line (CSS flexbox §9.7)
	for li := range lines {
		ln := &lines[li]
		its := items[ln.i0:ln.i1]
		gaps := gapMain * float32(len(its)-1)
		sumHyp := gaps
		for i := range its {
			sumHyp += outerHyp(&its[i])
		}
		growing := sumHyp < mainAvail
		for i := range its {
			it := &its[i]
			it.frozen = false
			it.target = it.hyp
			if growing && it.grow == 0 || !growing && it.shrinkF == 0 || growing && it.base > it.hyp || !growing && it.base < it.hyp {
				it.frozen = true
			}
		}
		initFree := mainAvail - gaps
		for i := range its {
			it := &its[i]
			if it.frozen {
				initFree -= it.target + it.mainExtra + it.mm0 + it.mm1
			} else {
				initFree -= it.base + it.mainExtra + it.mm0 + it.mm1
			}
		}
		for iter := 0; iter < len(its)+1; iter++ {
			free := mainAvail - gaps
			sumF := float32(0)
			sumScaled := float32(0)
			allFrozen := true
			for i := range its {
				it := &its[i]
				if it.frozen {
					free -= it.target + it.mainExtra + it.mm0 + it.mm1
					continue
				}
				allFrozen = false
				free -= it.base + it.mainExtra + it.mm0 + it.mm1
				if growing {
					sumF += it.grow
				} else {
					sumF += it.shrinkF
					sumScaled += it.shrinkF * it.base
				}
			}
			if allFrozen {
				break
			}
			if sumF < 1 {
				if f := initFree * sumF; abs32(f) < abs32(free) {
					free = f
				}
			}
			for i := range its {
				it := &its[i]
				if it.frozen {
					continue
				}
				if growing {
					if sumF > 0 {
						it.target = it.base + free*it.grow/sumF
					} else {
						it.target = it.base
					}
				} else if sumScaled > 0 {
					it.target = it.base + free*(it.shrinkF*it.base)/sumScaled
				} else {
					it.target = it.base
				}
			}
			total := float32(0)
			for i := range its {
				it := &its[i]
				if it.frozen {
					continue
				}
				cl := clampF(it.target, it.lo, it.hi)
				total += cl - it.target
			}
			for i := range its {
				it := &its[i]
				if it.frozen {
					continue
				}
				cl := clampF(it.target, it.lo, it.hi)
				switch {
				case total == 0:
					it.target = cl
					it.frozen = true
				case total > 0 && cl > it.target:
					it.target = cl
					it.frozen = true
				case total < 0 && cl < it.target:
					it.target = cl
					it.frozen = true
				}
			}
			if total == 0 {
				break
			}
		}
		used := gaps
		for i := range its {
			it := &its[i]
			it.target = max(it.target, 0)
			used += it.target + it.mainExtra + it.mm0 + it.mm1
			it.line = li
		}
		ln.usedMain = used
	}

	// hypothetical cross sizes
	crossDef := false
	var crossAvail float32
	if row {
		if chDef >= 0 {
			crossDef, crossAvail = true, chDef
		}
	} else {
		crossDef, crossAvail = true, cw
		if cw < 0 {
			crossDef, crossAvail = false, avail
		}
	}
	for li := range lines {
		ln := &lines[li]
		maxAbove, maxBelow, maxCross := float32(0), float32(0), float32(0)
		for i := ln.i0; i < ln.i1; i++ {
			it := &items[i]
			k := it.b
			al := v.alignSelf(b, k)
			if row {
				v.layout(k, lc{availW: it.target + it.mainExtra + it.mm0 + it.mm1, cbW: pctBase, cbH: chDef, measure: true, bfc: true,
					fixedW: true, fixW: it.target + it.mainExtra})
				it.cross = k.h
				oc := k.h + k.m[0] + k.m[2]
				if al == css.AlignBaseline && k.base >= 0 {
					a := k.m[0] + k.base
					maxAbove, maxBelow = max(maxAbove, a), max(maxBelow, oc-a)
					ln.hasBaseAln = true
				} else {
					maxCross = max(maxCross, oc)
				}
			} else {
				kc := lc{availW: crossAvail, cbW: pctBase, cbH: chDef, shrink: true, measure: true, bfc: true, fixedH: true, fixH: it.target + it.mainExtra}
				if al == css.AlignStretch && k.st.Width.IsAuto() && !k.st.Margin[1].IsAuto() && !k.st.Margin[3].IsAuto() && crossDef {
					kc.shrink = false
					kc.fixedW, kc.fixW = true, max(crossAvail-k.m[1]-k.m[3], 0)
				}
				v.layout(k, kc)
				it.cross = k.w
				maxCross = max(maxCross, k.w+k.m[1]+k.m[3])
			}
		}
		ln.maxAbove = maxAbove
		ln.cross = max(maxCross, maxAbove+maxBelow)
	}
	if len(lines) == 1 && crossDef && !wrap {
		lines[0].cross = crossAvail
	}

	// container cross size
	sumCross := gapCross * float32(max(len(lines)-1, 0))
	for i := range lines {
		sumCross += lines[i].cross
	}
	crossSize := crossAvail
	if !crossDef {
		crossSize = sumCross
		if row {
			crossSize = clampF(crossSize, hlo, hhi)
		} else {
			crossSize = clampF(crossSize, lo, hi)
		}
	}
	// align-content
	if len(lines) > 0 && (wrap || len(lines) > 1) {
		free := crossSize - sumCross
		n := float32(len(lines))
		var pos, between float32
		switch st.AlignContent {
		case css.AlignAuto, css.AlignStretch:
			if free > 0 {
				for i := range lines {
					lines[i].cross += free / n
				}
			}
		case css.AlignEnd:
			pos = free
		case css.AlignCenter:
			pos = free / 2
		case css.AlignSpaceBetween:
			if n > 1 && free > 0 {
				between = free / (n - 1)
			}
		case css.AlignSpaceAround:
			if free > 0 {
				between = free / n
				pos = between / 2
			}
		case css.AlignSpaceEvenly:
			if free > 0 {
				between = free / (n + 1)
				pos = between
			}
		}
		for i := range lines {
			lines[i].pos = pos
			pos += lines[i].cross + gapCross + between
		}
	} else if len(lines) == 1 {
		lines[0].pos = 0
		if !crossDef {
			lines[0].cross = crossSize
		}
	}
	if wrapRev {
		for i := range lines {
			lines[i].pos = crossSize - lines[i].pos - lines[i].cross
		}
	}

	// final item layout and placement
	for li := range lines {
		ln := &lines[li]
		its := items[ln.i0:ln.i1]
		free := mainAvail - ln.usedMain
		nAuto := 0
		for i := range its {
			if its[i].autoM0 {
				nAuto++
			}
			if its[i].autoM1 {
				nAuto++
			}
		}
		var pos, between float32
		if nAuto > 0 && free > 0 {
			per := free / float32(nAuto)
			for i := range its {
				if its[i].autoM0 {
					its[i].mm0 += per
				}
				if its[i].autoM1 {
					its[i].mm1 += per
				}
			}
		} else {
			n := float32(len(its))
			jc := st.JustifyContent
			if free < 0 && (jc == css.AlignSpaceBetween || jc == css.AlignSpaceAround || jc == css.AlignSpaceEvenly) {
				jc = css.AlignStart
			}
			switch jc {
			case css.AlignEnd:
				pos = free
			case css.AlignCenter:
				pos = free / 2
			case css.AlignSpaceBetween:
				if n > 1 {
					between = free / (n - 1)
				}
			case css.AlignSpaceAround:
				between = free / n
				pos = between / 2
			case css.AlignSpaceEvenly:
				between = free / (n + 1)
				pos = between
			}
		}
		for i := range its {
			it := &its[i]
			k := it.b
			al := v.alignSelf(b, k)
			mainB := it.target + it.mainExtra
			kc := lc{cbW: pctBase, cbH: chDef, measure: measure, bfc: true}
			var crossM0, crossM1 float32
			var crossAuto0, crossAuto1 bool
			if row {
				crossM0, crossM1 = k.m[0], k.m[2]
				crossAuto0, crossAuto1 = k.st.Margin[0].IsAuto(), k.st.Margin[2].IsAuto()
				kc.availW, kc.fixedW, kc.fixW = mainB, true, mainB
				if al == css.AlignStretch && k.st.Height.IsAuto() && !crossAuto0 && !crossAuto1 {
					hl, hh := v.minMaxH(k, chDef)
					cs := clampF(ln.cross-crossM0-crossM1-k.vpad(), hl, hh) + k.vpad()
					kc.fixedH, kc.fixH = true, max(cs, 0)
					kc.cbH = max(cs-k.vpad(), 0)
				}
			} else {
				crossM0, crossM1 = k.m[3], k.m[1]
				crossAuto0, crossAuto1 = k.st.Margin[3].IsAuto(), k.st.Margin[1].IsAuto()
				kc.fixedH, kc.fixH = true, mainB
				kc.cbH = it.target
				kc.availW = ln.cross
				if al == css.AlignStretch && k.st.Width.IsAuto() && !crossAuto0 && !crossAuto1 {
					wl, wh := v.minMaxW(k, pctBase)
					cs := clampF(ln.cross-crossM0-crossM1-k.hpad(), wl, wh) + k.hpad()
					kc.fixedW, kc.fixW = true, max(cs, 0)
				} else {
					kc.shrink = true
				}
			}
			v.layout(k, kc)
			k.lp = b
			cross := k.h
			if !row {
				cross = k.w
			}
			// cross position
			free := ln.cross - cross - crossM0 - crossM1
			var cpos float32
			switch {
			case crossAuto0 && crossAuto1:
				cpos = crossM0 + max(free, 0)/2
			case crossAuto0:
				cpos = crossM0 + max(free, 0)
			case crossAuto1:
				cpos = crossM0
			default:
				switch al {
				case css.AlignEnd:
					cpos = crossM0 + free
				case css.AlignCenter:
					cpos = crossM0 + free/2
				case css.AlignBaseline:
					if row && k.base >= 0 && ln.hasBaseAln {
						cpos = ln.maxAbove - k.base
					} else {
						cpos = crossM0
					}
				default:
					cpos = crossM0
				}
			}
			if wrapRev && !(crossAuto0 || crossAuto1) {
				switch al {
				case css.AlignStart, css.AlignStretch, css.AlignAuto:
					cpos = crossM0 + free
				case css.AlignEnd:
					cpos = crossM0
				}
			}
			mpos := pos + it.mm0
			if rev {
				mpos = mainAvail - mpos - mainB
			}
			if row {
				k.x = cx + mpos
				k.y = cy + ln.pos + cpos
			} else {
				k.x = cx + ln.pos + cpos
				k.y = cy + mpos
			}
			pos += it.mm0 + mainB + it.mm1 + gapMain + between
			if b.base < 0 && k.base >= 0 && li == 0 {
				b.base = k.y + k.base
			}
			if k.lastBase >= 0 {
				b.lastBase = k.y + k.lastBase
			}
		}
	}
	b.fitems = items
	b.flines = lines
	if row {
		return mainAvail, max(crossSize, 0)
	}
	if cw < 0 {
		cw = crossSize
	}
	return cw, mainAvail
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// layoutGrid lays out a grid as an N-column flow (grid-template-columns
// repeat(N, …) / N tracks): equal columns, rows as tall as their tallest
// item, items stretched.
func (v *View) layoutGrid(b *box, cw, ch, avail, lo, hi float32, c lc) (float32, float32) {
	st := &b.st
	cx := b.bd[3] + b.pad[3]
	cy := b.bd[0] + b.pad[0]
	n := st.GridColumns
	if n <= 0 {
		n = 1
	}
	items := b.fitems[:0]
	for _, k := range b.lk {
		if k.kind == kText || k.st.Display == css.DisplayNone {
			continue
		}
		if outOfFlow(&k.st) {
			k.staticX, k.staticY, k.staticLP = cx, cy, b
			continue
		}
		items = append(items, fitem{b: k})
	}
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].b.st.Order < items[j-1].b.st.Order; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	if cw < 0 {
		// shrink: columns as wide as the widest item's max-content
		wmax := float32(0)
		for i := range items {
			k := items[i].b
			v.layout(k, lc{availW: inf, cbW: avail, cbH: -1, shrink: true, measure: true, bfc: true})
			wmax = max(wmax, k.w+k.m[1]+k.m[3])
		}
		colGap := v.length(st.ColGap, avail)
		cw = clampF(min(wmax*float32(n)+colGap*float32(n-1), avail), lo, hi)
	}
	colGap := v.length(st.ColGap, cw)
	rowGap := v.length(st.RowGap, max(ch, 0))
	colW := (cw - colGap*float32(n-1)) / float32(n)
	if colW < 0 {
		colW = 0
	}
	y := float32(0)
	for r := 0; r*n < len(items); r++ {
		row := items[r*n : min((r+1)*n, len(items))]
		rh := float32(0)
		for i := range row {
			k := row[i].b
			v.edges(k, cw)
			fw := colW - k.m[1] - k.m[3]
			if !k.st.Width.IsAuto() {
				v.layout(k, lc{availW: colW, cbW: colW, cbH: -1, measure: true, bfc: true})
			} else {
				v.layout(k, lc{availW: colW, cbW: colW, cbH: -1, measure: true, bfc: true, fixedW: true, fixW: max(fw, 0)})
			}
			rh = max(rh, k.h+k.m[0]+k.m[2])
		}
		for i := range row {
			k := row[i].b
			kc := lc{availW: colW, cbW: colW, cbH: rh, measure: c.measure, bfc: true}
			if k.st.Width.IsAuto() {
				kc.fixedW, kc.fixW = true, max(colW-k.m[1]-k.m[3], 0)
			}
			al := v.alignSelf(b, k)
			if al == css.AlignStretch && k.st.Height.IsAuto() {
				kc.fixedH, kc.fixH = true, max(rh-k.m[0]-k.m[2], 0)
			}
			v.layout(k, kc)
			k.lp = b
			k.x = cx + float32(i)*(colW+colGap) + k.m[3]
			k.y = cy + y + k.m[0]
			switch al {
			case css.AlignCenter:
				k.y += (rh - k.h - k.m[0] - k.m[2]) / 2
			case css.AlignEnd:
				k.y += rh - k.h - k.m[0] - k.m[2]
			}
			if b.base < 0 && k.base >= 0 {
				b.base = k.y + k.base
			}
		}
		y += rh + rowGap
	}
	if len(items) > 0 {
		y -= rowGap
	}
	b.fitems = items
	if ch < 0 {
		ch = y
	}
	return cw, ch
}
