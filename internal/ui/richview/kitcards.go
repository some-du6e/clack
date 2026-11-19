package richview

import (
	"image"
	"image/color"

	"github.com/some-du6e/clack/internal/richtext"
)

// Block Kit's boxed blocks (richtext kitcards.go), from Slack's web
// client (build 846030e1ce97) and its client-boot-styles:
//
//	alert    .alertBlock{margin:8px 0} around Slack Kit's Alert (MlDl),
//	         type boxed: display flex, padding 12px 16px, 15px/1.46668
//	         content-pry, 1px border (transparent; default level's is
//	         otl-sec in dark), radius 8, the level's surface; the 20px
//	         icon 12px left of the message, 1px down, in the level's
//	         colour (default slack-logo, info info, success check-circle,
//	         warning and error warning)
//	callout  .calloutBlock: 1px border (otl-ter, otl-sec in dark),
//	         radius 12, padding 12, the colour's plt-*-0 surface; its
//	         child blocks render bare inside (renderBlocks: no block
//	         wrappers, so their margins collapse), a 20px empty box when
//	         it has none
//	loading  InfiniteSpinner jumbo (52px), blue, no margin

// alertTone is an alert level's surface and icon colour and its icon.
func (l *lay) alertTone(level string) (bg, ink color.NRGBA, icon string) {
	dark := l.st.Palette.Dark
	pick := func(light, dk color.NRGBA) color.NRGBA {
		if dark {
			return dk
		}
		return light
	}
	switch level {
	case "info":
		return pick(rgba(0x36, 0xc5, 0xf0, 0x21/255.0), rgba(0x03, 0x66, 0x96, 0x40/255.0)), pick(rgb(0x12, 0x64, 0xa3), rgb(0x2b, 0xa5, 0xce)), "info"
	case "success":
		return pick(rgba(0x4c, 0xc8, 0x94, 0x40/255.0), rgba(0x10, 0x6f, 0x4d, 0x54/255.0)), pick(rgb(0x00, 0x7a, 0x5a), rgb(0x3d, 0xaa, 0x7c)), "check-circle"
	case "warning":
		return pick(rgba(0xff, 0xc6, 0x00, 0x2e/255.0), rgba(0xa0, 0x73, 0x00, 0x2e/255.0)), pick(rgb(0x6b, 0x50, 0x00), rgb(0xde, 0xa7, 0x00)), "warning"
	case "error":
		return pick(rgba(0xff, 0x81, 0xaa, 0x21/255.0), rgba(0xb4, 0x15, 0x41, 0x40/255.0)), pick(rgb(0xc0, 0x13, 0x43), rgb(0xde, 0x67, 0x8a)), "warning"
	}
	return pick(rgb(0xea, 0xea, 0xea), rgb(0x21, 0x24, 0x28)), pick(rgb(0x5e, 0x5d, 0x60), rgb(0x9a, 0x9b, 0x9e)), "slack-logo"
}

// alert lays out an alert block's box at (x, y) (its 8px margins are the
// block's, blocks.go).
func (l *lay) alert(b *richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	bg, ink, icon := l.alertTone(b.Tone)
	bg = Over(bg, p.Panel)
	bw := max(1, l.dp(1))
	padX, padY := l.dp(16), l.dp(12)
	is := l.dp(20)
	box0 := len(l.d.boxes)
	ix, iy := x+bw+padX, y+bw+padY+l.dp(1) // translateY(1px)
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, iy, ix+is, iy+is), url: iconURL + icon, target: -1, px: is, tint: ink})
	tx := ix + is + l.dp(12)
	tw := max(x+w-bw-padX-tx, l.dp(40))
	th := 0
	if len(b.Children) > 0 {
		th = l.nestedText(b.Children, tx, y+bw+padY, tw)
	}
	h := bw + padY + max(th, l.dp(22)) + padY + bw
	stroke := color.NRGBA{}
	if p.Dark && b.Tone == "default" {
		stroke = Over(rgba(0x79, 0x7c, 0x81, 0x80/255.0), bg)
	}
	bx := box{kind: bFill, r: image.Rect(x, y, x+w, y+h), radius: l.dp(8), fill: bg, target: -1}
	if stroke.A != 0 {
		bx.kind, bx.stroke, bx.bw = bBorder, stroke, bw
	}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
	l.d.boxes[box0] = bx
	return h
}

// nestedText lays out a text slot's blocks (an alert's mrkdwn or
// plain_text) at 15px/22 one under the other, and returns the height.
func (l *lay) nestedText(bs []richtext.Block, x, y, w int) int {
	y0 := y
	for i := range bs {
		if i > 0 {
			y += l.gap(bs[i-1].Kind, bs[i].Kind)
		}
		y += l.block(&bs[i], x, y, w, false)
	}
	return y - y0
}

// calloutTone is a callout colour's plt-*-0 surface (light, dark).
func (l *lay) calloutTone(tone string) color.NRGBA {
	type pair struct{ light, dark color.NRGBA }
	t := map[string]pair{
		"indigo": {rgb(240, 243, 252), rgb(4, 11, 61)},
		"blue":   {rgb(227, 248, 255), rgb(0, 26, 45)},
		"jade":   {rgb(227, 255, 243), rgb(5, 36, 27)},
		"pink":   {rgb(255, 232, 239), rgb(48, 0, 5)},
		"gray":   {rgb(234, 234, 234), rgb(65, 69, 73)}, // light-dark(gray-5, gray-30)
		"purple": {rgb(246, 228, 255), rgb(48, 12, 49)},
		"orange": {rgb(252, 242, 217), rgb(54, 30, 0)},
		"brown":  {rgb(244, 237, 228), rgb(42, 23, 7)},
	}
	c, ok := t[tone]
	if !ok {
		c = pair{rgb(244, 255, 219), rgb(21, 34, 3)} // green: grass-0
	}
	if l.st.Palette.Dark {
		return c.dark
	}
	return c.light
}

// callout lays out a callout block at (x, y).
func (l *lay) callout(b *richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	bw := max(1, l.dp(1))
	pad := l.dp(12)
	box0 := len(l.d.boxes)
	inner := l.dp(20) // .calloutBlockEmpty
	if len(b.Children) > 0 {
		inner = l.nestedBlocks(b.Children, x+bw+pad, y+bw+pad, w-2*(bw+pad))
	}
	h := bw + pad + inner + pad + bw
	stroke := Over(rgba(0x5e, 0x5d, 0x60, 0x21/255.0), p.Panel) // otl-ter
	if p.Dark {
		stroke = Over(rgba(0x79, 0x7c, 0x81, 0x80/255.0), p.Panel) // otl-sec
	}
	bx := box{kind: bBorder, r: image.Rect(x, y, x+w, y+h), radius: l.dp(12), fill: l.calloutTone(b.Tone), stroke: stroke, bw: bw, target: -1}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
	l.d.boxes[box0] = bx
	return h
}

// nestedBlocks lays out child blocks the way renderBlocks draws them:
// no block wrappers, so the first block's top margin and the last one's
// bottom margin stay inside the parent's padding and two neighbours'
// margins collapse into the larger.
func (l *lay) nestedBlocks(bs []richtext.Block, x, y, w int) int {
	y0 := y
	prevBottom := 0
	for i := range bs {
		b := &bs[i]
		top, _, bottom := l.margins(b)
		if i == 0 {
			y += top
		} else {
			y += max(prevBottom, top)
		}
		y += l.block(b, x, y, w, false)
		prevBottom = bottom
	}
	return y + prevBottom - y0
}

// loading lays out a loading block: the 52px blue spinner.
func (l *lay) loading(x, y int) int {
	s := l.dp(52)
	l.d.spinners = append(l.d.spinners, spinPrim{r: image.Rect(x, y, x+s, y+s)})
	return s
}
