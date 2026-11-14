package richview

import (
	"image"
	"net/url"
	"regexp"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Link unfurls and legacy attachments the way Slack's web client draws
// them for accounts in the unfurl_attachments_redesign experiment (the
// test account is: in the live props capture, docs/PLUGINS-props-live.md,
// MessageAttachment's outer host is c-message_attachment_v3--hover_wrapper
// and ConnectedMessageAttachmentTwitterX's is c-message_attachment__body
// --social_media_card --fixed_layout --twitter_x). Sources: the gantry-v2
// shared-boot-async chunk of build a84662d2d135 (MessageAttachment,
// MessageAttachmentDefaultV3, MessageAttachmentGeneric, the per-service
// cards, AttachmentMedia, AttachmentText, AttachmentDeleteButton) and
// client-boot-styles.cf726c4f2eaecc74.min.css (.c-message_attachment_v3,
// .c-message_attachment__*), checked against the live client's boxes.
// docs/EMBEDS.md has the rules and where each comes from.

// attKind is the component MessageAttachmentDefaultV3 picks.
type attKind uint8

const (
	akOld       attKind = iota // not from a message's attachments (video/call blocks, huddles): own layouts
	akV1                       // renderV1Attachment(MessageAttachmentGeneric v1): color bar card
	akV2                       // renderV2Attachment: color bar + the attachment's blocks
	akGeneric                  // MessageAttachmentGeneric v3: the 106px row card
	akSocial                   // MessageAttachmentTwitterX useRedesign (twitter, bluesky)
	akVideo                    // YouTube, Vimeo, Loom, Wistia: video_service_card
	akMusic                    // Spotify, Apple Music: music_service_card
	akFlickr                   // MessageAttachmentFlickr
	akImageCard                // MessageAttachmentImageCard (image-only links, Threads)
)

var (
	appleMusicRe = regexp.MustCompile(`(?i)apple\s?music`)
	grafanaRe    = regexp.MustCompile(`(?i)https://grafana\.[\w.-]*?\.com`)
)

// v3Text is the attachment's own text: Text without its Block Kit blocks.
func v3Text(a *richtext.Attachment) []richtext.Block {
	if a.Kit {
		return a.Text[:min(a.KitFrom, len(a.Text))]
	}
	return a.Text
}

func hasAuthor(a *richtext.Attachment) bool {
	return a.ServiceIcon != "" || a.AuthorIcon != "" || a.ServiceName != "" || a.AuthorName != "" ||
		a.ServiceURL != "" || a.AuthorLink != "" || a.AuthorSubname != ""
}

func spansBlank(ss []richtext.Span) bool {
	for _, s := range ss {
		if s.Kind != richtext.Text || strings.TrimSpace(s.Text) != "" {
			return false
		}
	}
	return true
}

func blocksBlank(bs []richtext.Block) bool {
	for i := range bs {
		if !spansBlank(bs[i].Spans) || len(bs[i].Items) > 0 || len(bs[i].Children) > 0 || bs[i].Kind != richtext.Paragraph {
			return false
		}
	}
	return true
}

func isTwitter(a *richtext.Attachment) bool {
	return strings.Contains(strings.ToLower(a.ServiceName), "twitter") || a.ServiceName == "bluesky"
}

// kindOf is MessageAttachmentDefaultV3's choice: Block Kit attachments
// render as v2, then the service cards override it (TwitterX/Bluesky,
// Threads, Flickr, Wistia, YouTube, Vimeo, Apple Music, Spotify, Loom);
// with none of those an image-only link is MessageAttachmentImageCard
// (iR.Wx), a card iR.Id rules out (no title and no text, no author and no
// title, no original_url, a bot's, Grafana) is the v1 card, and the rest
// is the v3 generic card.
func kindOf(b *richtext.Block) attKind {
	a := b.Att
	if a == nil || !a.Legacy || a.IsShare || a.MsgCard {
		return akOld
	}
	sn, low := a.ServiceName, strings.ToLower(a.ServiceName)
	switch {
	case isTwitter(a):
		return akSocial
	case sn == "Threads":
		return akImageCard
	case sn == "Flickr":
		return akFlickr
	case strings.Contains(low, "wistia"), sn == "YouTube", strings.Contains(low, "vimeo"), sn == "Loom":
		return akVideo
	case sn == "Spotify", appleMusicRe.MatchString(sn):
		return akMusic
	}
	if a.Kit {
		return akV2
	}
	title, text := !spansBlank(a.Title), !blocksBlank(v3Text(a))
	if len(b.Buttons) == 0 && (!title || isGIF(a.Image)) && !text && !a.VideoHTML && a.OriginalURL != "" {
		return akImageCard
	}
	if !title && !text || !hasAuthor(a) && !title || a.OriginalURL == "" || a.BotID != "" || grafanaRe.MatchString(a.OriginalURL) {
		return akV1
	}
	return akGeneric
}

func isGIF(img *richtext.Image) bool {
	if img == nil {
		return false
	}
	u, err := url.Parse(img.URL)
	return err == nil && strings.HasSuffix(strings.ToLower(u.Path), ".gif")
}

// hasMedia is iR.zP: the attachment has an expandable key, i.e. an
// image_url, audio_html or video_html (cGuS getExpandableKey). A
// thumb_url alone is not media.
func hasMedia(a *richtext.Attachment) bool { return a.Image != nil || a.AudioHTML || a.VideoHTML }

// thumbOnly: thumb_url with no image_url, video_html or audio_html (eM).
func thumbOnly(a *richtext.Attachment) bool {
	return a.Thumb != nil && a.Image == nil && !a.VideoHTML && !a.AudioHTML
}

// shown is getVisibleAttachmentsForMessage's shouldShow (djIX): a link
// preview whose URL is in the message (from_url linked in its text or
// blocks) shows only with expand_non_media_attachments on, unless it is
// media with no text (audio, other or video html, an image, a "twitter"
// service). Hidden attachments never show.
func shown(a *richtext.Attachment) bool {
	if a.Hidden {
		return false
	}
	if !a.Legacy || a.IsShare || a.MsgCard || !a.LinkedInText {
		return true
	}
	if !uipref.Get().NoTextPreviews {
		return true
	}
	media := a.AudioHTML || a.OtherHTML || a.VideoHTML || a.Image != nil || strings.ToLower(a.ServiceName) == "twitter"
	return media && blocksBlank(a.Text)
}

// mediaExpanded is isAttachmentExpanded (maUm) without a toggle: Slack
// collapses attachment media when expand_inline_imgs is off.
func mediaExpanded() bool { return !uipref.Get().NoInlineImgs }

// isV3 reports whether b draws in a v3 hover wrapper (MessageAttachmentDefaultV3's tm).
func isV3(b *richtext.Block) bool {
	switch kindOf(b) {
	case akGeneric, akSocial, akVideo, akMusic, akFlickr, akImageCard:
		return shown(b.Att)
	}
	return false
}

// ---------- the v3 group ----------

// v3Group lays out consecutive v3 attachments. .c-message_kit__attachments
// with a v3 wrapper in it is a wrapping flex row (max-width 720,
// justify-content space-between; a column when v1/v2 attachments are in
// it too, so callers group only v3 runs). Each wrapper is width 100%
// capped by its card's max-width and max-height: the 8px margin under
// each card is inside the wrapper (clipped to 114px for row cards).
func (l *lay) v3Group(bs []*richtext.Block, x, y, w int) int {
	rowW := min(w, l.dp(720))
	narrow := rowW <= l.dp(475) // @container v3-attachment (width<=475px)
	type cell struct {
		b    *richtext.Block
		w, h int
	}
	y0 := y
	var row []cell
	used := 0
	flush := func() {
		if len(row) == 0 {
			return
		}
		gap := 0
		if len(row) > 1 {
			gap = (rowW - used) / (len(row) - 1)
		}
		cx := x
		rh := 0
		for _, c := range row {
			h := l.v3Card(c.b, cx, y, c.w, narrow)
			rh = max(rh, h)
			cx += c.w + gap
		}
		y += rh
		row, used = row[:0], 0
	}
	for _, b := range bs {
		cw := l.v3Width(b, rowW, narrow)
		if len(row) > 0 && used+cw > rowW {
			flush()
		}
		row = append(row, cell{b: b, w: cw})
		used += cw
	}
	flush()
	return y - y0
}

// v3Width is a wrapper's width: 716 for row cards, 358 for fixed-layout
// cards (social, video, music, flickr, image cards); within a container
// of 475px or less every wrapper is 100% wide, video/music/flickr capped
// at 375 and image cards their image's width.
func (l *lay) v3Width(b *richtext.Block, rowW int, narrow bool) int {
	k := kindOf(b)
	switch {
	case k == akImageCard:
		iw, _ := l.imageCardSize(b.Att)
		return min(rowW, iw+2*l.dp(1))
	case narrow && (k == akVideo || k == akMusic || k == akFlickr):
		return min(rowW, l.dp(375))
	case narrow:
		return rowW
	case k == akGeneric:
		return min(rowW, l.dp(716))
	}
	return min(rowW, l.dp(358))
}

// v3Card lays out one wrapper at (x, y), w wide; it returns the wrapper's
// height (the card's and its 8px margin, clipped to the max-height).
func (l *lay) v3Card(b *richtext.Block, x, y, w int, narrow bool) int {
	a := b.Att
	switch kindOf(b) {
	case akGeneric:
		return l.v3Generic(b, x, y, w, narrow)
	case akImageCard:
		h := l.imageCard(a, x, y, w)
		return min(h+l.dp(8), l.dp(366))
	}
	h := l.fixedCard(b, x, y, w)
	return min(h+l.dp(8), l.dp(408))
}

// v3Frame is .c-message_attachment_v3 > .c-message_attachment__body: base-pry,
// a 1px otl-ter border (otl-sec on hover), radius 12. link makes the whole
// card a link (iR.Rb: every service but YouTube, Vimeo, Apple Music,
// Spotify and Loom, when there is a title_link or original_url). The
// card's hit is registered here, before anything inside it, so the hits
// laid out in the card (its image, a video's link) sit on top of it
// (targetAt: later hits win); v3Close sets its box.
func (l *lay) v3Frame(a *richtext.Attachment, link bool) (cardT int16, bi, hi int) {
	p := &l.st.Palette
	cardT = -1
	if link {
		cardT = l.d.addTarget(target{kind: tCard, url: firstNonEmpty(a.TitleLink, a.OriginalURL)})
	} else {
		cardT = l.d.addTarget(target{kind: tCard})
	}
	bi = len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: cardT, hover: p.BorderHover})
	hi = len(l.d.hits)
	l.d.hits = append(l.d.hits, hit{t: cardT})
	return cardT, bi, hi
}

// v3Close sets the card's box and hit (hi, from v3Frame), its hover range
// (every target made inside the card keeps it hovered, as :hover on the
// card's ancestor does) and the remove button.
func (l *lay) v3Close(a *richtext.Attachment, bi, hi int, cardT int16, r image.Rectangle) {
	l.d.boxes[bi].r = r
	l.d.boxes[bi].tgTo = int16(len(l.d.targets) - 1)
	l.d.hits[hi].r = r
	l.d.cards = append(l.d.cards, v3Card{r: r, wrap: r, att: a, card: cardT, last: int16(len(l.d.targets) - 1), del: -1})
	l.removeButton(a, cardT, r)
}

// attImage is the click target of an attachment's image_url as
// AttachmentMedia draws it (AttachmentImage, NANx): its onClick calls
// preventDefault (so the card's <a> doesn't navigate) and opens the image
// in Slack's viewer (the media viewer with a mediaItem, not a file). r is
// the image's box. thumb_url alone is AttachmentThumb, a plain span with
// no handler, and video_html's poster is AttachmentVideo: neither comes
// here.
func (l *lay) attImage(img *richtext.Image, r image.Rectangle) int16 {
	t := l.d.addTarget(target{kind: tImage, img: img, inCard: true})
	l.d.hits = append(l.d.hits, hit{r: r, t: t})
	return t
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// removeButton is AttachmentDeleteButton's overlay style (attachmentVersion
// v3): a 20px base-inv-pry circle at top -9px right -9px of the card with
// the 12px close-circle-filled glyph in content-inv-pry, shown while the
// card is hovered. canDelete (4BNQ W9): the viewer sent the message (the
// owner checks Options.Own; pinned_item messages excluded), and the
// attachment has an id and a from_url.
func (l *lay) removeButton(a *richtext.Attachment, cardT int16, r image.Rectangle) {
	if !l.opts.Own || a.ID == "" || a.FromURL == "" {
		return
	}
	s := l.dp(20)
	off := l.dp(9)
	br := image.Rect(r.Max.X+off-s, r.Min.Y-off, r.Max.X+off, r.Min.Y-off+s)
	t := l.d.addTarget(target{kind: tRemove, id: a.ID})
	l.d.hits = append(l.d.hits, hit{r: br, t: t})
	l.d.removes = append(l.d.removes, removeBtn{r: br, card: cardT, del: t, cardTo: t - 1})
	if n := len(l.d.cards); n > 0 && l.d.cards[n-1].card == cardT {
		l.d.cards[n-1].del = t
	}
}

// ---------- shared rows ----------

// v3TitleRow is .c-message_attachment__left__title_author: a flex row
// (nowrap, baseline, line-height 1.25, margin-bottom 4) of the title (15px
// bold, content-pry, one line with an ellipsis) and the author (13px
// content-ter after a "|": margin-left 4, padding-left 4, the bar 4px
// left of the text), both shrinking to fit. column stacks them (video,
// music and flickr cards: the author full width, no bar). narrow is the
// container rule that wraps generic cards: the title clamps to 2 lines
// and the author goes under it. It returns the row's height (margin
// included).
func (l *lay) v3TitleRow(a *richtext.Attachment, title []richtext.Span, author string, x, y, w int, tg int16, column, narrow bool, emoji int) int {
	p := &l.st.Palette
	tfs := l.bodyStyle()
	tfs.face.Weight = font.Bold
	tfs.col = p.Text
	tfs.lineH = l.dp(18.75)
	tfs.maxLines, tfs.ellipsis = 1, true
	tfs.emoji, tfs.flatEmoji = emoji, true
	if narrow {
		tfs.maxLines = 2
	}
	afs := l.small(p.CardFooter)
	afs.lineH = l.dp(18)
	afs.maxLines, afs.ellipsis = 1, true
	h := 0
	if column || narrow || author == "" {
		if len(title) > 0 {
			ct := l.enter(attTitleClass)
			l.addSpansTarget(title, &tfs, tg)
			f0 := len(l.d.frags)
			th, _ := l.flow(x, y, w, &tfs)
			l.leave(ct)
			if l.inAtt && l.mode&modeAtts != 0 && isYouTube(a) {
				l.titleFrags = [2]int{f0, len(l.d.frags)} // AttachmentTitle (comps.go)
			}
			l.plainAdd("\n")
			h += th
		}
		if author != "" {
			ct := l.enter(attAuthorClass)
			ah, _ := l.plainFlow(author, x, y+h, w, &afs, tg)
			l.leave(ct)
			l.plainAdd("\n")
			h += ah
		}
		return h + l.dp(4)
	}
	// flex shrink: both items shrink in proportion to their widths
	bar := l.dp(8)
	tw := l.spansWidth(title, &tfs)
	pw := l.textWidth("|", &afs)
	aw := l.textWidth(author, &afs)
	ab := bar + pw + aw
	if over := tw + ab - w; over > 0 && tw+ab > 0 {
		tw -= over * tw / (tw + ab)
		ab = w - tw
	}
	ct := l.enter(attTitleClass)
	l.addSpansTarget(title, &tfs, tg)
	th, _ := l.flow(x, y, tw, &tfs)
	l.leave(ct)
	l.plainAdd(" ")
	ax := x + tw + l.dp(4)
	// baseline alignment: the 13px line sits on the title's baseline
	ay := y + (tfs.lineH-afs.lineH)/2 + l.dp(1)
	l.plainFlow("|", ax, ay, pw+l.dp(1), &afs, -1)
	cta := l.enter(attAuthorClass)
	ah, _ := l.plainFlow(author, ax+l.dp(4)+pw, ay, max(1, ab-bar-pw), &afs, tg)
	l.leave(cta)
	l.plainAdd("\n")
	return max(th, ah) + l.dp(4)
}

// spansWidth is the one-line width of spans in fs (no layout recorded).
func (l *lay) spansWidth(ss []richtext.Span, fs *flowStyle) int {
	w := 0
	for _, s := range ss {
		switch s.Kind {
		case richtext.Emoji:
			if s.URL != "" {
				w += fs.emoji + 2*l.dp(1)
				continue
			}
		case richtext.Break:
			continue
		}
		w += l.textWidth(s.Text, fs)
	}
	return w
}

// v3Text lays out the attachment text: AttachmentText's maxCharacters
// (700 when not given) and no line limit in shouldUseTruncate cards, then
// Truncate's line clamp (lines, ellipsis). 13px, line-height 1.25,
// content-sec. It returns the height.
func (l *lay) v3TextFlow(bs []richtext.Block, x, y, w, lines, emoji int) int {
	p := &l.st.Palette
	short, _ := truncateBlocks(bs, shareMaxChars, 1<<30)
	fs := l.small(p.CardText)
	fs.lineH = l.dp(16.25)
	fs.maxLines, fs.ellipsis = lines, true
	fs.emoji, fs.flatEmoji = emoji, true
	ct := l.enter(attTextClass)
	for i := range short {
		if i > 0 {
			l.addSpans([]richtext.Span{{Kind: richtext.Break}}, &fs)
		}
		l.addSpans(short[i].Spans, &fs)
		for j, it := range short[i].Items {
			if j > 0 {
				l.addSpans([]richtext.Span{{Kind: richtext.Break}}, &fs)
			}
			l.addSpans(it, &fs)
		}
	}
	h, _ := l.flow(x, y, w, &fs)
	l.leave(ct)
	l.plainAdd("\n")
	return h
}

// v3Footer is AttachmentFooter in a v3 card: the 16px icon (Slack's glyph
// for brand-x-filled, brand-youtube-filled, play-filled, music and
// brand-bluesky-filled, else an image with radius 4) 8px before the 13px
// content-ter text, no date (shouldHideUnfurlDate). It returns the
// footer's height without its padding.
func (l *lay) v3Footer(icon, text string, x, y, w int) int {
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
	fs.maxLines, fs.ellipsis = 2, true
	ct := l.enter(attFooterClass)
	h, _ := l.plainFlow(text, fx, y, max(1, x+w-fx), &fs, -1)
	l.leave(ct)
	l.plainAdd("\n")
	if icon != "" {
		h = max(h, s)
	}
	return h
}

// ---------- MessageAttachmentGeneric v3 ----------

// v3Generic is the row card: .c-message_attachment__left (padding 12,
// content centered in 106px, otl-ter border on its right) and
// .c-message_attachment__right (106x106, background-size cover): the
// image (image_url, or video_html's thumb), the thumb (thumb_url alone)
// or Slack's no-image placeholder. --short (no text and no media): 80px.
// The title is title, or the link's host when the title is the URL
// (iR.C0); the footer is footer || service_name || author_name with
// footer_icon || author_icon || service_icon. Fields, the author row and
// the date are not drawn in v3.
func (l *lay) v3Generic(b *richtext.Block, x, y, w int, narrow bool) int {
	a := b.Att
	p := &l.st.Palette
	text := v3Text(a)
	isURLTitle := a.OriginalURL != "" && richtext.SpansText(a.Title) == a.OriginalURL
	title := a.Title
	if isURLTitle {
		title = []richtext.Span{{Kind: richtext.Text, Text: hostOf(a.OriginalURL)}}
		text = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: a.OriginalURL}}}}
	}
	hasText := !blocksBlank(text) || isURLTitle
	media := hasMedia(a)
	noMedia := !(a.Image != nil || media || thumbOnly(a))
	short := !hasText && noMedia
	side := l.dp(106)
	if short {
		side = l.dp(80)
	}
	bw := l.dp(1)
	cardT, bi, hi := l.v3Frame(a, true)
	h := side + 2*bw
	leftW := w - 2*bw - side
	pad := l.dp(12)
	cx, cw := x+bw+pad, leftW-2*pad

	// lay out at the top, then center the column in the left box
	mark := l.mark()
	cy := y + bw + pad
	emoji := l.dp(16) // emojiSize SMALL in v3
	cy += l.v3TitleRow(a, title, "", cx, cy, cw, cardT, false, narrow, emoji)
	textShown := hasText && !narrow
	if textShown {
		cy += l.v3TextFlow(text, cx, cy, cw, 2, emoji)
	}
	ft := firstNonEmpty(richtext.SpansText(a.Footer), a.ServiceName, a.AuthorName)
	fi := firstNonEmpty(a.FooterIcon, a.AuthorIcon, a.ServiceIcon)
	if ft != "" || fi != "" || a.IsAppUnfurl {
		if textShown {
			cy += l.dp(4)
		}
		cy += l.footerRow(a, fi, ft, cx, cy, cw, false)
	}
	used := cy - (y + bw + pad)
	if dy := (side - 2*pad - used) / 2; dy != 0 {
		l.shiftSince(mark, dy)
	}
	// the left box's border, the right box
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+bw+leftW-bw, y+bw, x+bw+leftW, y+bw+side), fill: p.Border, target: -1})
	rr := l.dp(12) - bw
	rb := image.Rect(x+bw+leftW, y+bw, x+bw+leftW+side, y+bw+side)
	l.v3Side(a, rb, [4]int{0, rr, rr, 0}, cardT)
	l.v3Close(a, bi, hi, cardT, image.Rect(x, y, x+w, y+h))
	wh := min(h+l.dp(8), l.dp(114))
	if short {
		wh = min(h+l.dp(8), l.dp(88))
	}
	// MessageAttachmentDefaultV3's tO: HoverCardTrigger wraps the row card
	// when it has text, a thumb alone, media or fields (unfurl_hover.go)
	if c := &l.d.cards[len(l.d.cards)-1]; c.card == cardT {
		c.blk, c.wrap = b, image.Rect(x, y, x+w, y+wh)
		c.popover = !blocksBlank(v3Text(a)) || thumbOnly(a) || media || len(b.Fields) > 0
	}
	return wh
}

// v3Side fills the generic card's right box.
func (l *lay) v3Side(a *richtext.Attachment, r image.Rectangle, radii [4]int, cardT int16) {
	var img *richtext.Image
	switch {
	case a.Image != nil:
		img = a.Image
	case a.VideoHTML && a.Thumb != nil:
		img = a.Thumb
	case thumbOnly(a):
		img = a.Thumb
	}
	if img == nil {
		if !hasMedia(a) {
			l.noImage(r, radii)
		}
		return
	}
	if !thumbOnly(a) && !mediaExpanded() {
		return // the collapsed ExpandableContainer: an empty box
	}
	tg := cardT // the thumb and a video's poster: the card's link
	if img == a.Image {
		tg = l.attImage(img, r)
	}
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, radii: radii, target: tg, px: max(r.Dx(), r.Dy())})
}

// noImage is no-image-c399e8e.png as a cover background: #eaeaea with
// the gray (#9a9a9f) picture glyph, 40x36 of its 200px.
func (l *lay) noImage(r image.Rectangle, radii [4]int) {
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, fill: rgb(234, 234, 234), target: -1, perCorner: true, corners: radii})
	s := r.Dx() * 40 / 200
	c := r.Min.Add(r.Max).Div(2)
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(c.X-s/2, c.Y-s/2, c.X-s/2+s, c.Y-s/2+s), url: iconURL + "image-filled", target: -1, px: s, tint: rgb(154, 154, 159)})
}

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return strings.TrimPrefix(p.Hostname(), "www.")
	}
	return u
}

// ---------- the fixed-layout cards ----------

// fixedCard is the column cards (.c-message_attachment__body--fixed_layout):
// social (TwitterX/Bluesky), video service, music service and Flickr. The
// left box (padding 12, max-height 208) holds the title row, the text (6
// lines) and the footer; the media sits under it, 100% x 200 (cover).
func (l *lay) fixedCard(b *richtext.Block, x, y, w int) int {
	a := b.Att
	k := kindOf(b)
	bw := l.dp(1)
	pad := l.dp(12)
	cardT, bi, hi := l.v3Frame(a, k == akSocial || k == akFlickr)
	cx, cw := x+bw+pad, w-2*bw-2*pad
	cy := y + bw + pad
	text := v3Text(a)
	var title []richtext.Span
	author, footIcon, footText := "", "", a.ServiceName
	titleT := cardT
	switch k {
	case akSocial:
		// iR.zy: title || service_name; iR.IJ: author_name || author_subname
		// (bluesky: author_subname); footer "x.com" / "bsky.app" with
		// Slack's glyph.
		title = a.Title
		if len(title) == 0 {
			title = []richtext.Span{{Kind: richtext.Text, Text: a.ServiceName}}
		}
		author = firstNonEmpty(a.AuthorName, a.AuthorSubname)
		footIcon, footText = "brand-x-filled", "x.com"
		if a.ServiceName == "bluesky" {
			author = a.AuthorSubname
			footIcon, footText = "brand-bluesky-filled", "bsky.app"
		}
	default:
		title, author = a.Title, a.AuthorName
		if link := firstNonEmpty(a.TitleLink, a.OriginalURL); link != "" && k != akFlickr {
			// AttachmentTitle's link (HbXx): underline links reaches it
			titleT = l.d.addTarget(target{kind: tLink, url: link, underline: false, website: true})
		}
		footIcon = a.ServiceIcon
		if footIcon == "" {
			switch {
			case a.ServiceName == "YouTube":
				footIcon = "brand-youtube-filled"
			case k == akMusic:
				footIcon = "music"
			case k == akVideo && !strings.Contains(strings.ToLower(a.ServiceName), "wistia"):
				footIcon = "play-filled"
			}
		}
	}
	// title row (margin-bottom 4); the text: margin-top 8 in social and
	// flickr cards, padding-top 4 in the others; the footer: padding-top
	// 8 (social), 4 (flickr), else 4 after text and none right under the
	// title row (.c-message_attachment__left__title_author + footer).
	cy += l.v3TitleRow(a, title, author, cx, cy, cw, titleT, k != akSocial, false, l.dp(22))
	hasText := !blocksBlank(text)
	if hasText {
		if k == akSocial || k == akFlickr {
			cy += l.dp(8)
		} else {
			cy += l.dp(4)
		}
		cy += l.v3TextFlow(text, cx, cy, cw, 6, l.dp(22))
	}
	if footText != "" || footIcon != "" {
		switch {
		case k == akSocial:
			cy += l.dp(8)
		case hasText, k == akFlickr:
			cy += l.dp(4)
		}
		cy += l.v3Footer(footIcon, footText, cx, cy, cw)
	}
	cy += pad
	leftH := min(cy-(y+bw), l.dp(208))
	cy = y + bw + leftH
	if mr, ok := l.fixedMedia(a, k, x+bw, cy, w-2*bw, cardT); ok {
		cy = mr.Max.Y
	}
	h := cy + bw - y
	l.v3Close(a, bi, hi, cardT, image.Rect(x, y, x+w, y+h))
	return h
}

// fixedMedia is the cards' .c-message_attachment__right: AttachmentMedia
// with background-size cover and cropHeight, 100% x 200, radius 0 0 12 12,
// a base-ter hairline above it in video and flickr cards, the backup
// background (unable-to-load-error-4a330e9.png, white with a gray glyph)
// under it. Thumb-only attachments have no right box. video_html shows
// AttachmentVideo's thumb with its buttons (the play button, and the
// external link with a from_url), unless the thumb is under 120x60
// (c-message_attachment__video_thumb--small: no buttons).
func (l *lay) fixedMedia(a *richtext.Attachment, k attKind, x, y, w int, cardT int16) (image.Rectangle, bool) {
	if thumbOnly(a) || !hasMedia(a) {
		return image.Rectangle{}, false
	}
	p := &l.st.Palette
	r := image.Rect(x, y, x+w, y+l.dp(200))
	rr := l.dp(12) - l.dp(1)
	radii := [4]int{0, 0, rr, rr}
	if k == akVideo || k == akFlickr {
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y-l.dp(1), x+w, y), fill: p.Divider, target: -1, layer: 1})
	}
	// everything from here is AttachmentMedia's (the right box's hairline
	// above isn't): compBlock makes it its own component
	defer l.noteMedia(r, radii, len(l.d.frags), len(l.d.boxes), len(l.d.imgs))
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, fill: rgb(255, 255, 255), target: -1, perCorner: true, corners: radii})
	s := r.Dy() * 60 / 200
	c := r.Min.Add(r.Max).Div(2)
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(c.X-s/2, c.Y-s/2, c.X-s/2+s, c.Y-s/2+s), url: iconURL + "image", target: -1, px: s, tint: rgb(142, 141, 142)})
	if !mediaExpanded() {
		return r, true
	}
	img := a.Image
	video := img == nil && a.VideoHTML
	if video {
		img = a.Thumb
	}
	if img == nil {
		return r, true
	}
	var tg int16
	if video {
		tg = l.d.addTarget(target{kind: tLink, url: firstNonEmpty(a.FromURL, a.TitleLink, a.OriginalURL)})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	} else {
		tg = l.attImage(img, r) // the X/Bluesky, Flickr, video and music cards alike
	}
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, radii: radii, target: tg, px: max(r.Dx(), r.Dy())})
	if video && img.W >= 120 && img.H >= 60 || video && (img.W == 0 || img.H == 0) {
		l.videoButtons(r, a.FromURL != "", tg)
	}
	return r, true
}

// videoButtons is .c-message_attachment__video_buttons centered on the
// thumb: the legacy "play" and "external_link" glyphs, 3.4rem (54.4px),
// white at opacity .7 with a 0 1px 1px rgba(0,0,0,.5) shadow, each with
// padding 0 10% (of the buttons' own 54.4px-per-glyph width).
func (l *lay) videoButtons(r image.Rectangle, ext bool, tg int16) {
	g := l.dp(54.4)
	n := 1
	if ext {
		n = 2
	}
	inner := g * n
	pad := inner / 10
	c := r.Min.Add(r.Max).Div(2)
	bx := c.X - inner/2 + pad
	by := c.Y - g/2
	names := []string{"play", "external"}
	for i := 0; i < n; i++ {
		gr := image.Rect(bx, by, bx+g, by+g)
		sh := gr.Add(image.Pt(0, l.dp(1)))
		l.d.overlayIcons = append(l.d.overlayIcons,
			overlayIcon{r: sh, name: names[i], col: rgba(0, 0, 0, 0.5)},
			overlayIcon{r: gr, name: names[i], col: rgba(255, 255, 255, 0.7)})
		bx += g + 2*pad
	}
}

// ---------- MessageAttachmentImageCard ----------

// imageCardSize is AttachmentMedia's getAdjustedImageDimensions at its
// defaults (max 360x360, contain), inside the card's 358px.
func (l *lay) imageCardSize(a *richtext.Attachment) (int, int) {
	img := a.Image
	if img == nil {
		img = a.Thumb
	}
	if img == nil {
		return l.dp(356), l.dp(200)
	}
	iw, ih := adjustedDims(img.W, img.H, 360, 360)
	w, h := l.dp(float32(iw)), l.dp(float32(ih))
	if m := l.dp(356); w > m {
		h, w = h*m/w, m
	}
	if m := l.dp(356); h > m {
		w, h = w*m/h, m
	}
	return max(w, 1), max(h, 1)
}

// adjustedDims is getAdjustedImageDimensions (T7Yx): scale down to fit
// maxW x maxH (never up), floored; unknown sizes keep the max box.
func adjustedDims(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	u := 1.0
	if w > maxW || h > maxH {
		u = min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	}
	return int(float64(w) * u), int(float64(h) * u)
}

// imageCard: no border on the body; the right box has a 1px otl-sec border
// and radius 12 around the image.
func (l *lay) imageCard(a *richtext.Attachment, x, y, w int) int {
	p := &l.st.Palette
	iw, ih := l.imageCardSize(a)
	bw := l.dp(1)
	iw = min(iw, w-2*bw)
	link := firstNonEmpty(a.TitleLink, a.OriginalURL)
	cardT := l.d.addTarget(target{kind: tCard, url: link})
	r := image.Rect(x, y, x+iw+2*bw, y+ih+2*bw)
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.BorderHover, bw: bw, radius: l.dp(12), target: cardT, hover: p.BorderHover})
	hi := len(l.d.hits)
	l.d.hits = append(l.d.hits, hit{t: cardT})
	img := a.Image
	if img == nil {
		img = a.Thumb
	}
	if img != nil && mediaExpanded() {
		rr := l.dp(12) - bw
		ir := r.Inset(bw)
		tg := cardT // a thumb_url alone: the card's link
		if img == a.Image {
			tg = l.attImage(img, ir) // the image opens in the viewer, not the link
		}
		l.d.imgs = append(l.d.imgs, imgPrim{r: ir, url: img.URL, radii: [4]int{rr, rr, rr, rr}, target: tg, px: max(iw, ih), fit: true})
		if img == a.Image {
			l.imageControls(img, len(l.d.imgs)-1, ir) // AttachmentImage (animated.go)
		}
	}
	l.v3Close(a, bi, hi, cardT, r)
	return r.Dy()
}

// ---------- shifting laid-out content ----------

type layMark struct{ frags, boxes, imgs, hits, lines, slots, codes, icons int }

func (l *lay) mark() layMark {
	d := l.d
	return layMark{len(d.frags), len(d.boxes), len(d.imgs), len(d.hits), len(d.lines), len(d.slots), len(d.codes), len(d.overlayIcons)}
}

// shiftSince moves everything laid out since m down by dy.
func (l *lay) shiftSince(m layMark, dy int) {
	d := l.d
	for i := m.frags; i < len(d.frags); i++ {
		f := &d.frags[i]
		f.y += dy
		f.top += dy
		f.bot += dy
	}
	off := image.Pt(0, dy)
	for i := m.boxes; i < len(d.boxes); i++ {
		d.boxes[i].r = d.boxes[i].r.Add(off)
	}
	for i := m.imgs; i < len(d.imgs); i++ {
		d.imgs[i].r = d.imgs[i].r.Add(off)
	}
	for i := m.hits; i < len(d.hits); i++ {
		d.hits[i].r = d.hits[i].r.Add(off)
	}
	for i := m.lines; i < len(d.lines); i++ {
		d.lines[i].top += dy
		d.lines[i].bot += dy
	}
	for i := m.slots; i < len(d.slots); i++ {
		d.slots[i].r = d.slots[i].r.Add(off)
	}
	for i := m.codes; i < len(d.codes); i++ {
		d.codes[i].r = d.codes[i].r.Add(off)
	}
	for i := m.icons; i < len(d.overlayIcons); i++ {
		d.overlayIcons[i].r = d.overlayIcons[i].r.Add(off)
	}
}

// ---------- MessageAttachmentDefaultV2 ----------

// v2Card is a Block Kit attachment (renderV2Attachment): the color bar
// (.c-message_attachment__border, 4px, radius 8, base-ter unless color;
// .c-message_attachment_v2__border margin-right 12) and the body
// (padding-top 2) with the blocks; the attachment's title, text and
// fields are not drawn.
func (l *lay) v2Card(b *richtext.Block, x, y, w int) int {
	a := b.Att
	p := &l.st.Palette
	bar := l.dp(4)
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: parseColor(a.Color, p.Placeholder), radius: l.dp(8), target: -1})
	cx := x + bar + l.dp(12)
	cy := y + l.dp(2)
	h := l.blocks(a.Text[min(a.KitFrom, len(a.Text)):], cx, cy, x+w-cx, false)
	if len(b.Children) > 0 {
		h += l.dp(4) + l.blocks(b.Children, cx, cy+h+l.dp(4), x+w-cx, false)
	}
	h += l.v2Attribution(a, cx, cy+h, x+w-cx) // renderAttributionMaybe
	total := l.dp(2) + h
	l.d.boxes[bi].r = image.Rect(x, y, x+bar, y+total)
	l.removeRail(a, image.Rect(x, y, x+w, y+total), false)
	return total
}

// drawRemoves paints the remove buttons of hovered cards.
func (v *View) drawRemoves(gtx layout.Context, st *Style) {
	if !v.hovering || v.hover < 0 {
		return
	}
	for i := range v.d.removes {
		rm := &v.d.removes[i]
		if rm.rail {
			continue // drawRails (unfurl_hover.go)
		}
		if v.hover != rm.del && (v.hover < rm.card || v.hover > rm.cardTo) {
			continue
		}
		p := &st.Palette
		paint.FillShape(gtx.Ops, p.TooltipBg, clip.Ellipse{Min: rm.r.Min, Max: rm.r.Max}.Op(gtx.Ops))
		s := rm.r.Dx() * 12 / 20
		c := rm.r.Min.Add(rm.r.Max).Div(2)
		paintIconAt(gtx.Ops, "close-circle-filled", image.Rect(c.X-s/2, c.Y-s/2, c.X-s/2+s, c.Y-s/2+s), p.TooltipText)
	}
}

// textWidth is s's one-line width in fs (plain weight and face).
func (l *lay) textWidth(s string, fs *flowStyle) int {
	return int(l.measure(s, l.faceFor(fs.face, fs.size, 0)) + 0.999)
}
