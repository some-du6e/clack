package richview

import (
	"image"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/richtext"
)

// canvasFile is a canvas shared as a message's file (richtext
// canvasfile.go): Slack's MessageFile with p-message_file--quip (at most
// 594 wide) and p-message_file--show_preview, a pillow card (12px
// padding, radius 12) holding
//
//   - MessageFileHeader: the canvas tile (QuipIcon with the IA4 look:
//     canvas-content-filled 20px in a 36px, radius 8 tile, lagoon-0 on
//     lagoon-40; lagoon-100 on lagoon-60 in a dark theme's
//     p-file_thumbnail__container--dark-theme), 12px on the title
//     (CanvasFileName: title_blocks with their emoji) in 16px Bold and
//     "Canvas" under it in 13px;
//   - MessageFilePreview's FileQuip (p-file_quip p-file_quip--unfurl): a
//     1px foreground_low_solid rule 12px under the header, edge to edge
//     (margin 12px -12px -12px), padding 12px 12px 0 on the primary
//     background, then CanvasBlockKitPreview's p-file_quip__snippet,
//     collapsed: max-height 240px. FileQuip passes no allowExpand, so
//     there is neither the fade (mask 70% to 100%) nor Show more: the
//     preview stops at 240px. Its blocks are 12px apart
//     (:not(.p-section_block,:last-child) margin-bottom 12px); a header
//     is 18px Black at 1.33334 with no top margin. The snippet takes no
//     pointer events (:not(.allow-pointer-events)): a click anywhere opens
//     the canvas.
//
// The card is 1+12+36+12 (its border, padding, the header, the padding),
// the rule and the padding, the snippet, then its bottom border: 315px
// with a full snippet, as Slack's. The pillow's overflow: hidden clips at
// its padding box, which the snippet's bottom meets (FileQuip's margin
// -12px), so a line the snippet's cut crosses stops at the border's inner
// edge and the border stays whole.
func (l *lay) canvasFile(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(594))
	bw := l.dp(1)
	pad := l.dp(12)
	in := bw + pad // the border, the padding
	ic := l.dp(36)
	tg := l.d.addTarget(target{kind: tFile, file: f})
	bi := l.cardFrame(x, y, cw, 0, tg)
	l.canvasTile(x+in, y+in, ic)
	tx := x + in + ic + pad
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(16), l.dp(20)
	fs.emoji = l.dp(18)
	fs.maxLines, fs.ellipsis = 1, true
	if len(f.TitleSpans) > 0 {
		l.addSpansTarget(f.TitleSpans, &fs, tg)
		l.flow(tx, y+in-l.dp(1), x+cw-tx-in, &fs)
		l.plainAdd("\n")
	} else {
		l.plainFlow(fileName(f), tx, y+in-l.dp(1), x+cw-tx-in, &fs, tg)
	}
	ms := l.small(p.Text2)
	ms.maxLines, ms.ellipsis, ms.noSelect = 1, true, true
	l.plainFlow("Canvas", tx, y+in+l.dp(20), x+cw-tx-in, &ms, -1)
	h := in + ic + pad
	if len(f.CanvasPreview) > 0 {
		h += l.canvasPreview(f.CanvasPreview, x, y+h, cw)
	}
	h += bw // the bottom border, under the snippet's cut
	l.d.boxes[bi].r = image.Rect(x, y, x+cw, y+h)
	if tg >= 0 {
		l.d.hits = append(l.d.hits, hit{r: image.Rect(x, y, x+cw, y+h), t: tg})
	}
	l.pillowBar(f, image.Rect(x, y, x+cw, y+h)) // FileActions (file_actions.go)
	return h
}

// canvasTile is QuipIcon's IA4 tile.
func (l *lay) canvasTile(x, y, s int) {
	bg, fg := rgb(28, 182, 235), rgb(227, 248, 255) // lagoon-40, lagoon-0 (light)
	if lum(l.st.Palette.Panel) < 0.5 {
		bg, fg = rgb(43, 165, 206), rgb(216, 238, 246) // lagoon-60, lagoon-100 (dark)
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y, x+s, y+s), radius: l.dp(8), fill: bg, target: -1})
	is := l.dp(20)
	g := image.Rect(x+(s-is)/2, y+(s-is)/2, x+(s-is)/2+is, y+(s-is)/2+is)
	l.d.imgs = append(l.d.imgs, imgPrim{r: g, url: iconURL + "canvas-content-filled", target: -1, px: is, tint: fg})
}

// canvasPreview lays out FileQuip's preview under a canvas card's header
// at y and returns its height (the rule, the padding and the snippet).
func (l *lay) canvasPreview(bs []richtext.Block, x, y, cw int) int {
	p := &l.st.Palette
	line := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.13), p.Panel) // foreground_low_solid
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+l.dp(1), y, x+cw-l.dp(1), y+l.dp(1)), fill: line, target: -1})
	pad := l.dp(12)
	top := y + l.dp(1) + pad
	cut := top + l.dp(240) // p-file_quip__snippet.collapsed
	f0, b0, i0, h0, n0, c0 := len(l.d.frags), len(l.d.boxes), len(l.d.imgs), len(l.d.hits), len(l.d.lines), len(l.d.codes)
	cy := top
	for i := range bs {
		if cy >= cut {
			break
		}
		if i > 0 {
			cy += l.dp(12)
		}
		b := &bs[i]
		if b.Kind == richtext.Header {
			fs := l.bodyStyle()
			fs.face.Weight = font.Black
			fs.size, fs.lineH = l.sp(18), l.dp(24)
			fs.emoji = l.dp(22)
			l.addSpans(b.Spans, &fs)
			h, _ := l.flow(x+pad, cy, cw-2*pad, &fs)
			l.plainAdd("\n")
			cy += h
			continue
		}
		cy += l.blocks([]richtext.Block{*b}, x+pad, cy, cw-2*pad, false)
	}
	l.trimBelow(cut, f0, b0, i0, h0, n0, c0)
	return min(cy, cut) - y
}

// trimBelow drops what the snippet's max-height hides: the frags, lines,
// boxes and images laid out since f0, b0, i0, n0 that start at or under
// cut. A line the cut crosses keeps its frags and images, painted above
// the cut only (fragCuts, imgCuts), its frags' bot clamped to it. The
// snippet takes no pointer events: every hit since h0 and every code run
// since c0 (the copy button, the code menu) go, and its frags are left
// without a target.
func (l *lay) trimBelow(cut, f0, b0, i0, h0, n0, c0 int) {
	d := l.d
	k := len(d.frags)
	for i := f0; i < len(d.frags); i++ {
		if d.frags[i].top >= cut {
			k = i
			break
		}
	}
	d.frags = d.frags[:k]
	for i := f0; i < len(d.frags); i++ {
		d.frags[i].target = -1
		d.frags[i].bot = min(d.frags[i].bot, cut) // the card ends at the cut
	}
	if k > f0 {
		d.fragCuts = append(d.fragCuts, cutRange{from: int32(f0), to: int32(k), bot: cut})
	}
	ls := d.lines[:n0]
	for _, ln := range d.lines[n0:] {
		if ln.f0 >= k {
			continue
		}
		ln.f1 = min(ln.f1, k)
		ls = append(ls, ln)
	}
	d.lines = ls
	bx := d.boxes[:b0]
	for _, b := range d.boxes[b0:] {
		if b.r.Min.Y >= cut {
			continue
		}
		if b.r.Max.Y > cut {
			b.r.Max.Y = cut
		}
		b.target = -1
		bx = append(bx, b)
	}
	d.boxes = bx
	im := d.imgs[:i0]
	for _, m := range d.imgs[i0:] {
		if m.r.Min.Y < cut {
			m.target = -1
			im = append(im, m)
		}
	}
	if len(im) > i0 {
		d.imgCuts = append(d.imgCuts, cutRange{from: int32(i0), to: int32(len(im)), bot: cut})
	}
	d.imgs = im
	d.hits = d.hits[:h0]
	d.codes = d.codes[:c0]
}
