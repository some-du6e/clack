package richview

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

func fixedPx(px int) fixed.Int26_6 { return fixed.I(px) }

// parseColor parses Slack attachment colors: "#rrggbb", "rrggbb", "good",
// "warning", "danger".
func parseColor(s string, def color.NRGBA) color.NRGBA {
	switch s {
	case "good":
		return rgb(46, 182, 125)
	case "warning":
		return rgb(236, 178, 46)
	case "danger":
		return rgb(224, 30, 90)
	case "":
		return def
	}
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return def
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return def
	}
	return rgb(uint8(v>>16), uint8(v>>8), uint8(v))
}

func ordinal(d int) string {
	if d >= 11 && d <= 13 {
		return "th"
	}
	switch d % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

// slackDate formats a unix time like Slack's attachment footers.
func slackDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	t := time.Unix(ts, 0).Local()
	now := time.Now()
	if t.Year() == now.Year() {
		return t.Format("Jan 2") + ordinal(t.Day())
	}
	return t.Format("Jan 2") + ordinal(t.Day()) + t.Format(", 2006")
}

func (l *lay) small(col color.NRGBA) flowStyle {
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col, fs.emoji = l.sp(13), l.dp(18), col, l.dp(16)
	return fs
}

// ---------- attachments ----------

func (l *lay) attachment(b *richtext.Block, x, y, w int) int {
	a := b.Att
	if a.IsUnfurl && !a.IsShare && len(b.Fields) == 0 && len(b.Buttons) == 0 && len(b.Children) == 0 {
		return l.unfurl(a, x, y, w)
	}
	p := &l.st.Palette
	y0 := y
	if w > l.dp(600) {
		w = l.dp(600)
	}
	if len(a.Pretext) > 0 {
		fs := l.bodyStyle()
		l.addSpans(a.Pretext, &fs)
		h, _ := l.flow(x, y, w, &fs)
		y += h + l.dp(4)
		l.plainAdd("\n")
	}
	bar := l.dp(4)
	bi := len(l.d.boxes)
	barCol := parseColor(a.Color, p.Placeholder)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: barCol, radius: l.dp(8), target: -1})
	cx := x + bar + l.dp(12)
	cw := x + w - cx
	thumbW := 0
	if a.Thumb != nil && a.Image == nil {
		thumbW = l.dp(80)
		cw -= thumbW + l.dp(12)
	}
	cy := y
	row := func(h int) {
		if h > 0 {
			cy += h
		}
	}
	if a.ServiceName != "" && !a.IsShare {
		row(l.iconRow(a.ServiceIcon, a.ServiceName, cx, cy, cw, l.dp(16), font.Black, 15, p.Text, l.d.addTarget(target{kind: tLink, url: a.FromURL, underline: true})))
	}
	if a.AuthorName != "" {
		var tg int16 = -1
		if a.AuthorLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.AuthorLink, underline: true})
		}
		ic := l.dp(16)
		weight := font.Bold
		if a.IsShare {
			ic = l.dp(20)
			weight = font.Black
		}
		name := a.AuthorName
		if a.AuthorSubname != "" {
			name += "  " + a.AuthorSubname
		}
		if l.inAtt && a.AuthorIcon != "" {
			l.authorImg = len(l.d.imgs) + 1 // drawn as ConnectedBaseAvatar (comps.go)
		}
		row(l.iconRow(a.AuthorIcon, name, cx, cy, cw, ic, weight, 15, p.Text, tg))
	}
	if len(a.Title) > 0 {
		fs := l.bodyStyle()
		fs.face.Weight = font.Bold
		fs.col = p.Link
		var tg int16 = -1
		if a.TitleLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.TitleLink, underline: true})
		}
		l.addSpansTarget(a.Title, &fs, tg)
		h, _ := l.flow(cx, cy, cw, &fs)
		row(h)
		l.plainAdd("\n")
	}
	if len(a.Text) > 0 {
		k := a
		text := a.Text
		maxC, maxN := attMaxChars, attMaxNewlines
		if a.IsShare {
			maxC, maxN = shareMaxChars, shareMaxNewlines
		}
		if short, cut := truncateBlocks(text, maxC, maxN); cut {
			if l.expanded[k] {
				text = withExpander(append([]richtext.Block(nil), text...), false)
			} else {
				text = withExpander(short, true)
			}
		}
		l.expandAt = k
		row(l.blocks(text, cx, cy, cw, false))
		l.expandAt = nil
		l.plainAdd("\n")
	}
	if len(b.Fields) > 0 {
		cy += l.dp(4)
		row(l.fields(b.Fields, b.FieldShort, cx, cy, cw))
	}
	if a.Image != nil {
		cy += l.dp(6)
		tg := l.d.addTarget(target{kind: tImage, img: a.Image})
		row(l.image(a.Image, cx, cy, min(cw, l.dp(360)), l.dp(360), l.dp(8), tg))
	}
	if len(b.Children) > 0 {
		cy += l.dp(4)
		row(l.blocks(b.Children, cx, cy, cw, false))
	}
	if len(b.Buttons) > 0 {
		cy += l.dp(8)
		row(l.buttons(b.Buttons, cx, cy, cw))
	}
	if len(a.Footer) > 0 || a.TS > 0 || (a.IsShare && a.ShareChannel != "") {
		cy += l.dp(4)
		fs := l.small(p.Text2)
		fs.size = l.sp(12)
		fx := cx
		if a.FooterIcon != "" {
			s := l.dp(16)
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(fx, cy+1, fx+s, cy+1+s), url: a.FooterIcon, radii: [4]int{2, 2, 2, 2}, target: -1, px: s})
			fx += s + l.dp(6)
		}
		footer := a.Footer
		if a.IsShare && a.ShareChannel != "" && len(footer) == 0 {
			footer = []richtext.Span{{Text: "Posted in "}, {Kind: richtext.Channel, Text: "#" + strings.TrimPrefix(a.ShareChannel, "#")}}
		}
		l.addSpansPlain(footer, &fs)
		if d := slackDate(a.TS); d != "" {
			k := l.faceFor(fs.face, fs.size, 0)
			if len(footer) > 0 {
				l.addText(" | ", &fs, k, fs.col, 0, -1, -1)
			}
			l.addText(d, &fs, k, fs.col, 0, -1, -1)
		}
		h, _ := l.flow(fx, cy, x+w-fx, &fs)
		row(max(h, l.dp(18)))
	}
	if len(a.Text) == 0 && len(a.Title) == 0 && a.AuthorName == "" && a.Image == nil && cy == y {
		// Fields-only or empty attachments still get a little bar.
		cy += l.dp(4)
	}
	h := cy - y
	if thumbW > 0 {
		tg := l.d.addTarget(target{kind: tImage, img: a.Thumb})
		s := thumbW
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(x+w-s, y, x+w, y+s), url: a.Thumb.URL, radii: [4]int{8, 8, 8, 8}, target: tg, px: s})
		l.d.hits = append(l.d.hits, hit{r: image.Rect(x+w-s, y, x+w, y+s), t: tg})
		if s > h {
			h = s
		}
	}
	l.d.boxes[bi].r = image.Rect(x, y, x+bar, y+h)
	return y + h - y0
}

// addSpansTarget lays out spans forcing every text span to one target.
func (l *lay) addSpansTarget(spans []richtext.Span, fs *flowStyle, tg int16) {
	if tg < 0 {
		l.addSpans(spans, fs)
		return
	}
	for _, sp := range spans {
		if sp.Kind != richtext.Text {
			l.addSpans([]richtext.Span{sp}, fs)
			continue
		}
		k := l.faceFor(fs.face, fs.size, sp.Style)
		l.addText(sp.Text, fs, k, fs.col, sp.Style, -1, tg)
	}
}

// addSpansPlain is addSpans where links keep the flow color (footers).
func (l *lay) addSpansPlain(spans []richtext.Span, fs *flowStyle) {
	for _, sp := range spans {
		switch sp.Kind {
		case richtext.Link:
			tg := l.d.addTarget(target{kind: tLink, url: sp.URL, underline: true})
			k := l.faceFor(fs.face, fs.size, sp.Style)
			l.addText(sp.Text, fs, k, fs.col, sp.Style, -1, tg)
		default:
			l.addSpans([]richtext.Span{sp}, fs)
		}
	}
}

// iconRow draws an optional icon and a bold one-line label; returns height.
func (l *lay) iconRow(icon, label string, x, y, w, iconPx int, weight font.Weight, size float32, col color.NRGBA, tg int16) int {
	lh := l.dp(22)
	cx := x
	if icon != "" {
		top := y + (lh-iconPx)/2
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, top, cx+iconPx, top+iconPx), url: icon, radii: [4]int{l.dp(4), l.dp(4), l.dp(4), l.dp(4)}, target: -1, px: iconPx})
		cx += iconPx + l.dp(6)
	}
	fs := l.bodyStyle()
	fs.face.Weight = weight
	fs.size = l.sp(size)
	fs.col = col
	fs.maxLines, fs.ellipsis = 1, true
	h, _ := l.plainFlow(label, cx, y, x+w-cx, &fs, tg)
	l.plainAdd("\n")
	return max(h, lh)
}

// unfurl is the 2026 rounded link-preview card with a right-side thumbnail.
func isVideoUnfurl(a *richtext.Attachment) bool {
	img := a.Thumb
	if img == nil {
		img = a.Image
	}
	if img == nil {
		return false
	}
	switch strings.ToLower(a.ServiceName) {
	case "youtube", "vimeo", "loom", "twitch":
		return true
	}
	return a.VideoURL != ""
}

// videoCard is the 2026 video unfurl: 358 wide, title and service on top,
// a 356x200 thumbnail with a play overlay below.
func (l *lay) videoCard(a *richtext.Attachment, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(358))
	pad := l.dp(12)
	url := a.VideoURL
	if url == "" {
		url = a.TitleLink
	}
	if url == "" {
		url = a.FromURL
	}
	cardT := l.d.addTarget(target{kind: tCard, url: url})
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: cardT, hover: p.BorderHover})
	cy := y + pad
	if len(a.Title) > 0 {
		fs := l.bodyStyle()
		fs.face.Weight = font.Bold
		fs.col = p.Text
		fs.lineH = l.dp(18.75)
		fs.maxLines, fs.ellipsis = 2, true
		l.addSpansTarget(a.Title, &fs, cardT)
		h, _ := l.flow(x+pad, cy, cw-2*pad, &fs)
		cy += h + l.dp(4)
		l.plainAdd("\n")
	}
	if a.ServiceName != "" {
		fs := l.small(p.CardFooter)
		fs.lineH = l.dp(16.25)
		ix := x + pad
		if a.ServiceIcon != "" {
			s := l.dp(16)
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, cy, ix+s, cy+s), url: a.ServiceIcon, radii: [4]int{l.dp(4), l.dp(4), l.dp(4), l.dp(4)}, target: -1, px: s})
			ix += s + l.dp(8)
		}
		fs.maxLines, fs.ellipsis, fs.noSelect = 1, true, true
		h, _ := l.plainFlow(a.ServiceName, ix, cy, x+cw-pad-ix, &fs, -1)
		cy += max(h, l.dp(16))
	}
	cy += pad
	img := a.Thumb
	if img == nil {
		img = a.Image
	}
	ih := (cw - 2*l.dp(1)) * 200 / 356
	rr := l.dp(12) - l.dp(1)
	ir := image.Rect(x+l.dp(1), cy, x+cw-l.dp(1), cy+ih)
	l.d.imgs = append(l.d.imgs, imgPrim{r: ir, url: img.URL, radii: [4]int{0, 0, rr, rr}, target: cardT, px: max(ir.Dx(), ir.Dy())})
	l.playOverlay(ir)
	cy += ih + l.dp(1)
	r := image.Rect(x, y, x+cw, cy)
	l.d.boxes[bi].r = r
	l.d.hits = append(l.d.hits, hit{r: r, t: cardT})
	return cy - y
}

func (l *lay) unfurl(a *richtext.Attachment, x, y, w int) int {
	if isVideoUnfurl(a) {
		return l.videoCard(a, x, y, w)
	}
	p := &l.st.Palette
	if w > l.dp(716) {
		w = l.dp(716)
	}
	pad := l.dp(12)
	bi := len(l.d.boxes)
	url := a.TitleLink
	if url == "" {
		url = a.FromURL
	}
	cardT := l.d.addTarget(target{kind: tCard, url: url})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: cardT, hover: p.BorderHover})
	thumbW := 0
	if a.Thumb != nil || (a.Image != nil && a.Image.W > 0 && a.Image.W < 300) {
		thumbW = l.dp(106)
	}
	cx := x + pad
	cw := w - 2*pad - thumbW
	cy := y + pad
	if len(a.Title) > 0 {
		fs := l.bodyStyle()
		fs.face.Weight = font.Bold
		fs.col = p.Text
		fs.maxLines, fs.ellipsis = 2, true
		fs.lineH = l.dp(18.75)
		l.addSpansTarget(a.Title, &fs, cardT)
		h, _ := l.flow(cx, cy, cw, &fs)
		cy += h + l.dp(4)
		l.plainAdd("\n")
	}
	if len(a.Text) > 0 {
		fs := l.small(p.CardText)
		fs.lineH = l.dp(16.25)
		fs.maxLines, fs.ellipsis = 2, true
		for i := range a.Text {
			if i > 0 {
				l.addText(" ", &fs, l.faceFor(fs.face, fs.size, 0), fs.col, 0, -1, -1)
			}
			l.addSpans(a.Text[i].Spans, &fs)
			for _, it := range a.Text[i].Items {
				l.addSpans(it, &fs)
			}
		}
		h, _ := l.flow(cx, cy, cw, &fs)
		cy += h
		l.plainAdd("\n")
	}
	if a.Image != nil && thumbW == 0 {
		cy += l.dp(8)
		tg := l.d.addTarget(target{kind: tImage, img: a.Image})
		if a.VideoURL != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.VideoURL})
		}
		cy += l.image(a.Image, cx, cy, cw, l.dp(360), l.dp(8), tg)
		if a.VideoURL != "" {
			l.playOverlay(l.d.imgs[len(l.d.imgs)-1].r)
		}
	}
	// service line at the bottom
	if a.ServiceName != "" {
		cy += l.dp(4)
		fs := l.small(p.CardFooter)
		fs.lineH = l.dp(16.25)
		ix := cx
		if a.ServiceIcon != "" {
			s := l.dp(16)
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, cy, ix+s, cy+s), url: a.ServiceIcon, radii: [4]int{l.dp(4), l.dp(4), l.dp(4), l.dp(4)}, target: -1, px: s})
			ix += s + l.dp(8)
		}
		fs.maxLines, fs.ellipsis = 1, true
		h, _ := l.plainFlow(a.ServiceName, ix, cy, cx+cw-ix, &fs, -1)
		cy += max(h, l.dp(16))
	}
	cy += pad
	h := cy - y
	if thumbW > 0 && h < l.dp(106) {
		h = l.dp(106)
	}
	r := image.Rect(x, y, x+w, y+h)
	l.d.boxes[bi].r = r
	l.d.hits = append(l.d.hits, hit{r: r, t: cardT})
	if thumbW > 0 {
		img := a.Thumb
		if img == nil {
			img = a.Image
		}
		rr := l.dp(12)
		tr := image.Rect(x+w-thumbW, y+l.dp(1), x+w-l.dp(1), y+h-l.dp(1))
		l.d.imgs = append(l.d.imgs, imgPrim{r: tr, url: img.URL, radii: [4]int{0, rr - 1, rr - 1, 0}, target: cardT, px: max(tr.Dx(), tr.Dy())})
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(tr.Min.X-l.dp(1), y, tr.Min.X, y+h), fill: p.Border, target: -1, layer: 1})
	}
	return h
}

// ---------- files ----------

func durationText(ms int) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func sizeText(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}

func fileName(f *richtext.File) string {
	if f.Title != "" {
		return f.Title
	}
	if f.Name != "" {
		return f.Name
	}
	return "Untitled"
}

func (l *lay) file(f *richtext.File, x, y, w int) int {
	switch {
	case f.IsImage && f.Thumb != nil:
		nh := l.fileNameRow(f, x, y, w)
		tg := l.d.addTarget(target{kind: tFile, file: f})
		iw, ih := fitSize(f.Thumb.W, f.Thumb.H, w, l.dp(360), l)
		bh := max(ih, l.dp(108))
		r := image.Rect(x, y+nh, x+iw, y+nh+bh)
		rad := l.dp(8)
		l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: f.Thumb.URL, radii: [4]int{rad, rad, rad, rad}, target: tg, px: max(iw, ih), fit: bh != ih})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: rad, stroke: l.st.Palette.Border, bw: l.dp(1), target: -1, layer: 1})
		return nh + bh
	case f.IsVideo:
		return l.video(f, x, y, w)
	case f.IsAudio:
		return l.audio(f, x, y, w)
	case f.IsSnippet || f.Preview != "":
		return l.snippet(f, x, y, w)
	}
	return l.genericFile(f, x, y, w)
}

// fileNameRow is the "name.png ▾" line above image and video files.
func (l *lay) fileNameRow(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	fs := l.small(p.Text)
	fs.col.A = uint8(float32(fs.col.A) * 0.7)
	fs.lineH = l.dp(22)
	fs.maxLines, fs.ellipsis = 1, true
	name := fileName(f)
	k := l.faceFor(fs.face, fs.size, 0)
	nw := min(int(l.measure(name, k)+0.999), w-l.dp(28))
	l.plainFlow(name, x, y, nw+l.dp(2), &fs, -1)
	cx := x + nw + l.dp(4) + l.dp(10)
	cy := y + l.dp(11)
	l.d.boxes = append(l.d.boxes, box{kind: bCaret, r: image.Rect(cx-l.dp(4), cy-l.dp(2), cx+l.dp(4), cy+l.dp(3)), fill: p.Text3, target: -1})
	return l.dp(26)
}

// gallery lays out several image files as square tiles.
func (l *lay) gallery(files []*richtext.File, x, y, w int) int {
	gap := l.dp(8)
	n := len(files)
	tile := l.dp(180)
	if n == 2 {
		tile = min((w-gap)/2, l.dp(240))
	}
	if tile*2+gap > w {
		tile = (w - gap) / 2
	}
	if tile < 1 {
		tile = 1
	}
	cx, cy := x, y
	for i, f := range files {
		if i > 0 && cx+tile > x+w {
			cx = x
			cy += tile + gap
		}
		r := image.Rect(cx, cy, cx+tile, cy+tile)
		tg := l.d.addTarget(target{kind: tFile, file: f})
		url := ""
		if f.Thumb != nil {
			url = f.Thumb.URL
		}
		rad := l.dp(8)
		l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: url, radii: [4]int{rad, rad, rad, rad}, target: tg, px: tile})
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: rad, stroke: rgba(0, 0, 0, 0.1), bw: l.dp(1), target: -1, layer: 1})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
		l.plainAdd(fileName(f))
		l.plainAdd("\n")
		cx += tile + gap
	}
	return cy + tile - y
}

func (l *lay) video(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	nh := l.fileNameRow(f, x, y, w)
	y += nh
	tg := l.d.addTarget(target{kind: tFile, file: f})
	var h int
	var r image.Rectangle
	if f.Thumb != nil {
		h = l.image(f.Thumb, x, y, min(w, l.dp(480)), l.dp(360), l.dp(8), tg)
		r = l.d.imgs[len(l.d.imgs)-1].r
	} else {
		r = image.Rect(x, y, x+min(w, l.dp(480)), y+l.dp(270))
		h = r.Dy()
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(8), fill: rgb(0, 0, 0), target: -1})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	}
	l.playOverlay(r)
	if f.DurationMS > 0 {
		fs := l.small(p.PlayFg)
		fs.size = l.sp(12)
		txt := durationText(f.DurationMS)
		tw := int(l.measure(txt, l.faceFor(fs.face, fs.size, 0))) + l.dp(12)
		br := image.Rect(r.Min.X+l.dp(8), r.Max.Y-l.dp(8)-l.dp(20), r.Min.X+l.dp(8)+tw, r.Max.Y-l.dp(8))
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: br, radius: l.dp(4), fill: p.PlayBg, target: -1, layer: 1})
		fs.lineH = br.Dy()
		fs.noSelect = true
		l.plainFlow(txt, br.Min.X+l.dp(6), br.Min.Y, tw, &fs, -1)
	}
	return nh + h
}

// playOverlay draws the round play button centered on r.
func (l *lay) playOverlay(r image.Rectangle) {
	p := &l.st.Palette
	c := r.Min.Add(r.Max).Div(2)
	s := l.dp(24)
	l.d.boxes = append(l.d.boxes, box{kind: bCircle, r: image.Rect(c.X-s, c.Y-s, c.X+s, c.Y+s), fill: p.PlayBg, target: -1, layer: 1})
	t := l.dp(9)
	l.d.boxes = append(l.d.boxes, box{kind: bPlay, r: image.Rect(c.X-t+l.dp(2), c.Y-t, c.X+t+l.dp(2), c.Y+t), fill: p.PlayFg, target: -1, layer: 1})
}

func (l *lay) cardFrame(x, y, w, h int, tg int16) int {
	p := &l.st.Palette
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: image.Rect(x, y, x+w, y+h), fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: tg, hover: p.BorderHover})
	if tg >= 0 {
		l.d.hits = append(l.d.hits, hit{r: image.Rect(x, y, x+w, y+h), t: tg})
	}
	return bi
}

func (l *lay) audio(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(426))
	h := l.dp(64)
	tg := l.d.addTarget(target{kind: tFile, file: f})
	l.cardFrame(x, y, cw, h, tg)
	s := l.dp(36)
	cy := y + (h-s)/2
	l.d.boxes = append(l.d.boxes, box{kind: bCircle, r: image.Rect(x+l.dp(14), cy, x+l.dp(14)+s, cy+s), fill: p.Link, target: -1})
	t := l.dp(7)
	cc := image.Pt(x+l.dp(14)+s/2+l.dp(1), cy+s/2)
	l.d.boxes = append(l.d.boxes, box{kind: bPlay, r: image.Rect(cc.X-t, cc.Y-t, cc.X+t, cc.Y+t), fill: p.PlayFg, target: -1, layer: 1})
	tx := x + l.dp(14) + s + l.dp(12)
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow(fileName(f), tx, y+l.dp(11), x+cw-tx-l.dp(12), &fs, tg)
	meta := "Audio"
	if f.DurationMS > 0 {
		meta = durationText(f.DurationMS)
	}
	ms := l.small(p.Text2)
	ms.noSelect = true
	l.plainFlow(meta, tx, y+l.dp(33), x+cw-tx-l.dp(12), &ms, -1)
	return h
}

func (l *lay) fileIcon(f *richtext.File, x, y, s int) {
	p := &l.st.Palette
	bg, fg := p.FileIconBg, p.FileIconText
	label := strings.ToUpper(f.Type)
	switch strings.ToLower(f.Type) {
	case "pdf":
		bg, fg = p.FileIconPDF, rgb(255, 255, 255)
	case "zip", "gzip", "tar", "rar", "7z":
		bg, fg = p.FileIconZip, rgb(255, 255, 255)
	}
	if len(label) > 4 {
		label = label[:4]
	}
	if label == "" {
		label = "FILE"
	}
	r := image.Rect(x, y, x+s, y+s)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(8), fill: bg, target: -1})
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Black}, size: l.sp(10), lineH: l.dp(12), col: fg, emoji: l.dp(12), maxLines: 1, noSelect: true}
	k := l.faceFor(fs.face, fs.size, 0)
	tw := int(l.measure(label, k) + 0.5)
	l.plainFlow(label, x+(s-tw)/2, y+(s-fs.lineH)/2, s, &fs, -1)
}

func (l *lay) genericFile(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(426))
	pad := l.dp(12)
	ic := l.dp(36)
	h := ic + 2*pad + l.dp(2)
	tg := l.d.addTarget(target{kind: tFile, file: f})
	l.cardFrame(x, y, cw, h, tg)
	l.fileIcon(f, x+pad, y+pad, ic)
	tx := x + pad + ic + pad
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(16), l.dp(20)
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow(fileName(f), tx, y+pad-l.dp(1), x+cw-tx-pad, &fs, tg)
	meta := f.PrettyType
	if meta == "" {
		meta = strings.ToUpper(f.Type)
	}
	if s := sizeText(f.Size); s != "" {
		if meta != "" {
			meta += " · "
		}
		meta += s
	}
	ms := l.small(p.Text2)
	ms.maxLines, ms.ellipsis, ms.noSelect = 1, true, true
	l.plainFlow(meta, tx, y+pad+l.dp(20), x+cw-tx-pad, &ms, -1)
	return h
}

func (l *lay) snippet(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(560))
	pad := l.dp(12)
	tg := l.d.addTarget(target{kind: tFile, file: f})
	bi := l.cardFrame(x, y, cw, 0, tg)
	// header
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.maxLines, fs.ellipsis = 1, true
	hh, _ := l.plainFlow(fileName(f), x+pad, y+pad, cw-2*pad, &fs, tg)
	cy := y + pad + hh
	meta := f.PrettyType
	if f.Lines > 0 {
		if meta != "" {
			meta += " · "
		}
		meta += strconv.Itoa(f.Lines) + " lines"
	}
	if meta != "" {
		ms := l.small(p.Text2)
		ms.noSelect = true
		mh, _ := l.plainFlow(meta, x+pad, cy, cw-2*pad, &ms, -1)
		cy += mh
	}
	cy += l.dp(8)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, cy, x+cw, cy+l.dp(1)), fill: p.Border, target: -1})
	cy += l.dp(1)
	if f.Preview != "" {
		ps := l.bodyStyle()
		ps.face = font.Font{Typeface: l.st.Mono}
		ps.size, ps.lineH, ps.pre = l.sp(12), l.dp(18), true
		ps.maxLines = 8
		ps.col = p.Text
		l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: p.PreBg, target: -1})
		pb := len(l.d.boxes) - 1
		l.plainAdd("\n")
		ph, _ := l.plainFlow(strings.TrimRight(f.Preview, "\n"), x+pad, cy+l.dp(8), cw-2*pad, &ps, -1)
		ph += l.dp(16)
		rr := l.dp(12) - l.dp(1)
		l.d.boxes[pb].r = image.Rect(x+l.dp(1), cy, x+cw-l.dp(1), cy+ph-l.dp(1))
		l.d.boxes[pb].corners = [4]int{0, 0, rr, rr}
		l.d.boxes[pb].perCorner = true
		cy += ph
	}
	l.d.boxes[bi].r = image.Rect(x, y, x+cw, cy)
	if tg >= 0 {
		l.d.hits = append(l.d.hits, hit{r: image.Rect(x, y, x+cw, cy), t: tg})
	}
	return cy - y
}
