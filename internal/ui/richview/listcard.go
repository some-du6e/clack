package richview

import (
	"image"
	"strings"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/richtext"
)

// A list shared in a message. Slack's MessageFileListUnfurl
// (modern.vendor K8uy) draws the compact ListEntity (qIR+, iEuI) in an
// EntityContainer (11Ex): 62px tall, the width of the message up to 328px
// (getListCustomMaxWidth: a single file without a snapshot; no limit in
// a gallery), radius 12 and a 1px otl-ter border, otl-sec on hover. Inside,
// 12px of padding and a 12px gap: the 36px list thumbnail
// (listThumbnail__Kn5Mo: honeycomb-40, radius 8, the lists-filled glyph
// in honeycomb-0 at 20px), then the stacked labels of
// getListUnfurlLabelProps: the title in body bold, "List" in the tertiary
// caption, 4px apart, from the top. A click opens the list (the file's
// target: Clack's list view).

// isListFile reports a Slack list (filetype list, pretty_type List).
func isListFile(f *richtext.File) bool {
	return f != nil && (strings.EqualFold(f.Type, "list") || strings.EqualFold(f.PrettyType, "list"))
}

// listFile lays out the list card at x, y in at most w (gallery: no
// 328px limit) and returns its height.
func (l *lay) listFile(f *richtext.File, x, y, w int, gallery bool) int {
	p := &l.st.Palette
	cw := w
	if !gallery {
		cw = min(w, l.dp(328))
	}
	h := l.dp(62)
	tg := l.d.addTarget(target{kind: tFile, file: f})
	l.cardFrame(x, y, cw, h, tg)
	in := l.dp(1) + l.dp(12) // the border, the padding
	ic := l.dp(36)
	tr := image.Rect(x+in, y+in, x+in+ic, y+in+ic)
	bg, fg := rgb(225, 155, 6), rgb(252, 242, 217) // --dt_color-plt-honeycomb-40 / -0
	if lum(p.Panel) < 0.5 {
		bg, fg = rgb(170, 108, 0), rgb(54, 30, 0)
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: tr, radius: l.dp(8), fill: bg, target: -1})
	s := l.dp(20)
	g := tr.Min.Add(image.Pt((ic-s)/2, (ic-s)/2))
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(g.X, g.Y, g.X+s, g.Y+s), url: iconURL + "lists-filled", target: -1, px: s, tint: fg})
	tx := tr.Max.X + l.dp(12)
	tw := max(x+cw-in-tx, 1)
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(15), l.dp(22)
	fs.maxLines, fs.ellipsis = 1, true
	title := f.Title
	if title == "" {
		title = f.Name
	}
	if title == "" {
		title = "Untitled list"
	}
	l.plainFlow(title, tx, y+in, tw, &fs, tg)
	ms := l.small(p.Text2)
	ms.maxLines, ms.ellipsis, ms.noSelect = 1, true, true
	l.plainFlow("List", tx, y+in+l.dp(22)+l.dp(4), tw, &ms, -1)
	return h
}
