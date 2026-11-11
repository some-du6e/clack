package richview

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
)

// What a link preview does besides drawing, ported from Slack's web client
// (build a84662d2d135, gantry-v2-async-gantry-v2-shared-boot-async:
// MessageAttachmentDefaultV3, MessageAttachmentDefaultV3HoverCard,
// AttachmentDeleteButton qrgA, AttachmentFooter CW6U, HoverCardTrigger
// 9X4J, HoverCard vPpv, ContextMenu v+x+; client-boot-styles CSS).
// docs/EMBEDS.md has the rules and where each comes from.
//
//   - The hover card. MessageAttachmentDefaultV3 wraps a row card
//     (MessageAttachmentGeneric v3) in HoverCardTrigger (renderCard: the
//     HoverCard, position "top", popoverZIndex Modal) when it has text, a
//     thumb_url alone, media (image_url, video_html, audio_html) or fields
//     (tO); the service cards and image cards never get one. The trigger
//     is the hover wrapper (.c-message_attachment_v3--hover_wrapper, the
//     card and the 6px of its margin the 114px max-height keeps). Its
//     timing and placement are hovercard's (350ms open, 250ms close,
//     Escape, a click closes it); the card is centered over the wrapper,
//     12px above (PlaceTop). Preview is the card's content, drawn by the
//     ui root inside popups' card frame.
//   - The context menu. renderV3Attachment's onContextMenu (every v3
//     card: row, social, video, music, Flickr and image cards) opens
//     ContextMenu at the pointer with "Remove preview" (showDelete), a
//     separator, then "Copy link" (copies title_link || from_url).
//     AttachmentAt reports it; v1 and v2 cards have none (the message's
//     menu opens).
//   - The rail remove button (attachmentVersion v1, v2: deleteStyle
//     "rail"): .c-message_attachment__delete_container--rail, 20px wide,
//     left -20px of the card from its top to its bottom, holding the
//     legacy times_small glyph (c-icon, 20px) in content-ter, content-sec
//     when hovered. v1 shows it while the card is hovered
//     (.c-message_attachment:hover .c-message_attachment__delete--rail);
//     v2 has no such rule, so its button only shows under the pointer.
//   - "Added by <app>": AttachmentFooterBotAttribution in a footer (v1,
//     v3 and the hover card) and renderAttributionMaybe under a Block Kit
//     attachment's blocks, when the attachment is an app unfurl or names
//     a bot (getBotById: the bots store, fetched with bots.info).

// v3Card is a laid-out v3 card: its box, the hover wrapper around it,
// the targets made inside it and its remove button.
type v3Card struct {
	r, wrap    image.Rectangle
	att        *richtext.Attachment
	blk        *richtext.Block
	card, last int16 // targets [card, last] belong to the card
	del        int16 // the remove button's target, -1 none
	popover    bool  // tO: HoverCardTrigger wraps the card
}

// AttachmentHit is a right-click on a v3 card: Slack's attachment
// context menu.
type AttachmentHit struct {
	Att *richtext.Attachment
	// CanRemove is showDelete: "Remove preview" (and the separator).
	CanRemove bool
	// CopyURL is what "Copy link" copies: title_link || from_url.
	CopyURL string
}

// AttachmentAt reports the v3 card under p (View coordinates): its
// onContextMenu takes every right-click inside it, the image and links
// included.
func (v *View) AttachmentAt(p f32.Point) (AttachmentHit, bool) {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := len(v.d.cards) - 1; i >= 0; i-- {
		c := &v.d.cards[i]
		in := pt.In(c.r)
		if !in && c.del >= 0 {
			for _, rm := range v.d.removes {
				if rm.del == c.del && pt.In(rm.r) {
					in = true
				}
			}
		}
		if !in {
			continue
		}
		return AttachmentHit{Att: c.att, CanRemove: c.del >= 0, CopyURL: firstNonEmpty(c.att.TitleLink, c.att.FromURL)}, true
	}
	return AttachmentHit{}, false
}

// popoverAt is the hover wrapper under p with a hover card (-1 none).
func (v *View) popoverAt(p f32.Point) int {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := len(v.d.cards) - 1; i >= 0; i-- {
		if c := &v.d.cards[i]; c.popover && pt.In(c.wrap) {
			return i
		}
	}
	return -1
}

// previewKey identifies one card's hover trigger to hovercard.
type previewKey struct {
	v *View
	a *richtext.Attachment
}

// reportPreview reports the hovered card's trigger (after the mentions:
// one trigger per frame). It returns whether one was reported.
func (v *View) reportPreview(c *hovercard.Controller, st *Style, h Handler) {
	i := -1
	if v.hovering && !v.opts.HoverCard {
		i = v.popoverAt(v.ptr)
	}
	var k previewKey
	if i >= 0 {
		k = previewKey{v, v.d.cards[i].att}
	}
	if v.prevOn && (i < 0 || k != v.prevKey) {
		c.Left(v.prevKey)
	}
	v.prevOn, v.prevKey = i >= 0, k
	if i < 0 {
		return
	}
	cd := &v.d.cards[i]
	c.ReportPreview(k, v.preview(cd, st, h), cd.wrap, v.ptr)
}

// pressPreview is a press on a card with a hover card: HoverCardTrigger's
// handleClick closes the card (Slack's click then opens the link).
func (v *View) pressPreview(p f32.Point) {
	c := hovercard.Default()
	if c == nil {
		return
	}
	if i := v.popoverAt(p); i >= 0 {
		c.Pressed(previewKey{v, v.d.cards[i].att})
	}
}

// preview returns the hover card content for card cd, kept per
// attachment so its layout survives across frames.
func (v *View) preview(cd *v3Card, st *Style, h Handler) *Preview {
	if v.previews == nil {
		v.previews = map[*richtext.Attachment]*Preview{}
	}
	p := v.previews[cd.att]
	if p == nil {
		blk := richtext.Block{Kind: richtext.AttachmentBlock, Att: cd.att}
		if cd.blk != nil {
			blk = *cd.blk
		}
		p = &Preview{v: New(richtext.Doc{Blocks: []richtext.Block{blk}}, Options{HoverCard: true, Channel: v.opts.Channel, TS: v.opts.TS})}
		v.previews[cd.att] = p
	}
	p.st, p.h = st, h
	return p
}

// Preview is MessageAttachmentDefaultV3HoverCard's content for one
// attachment: HoverCard's 340px column (the ui root draws the frame:
// elevation_3, sk_primary_background, rounded-lg).
type Preview struct {
	v  *View
	st *Style
	h  Handler
}

// PreviewWidth is HoverCard's width (style width 340).
const PreviewWidth = 340

// Layout draws the content at the origin, PreviewWidth dp wide.
func (p *Preview) Layout(gtx layout.Context) layout.Dimensions {
	w := gtx.Dp(unit.Dp(PreviewWidth))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, max(gtx.Constraints.Max.Y, 1<<20))}
	d := p.v.Layout(gtx, p.st, p.h)
	d.Size.X = w
	return d
}

// SetOrigin sets the content's window position (click anchors).
func (p *Preview) SetOrigin(o image.Point) { p.v.Origin = o }

// ---------- MessageAttachmentDefaultV3HoverCard ----------

// hoverCardDoc lays out a HoverCard-mode doc: its attachment as the hover
// card's content.
func (l *lay) hoverCardDoc(bs []richtext.Block, w int) int {
	for i := range bs {
		if bs[i].Kind == richtext.AttachmentBlock && bs[i].Att != nil {
			l.inAtt = true
			h := l.hoverCard(&bs[i], 0, 0, w)
			l.inAtt = false
			return h
		}
	}
	return 0
}

// hoverCard is MessageAttachmentDefaultV3HoverCard in .c-message_attachment_hover
// (a column), measured live against the CSS:
//   - --title (title, linked to title_link): padding 12 12 0, margin-bottom
//     2, 15px bold content-pry on the 22px body line.
//   - --text: padding 0 12, 13px/1.25 content-sec; AttachmentText with
//     maxCharacters 500, suppressBreaks and Truncate's 5 lines.
//   - --fields: margin-top 4, padding 2 12 0, a wrapping row of 12px
//     content-ter fields, each calc(50% - 4px) wide (every second one 8px
//     in), the bold title 4px before its value.
//   - --footer: margin-top 4, padding 0 12; the footer padding-top 4: the
//     16px icon (footer_icon || author_icon || service_icon, radius 4,
//     8px before the text), footer || service_name || author_name at
//     13px content-ter, no date, then " | Added by <app>".
//   - --media: the thumb alone (AttachmentThumb isHoverCard: 340x192), or
//     AttachmentMedia (isHoverCard, cover, cropHeight: the image fitted in
//     340x192, then 340x192 when narrower), margin-top 12, a base-ter
//     hairline above it, square top corners; empty, its margin-top 12 is
//     the card's bottom padding.
func (l *lay) hoverCard(b *richtext.Block, x, y, w int) int {
	a := b.Att
	p := &l.st.Palette
	pad := l.dp(12)
	cx, cw := x+pad, max(1, w-2*pad)
	cy := y
	if !spansBlank(a.Title) {
		cy += pad
		fs := l.bodyStyle()
		fs.face.Weight = font.Bold
		fs.col = p.Text
		fs.emoji, fs.flatEmoji = l.dp(16), true
		var tg int16 = -1
		ct := l.enter(attTitleClass)
		if a.TitleLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.TitleLink, underline: true, website: true}) // AttachmentTitle (HbXx)
			l.enter(linkClass)
		}
		l.addSpansTarget(a.Title, &fs, tg)
		h, _ := l.flow(cx, cy, cw, &fs)
		l.leave(ct)
		l.plainAdd("\n")
		cy += h + l.dp(2)
	}
	if text := v3Text(a); !blocksBlank(text) {
		cy += l.hoverText(text, cx, cy, cw)
	}
	if len(b.Fields) > 0 {
		cy += l.dp(4) + l.dp(2)
		cy += l.hoverFields(b.Fields, cx, cy, cw)
	}
	ft := firstNonEmpty(richtext.SpansText(a.Footer), a.ServiceName, a.AuthorName)
	if ft != "" || a.FooterIcon != "" || a.IsAppUnfurl && a.BotID != "" {
		cy += l.dp(4) + l.dp(4)
		cy += l.footerRow(a, firstNonEmpty(a.FooterIcon, a.AuthorIcon, a.ServiceIcon), ft, cx, cy, cw, true)
	}
	return cy - y + l.hoverMedia(a, x, cy, w)
}

// hoverText is the hover card's AttachmentText: 500 characters, line
// breaks as spaces (suppressBreaks), 5 lines.
func (l *lay) hoverText(bs []richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	short, _ := truncateBlocks(bs, 500, 1<<30)
	fs := l.small(p.CardText)
	fs.lineH = l.dp(16.25)
	fs.maxLines, fs.ellipsis = 5, true
	fs.emoji, fs.flatEmoji = l.dp(16), true
	sp := richtext.Span{Kind: richtext.Text, Text: " "}
	var ss []richtext.Span
	add := func(in []richtext.Span) {
		for _, s := range in {
			if s.Kind == richtext.Break {
				s = sp
			}
			ss = append(ss, s)
		}
	}
	for i := range short {
		if i > 0 {
			ss = append(ss, sp)
		}
		add(short[i].Spans)
		for j, it := range short[i].Items {
			if j > 0 || len(short[i].Spans) > 0 {
				ss = append(ss, sp)
			}
			add(it)
		}
	}
	ct := l.enter(attTextClass)
	l.addSpans(ss, &fs)
	h, _ := l.flow(x, y, w, &fs)
	l.leave(ct)
	l.plainAdd("\n")
	return h
}

// hoverFields lays out the fields two to a row; it returns the height.
func (l *lay) hoverFields(fields [][]richtext.Span, x, y, w int) int {
	p := &l.st.Palette
	gap := l.dp(8)
	fw := max(1, (w-gap)/2)
	cy, rowH := y, 0
	for i, f := range fields {
		col := i % 2
		if col == 0 && i > 0 {
			cy += rowH
			rowH = 0
		}
		fx := x + col*(fw+gap)
		title, value := f, []richtext.Span(nil)
		for j := range f {
			if f[j].Kind == richtext.Break {
				title, value = f[:j], f[j+1:]
				break
			}
		}
		fs := l.small(p.CardFooter)
		fs.size, fs.lineH = l.sp(12), l.dp(15)
		fs.emoji, fs.flatEmoji = l.dp(16), true
		bs := fs
		bs.face.Weight = font.Bold
		h := 0
		tw := 0
		if len(title) > 0 {
			tw = min(fw, l.spansWidth(title, &bs))
			l.plainAdd("\n")
			l.addSpans(title, &bs)
			th, _ := l.flow(fx, cy, fw, &bs)
			h = th
		}
		if len(value) > 0 {
			vx, vy := fx+tw+l.dp(4), cy
			if tw > 0 && (vx+l.spansWidth(value, &fs) > fx+fw || h > fs.lineH) {
				vx, vy = fx, cy+h // flex-wrap: the value takes the next line
			}
			l.plainAdd(" ")
			l.addSpans(value, &fs)
			vh, _ := l.flow(vx, vy, fx+fw-vx, &fs)
			h = max(h, vy-cy+vh)
		}
		rowH = max(rowH, h)
	}
	return cy + rowH - y
}

// hoverMedia is the --media span at (x, y), w wide; it returns its height.
func (l *lay) hoverMedia(a *richtext.Attachment, x, y, w int) int {
	p := &l.st.Palette
	var img *richtext.Image
	iw, ih := PreviewWidth, 192
	switch {
	case thumbOnly(a):
		img = a.Thumb // getImageMetadata isHoverCard: landscape 340x192
	case a.Image != nil:
		img = a.Image
		if a.Image.W > 0 && a.Image.H > 0 {
			iw, ih = adjustedDims(a.Image.W, a.Image.H, PreviewWidth, 192)
			if iw < PreviewWidth { // cover + cropHeight
				iw, ih = PreviewWidth, 192
			}
		}
	case a.VideoHTML && a.Thumb != nil:
		img = a.Thumb // AttachmentVideo's poster
	}
	if img == nil || !thumbOnly(a) && !mediaExpanded() {
		return l.dp(12)
	}
	top := y + l.dp(12)
	r := image.Rect(x, top, x+min(w, l.dp(float32(iw))), top+l.dp(float32(ih)))
	rr := l.dp(8)
	radii := [4]int{0, 0, rr, rr}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(r.Min.X, r.Min.Y-l.dp(1), r.Max.X, r.Min.Y), fill: p.Divider, target: -1, layer: 1})
	if !thumbOnly(a) { // useBackupBackground
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, fill: rgb(255, 255, 255), target: -1, perCorner: true, corners: radii})
		s := r.Dy() * 60 / 200
		c := r.Min.Add(r.Max).Div(2)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(c.X-s/2, c.Y-s/2, c.X-s/2+s, c.Y-s/2+s), url: iconURL + "image", target: -1, px: s, tint: rgb(142, 141, 142)})
	}
	var tg int16 = -1
	switch {
	case img == a.Image:
		tg = l.attImage(img, r) // AttachmentImage: the viewer
	case a.VideoHTML:
		tg = l.d.addTarget(target{kind: tLink, url: firstNonEmpty(a.FromURL, a.TitleLink, a.OriginalURL)})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	}
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, radii: radii, target: tg, px: max(r.Dx(), r.Dy())})
	if a.VideoHTML && img == a.Thumb && (img.W >= 120 && img.H >= 60 || img.W == 0 || img.H == 0) {
		l.videoButtons(r, a.FromURL != "", tg)
	}
	return r.Max.Y - y
}

// ---------- AttachmentFooter ----------

// attribution reports whether the footer gets AttachmentFooterBotAttribution
// (isAppUnfurl || bot: getBotById is truthy for any bot id, a placeholder
// while bots.info runs).
func attribution(a *richtext.Attachment) bool { return a.IsAppUnfurl || a.BotID != "" }

// footerRow is AttachmentFooter in a v3 card or the hover card (display
// flex): the 16px icon (radius 4, 8px before the text), the 13px
// content-ter text, then " | " in the footer's own color (content-inv-sec)
// and the 12px content-ter "Added by <app>". It returns the height.
func (l *lay) footerRow(a *richtext.Attachment, icon, text string, x, y, w int, hover bool) int {
	p := &l.st.Palette
	fx := x
	s := l.dp(16)
	if icon != "" {
		switch icon {
		case "brand-x-filled", "brand-youtube-filled", "play-filled", "music", "brand-bluesky-filled":
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(fx, y, fx+s, y+s), url: iconURL + icon, target: -1, px: s, tint: p.CardFooter})
		default:
			r4 := l.dp(4)
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(fx, y, fx+s, y+s), url: icon, radii: [4]int{r4, r4, r4, r4}, target: -1, px: s})
		}
		fx += s + l.dp(8)
	}
	fs := l.small(p.CardFooter)
	fs.lineH = l.dp(16.25)
	if !hover {
		fs.maxLines, fs.ellipsis = 2, true
	}
	ct := l.enter(attFooterClass)
	k := l.faceFor(fs.face, fs.size, 0)
	if text != "" {
		l.addText(text, &fs, k, fs.col, 0, -1, -1)
	}
	if attribution(a) {
		small := fs
		small.size = l.sp(12)
		ks := l.faceFor(small.face, small.size, 0)
		if text != "" {
			l.addText(" | ", &small, ks, p.FooterSep, 0, -1, -1)
		}
		l.addAttribution(a, &small, ks, p.CardFooter, false)
	}
	h, _ := l.flow(fx, y, max(1, x+w-fx), &fs)
	l.leave(ct)
	l.plainAdd("\n")
	if icon != "" {
		h = max(h, s)
	}
	return h
}

// addAttribution adds "Added by <app>": the bot's name (Slack's "a bot"
// while it is unknown) linked to /services/<bot_id>. In a footer the
// link takes the footer's color and an unknown bot is plain text
// (AttachmentFooterBotAttribution); under Block Kit blocks the name is a
// c-link that opens the bot's profile card (renderAttributionMaybe's
// ProfileCardTrigger).
func (l *lay) addAttribution(a *richtext.Attachment, fs *flowStyle, k faceKey, linkCol color.NRGBA, v2 bool) {
	l.addText("Added by ", fs, k, fs.col, 0, -1, -1)
	name := a.BotName
	if name == "" {
		name = "a bot"
	}
	var tg int16 = -1
	switch {
	case v2 && a.BotID != "":
		tg = l.d.addTarget(target{kind: tBot, id: a.BotID, underline: true})
	case !v2 && a.BotID != "" && a.BotName != "":
		tg = l.d.addTarget(target{kind: tLink, url: "/services/" + a.BotID, underline: true})
	}
	l.addText(name, fs, k, linkCol, 0, -1, tg)
}

// v2Attribution is renderAttributionMaybe under a Block Kit attachment's
// blocks (.c-message_attachment_v2__attribution, no style of its own: the
// message's 15px text, the name in link blue). It returns the height.
func (l *lay) v2Attribution(a *richtext.Attachment, x, y, w int) int {
	if !attribution(a) {
		return 0
	}
	fs := l.bodyStyle()
	k := l.faceFor(fs.face, fs.size, 0)
	l.addAttribution(a, &fs, k, l.st.Palette.Link, true)
	h, _ := l.flow(x, y, w, &fs)
	l.plainAdd("\n")
	return h
}

// ---------- the rail remove button ----------

// removeRail is AttachmentDeleteButton's rail style for a v1 or v2 card
// whose box is r: the 20px button at the top of the 20px column left of
// the card. hoverArea: v1 shows it while the card (rail included) is
// hovered; v2 only while the button is.
func (l *lay) removeRail(a *richtext.Attachment, r image.Rectangle, v1 bool) {
	if !l.opts.Own || a.ID == "" || a.FromURL == "" || l.opts.HoverCard {
		return
	}
	s := l.dp(20)
	br := image.Rect(r.Min.X-s, r.Min.Y, r.Min.X, r.Min.Y+s)
	t := l.d.addTarget(target{kind: tRemove, id: a.ID})
	l.d.hits = append(l.d.hits, hit{r: br, t: t})
	rm := removeBtn{r: br, card: -1, del: t, cardTo: -1, rail: true, col: image.Rect(r.Min.X-s, r.Min.Y, r.Min.X, r.Max.Y)}
	if v1 {
		rm.area = image.Rect(r.Min.X-s, r.Min.Y, r.Max.X, r.Max.Y)
	}
	l.d.removes = append(l.d.removes, rm)
}

// railInput registers the rail columns' input areas: they sit left of
// the View's own box, outside its clip (Slack's left -20px).
func (v *View) railInput(gtx layout.Context) {
	for i := range v.d.removes {
		rm := &v.d.removes[i]
		if !rm.rail {
			continue
		}
		s := clip.Rect(rm.col).Push(gtx.Ops)
		event.Op(gtx.Ops, v)
		pointer.CursorDefault.Add(gtx.Ops)
		s.Pop()
		s = clip.Rect(rm.r).Push(gtx.Ops)
		event.Op(gtx.Ops, v)
		pointer.CursorPointer.Add(gtx.Ops)
		s.Pop()
	}
}

// drawRails paints the rail buttons that show (outside the View's clip).
func (v *View) drawRails(gtx layout.Context, st *Style) {
	pt := image.Pt(int(v.ptr.X), int(v.ptr.Y))
	for i := range v.d.removes {
		rm := &v.d.removes[i]
		if !rm.rail {
			continue
		}
		on := v.hovering && v.hover == rm.del
		if !on && !(v.hovering && pt.In(rm.area)) {
			continue
		}
		col := st.Palette.CardFooter // content-ter
		if on {
			col = st.Palette.Text3 // content-sec
		}
		paintIconAt(gtx.Ops, "times-small", rm.r, col)
	}
}

// CardRect is a v3 card's box (View coordinates): Popover says whether
// it has a hover card.
type CardRect struct {
	Att     *richtext.Attachment
	R       image.Rectangle
	Popover bool
}

// CardRects lists the v3 cards (after the first Layout).
func (v *View) CardRects() []CardRect {
	out := make([]CardRect, 0, len(v.d.cards))
	for _, c := range v.d.cards {
		out = append(out, CardRect{Att: c.att, R: c.r, Popover: c.popover})
	}
	return out
}

// RemoveRect is a remove button's box (View coordinates): Rail for the
// v1/v2 style left of the card.
type RemoveRect struct {
	ID   string
	R    image.Rectangle
	Rail bool
}

// RemoveRects lists the remove buttons (after the first Layout).
func (v *View) RemoveRects() []RemoveRect {
	out := make([]RemoveRect, 0, len(v.d.removes))
	for _, rm := range v.d.removes {
		id := ""
		if rm.del >= 0 && int(rm.del) < len(v.d.targets) {
			id = v.d.targets[rm.del].id
		}
		out = append(out, RemoveRect{ID: id, R: rm.r, Rail: rm.rail})
	}
	return out
}
