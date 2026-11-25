package canvasview

// MessageUnfurl (client-boot-shared ko6Y), the card Slack draws in a
// canvas for a message permalink or a message unfurl: MessageHeader (Fi8K)
// over MessageContent (VtxR). Build 889ac449a32d; the notes are in
// /root/clack-ops/scratch/link-url-box-3/cards-spec.md §5.

import (
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

var nsMessage = i18n.NS("message")

// msgCardData is what MessageUnfurl reads: the linked message from the
// store (getMessageByTimestamp: messages.list by id when it misses), and
// the views made of it.
type msgCardData struct {
	known  bool         // the store holds the message
	gone   bool         // Slack said it doesn't exist (isMessageNonExistent)
	gaveUp bool         // core's fetch of it failed (MessageGaveUp): nothing says it's gone
	asked  bool         // fetched once (ensureMessages)
	expand bool         // TruncatedBlocks' Show more was pressed
	tier   int          // the width tier last drawn (getNextContainerSize's hysteresis)
	m      core.Message // the message, when known
	key    string       // what the views below are of
	avatar string       // the sender's avatar
	name   *richview.View
	sub    *richview.View   // "channel | {date} at {time}"
	blocks []richtext.Block // the body, before TruncatedBlocks' limits
	body   *richview.View   // the body within them
	limits [4]int           // the lines, blocks, characters and Show more state body was made for
}

// readMessage reads the route's message from the store: true when it
// should be fetched (not held, not known gone, not asked yet).
func (c *linkCard) readMessage(a *core.App) bool {
	d := &c.mc
	r := c.rt
	if r.kind != "message" || r.channel == "" || r.ts == "" {
		return false
	}
	d.m, d.known = a.HeldMessage(r.channel, r.ts)
	d.gone, d.gaveUp = false, false
	if ch, ok := a.MessageChange(r.channel, r.ts); ok && ch.Deleted {
		d.known, d.gone = false, true
	}
	if !d.known && !d.gone {
		d.gaveUp = a.MessageGaveUp(r.channel, r.ts)
	}
	if !d.known {
		d.m = core.Message{}
	}
	return !d.known && !d.gone && !d.asked
}

// decideMessage is MessageUnfurl's own order: the skeleton while the
// container unfurls and the message isn't known; with neither the message
// nor an attachment, EmptyUnfurl's msg_non_existent once Slack said there
// is no such message (isMessageNonExistent), the skeleton while it's
// being fetched (Card's wait ends it), and EmptyUnfurl's plain line when
// the fetch failed (nothing says it's gone); else the card (from the
// attachment when the message never arrives).
func (c *linkCard) decideMessage(loading bool, att *slack.Attachment) (cardKind, string) {
	d := &c.mc
	if loading && !d.known {
		return cardMessageLoading, ""
	}
	if !d.known && att == nil {
		switch {
		case d.gone:
			return cardEmpty, emptyLine("msg_non_existent")
		case d.gaveUp || c.rt.channel == "" || c.rt.ts == "":
			return cardEmpty, emptyLine("")
		}
		return cardMessageLoading, ""
	}
	return cardMessage, ""
}

// messageViews makes the header and body views of the message card.
func (v *View) messageViews(c *linkCard) {
	a := v.h.Core()
	if a == nil {
		return
	}
	d := &c.mc
	r := c.rt
	var att *slack.Attachment
	if c.held && len(c.msg.Attachments) > 0 {
		att = &c.msg.Attachments[0]
	}
	// the author (from the attachment only when it unfurled the message
	// itself: from_url === original_url), read every frame: a sender who
	// loads after the card fills in (getMemberById's stub, then users.info)
	user, name, avatar := d.m.User, "", ""
	sameURL := att != nil && att.FromURL == att.OriginalURL
	if d.known {
		if user != "" {
			name = a.S.UserName(user)
			if u, ok := a.User(user); ok {
				avatar = u.Avatar
			}
		}
		name, avatar = firstNonEmpty(name, d.m.Name), firstNonEmpty(avatar, d.m.Avatar)
	} else if att != nil && sameURL {
		user = att.AuthorID
		name = att.AuthorName
		if user != "" {
			name = firstNonEmpty(a.S.UserName(user), name)
			if u, ok := a.User(user); ok {
				avatar = u.Avatar
			}
		}
		avatar = firstNonEmpty(avatar, att.AuthorIcon)
	}
	if user != "" {
		v.askCardUser(user)
	}
	key := strings.Join([]string{c.attKey, d.m.TS, d.m.EditedTS, d.m.Text, d.m.User, d.m.Name, d.m.Avatar, name, avatar,
		boolStr(d.known), boolStr(d.expand)}, "\x00")
	if key == d.key && d.sub != nil {
		return
	}
	d.key = key
	para := func(sp ...richtext.Span) richtext.Doc {
		return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: sp}}}
	}
	d.avatar = avatar
	d.name = nil
	if name != "" {
		d.name = richview.New(para(richtext.Span{Kind: richtext.Text, Text: name, Style: richtext.Bold}), richview.Options{MaxLines: 1})
	}
	// the secondary line: the channel ("Thread in …" for a reply), then the
	// full timestamp when the message is known, both linked to it
	link := v.messageLink(r.channel, r.ts, firstNonEmpty(d.m.ThreadTS, r.threadTS))
	var sp []richtext.Span
	chName := ""
	if cv, ok := a.Conv(r.channel); ok {
		switch cv.Kind {
		case "im":
			chName = nsMessage.T("Direct Message")
		default:
			chName = cv.Name
		}
	} else if att != nil {
		chName = att.ChannelName
	}
	if chName != "" {
		if thread := firstNonEmpty(d.m.ThreadTS, r.threadTS); d.known && thread != "" && thread != r.ts {
			chName = nsMessage.T("Thread in {channelName}", "channelName", chName)
		}
		if d.known && link != "" {
			sp = append(sp, richtext.Span{Kind: richtext.Link, Text: chName, URL: link})
		} else {
			sp = append(sp, richtext.Span{Kind: richtext.Text, Text: chName})
		}
	}
	if d.known {
		if full := richview.FullTimestamp(r.ts, time.Now()); full != "" {
			if len(sp) > 0 {
				sp = append(sp, richtext.Span{Kind: richtext.Text, Text: " | "})
			}
			if link != "" {
				sp = append(sp, richtext.Span{Kind: richtext.Link, Text: full, URL: link})
			} else {
				sp = append(sp, richtext.Span{Kind: richtext.Text, Text: full})
			}
		}
	}
	d.sub = richview.New(para(sp...), richview.Options{MaxLines: 1})
	// the body: the message's own blocks or text, else the attachment's
	var blocks []richtext.Block
	if d.known {
		var atts []richtext.Block
		for _, b := range d.m.Doc.Blocks {
			if b.Kind == richtext.AttachmentBlock {
				atts = append(atts, b)
				continue
			}
			blocks = append(blocks, b)
		}
		if len(blocks) == 0 && len(atts) > 0 { // its first attachment (XFqE.dK)
			b := atts[0]
			if b.Att != nil {
				x := *b.Att
				x.HideBorder = true
				b.Att = &x
			}
			blocks = []richtext.Block{b}
		}
	} else if att != nil {
		m := c.msg
		m.Text, m.Blocks, m.Files = "", nil, nil
		m.Attachments = m.Attachments[:1]
		for _, b := range richtext.FromMessage(a.S, &m).Blocks {
			if b.Kind == richtext.AttachmentBlock && b.Att != nil {
				blocks = append(blocks, b.Att.Text...)
			}
		}
		if len(blocks) == 0 && att.Fallback != "" {
			blocks = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: att.Fallback}}}}
		}
	}
	d.blocks, d.body, d.limits = blocks, nil, [4]int{}
}

// messageLink is the message's archives permalink on the team's host, which
// the canvas's link handler routes to the conversation, or a reply's thread.
func (v *View) messageLink(ch, ts, thread string) string {
	a := v.h.Core()
	if a == nil || ch == "" || ts == "" || a.S.TeamDomain == "" {
		return ""
	}
	u := "https://" + a.S.TeamDomain + ".slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
	if thread != "" && thread != ts {
		u += "?thread_ts=" + thread + "&cid=" + ch
	}
	return u
}

// cardLimits are MessageContent's TruncatedBlocks limits by width tier
// (under 300px, under 500, else): lines, blocks, characters; with
// interactive blocks (actions, a section with a button) the larger set.
var cardLimits = [2][3][3]int{
	{{2, 2, 190}, {4, 3, 300}, {9, 4, 680}},
	{{8, 5, 400}, {12, 7, 600}, {16, 9, 900}},
}

// cardTier is the content width's tier: 0 under 300px, 1 under 500, else
// 2, moving up from prev (-1: none yet) only once the width is 40px past
// the next threshold (getNextContainerSize).
func cardTier(gtx layout.Context, w, prev int) int {
	thr := [2]int{gtx.Dp(300), gtx.Dp(500)}
	t := 0
	for t < 2 && w >= thr[t] {
		t++
	}
	for prev >= 0 && t > prev && w < thr[t-1]+gtx.Dp(40) {
		t--
	}
	return t
}

// interactiveBlocks: blocks with an actions block or a section with an
// element accessory (not an image), which take the larger limits.
func interactiveBlocks(bs []richtext.Block) bool {
	for _, b := range bs {
		if b.Kind == richtext.Actions || b.Kind == richtext.Section && len(b.Buttons) > 0 {
			return true
		}
	}
	return false
}

// cardExpandURL marks a card's Show more / Show less link (never a real
// URL): the view's link handler toggles the card's body (toggleCardExpand).
const cardExpandURL = "clack-card-expand:"

// truncatedBody is TruncatedBlocks over bs: the first blocks of the limit,
// cut at the characters and lines (richview.TruncateBlocks), with " Show
// more" when anything was cut; expanded, all of them with "Show less".
func truncatedBody(bs []richtext.Block, lim [3]int, expand bool, link string) []richtext.Block {
	short := bs[:min(lim[1], len(bs))]
	short, cut := richview.TruncateBlocks(short, lim[2], lim[0])
	cut = cut || len(bs) > lim[1]
	if !cut {
		return bs
	}
	if expand {
		return withCardExpander(append([]richtext.Block(nil), bs...), false, link)
	}
	return withCardExpander(short, true, link)
}

// withCardExpander adds the " Show more" / "Show less" link: after the
// last block's text, or in a paragraph of its own.
func withCardExpander(bs []richtext.Block, more bool, u string) []richtext.Block {
	label := "Show less" // richview's expander labels (truncate.go)
	if more {
		label = "Show more"
	}
	link := richtext.Span{Kind: richtext.Link, Text: label, URL: u}
	if more && len(bs) > 0 {
		last := &bs[len(bs)-1]
		switch last.Kind {
		case richtext.Paragraph, richtext.Section, richtext.Quote, richtext.Context:
			if len(last.Children) == 0 {
				last.Spans = append(append(last.Spans[:len(last.Spans):len(last.Spans)], richtext.Span{Kind: richtext.Text, Text: " "}), link)
				return bs
			}
		}
	}
	return append(bs, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{link}})
}

// layoutMessage draws MessageUnfurl w wide and returns its height: the
// header padded 12 (the 36px avatar, 8 from the sender's name in 15px
// Black, under it the 13px secondary line in foreground_max_solid; no
// avatar in the compact display), then the content padded 0 12 12.
func (v *View) layoutMessage(gtx layout.Context, c *linkCard, w int) int {
	d := &c.mc
	pad, av := gtx.Dp(12), gtx.Dp(36)
	x := pad
	dense := uipref.Get().Dense
	if !dense {
		if d.avatar != "" {
			if img, ok := v.h.Images().Get(d.avatar, av*2); ok {
				func() {
					defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
					theme.Image(gtx, img, image.Pt(av, av), gtx.Dp(8))
				}()
			} else {
				theme.RRect(gtx, image.Rect(pad, pad, pad+av, pad+av), gtx.Dp(8), v.styles.card.Palette.Missing)
			}
		} else { // MissingIcon 36
			theme.RRect(gtx, image.Rect(pad, pad, pad+av, pad+av), gtx.Dp(8), v.styles.card.Palette.Missing)
		}
		x += av + gtx.Dp(8)
	}
	tw := max(0, w-x-pad)
	y := pad - gtx.Dp(4) // the gutter's right column starts 4px above the padding
	place := func(rv *richview.View, st *richview.Style) {
		if rv == nil {
			return
		}
		func() {
			defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Constraints.Max.Y)}
			y += rv.Layout(g, st, v.handler).Size.Y
		}()
	}
	if d.name != nil {
		place(d.name, v.styles.msgName)
	} else { // a non-existent MissingText for an unknown sender
		theme.RRect(gtx, image.Rect(x, y+gtx.Dp(7), x+gtx.Dp(120), y+gtx.Dp(15)), gtx.Dp(4), cardInks(v.h.Theme().Dark).lowSolid)
		y += gtx.Dp(22)
	}
	place(d.sub, v.styles.msgSub)
	h := max(y, pad+av*boolInt(!dense)) + pad
	if len(d.blocks) > 0 {
		bw := max(0, w-2*pad)
		if d.body == nil {
			d.tier = -1 // a new body: no tier to hold to
		}
		d.tier = cardTier(gtx, bw, d.tier)
		lim := cardLimits[boolInt(interactiveBlocks(d.blocks))][d.tier]
		if key := [4]int{lim[0], lim[1], lim[2], boolInt(d.expand)}; d.body == nil || d.limits != key {
			bs := truncatedBody(d.blocks, lim, d.expand, cardExpandURL+c.url)
			d.body = richview.New(richtext.Doc{Blocks: bs}, richview.Options{})
			d.limits = key
		}
		func() {
			defer op.Offset(image.Pt(pad, h)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(bw, gtx.Constraints.Max.Y)}
			h += d.body.Layout(g, v.styles.card, v.handler).Size.Y
		}()
		h += pad
	}
	return h
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// layoutMessageSkeleton is LoadingMessageUnfurl: a 36px MissingIcon and two
// bars (120 and 80) beside it, padded 12, then four full-width bars padded
// 0 12 12, shimmering.
func (v *View) layoutMessageSkeleton(gtx layout.Context, w int, now time.Time) int {
	col := v.skeletonInk(gtx, now)
	pad, av, lh := gtx.Dp(12), gtx.Dp(36), gtx.Dp(8)
	theme.RRect(gtx, image.Rect(pad, pad, pad+av, pad+av), gtx.Dp(8), col)
	tx := pad + av + gtx.Dp(12)
	gap := (av - 2*lh) / 2 // justify-content: space-around
	theme.RRect(gtx, image.Rect(tx, pad+gap/2, tx+gtx.Dp(120), pad+gap/2+lh), lh/2, col)
	theme.RRect(gtx, image.Rect(tx, pad+gap/2+lh+gap, tx+gtx.Dp(80), pad+gap/2+2*lh+gap), lh/2, col)
	y := pad + av + pad
	for i := 0; i < 4; i++ {
		y += gtx.Dp(8)
		theme.RRect(gtx, image.Rect(pad, y, w-pad, y+lh), lh/2, col)
		y += lh + gtx.Dp(8)
	}
	return y + pad
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// toggleCardExpand shows the whole body of the message cards for url, or
// their truncated one again (TruncatedBlocks' isExpanded).
func (v *View) toggleCardExpand(url string) {
	for _, b := range cardBlocks(v.blocks) {
		if c := b.card; c.url == url {
			c.mc.expand = !c.mc.expand
		}
	}
	v.h.Invalidate()
}
