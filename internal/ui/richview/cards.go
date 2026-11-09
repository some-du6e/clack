package richview

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
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
	if a.MsgCard {
		return l.msgCard(b, x, y, w)
	}
	if a.IsUnfurl && !a.IsShare && len(b.Fields) == 0 && len(b.Buttons) == 0 && len(b.Children) == 0 {
		pf := uipref.Get()
		media := a.Image != nil || a.Thumb != nil || isVideoUnfurl(a)
		// Messages & media > Inline media & links: expand_non_media_attachments
		// (text previews of linked websites), expand_inline_imgs (their images).
		if (!media && pf.NoTextPreviews) || (media && pf.NoInlineImgs && pf.NoTextPreviews) {
			return 0
		}
		return l.unfurl(a, x, y, w)
	}
	p := &l.st.Palette
	y0 := y
	if w > l.dp(600) {
		w = l.dp(600)
	}
	if len(a.Pretext) > 0 {
		fs := l.bodyStyle()
		ct := l.enter(attPretextClass)
		l.addSpans(a.Pretext, &fs)
		h, _ := l.flow(x, y, w, &fs)
		l.leave(ct)
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
		ct := l.enter(attServiceClass)
		row(l.iconRow(a.ServiceIcon, a.ServiceName, cx, cy, cw, l.dp(16), font.Black, 15, p.Text, l.d.addTarget(target{kind: tLink, url: a.FromURL, underline: true})))
		l.leave(ct)
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
		ct := l.enter(attAuthorClass)
		row(l.iconRow(a.AuthorIcon, name, cx, cy, cw, ic, weight, 15, p.Text, tg))
		l.leave(ct)
	}
	if len(a.Title) > 0 {
		fs := l.bodyStyle()
		fs.face.Weight = font.Bold
		fs.col = p.Link
		var tg int16 = -1
		ct := l.enter(attTitleClass)
		if a.TitleLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.TitleLink, underline: true})
			l.enter(linkClass)
		}
		l.addSpansTarget(a.Title, &fs, tg)
		h, _ := l.flow(cx, cy, cw, &fs)
		l.leave(ct)
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
		ct := l.enter(attTextClass)
		row(l.blocks(text, cx, cy, cw, false))
		l.leave(ct)
		l.expandAt = nil
		l.plainAdd("\n")
	}
	if len(b.Fields) > 0 {
		cy += l.dp(4)
		ct := l.enter(attFieldsClass)
		row(l.fields(b.Fields, b.FieldShort, cx, cy, cw))
		l.leave(ct)
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
		ct := l.enter(attFooterClass)
		defer l.leave(ct)
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
		l.addTextPart(&sp, fs, k, tg)
	}
}

// addTextPart is addText for a span of the flow's color, tagged with the
// span's CSS part.
func (l *lay) addTextPart(sp *richtext.Span, fs *flowStyle, k faceKey, tg int16) {
	if l.css == nil {
		l.addText(sp.Text, fs, k, fs.col, sp.Style, -1, tg)
		return
	}
	ctr := l.curPart
	l.curPart = l.spanPart(sp)
	l.addText(sp.Text, fs, l.partFace(k, l.curPart), fs.col, sp.Style, -1, tg)
	l.curPart = ctr
}

// addSpansPlain is addSpans where links keep the flow color (footers).
func (l *lay) addSpansPlain(spans []richtext.Span, fs *flowStyle) {
	for _, sp := range spans {
		switch sp.Kind {
		case richtext.Link:
			tg := l.d.addTarget(target{kind: tLink, url: sp.URL, underline: true})
			k := l.faceFor(fs.face, fs.size, sp.Style)
			l.addTextPart(&sp, fs, k, tg)
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
		ct := l.enter(attTitleClass)
		l.addSpansTarget(a.Title, &fs, cardT)
		f0 := len(l.d.frags)
		h, _ := l.flow(x+pad, cy, cw-2*pad, &fs)
		l.leave(ct)
		if l.inAtt && l.mode&modeAtts != 0 && isYouTube(a) {
			l.titleFrags = [2]int{f0, len(l.d.frags)} // AttachmentTitle (comps.go)
		}
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
		ct := l.enter(attServiceClass)
		h, _ := l.plainFlow(a.ServiceName, ix, cy, x+cw-pad-ix, &fs, -1)
		l.leave(ct)
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
	noImg := uipref.Get().NoInlineImgs // expand_inline_imgs off: the card without its image
	if isVideoUnfurl(a) && !noImg {
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
	if !noImg && (a.Thumb != nil || (a.Image != nil && a.Image.W > 0 && a.Image.W < 300)) {
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
		ct := l.enter(attTitleClass)
		l.addSpansTarget(a.Title, &fs, cardT)
		h, _ := l.flow(cx, cy, cw, &fs)
		l.leave(ct)
		cy += h + l.dp(4)
		l.plainAdd("\n")
	}
	if len(a.Text) > 0 {
		fs := l.small(p.CardText)
		fs.lineH = l.dp(16.25)
		fs.maxLines, fs.ellipsis = 2, true
		ct := l.enter(attTextClass)
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
		l.leave(ct)
		cy += h
		l.plainAdd("\n")
	}
	if a.Image != nil && thumbW == 0 && !noImg {
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
		ct := l.enter(attServiceClass)
		h, _ := l.plainFlow(a.ServiceName, ix, cy, cx+cw-ix, &fs, -1)
		l.leave(ct)
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
	case mediaFile(f):
		return l.mediaGallery([]*richtext.File{f}, x, y, w)
	case f.IsAudio:
		return l.audio(f, x, y, w)
	case f.IsSnippet || f.Preview != "":
		return l.snippet(f, x, y, w)
	}
	return l.genericFile(f, x, y, w)
}

// mediaFile reports whether f lays out in Slack's media gallery (gallery
// v3, isMediaGalleryViewContext in channels and threads): images with a
// thumbnail (unless expand_internal_inline_imgs is off) and playable
// videos (fileIsImage / fileIsPlayableVideo).
func mediaFile(f *richtext.File) bool {
	if f == nil {
		return false
	}
	if f.IsVideo {
		return true
	}
	return f.IsImage && f.Thumb != nil && !uipref.Get().NoInternalImgs
}

// galleryCaps is Slack's media-gallery v3 max image height (CSS px) per
// gallery breakpoint and orientation: {landscape, portrait, square}.
var galleryCaps = [4][3]float64{
	{235, 354, 314}, // large: gallery ≥ 636
	{154, 276, 206}, // medium: ≥ 472
	{115, 206, 206}, // small: ≥ 448
	{115, 206, 154}, // tiny
}

// Gallery v3 constants (seRe): the 8px gutter, the 108px minimum tile, and
// the flex gallery's width cap (FileGallery: min(width, 936); 716 under the
// attachments_and_unfurls_max_width experiment).
const (
	galleryGap    = 8
	galleryMinBox = 108
	galleryMaxW   = 936
)

func galleryBreakpoint(gw float64) int {
	switch {
	case gw >= 636:
		return 0
	case gw >= 472:
		return 1
	case gw >= 448:
		return 2
	}
	return 3
}

// galleryOrient is getGalleryV3ImageType: 0 landscape, 1 portrait, 2 square.
func galleryOrient(w, h float64) int {
	switch {
	case w > h:
		return 0
	case w < h:
		return 1
	}
	return 2
}

// galleryAspect is getGalleryV3AspectRatio: 4/3, 3/4 or 1 by orientation.
func galleryAspect(w, h float64) float64 {
	return [3]float64{4.0 / 3, 0.75, 1}[galleryOrient(w, h)]
}

// galleryMaxVisible is getGalleryV3MaxVisibleFiles: how many tiles fit in
// a gallery gw CSS px wide before the last one becomes "+ N".
func galleryMaxVisible(gw float64) int {
	switch {
	case gw >= 600:
		return 4
	case gw >= 448:
		return 3
	case gw >= 224:
		return 2
	}
	return 1
}

// galleryWidth is the media gallery's width in CSS px for a message w
// device px wide: the content width minus 26px (measured), at most 936.
func (l *lay) galleryWidth(w int) float64 {
	sc := float64(l.scale)
	if sc <= 0 {
		sc = 1
	}
	return math.Max(math.Min(float64(w)/sc-26, galleryMaxW), 1)
}

// galleryBox is the box (device px) Slack gives a single inline image of
// iw×ih (thumb px) in a message w device px wide (getDimensions with one
// file): the height is capped by the breakpoint table, the width follows
// the aspect, both at least 108px (but never wider than the message).
func galleryBox(iw, ih, w int, l *lay) (int, int) {
	sc := float64(l.scale)
	if sc <= 0 {
		sc = 1
	}
	gw := l.galleryWidth(w)
	if iw <= 0 || ih <= 0 {
		return max(min(w, l.dp(360)), 1), l.dp(240)
	}
	fw, fh := float64(iw), float64(ih)
	bw, bh := gallerySingle(gw, fw, fh)
	return max(min(int(bw*sc+0.5), w), 1), int(bh*sc + 0.5)
}

// gallerySingle is getDimensions for one file (isContainer): the box in
// CSS px for a thumb fw×fh in a gallery gw wide.
func gallerySingle(gw, fw, fh float64) (float64, float64) {
	fw, fh = math.Max(fw, 1), math.Max(fh, 1)
	s := fw / fh
	c := galleryCaps[galleryBreakpoint(gw)][galleryOrient(fw, fh)]
	d := math.Max(math.Min(fw, gw), galleryMinBox)
	u := math.Max(math.Floor(math.Min(math.Min(d/s, c), fh)), galleryMinBox)
	d = math.Max(math.Min(math.Floor(u*s), d), galleryMinBox)
	return d, u
}

// galleryPair is getDimensions for two files (isContainer): each tile's
// width and their shared height, in CSS px, for original sizes dims.
func galleryPair(gw float64, dims [][2]float64) ([]float64, float64) {
	r := len(dims)
	avail := gw - float64(r-1)*galleryGap
	o := make([][2]float64, r)
	for i, d := range dims {
		o[i] = [2]float64{math.Max(d[0], 1), math.Max(d[1], 1)}
	}
	tall := 0
	hs := make([]float64, r)
	for i, d := range o {
		if d[0] < galleryMinBox {
			hs[i] = math.Max(galleryMinBox*d[1]/d[0], galleryMinBox)
		} else {
			hs[i] = math.Max(d[1], galleryMinBox)
		}
		if hs[i] > hs[tall] {
			tall = i
		}
	}
	uw, uh := math.Max(o[tall][0], galleryMinBox), math.Max(o[tall][1], galleryMinBox)
	cap := math.Min(hs[tall], galleryCaps[galleryBreakpoint(gw)][galleryOrient(uw, uh)])
	grow := make([]float64, r)
	sum := 0.0
	for i, d := range o {
		grow[i] = math.Max(math.Min(cap*(d[0]/d[1]), d[0])-galleryMinBox, 0)
		sum += grow[i]
	}
	extra := math.Min(avail-galleryMinBox*float64(r), sum)
	ws := make([]float64, r)
	h := galleryMinBox * 1.0
	for i, d := range o {
		ws[i] = galleryMinBox
		if sum > 0 && extra > 0 {
			ws[i] = math.Floor(galleryMinBox + grow[i]/sum*extra)
		}
		h = math.Max(h, math.Min(math.Min(ws[i]/(d[0]/d[1]), d[1]), cap))
	}
	return ws, h
}

// galleryRow is getProportionalWidthV3 for three or more tiles: widths in
// proportion to each tile's v3 aspect (4/3, 3/4, 1) filling the gallery,
// all one height.
func galleryRow(gw float64, dims [][2]float64) ([]float64, float64) {
	avail := gw - float64(len(dims)-1)*galleryGap
	sum := 0.0
	for _, d := range dims {
		sum += galleryAspect(d[0], d[1])
	}
	ws := make([]float64, len(dims))
	for i, d := range dims {
		ws[i] = galleryAspect(d[0], d[1]) / sum * avail
	}
	return ws, avail / sum
}

// galleryDims is getFileDimensions with the media gallery on: a lone image
// sizes from its thumbnail, several from their originals; videos from
// thumb_video (or the original, or 108).
func galleryDims(f *richtext.File, single bool) [2]float64 {
	w, h := f.OrigW, f.OrigH
	switch {
	case f.IsVideo:
		if f.Thumb != nil && f.Thumb.W > 0 && f.Thumb.H > 0 {
			w, h = f.Thumb.W, f.Thumb.H
		}
		if w <= 0 || h <= 0 {
			w, h = galleryMinBox, galleryMinBox
		}
	case single || w <= 0 || h <= 0:
		if f.Thumb != nil {
			w, h = f.Thumb.W, f.Thumb.H
		}
	}
	return [2]float64{float64(w), float64(h)}
}

// mediaGallery is Slack's FileGallery for visual files in a channel or
// thread (c-file_gallery__flex): one file at its single-image size, two
// side by side at a shared height (getDimensions), three or more in one
// row proportional to their v3 aspects; past getGalleryV3MaxVisibleFiles
// the last tile carries the "+ N" overlay (FileGalleryOverflowOverlay).
// There's no file name above the tiles.
func (l *lay) mediaGallery(files []*richtext.File, x, y, w int) int {
	if len(files) == 0 {
		return 0
	}
	gw := l.galleryWidth(w)
	shown, over := files, 0
	if mv := galleryMaxVisible(gw); len(files) > mv {
		shown, over = files[:mv], len(files)-mv+1
	}
	dims := make([][2]float64, len(shown))
	for i, f := range shown {
		dims[i] = galleryDims(f, len(shown) == 1)
	}
	var ws []float64
	var h float64
	switch len(shown) {
	case 1:
		bw, bh := gallerySingle(gw, dims[0][0], dims[0][1])
		ws, h = []float64{bw}, bh
	case 2:
		ws, h = galleryPair(gw, dims)
	default:
		ws, h = galleryRow(gw, dims)
	}
	sc := float64(l.scale)
	if sc <= 0 {
		sc = 1
	}
	px := func(v float64) int { return int(math.Floor(v*sc + 0.5)) }
	th := min(px(h), 1<<20)
	if len(shown) == 1 {
		th = galleryClampH(th, w, px(ws[0]))
	}
	cx := 0.0
	for i, f := range shown {
		x0, x1 := x+px(cx), x+px(cx+ws[i])
		if len(shown) == 1 {
			x1 = min(x1, x+w)
		}
		r := image.Rect(x0, y, max(x1, x0+1), y+th)
		if i > 0 {
			l.plainAdd("\n")
		}
		l.galleryTile(f, r, dims[i], len(shown), i == len(shown)-1 && over > 0, over, len(files) == 1)
		cx += ws[i] + galleryGap
	}
	return th
}

// galleryClampH keeps a lone tile's height when the message is narrower
// than the tile (the box was already clamped to w).
func galleryClampH(h, w, bw int) int {
	if bw > w && bw > 0 {
		return max(h*w/bw, 1)
	}
	return h
}

// galleryTile draws one gallery tile in r: the image (or video poster)
// covering the tile, or contained at its own size when it is smaller than
// the tile (c-file_gallery_image_file__smaller_than_container), on Slack's
// base-ter fill with the inset 1px otl-ter outline, radius 8; videos get
// the play pill; the overflow tile the "+ N" scrim.
func (l *lay) galleryTile(f *richtext.File, r image.Rectangle, dim [2]float64, n int, overflow bool, count int, alone bool) {
	p := &l.st.Palette
	tg := l.d.addTarget(target{kind: tFile, file: f})
	rad := l.dp(8)
	radii := [4]int{rad, rad, rad, rad}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: rad, fill: p.Placeholder, target: -1})
	if f.Thumb != nil && f.Thumb.URL != "" {
		sc := float64(l.scale)
		if sc <= 0 {
			sc = 1
		}
		ow, oh := dim[0]*sc, dim[1]*sc
		if !f.IsVideo && n > 1 && f.Thumb.W > 0 && f.Thumb.H > 0 && (f.OrigW <= 0 || f.OrigH <= 0) {
			ow, oh = float64(f.Thumb.W)*sc, float64(f.Thumb.H)*sc
		}
		ir := r
		smaller := ow < float64(r.Dx()) || oh < float64(r.Dy())
		if n > 2 {
			smaller = false // aspect-ratio tiles always cover
		}
		if smaller || (f.IsVideo && n <= 2) {
			// getImageDimensionsForContainer: fit, never upscaled, centered
			k := math.Min(math.Min(float64(r.Dx())/math.Max(ow, 1), float64(r.Dy())/math.Max(oh, 1)), 1)
			iw, ih := int(ow*k+0.5), int(oh*k+0.5)
			c := r.Min.Add(r.Max).Div(2)
			ir = image.Rect(c.X-iw/2, c.Y-ih/2, c.X-iw/2+iw, c.Y-ih/2+ih).Intersect(r)
			if ir.Dx() < r.Dx() || ir.Dy() < r.Dy() {
				radii = [4]int{}
				if ir == r {
					radii = [4]int{rad, rad, rad, rad}
				}
			}
		}
		if ir.Empty() {
			ir = r
		}
		l.d.imgs = append(l.d.imgs, imgPrim{r: ir, url: f.Thumb.URL, radii: radii, target: tg, px: max(ir.Dx(), ir.Dy())})
	}
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: rad, stroke: p.Border, bw: l.dp(1), target: -1, layer: 1})
	l.plainAdd(fileName(f))
	if f.IsVideo && !overflow {
		l.videoPill(f, r)
	}
	if overflow {
		l.galleryOverflow(r, count)
	}
}

// videoPill is the media gallery video's play control at rest
// (p-video_controls_overlay__controls__pill--gallery_v3): 8px margin plus
// 4px padding from the bottom-left corner, 28 tall, at least 56 wide,
// padding 2px 4px, #1d1c1d at .8 with a 1px otl-inv-ter border, radius 4;
// a 16px play glyph, then the duration in 13px bold 4px after it ("Video"
// with no duration). Tiles ≤154px wide use the x-small pill (64 wide, 4px
// gap).
func (l *lay) videoPill(f *richtext.File, r image.Rectangle) {
	txt := "Video"
	if f.IsAudio {
		txt = "Audio"
	}
	if f.DurationMS > 0 {
		txt = durationText(f.DurationMS)
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dp(18),
		col: rgb(255, 255, 255), emoji: l.dp(13), maxLines: 1, noSelect: true}
	tw := int(l.measure(txt, l.faceFor(fs.face, fs.size, 0)) + 0.999)
	xs := r.Dx() <= l.dp(154)
	pw := l.dp(1) + l.dp(4) + l.dp(16) + l.dp(4) + tw + l.dp(4) + l.dp(1)
	pw = max(pw, l.dp(56))
	if xs {
		pw = max(pw, l.dp(64))
	}
	ph := l.dp(28)
	x0 := r.Min.X + l.dp(12)
	y1 := r.Max.Y - l.dp(12)
	pr := image.Rect(x0, y1-ph, x0+pw, y1)
	if pr.Max.X > r.Max.X-l.dp(4) || pr.Min.Y < r.Min.Y {
		return
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: pr, radius: l.dp(4), fill: rgba(29, 28, 29, 0.8), target: -1, layer: 1})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: pr, radius: l.dp(4), stroke: rgba(255, 255, 255, 0.13), bw: l.dp(1), target: -1, layer: 1})
	// play-filled, 16px: a triangle in the glyph's 10x12 box
	ix := pr.Min.X + l.dp(5)
	cy := pr.Min.Y + ph/2
	l.d.boxes = append(l.d.boxes, box{kind: bPlay, r: image.Rect(ix+l.dp(4), cy-l.dp(6), ix+l.dp(14), cy+l.dp(6)), fill: rgb(255, 255, 255), target: -1, layer: 1})
	l.plainFlow(txt, ix+l.dp(16)+l.dp(4), pr.Min.Y+(ph-fs.lineH)/2, tw+l.dp(2), &fs, -1)
}

// galleryOverflow is FileGalleryOverflowOverlay on the last tile: a scrim
// of sk_foreground_high at .5 (radius 8) and "+ N" centered in 22px Black
// sk_primary_background (white on light, the pane color on dark).
func (l *lay) galleryOverflow(r image.Rectangle, n int) {
	p := &l.st.Palette
	scrim, fg := rgba(29, 28, 29, 0.5), rgb(255, 255, 255)
	if lum(p.Panel) < 0.5 {
		scrim, fg = rgba(209, 210, 211, 0.5), p.Panel
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(8), fill: scrim, target: -1, layer: 1})
	txt := "+ " + strconv.Itoa(n)
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Black}, size: l.sp(22), lineH: l.dp(30),
		col: fg, emoji: l.dp(22), maxLines: 1, noSelect: true}
	tw := int(l.measure(txt, l.faceFor(fs.face, fs.size, 0)) + 0.999)
	l.plainFlow(txt, r.Min.X+(r.Dx()-tw)/2, r.Min.Y+(r.Dy()-fs.lineH)/2, tw+l.dp(2), &fs, -1)
}

// lum is a color's relative brightness, 0..1.
func lum(c color.NRGBA) float64 {
	return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
}

// playOverlay draws the round play button centered on r (video unfurls).
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
	case "quip", "canvas": // Slack's canvas tile: a white sheet on blue
		r := image.Rect(x, y, x+s, y+s)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(8), fill: rgb(0x1d, 0x9b, 0xd1), target: -1})
		sw, sh := s*2/5, s/2
		sr := image.Rect(x+(s-sw)/2, y+(s-sh)/2, x+(s+sw)/2, y+(s+sh)/2)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: sr, radius: l.dp(2), fill: rgb(255, 255, 255), target: -1})
		return
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

// pillowMeta is FileSize's line under a file pillow's name: the
// pretty_type ("PDF", "Zip", "Plain Text"; Slack's "Arugula" posts read
// "Post"). Slack shows no size here.
func pillowMeta(f *richtext.File) string {
	t := f.PrettyType
	if t == "" {
		t = strings.ToUpper(f.Type)
	}
	if t == "Arugula" {
		t = "Post"
	}
	return t
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
	ms := l.small(p.Text2)
	meta := pillowMeta(f)
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
