package richview

import (
	"image"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/expandable"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// Slack's VideoBlock (boot-shared u8h8, build 9f450b8ad3db): a video
// block's body (.p-video_block__body: padding 8px 0, 15px / 1.46667):
//
//   - renderProvider (.p-video_block__row--subtle_color): AttachmentAuthor
//     (DRR3) with the provider's icon (16px, radius 2, 8 to its right),
//     its name (Black, content-pry) and the author as its subname
//     (content-ter); the author alone as the name; nothing without an
//     author;
//   - .p-video_block__text: the description (AttachmentText: 700
//     characters, 5 newlines, its Show more under getExpandableKey(
//     "attachmentText", ts, {id: blockId, viewContext: THREAD in a
//     thread, else MESSAGE_PANE})), then the title (mrkdwn without
//     specials, linked to title_url) and its AttachmentMediaTrigger (the
//     caret, no size) under getExpandableKey("blockVideo", ts, {blockId})
//     (expandable.VideoBlockKey);
//   - .p-video_block__video_row (24px on the right): the player in an
//     ExpandableContainer with the attachment rules (expand_inline_imgs,
//     the unverified sender's disclosure): VideoPlayer at 360x283 in a
//     message and 450x354 in a modal or App Home (getAdjustedDimensions),
//     its thumbnail letterboxed on black (.p-video_block__thumb_img:
//     object-fit contain), VideoOverlay's "Video" pill 16px in from its
//     bottom left (#1d1c1dcc, radius 4, padding 4 8 4 6, play-filled 20,
//     the bold white label 8px after it). Slack plays the video in place
//     (its video_embed iframe); Clack opens its URL.
//
// Slack draws nothing for a video block without a video_url.

var nsVideoBlock = i18n.NS("video_block")

// videoBlock lays out VideoBlock for b at (x, y) in width w.
func (l *lay) videoBlock(b *richtext.Block, x, y, w int) int {
	a := b.Att
	if a.VideoURL == "" {
		return 0
	}
	p := &l.st.Palette
	y0 := y
	y += l.dp(8)
	lh := l.dp(22)
	// renderProvider
	if a.AuthorName != "" {
		if a.ServiceName != "" {
			y += l.videoAuthor(a.ServiceIcon, a.ServiceName, a.AuthorName, x, y, w)
		} else {
			y += l.videoAuthor("", a.AuthorName, "", x, y, w)
		}
	}
	// renderDescription: AttachmentText
	if len(a.Text) > 0 {
		vc := expandable.ViewChannel
		if l.opts.ViewContext == expandable.ViewThread {
			vc = expandable.ViewThread
		}
		key := expandable.Key(expandable.AttachmentText, l.opts.TS, expandable.Opts{ID: a.BlockID, ViewContext: vc})
		text := a.Text
		if short, cut := truncateBlocks(text, shareMaxChars, attMaxNewlines); cut {
			if key != "" && xpOn(key) || key == "" && l.expanded[a] {
				text = withExpander(append([]richtext.Block(nil), text...), false)
			} else {
				text = withExpander(short, true)
			}
		}
		l.expandAt, l.expandAtKey = a, key
		y += l.blocks(text, x, y, w, false)
		l.expandAt, l.expandAtKey = nil, ""
		l.plainAdd("\n")
	}
	// renderTitle: the title, then renderVideoExpander
	key := expandable.VideoBlockKey(l.opts.TS, a.BlockID)
	sender, ch := l.opts.Sender, l.opts.Channel
	if len(a.Title) > 0 {
		fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(15), lineH: lh, col: p.Text, emoji: l.dp(22)}
		tg := int16(-1)
		if a.TitleLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.TitleLink, underline: true})
			fs.col = p.Link
		}
		l.addSpansTarget(a.Title, &fs, tg)
		h, _ := l.flow(x, y, w, &fs)
		if key != "" {
			endX, top := l.lastLineEnd(x, y)
			open := xp().AttachmentExpanded(key, 0, sender, ch)
			_, top = l.mediaTrigger(key, a.VideoURL, 0, open, false, x, endX, top, w, fs)
			h = max(h, top+lh-y)
		}
		y += h
		l.plainAdd("\n")
	}
	// renderVideo: the player in its ExpandableContainer
	switch {
	case key == "" || containerExpanded(key, 0, sender, ch, true, true):
		y += l.videoPlayer(a, x, y, w)
	case xp().UnverifiedDisclosure(key, 0, sender, ch):
		y += l.unverifiedDisclosure(key, x, y, w, false)
	}
	y += l.dp(8)
	return y - y0
}

// videoAuthor is the provider row's AttachmentAuthor: icon, name and
// subname on one 22px line; its height.
func (l *lay) videoAuthor(icon, name, sub string, x, y, w int) int {
	p := &l.st.Palette
	lh := l.dp(22)
	cx := x
	if icon != "" {
		s, r := l.dp(16), l.dp(2)
		top := y + (lh-s)/2
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, top, cx+s, top+s), url: icon, radii: [4]int{r, r, r, r}, target: -1, px: s})
		cx += s + l.dp(8)
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Black}, size: l.sp(15), lineH: lh, col: p.Text,
		emoji: l.dp(22), maxLines: 1, ellipsis: true}
	l.addText(name, &fs, l.faceFor(fs.face, fs.size, 0), fs.col, 0, -1, -1)
	if sub != "" {
		rf := font.Font{Typeface: l.st.Face}
		l.addText(" "+sub, &fs, l.faceFor(rf, fs.size, 0), p.Text3, 0, -1, -1)
	}
	h, _ := l.flow(cx, y, max(1, x+w-cx), &fs)
	l.plainAdd("\n")
	return max(h, lh)
}

// videoPlayer is VideoPlayer with its thumbnail (T8FE): the box, the
// letterboxed thumbnail and VideoOverlay's pill; its height. A click on
// it opens the video.
func (l *lay) videoPlayer(a *richtext.Attachment, x, y, w int) int {
	bw, bh := 360, 283 // getAdjustedDimensions: 450x354 scaled into 360x360
	if l.opts.FillBlocks {
		bw, bh = 450, 354 // a modal's or App Home's 500x500
	}
	pw := min(l.dp(float32(bw)), w-l.dp(24)) // the row's 24px; .p-video_block__player max-width 100%
	if pw <= 0 {
		return 0
	}
	ph := pw * bh / bw
	r := image.Rect(x, y, x+pw, y+ph)
	rad := l.dp(4)
	tg := l.d.addTarget(target{kind: tCard, url: a.VideoURL})
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, fill: rgb(0, 0, 0), radius: rad, target: -1})
	if img := a.Image; img != nil && img.URL != "" {
		l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, fit: true, radii: [4]int{rad, rad, rad, rad}, target: tg, px: max(pw, ph)})
	}
	// VideoOverlay's controls: the pill 16px in from the bottom left
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(15), lineH: l.dp(22), col: rgb(255, 255, 255), emoji: l.dp(22)}
	label := nsVideoBlock.T("Video")
	icon := l.dp(20)
	lw := l.textWidth(label, &fs)
	pillW := l.dp(6) + icon + l.dp(8) + lw + l.dp(8)
	pillH := l.dp(4) + max(icon, fs.lineH) + l.dp(4)
	px, py := x+l.dp(16), y+ph-l.dp(16)-pillH
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(px, py, px+pillW, py+pillH), fill: rgba(29, 28, 29, 0.8), radius: l.dp(4), target: -1, layer: 2})
	ix, iy := px+l.dp(6), py+(pillH-icon)/2
	// play-filled's triangle in its 20-unit box (icons' "play")
	l.d.boxes = append(l.d.boxes, box{kind: bPlay, r: image.Rect(ix+icon*13/80, iy+icon/8, ix+icon*84/100, iy+icon*87/100), fill: rgb(255, 255, 255), target: -1, layer: 1})
	l.plainFlow(label, ix+icon+l.dp(8), py+(pillH-fs.lineH)/2, lw+l.dp(1), &fs, tg)
	l.plainAdd("\n")
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return ph
}
