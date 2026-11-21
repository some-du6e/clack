package richview

import (
	"image"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/richtext"
)

// card lays out a card block: Card (boot-shared 7Xkb) on Slack Kit's
// entity card (aJif expanded, full width and height, not interactive),
// from client-boot-styles:
//
//   - .card: width 100% between 180 and 340px, margin 8px 0 (the block's,
//     blocks.go); the entity's 1px otl-ter border, radius 12, base-pry;
//   - .content: a column, 12px padding, 12px between its parts;
//   - .heroImage: the content's width at 4:3 (aspect-ratio 1.33333),
//     cover, radius 4;
//   - .headerContainer: items centred, 12px apart: the 36px icon box
//     (.iconAtomContainer: the slack_icon glyph at 24px; else
//     .iconImageContainer: the image contained, radius 8), then
//     StackedLabels (expanded, no label gap): the title body bold, the
//     subtitle caption in content-ter, one line each;
//   - .bodyContainer: 15px/1.46668;
//   - .subtextContainer: 13px/1.38463, rgba(sk_foreground_max, .7);
//   - .actionsContainer: the right group at the right edge, the left
//     group (.actionsLeft, flex 1) at the left, buttons 8px apart.
//
// Left out: the hero image's load-error picture (Ec61 on base-pry) and
// the buttons' label truncation (truncate: true).
func (l *lay) card(cd *richtext.Card, x, y, w int) int {
	if cd == nil {
		return 0
	}
	p := &l.st.Palette
	cw := max(l.dp(180), min(w, l.dp(340)))
	bw := max(1, l.dp(1))
	pad, gap := l.dp(12), l.dp(12)
	box0 := len(l.d.boxes)
	ix, iw := x+bw+pad, cw-2*(bw+pad)
	yy := y + bw + pad
	parts := 0
	next := func() {
		if parts > 0 {
			yy += gap
		}
		parts++
	}
	if cd.Hero != "" {
		next()
		hh := int(float32(iw)/1.33333 + 0.5)
		rad := l.dp(4)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, yy, ix+iw, yy+hh), url: cd.Hero, radii: [4]int{rad, rad, rad, rad}, target: -1, px: iw})
		yy += hh
	}
	hasIcon := cd.SlackIcon != "" || cd.Icon != ""
	if len(cd.Title) > 0 || len(cd.Subtitle) > 0 || hasIcon {
		next()
		ic := l.dp(36)
		// StackedLabels' Panda text styles: body 15px/1.5, caption 13px/1.25
		ts := l.bodyStyle()
		ts.face.Weight = font.Bold
		ts.lineH = int(l.dpf(22.5) + 0.5)
		ts.maxLines, ts.ellipsis = 1, true
		ss := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: int(l.dpf(16.25) + 0.5), col: l.tok("content-ter"),
			emoji: l.dp(16), maxLines: 1, ellipsis: true}
		labelsH := 0
		if len(cd.Title) > 0 {
			labelsH += ts.lineH
		}
		if len(cd.Subtitle) > 0 {
			labelsH += ss.lineH
		}
		rowH := labelsH
		lx := ix
		if hasIcon {
			rowH = max(rowH, ic)
			iy := yy + (rowH-ic)/2
			if cd.SlackIcon != "" {
				// BlockKitIcon: deriveIconName's SvgIcon, nothing for a name it lacks
				if name := blockKitIcon(cd.SlackIcon); name != "" {
					gs := l.dp(24)
					gx, gy := ix+(ic-gs)/2, iy+(ic-gs)/2
					l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(gx, gy, gx+gs, gy+gs), url: iconURL + name, target: -1, px: gs, tint: p.Text})
				}
			} else {
				// .iconImageContainer: ThumbnailContainer's surface.secondary
				// box (radius 8, overflow hidden) behind the image, which is
				// object-fit contain at max 100%: never scaled up
				rad := l.dp(8)
				ir := image.Rect(ix, iy, ix+ic, iy+ic)
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: ir, radius: rad, fill: l.tok("surf-sec"), target: -1})
				l.d.imgs = append(l.d.imgs, imgPrim{r: ir, url: cd.Icon, radii: [4]int{rad, rad, rad, rad},
					fit: true, bare: true, noUp: l.scale, target: -1, px: ic})
			}
			lx += ic + l.dp(12)
		}
		lw := max(ix+iw-lx, l.dp(20))
		ly := yy + (rowH-labelsH)/2
		if len(cd.Title) > 0 {
			l.plainAdd("\n")
			l.textFlow(cd.Title, lx, ly, lw, &ts)
			ly += ts.lineH
		}
		if len(cd.Subtitle) > 0 {
			l.plainAdd("\n")
			l.textFlow(cd.Subtitle, lx, ly, lw, &ss)
		}
		yy += rowH
	}
	if len(cd.Body) > 0 {
		next()
		l.plainAdd("\n")
		yy += l.nestedText(cd.Body, ix, yy, iw)
	}
	if len(cd.Subtext) > 0 {
		next()
		fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dp(18),
			col: Over(l.skRGBA("sk_foreground_max", 0.7), p.Panel), emoji: l.dp(16)}
		l.plainAdd("\n")
		h, _ := l.textFlow(cd.Subtext, ix, yy, iw, &fs)
		yy += h
	}
	if len(cd.Left) > 0 || len(cd.Right) > 0 {
		next()
		yy += l.cardActions(cd, ix, yy, iw)
	}
	h := yy + pad + bw - y
	frame := box{kind: bBorder, r: image.Rect(x, y, x+cw, y+h), radius: l.dp(12), fill: p.Panel, stroke: l.tok("otl-ter"), bw: bw, target: -1}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
	l.d.boxes[box0] = frame
	return h
}

// BlockKitIcon is Slack's deriveIconName (Qw/u): a Block Kit icon name's
// SvgIcon, "" for one it doesn't know (blockkit installs its table).
var BlockKitIcon func(name string) string

func blockKitIcon(name string) string {
	if BlockKitIcon == nil {
		return ""
	}
	return BlockKitIcon(name)
}

// cardActions lays out a card's two button groups on one row: the right
// group against the right edge, the left one at the left (8px apart).
func (l *lay) cardActions(cd *richtext.Card, x, y, w int) int {
	gap := l.dp(8)
	if el := l.opts.Elements; el != nil {
		var lsz, rsz image.Point
		if len(cd.Right) > 0 {
			rsz, _ = el.ElementsSize(cd.Right, w, false, l.metric())
			l.addSlot(cd.Right, image.Rect(x+w-rsz.X, y, x+w, y+rsz.Y), false)
		}
		if len(cd.Left) > 0 {
			lsz, _ = el.ElementsSize(cd.Left, max(w-rsz.X-gap, 0), false, l.metric())
			l.addSlot(cd.Left, image.Rect(x, y, x+lsz.X, y+lsz.Y), false)
		}
		return max(lsz.Y, rsz.Y)
	}
	rowH, cx := 0, x
	for _, bt := range cd.Left {
		rowH = max(rowH, l.button(bt, cx, y))
		cx += l.buttonWidth(bt) + gap
	}
	rw := 0
	for i, bt := range cd.Right {
		if i > 0 {
			rw += gap
		}
		rw += l.buttonWidth(bt)
	}
	cx = x + w - rw
	for _, bt := range cd.Right {
		rowH = max(rowH, l.button(bt, cx, y))
		cx += l.buttonWidth(bt) + gap
	}
	return rowH
}
