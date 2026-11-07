package richview

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/font"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

func (l *lay) bodyStyle() flowStyle {
	p := &l.st.Palette
	col := p.Text
	if l.opts.Muted {
		col = p.Text3
	}
	size, lh := l.st.TextSize, l.st.LineHeight
	if l.opts.Compact {
		size, lh = 13, 19.5
	}
	fs := flowStyle{
		face:  font.Font{Typeface: l.st.Face},
		size:  l.sp(size),
		lineH: l.dp(lh),
		col:   col,
		emoji: l.dp(22),
	}
	if l.opts.Compact {
		fs.emoji = l.dp(16)
	}
	if l.jumbo {
		fs.emoji = l.dp(32)
	}
	return fs
}

func isTextual(k richtext.BlockKind) bool {
	switch k {
	case richtext.Paragraph, richtext.Quote, richtext.List, richtext.Tombstone:
		return true
	}
	return false
}

// gap is the vertical space between two consecutive blocks.
func (l *lay) gap(prev, cur richtext.BlockKind) int {
	if isTextual(prev) && isTextual(cur) && prev != richtext.Quote && cur != richtext.Quote {
		return 0
	}
	if prev == richtext.FileBlock || prev == richtext.AttachmentBlock {
		return l.dp(8)
	}
	return l.dp(4)
}

// lastTextual returns the index of the block that gets the "(edited)" label.
func lastTextual(bs []richtext.Block) int {
	for i := len(bs) - 1; i >= 0; i-- {
		switch bs[i].Kind {
		case richtext.Paragraph, richtext.Quote, richtext.List, richtext.Section, richtext.Header, richtext.Preformatted:
			if len(bs[i].Children) > 0 {
				return -1
			}
			return i
		case richtext.Tombstone:
			return -1
		default:
			// attachments, files, actions, images, context: the label goes
			// after the last text above them, like Slack.
			continue
		}
	}
	return -1
}

func (l *lay) addEdited() {
	p := &l.st.Palette
	k := faceKey{f: font.Font{Typeface: l.st.Face}, size: fixedPx(l.sp(13))}
	sp := l.st.spaceOf(faceKey{f: font.Font{Typeface: l.st.Face}, size: fixedPx(l.sp(15))})
	l.items = append(l.items, item{kind: itSpace, w: sp.adv, deco: -1, target: -1, p0: int32(len(l.d.plain)), p1: int32(len(l.d.plain))})
	w := l.st.word(k, "(edited)")
	n := int32(len(l.d.plain))
	l.items = append(l.items, item{kind: itWord, sh: w, face: k, w: w.adv, col: p.Edited, deco: -1, target: -1, p0: n, p1: n})
}

// blocks lays out bs at (x, y) in width w and returns the height.
func (l *lay) blocks(bs []richtext.Block, x, y, w int, top bool) int {
	y0 := y
	edited := -1
	if top && l.opts.Edited {
		edited = lastTextual(bs)
	}
	var gal []*richtext.File
	for i := 0; i < len(bs); i++ {
		b := &bs[i]
		if i > 0 {
			y += l.gap(bs[i-1].Kind, b.Kind)
			l.plainAdd("\n")
		}
		// Gallery: consecutive image files lay out as tiles.
		if isImageFile(b) && i+1 < len(bs) && isImageFile(&bs[i+1]) {
			gal = gal[:0]
			j := i
			for j < len(bs) && isImageFile(&bs[j]) {
				gal = append(gal, bs[j].File)
				j++
			}
			y += l.gallery(gal, x, y, w)
			i = j - 1
			continue
		}
		y += l.block(b, x, y, w, i == edited)
	}
	if edited < 0 && top && l.opts.Edited {
		// No text block to attach to: a line of its own.
		fs := l.bodyStyle()
		l.addEdited()
		l.items = l.items[1:]
		h, _ := l.flow(x, y, w, &fs)
		y += h
	}
	return y - y0
}

func isImageFile(b *richtext.Block) bool {
	return b.Kind == richtext.FileBlock && b.File != nil && b.File.IsImage && b.File.Thumb != nil
}

func (l *lay) block(b *richtext.Block, x, y, w int, edited bool) int {
	p := &l.st.Palette
	switch b.Kind {
	case richtext.Paragraph:
		fs := l.bodyStyle()
		l.addSpans(b.Spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.Tombstone:
		fs := l.bodyStyle()
		fs.col = p.Text2
		spans := b.Spans
		if len(spans) == 0 {
			spans = []richtext.Span{{Text: "This message was deleted."}}
		}
		l.addSpans(spans, &fs)
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.Preformatted:
		pad := l.dp(8)
		fs := l.bodyStyle()
		fs.face = font.Font{Typeface: l.st.Mono}
		fs.size = l.sp(12)
		fs.lineH = l.dp(18)
		fs.pre = true
		fs.emoji = l.dp(16)
		fs.col = p.Text
		if l.opts.Muted {
			fs.col = p.Text2
		}
		bi := len(l.d.boxes)
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.PreBg, stroke: p.PreBorder, bw: l.dp(1), radius: l.dp(4), target: -1})
		l.addSpans(b.Spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ := l.flow(x+pad, y+pad, w-2*pad, &fs)
		hh := h + 2*pad
		l.d.boxes[bi].r = image.Rect(x, y, x+w, y+hh)
		return hh

	case richtext.Quote:
		bar := l.dp(4)
		bi := len(l.d.boxes)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: p.QuoteBar, radius: l.dp(8), target: -1})
		var h int
		if len(b.Children) > 0 {
			h = l.blocks(b.Children, x+l.dp(16), y, w-l.dp(16), false)
		} else {
			fs := l.bodyStyle()
			l.addSpans(b.Spans, &fs)
			if edited {
				l.addEdited()
			}
			h, _ = l.flow(x+l.dp(16), y, w-l.dp(16), &fs)
		}
		l.d.boxes[bi].r = image.Rect(x, y, x+bar, y+h)
		return h

	case richtext.List:
		return l.list(b, x, y, w, edited)

	case richtext.Header:
		fs := l.bodyStyle()
		fs.face.Weight = font.Black
		l.addSpans(b.Spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.Divider:
		m := l.dp(8)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y+m, x+w, y+m+l.dp(1)), fill: p.Divider, target: -1})
		return 2*m + l.dp(1)

	case richtext.Context:
		return l.context(b, x, y, w)

	case richtext.Section:
		return l.section(b, x, y, w, edited)

	case richtext.ImageBlock:
		if b.Image == nil {
			return 0
		}
		h := 0
		if b.Image.Title != "" {
			fs := l.bodyStyle()
			fs.size, fs.lineH, fs.col = l.sp(13), l.dp(18), p.Text2
			th, _ := l.plainFlow(b.Image.Title, x, y, w, &fs, -1)
			h += th + l.dp(4)
		}
		tg := l.d.addTarget(target{kind: tImage, img: b.Image})
		return h + l.image(b.Image, x, y+h, min(w, l.dp(360)), l.dp(360), l.dp(4), tg)

	case richtext.Actions:
		return l.buttons(b.Buttons, x, y, w)

	case richtext.Table:
		return l.table(b, x, y, w)

	case richtext.AttachmentBlock:
		if b.Att == nil {
			return 0
		}
		return l.attachment(b, x, y, w)

	case richtext.FileBlock:
		if b.File == nil {
			return 0
		}
		return l.file(b.File, x, y, w)
	}
	// Unknown: lay out whatever text it has.
	if len(b.Spans) > 0 {
		fs := l.bodyStyle()
		l.addSpans(b.Spans, &fs)
		h, _ := l.flow(x, y, w, &fs)
		return h
	}
	return 0
}

// ---------- lists ----------

func toAlpha(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('a'+n%26)) + s
		n /= 26
	}
	return s
}

func toRoman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var sb strings.Builder
	for i, v := range vals {
		for n >= v {
			sb.WriteString(syms[i])
			n -= v
		}
	}
	return sb.String()
}

func (l *lay) list(b *richtext.Block, x, y, w int, edited bool) int {
	p := &l.st.Palette
	y0 := y
	indentW := l.dp(28)
	cx := x + indentW*(b.Indent+1)
	if b.Border {
		cx += l.dp(8)
	}
	fs := l.bodyStyle()
	for i, it := range b.Items {
		if i > 0 {
			l.plainAdd("\n")
		}
		l.addSpans(it, &fs)
		if edited && i == len(b.Items)-1 {
			l.addEdited()
		}
		lineStart := len(l.d.lines)
		h, _ := l.flow(cx, y, x+w-cx, &fs)
		if h == 0 {
			h = fs.lineH
		}
		// Marker aligned with the first line's baseline.
		baseline := y + fs.lineH*3/4
		if lineStart < len(l.d.lines) {
			ln := l.d.lines[lineStart]
			for fi := ln.f0; fi < ln.f1; fi++ {
				if l.d.frags[fi].kind == fText {
					baseline = l.d.frags[fi].y
					break
				}
			}
		}
		if b.Ordered {
			n := b.Offset + i + 1
			var label string
			switch b.Indent % 3 {
			case 1:
				label = toAlpha(n)
			case 2:
				label = toRoman(n)
			default:
				label = strconv.Itoa(n)
			}
			label += "."
			k := l.faceFor(fs.face, fs.size, 0)
			wd := l.st.word(k, label)
			gapW := l.dpf(8)
			l.d.frags = append(l.d.frags, frag{kind: fText, x: float32(cx) - gapW - wd.adv, y: baseline, w: wd.adv, size: fs.size, sh: wd, col: fs.col, target: -1, top: y, bot: y + fs.lineH, line: -1, p0: -1, p1: -1})
		} else {
			// disc, circle, square by level (Slack cycles every 3).
			r := l.dpf(2.75)
			cyf := float32(baseline) - float32(fs.size)*0.32
			cxf := float32(cx) - l.dpf(17)
			rect := image.Rect(int(cxf-r+0.5), int(cyf-r+0.5), int(cxf+r+0.5), int(cyf+r+0.5))
			kind := bCircle
			switch b.Indent % 3 {
			case 1:
				kind = bRing
			case 2:
				kind = bSquare
			}
			l.d.boxes = append(l.d.boxes, box{kind: kind, r: rect, fill: fs.col, bw: l.dp(1), target: -1})
		}
		y += h
	}
	if b.Border {
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y0, x+l.dp(4), y), fill: p.QuoteBar, target: -1})
	}
	return y - y0
}

// ---------- block kit ----------

func (l *lay) context(b *richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col, fs.emoji = l.sp(13), l.dp(18), p.Text2, l.dp(16)
	cx := x
	if b.Image != nil {
		s := l.dp(20)
		l.image(b.Image, cx, y-l.dp(1), s, s, l.dp(2), -1)
		cx += s + l.dp(4)
	}
	for i := range b.Children {
		if c := &b.Children[i]; c.Kind == richtext.ImageBlock && c.Image != nil {
			s := l.dp(20)
			l.image(c.Image, cx, y-l.dp(1), s, s, l.dp(2), -1)
			cx += s + l.dp(4)
		}
	}
	l.addSpans(b.Spans, &fs)
	h, _ := l.flow(cx, y, x+w-cx, &fs)
	if b.Image != nil && h < l.dp(18) {
		h = l.dp(18)
	}
	return h
}

func (l *lay) section(b *richtext.Block, x, y, w int, edited bool) int {
	y0 := y
	tw := w
	accW := 0
	if b.Image != nil {
		accW = l.dp(88)
		tw = w - accW - l.dp(12)
	} else if len(b.Buttons) > 0 {
		accW = l.buttonWidth(b.Buttons[0])
		tw = w - accW - l.dp(12)
	}
	h := 0
	if len(b.Children) > 0 {
		h = l.blocks(b.Children, x, y, tw, false)
	} else if len(b.Spans) > 0 {
		fs := l.bodyStyle()
		l.addSpans(b.Spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ = l.flow(x, y, tw, &fs)
	}
	if len(b.Fields) > 0 {
		if h > 0 {
			h += l.dp(4)
		}
		h += l.fields(b.Fields, nil, x, y+h, tw)
	}
	if b.Image != nil {
		tg := l.d.addTarget(target{kind: tImage, img: b.Image})
		ih := l.image(b.Image, x+w-accW, y, accW, accW, l.dp(8), tg)
		_ = ih
		if accW > h {
			h = accW
		}
	} else if len(b.Buttons) > 0 {
		bh := l.button(b.Buttons[0], x+w-accW, y)
		if bh > h {
			h = bh
		}
	}
	return y + h - y0
}

// fields lays out Block Kit / attachment fields in two columns. short (may
// be nil) marks fields that share a row; nil means all are short.
func (l *lay) fields(fields [][]richtext.Span, short []bool, x, y, w int) int {
	colW := (w - l.dp(12)) / 2
	fy := y
	rowH := 0
	col := 0
	for i, f := range fields {
		isShort := short == nil || (i < len(short) && short[i])
		if col == 1 && !isShort {
			fy += rowH + l.dp(4)
			rowH, col = 0, 0
		}
		fx := x + col*(colW+l.dp(12))
		fw := colW
		if !isShort {
			fw = w
		}
		fs := l.bodyStyle()
		l.plainAdd("\n")
		l.addSpans(f, &fs)
		fh, _ := l.flow(fx, fy, fw, &fs)
		if fh > rowH {
			rowH = fh
		}
		if isShort && col == 0 {
			col = 1
		} else {
			fy += rowH + l.dp(4)
			rowH, col = 0, 0
		}
	}
	if col == 1 {
		fy += rowH + l.dp(4)
	}
	return fy - l.dp(4) - y
}

func (l *lay) buttonWidth(bt richtext.Button) int {
	k := faceKey{f: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: fixedPx(l.sp(13))}
	return max(l.dp(56), int(l.measure(bt.Text, k)+0.999)+2*l.dp(8))
}

// button draws one Block Kit button with its top-left at (x, y).
func (l *lay) button(bt richtext.Button, x, y int) int {
	p := &l.st.Palette
	bw := l.buttonWidth(bt)
	bh := l.dp(28)
	b := &bt
	tg := l.d.addTarget(target{kind: tButton, btn: b, url: bt.URL})
	r := image.Rect(x, y, x+bw, y+bh)
	col := p.Text
	switch bt.Style {
	case "primary":
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: p.Primary, target: tg})
		col = p.PrimaryText
	case "danger":
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: p.Danger, target: tg})
		col = p.DangerText
	default:
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(4), fill: p.Panel, stroke: p.ButtonBorder, bw: l.dp(1), target: tg, hover: p.ButtonHoverBg})
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: bh, col: col, emoji: l.dp(16), maxLines: 1, noSelect: true}
	tw := int(l.measure(bt.Text, l.faceFor(fs.face, fs.size, 0)) + 0.999)
	l.plainFlow(bt.Text, x+(bw-tw)/2, y-l.dp(1), tw+l.dp(2), &fs, -1)
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return bh
}

func (l *lay) buttons(bs []richtext.Button, x, y, w int) int {
	cx, cy := x, y
	rowH := 0
	for _, bt := range bs {
		bw := l.buttonWidth(bt)
		if cx > x && cx+bw > x+w {
			cx = x
			cy += rowH + l.dp(8)
			rowH = 0
		}
		h := l.button(bt, cx, cy)
		if h > rowH {
			rowH = h
		}
		cx += bw + l.dp(8)
	}
	return cy + rowH - y
}

// image places an image scaled to fit (maxW, maxH) preserving its aspect
// ratio; returns its height.
func (l *lay) image(img *richtext.Image, x, y, maxW, maxH, radius int, tg int16) int {
	w, h := fitSize(img.W, img.H, maxW, maxH, l)
	r := image.Rect(x, y, x+w, y+h)
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, radii: [4]int{radius, radius, radius, radius}, target: tg, px: max(w, h)})
	if tg >= 0 {
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	}
	return h
}

func fitSize(iw, ih, maxW, maxH int, l *lay) (int, int) {
	if iw <= 0 || ih <= 0 {
		return min(maxW, l.dp(360)), min(maxH, l.dp(240))
	}
	// Image sizes are CSS px; scale to device px.
	w := float32(iw) * l.scale
	h := float32(ih) * l.scale
	if w > float32(maxW) {
		h = h * float32(maxW) / w
		w = float32(maxW)
	}
	if h > float32(maxH) {
		w = w * float32(maxH) / h
		h = float32(maxH)
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return int(w + 0.5), int(h + 0.5)
}

// table lays out a Block Kit table: a rounded bordered grid, header row in
// bold, content-sized columns shrunk to fit, per-column alignment.
func (l *lay) table(b *richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	ncol := 0
	for _, r := range b.Rows {
		ncol = max(ncol, len(r))
	}
	if ncol == 0 || len(b.Rows) == 0 {
		return 0
	}
	padX, padY := l.dp(12), l.dp(8)
	bw := l.dp(1)
	// natural (single-line) column widths
	nat := make([]int, ncol)
	k := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(l.st.TextSize), 0)
	kb := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(l.st.TextSize), richtext.Bold)
	for ri, r := range b.Rows {
		for ci, cell := range r {
			kk := k
			if ri == 0 {
				kk = kb
			}
			cw := 0
			for _, line := range strings.Split(richtext.SpansText(cell), "\n") {
				cw = max(cw, int(l.measure(line, kk)+0.999))
			}
			nat[ci] = max(nat[ci], min(cw, l.dp(400))+2*padX)
		}
	}
	total := 0
	for _, c := range nat {
		total += c
	}
	avail := w - bw*(ncol+1)
	cols := make([]int, ncol)
	for i := range cols {
		cols[i] = nat[i]
		if total > avail && total > 0 {
			cols[i] = max(l.dp(48), nat[i]*avail/total)
		}
	}
	tw := bw * (ncol + 1)
	for _, c := range cols {
		tw += c
	}
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: bw, radius: l.dp(8), target: -1})
	cy := y + bw
	for ri, r := range b.Rows {
		if ri > 0 {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+bw, cy, x+tw-bw, cy+bw), fill: p.Border, target: -1})
			cy += bw
		}
		rowH := 0
		cx := x + bw
		for ci := 0; ci < ncol; ci++ {
			if ci > 0 {
				cx += bw
			}
			cw := cols[ci]
			if ci < len(r) && len(r[ci]) > 0 {
				fs := l.bodyStyle()
				if ri == 0 {
					fs.face.Weight = font.Bold
				}
				if ci > 0 {
					l.plainAdd("\t")
				}
				f0, b0, h0 := len(l.d.frags), len(l.d.boxes), len(l.d.hits)
				l.addSpans(r[ci], &fs)
				h, wd := l.flow(cx+padX, cy+padY, cw-2*padX, &fs)
				align := ""
				if ci < len(b.ColAlign) {
					align = b.ColAlign[ci]
				}
				if dx := cw - 2*padX - wd; dx > 0 && (align == "right" || align == "center") {
					if align == "center" {
						dx /= 2
					}
					l.shift(f0, b0, h0, dx)
				}
				rowH = max(rowH, h)
			}
			cx += cw
		}
		rowH = max(rowH, l.dp(l.st.LineHeight)) + 2*padY
		// column separators for this row
		sx := x + bw
		for ci := 0; ci < ncol-1; ci++ {
			sx += cols[ci]
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(sx, cy, sx+bw, cy+rowH), fill: p.Border, target: -1})
			sx += bw
		}
		cy += rowH
		l.plainAdd("\n")
	}
	cy += bw
	l.d.boxes[bi].r = image.Rect(x, y, x+tw, cy)
	return cy - y
}

// shift moves the frags, boxes and hit areas laid out since (f0, b0, h0)
// right by dx (aligned table cells).
func (l *lay) shift(f0, b0, h0, dx int) {
	for i := f0; i < len(l.d.frags); i++ {
		l.d.frags[i].x += float32(dx)
	}
	for i := b0; i < len(l.d.boxes); i++ {
		l.d.boxes[i].r = l.d.boxes[i].r.Add(image.Pt(dx, 0))
	}
	for i := h0; i < len(l.d.hits); i++ {
		l.d.hits[i].r = l.d.hits[i].r.Add(image.Pt(dx, 0))
	}
}
