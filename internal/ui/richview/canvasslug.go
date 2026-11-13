package richview

import (
	"image"

	"github.com/some-du6e/clack/internal/richtext"
)

// canvasSlug lays out a rich_text canvas element (richtext canvasslug.go):
// Slack's Canvas, a RichTextSlug like the message slug (messageMention):
// the .p-rich_text_slug pill, its icon (canvas-content, lock, trash; 4px
// gap) and the label, emoji in it 15px (.p-rich_text_slug
// .c-emoji--inline img). A deleted canvas's slug is disabled
// (p-rich_text_slug--disabled: content-pry on foreground_soft .06, no
// link); the others link to the canvas (the span's URL, with the section
// as focus_section_id).
func (l *lay) canvasSlug(sp *richtext.Span, fs *flowStyle, k faceKey, pt uint8) {
	p := &l.st.Palette
	bg, fg := p.MentionBg, p.MentionText
	var tg int16 = -1
	if sp.Off {
		bg, fg = rgba(p.Text.R, p.Text.G, p.Text.B, 0.06), p.Text
	} else if sp.URL != "" {
		tg = l.d.addTarget(target{kind: tLink, url: sp.URL, slug: true})
	}
	pad, padV := l.dpf(2), l.dpf(1)
	l.decos = append(l.decos, deco{kind: 1, bg: bg, padL: pad, padR: pad, padT: padV, padB: padV, radius: l.dp(3), target: tg, face: k, self: sp.Off, part: pt})
	dc := int16(len(l.decos) - 1)
	l.addPad(pad, dc, k)
	icon := sp.Icon
	if icon == "" {
		icon = "canvas-content"
	}
	l.addIcon(icon, fs.size, k, fg, dc, tg)
	l.addPad(l.dpf(3), dc, k) // svg margin-right 4 (the icon item ends with 1px)
	ep := l.dp(15)
	parts := sp.PartSpans()
	for i := range parts {
		s := &parts[i]
		if s.Kind == richtext.Emoji && s.URL != "" {
			p0, p1 := l.plainAdd(":" + s.ID + ":")
			l.items = append(l.items, item{kind: itImage, face: k, w: float32(ep + 2*l.dp(1)), url: s.URL, size: ep,
				deco: dc, target: tg, p0: p0, p1: p1, brk: true, alt: true, part: pt})
			continue
		}
		if s.Text != "" {
			l.addText(s.Text, fs, k, fg, sp.Style|s.Style, dc, tg)
		}
	}
	l.addPad(pad, dc, k)
}

// channelTabSlug lays out a resolved channel-tab slug (richtext
// tabslug.go): Slack's ResolvedChannelTabSlug, a RichTextSlug like the
// canvas slug: the tab's icon (4px gap), then the label, with the
// channel's icon (lock, code, …; a space after it) where its name starts
// instead of "#". It links to the tab (the span's URL) and, like the
// plain slug, opens the channel hover card when TabSlugCard says so.
func (l *lay) channelTabSlug(sp *richtext.Span, fs *flowStyle, k faceKey, pt uint8) {
	p := &l.st.Palette
	bg, fg := p.MentionBg, p.MentionText
	tg := l.d.addTarget(target{kind: tLink, url: sp.URL, id: sp.ID, tab: tabOfSlug(sp.URL), slug: true})
	pad, padV := l.dpf(2), l.dpf(1)
	l.decos = append(l.decos, deco{kind: 1, bg: bg, padL: pad, padR: pad, padT: padV, padB: padV, radius: l.dp(3), target: tg, face: k, part: pt})
	dc := int16(len(l.decos) - 1)
	l.addPad(pad, dc, k)
	if sp.Icon != "" {
		l.addIcon(sp.Icon, fs.size, k, fg, dc, tg)
		l.addPad(l.dpf(3), dc, k) // svg margin-right 4 (the icon item ends with 1px)
	}
	txt := sp.Text
	if sp.ChIcon != "" && sp.Pre > 0 && sp.Pre <= len(txt) {
		l.addText(txt[:sp.Pre], fs, k, fg, sp.Style, dc, tg)
		l.addIcon(sp.ChIcon, fs.size, k, fg, dc, tg)
		txt = " " + txt[sp.Pre:]
	}
	if txt != "" {
		l.addText(txt, fs, k, fg, sp.Style, dc, tg)
	}
	l.addPad(pad, dc, k)
}

// quipAnchor lays out a canvas-section unfurl (document_section):
// MessageAttachmentQuipAnchorLink in renderV1Attachment's card. The 4px
// bar, then the section's blocks (c-message_attachment__row), then
// AttachmentFooter (12px, content-sec): the footer text is an
// .sk_highlight link to from_url holding the canvas-content icon (lock
// for a private canvas) and, 4px on (margin_left_25), the canvas's title
// (QuipTitle micro, 12px emoji, from title_blocks; else
// FormatTextWithEmoji's 16px), then " | View section" (the permalink:
// hidden when the link's channel counts as private, which a canvas link's
// always does, and the team allows private_message_forwarding) and
// " | Canvas". No author row, no date (shouldHideUnfurlDate). A deleted
// canvas's footer is the trash icon and "Deleted canvas", unlinked, with
// neither part after it.
func (l *lay) quipAnchor(b *richtext.Block, x, y, w int) int {
	a := b.Att
	an := a.Anchor
	p := &l.st.Palette
	if w > l.dp(600) {
		w = l.dp(600)
	}
	bar := l.dp(4)
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: parseColor(a.Color, p.Placeholder), radius: l.dp(8), target: -1})
	cx := x + bar + l.dp(12)
	cw := x + w - cx
	cy := y
	if len(a.Text) > 0 {
		ct := l.enter(attTextClass)
		cy += l.blocks(a.Text, cx, cy, cw, false)
		l.leave(ct)
		l.plainAdd("\n")
	}
	cy += l.dp(4)
	ct := l.enter(attFooterClass)
	fs := l.small(p.Text2)
	fs.size = l.sp(12)
	k := l.faceFor(fs.face, fs.size, 0)
	var tg int16 = -1
	col := fs.col
	if a.FromURL != "" && !an.Deleted {
		tg = l.d.addTarget(target{kind: tLink, url: a.FromURL, underline: true})
		col = p.Link
	}
	l.addIcon(an.Icon, fs.size, k, col, -1, tg)
	l.addPad(l.dpf(3), -1, k) // margin_left_25 (the icon item ends with 1px)
	ep := l.dp(16)
	if an.FooterFromBlocks {
		ep = l.dp(12)
	}
	for i := range an.FooterTitle {
		s := &an.FooterTitle[i]
		if s.Kind == richtext.Emoji && s.URL != "" {
			p0, p1 := l.plainAdd(":" + s.ID + ":")
			l.items = append(l.items, item{kind: itImage, face: k, w: float32(ep + 2*l.dp(1)), url: s.URL, size: ep,
				deco: -1, target: tg, p0: p0, p1: p1, brk: true, alt: true})
			continue
		}
		if s.Text != "" {
			l.addText(s.Text, &fs, k, col, s.Style, -1, tg)
		}
	}
	if !an.Deleted {
		if a.FromURL != "" && (l.st.PrivateForwarding == nil || !l.st.PrivateForwarding()) {
			l.addText(" | ", &fs, k, fs.col, 0, -1, -1)
			pt := l.d.addTarget(target{kind: tLink, url: a.FromURL, underline: true})
			l.addText("View section", &fs, k, p.Link, 0, -1, pt)
		}
		l.addText(" | ", &fs, k, fs.col, 0, -1, -1)
		l.addText("Canvas", &fs, k, fs.col, 0, -1, -1)
	}
	h, _ := l.flow(cx, cy, x+w-cx, &fs)
	cy += max(h, l.dp(18))
	l.leave(ct)
	l.d.boxes[bi].r = image.Rect(x, y, x+bar, cy)
	return cy - y
}
