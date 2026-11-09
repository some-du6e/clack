package richview

// Slack component boundaries inside a message body (docs/PLUGINS-
// components.md §1.2, §1.6, §9): mention pills (MemberLink → MemberSlug,
// BaseMrkdwnChannel), attachment cards (MessageAttachment,
// BaseMessageAttachmentSlackMessage, MessageAttachmentYouTube →
// AttachmentMedia + AttachmentTitle, AttachmentMedia,
// MessageAttachmentHuddleLink, the huddle join gate) and file cards
// (MessageFile).
//
// The layout stays native and cached. Only while a plugin patches one of
// these names, or some CSS is installed, a pill's or card's primitives are
// tagged as a component: the static recording leaves them out and Layout
// draws them each frame through slackc, under their Slack classes, where a
// plugin can wrap, replace or extend them. A size the plugin's tree comes
// back with is fed into the next layout (pills: extra width; cards: extra
// height). With neither, nothing here runs.

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// layout modes: which kinds lay out as components.
const (
	modePills uint8 = 1 << iota
	modeAtts
)

var (
	richBlockClass   = slackc.Class("p-rich_text_block")
	richSectionClass = slackc.Class("p-rich_text_section")
	memberClass      = slackc.Class("c-link c-member_slug c-member_slug--link", "data-stringify-type", "mention").Tag("a")
	channelClass     = slackc.Class("c-link c-inline_channel_entity", "data-stringify-type", "mention").Tag("a")
	channelNameClass = slackc.Class("c-channel_entity__name").Tag("span")
	attachmentClass  = slackc.Class("c-message_attachment")
	shareClass       = slackc.Class("c-message_attachment__body c-message_kit__attachment_message")
	mediaClass       = slackc.Class("c-message_attachment__media")
	huddleClass      = slackc.Class("c-message_attachment__huddle")
	titleClass       = slackc.Class("c-message_attachment__title", "data-qa", "message_attachment_title").Tag("span")
	fileClass        = slackc.Class("c-file_container")
)

// compMode is the current layout mode: one atomic load per name while no
// plugin patches anything, plus the CSS engine's empty flag.
func compMode() uint8 {
	css := !slackc.Styles.Empty()
	var m uint8
	if css || slackc.Patched("MemberLink") || slackc.Patched("MemberSlug") || slackc.Patched("BaseMrkdwnChannel") {
		m |= modePills
	}
	if css || slackc.Patched("MessageAttachment") || slackc.Patched("BaseMessageAttachmentSlackMessage") ||
		slackc.Patched("AttachmentMedia") || slackc.Patched("MessageAttachmentHuddleLink") || slackc.Patched("HuddleLinkJoinGate") ||
		slackc.Patched("MessageAttachmentYouTube") || slackc.Patched("AttachmentTitle") || slackc.Patched("MessageFile") {
		m |= modeAtts
	}
	return m
}

type compKind uint8

const (
	compUser compKind = iota
	compChannel
	compAtt
	compFile  // a file card (MessageFile)
	compTitle // a YouTube card's title (AttachmentTitle), drawn by its card
)

// compBox is one component in the display list; primitives with comp ==
// its index+1 belong to it.
type compBox struct {
	kind compKind
	span richtext.Span   // pills
	att  *richtext.Block // cards
	r    image.Rectangle // its box in View coordinates
	// author is a card's author icon (imgs index+1), drawn as
	// ConnectedBaseAvatar className c-message_attachment__author_icon.
	author int
	// title is a YouTube card's title component (comps index+1).
	title int16
}

// compState is what a component's plugin tree told the layout, kept
// across relayouts of the same doc (index = comp order).
type compState struct {
	extra   int    // px the plugin's tree is wider (pills) / taller (cards) than the native
	name    string // BaseMrkdwnChannel: the plugin's channelName
	share   string // BaseMessageAttachmentSlackMessage: the plugin's channelName
	private bool   // … and its isPrivate
	title   string // MessageAttachmentYouTube: the plugin's title
	thumb   string // … and thumbUrl
	noAt    bool   // MemberSlug: the plugin's supressMentionAtSymbol
}

// compRef is the receiver of a component's props and natives.
type compRef struct {
	v  *View
	i  int
	st *Style
}

// addComp registers a component and returns its tag (index+1).
func (l *lay) addComp(c compBox) int16 {
	l.d.comps = append(l.d.comps, c)
	n := len(l.d.comps)
	for len(*l.cs) < n {
		*l.cs = append(*l.cs, compState{})
	}
	return int16(n)
}

func (l *lay) compState(tag int16) *compState { return &(*l.cs)[tag-1] }

// compBlock lays out an attachment card as a component.
func (l *lay) compBlock(b *richtext.Block, x, y, w int) int {
	tag := l.addComp(compBox{kind: compAtt, att: b})
	cs := l.compState(tag)
	if b.Att != nil && (cs.share != "" || cs.title != "" || cs.thumb != "") {
		bb, a := *b, *b.Att
		if cs.share != "" {
			a.ShareChannel = cs.share
		}
		if cs.title != "" {
			a.Title = []richtext.Span{{Kind: richtext.Text, Text: cs.title}}
		}
		if cs.thumb != "" {
			if im := a.Thumb; im != nil {
				th := *im
				th.URL = cs.thumb
				a.Thumb = &th
			} else if im := a.Image; im != nil {
				th := *im
				th.URL = cs.thumb
				a.Image = &th
			}
		}
		bb.Att = &a
		b = &bb
	}
	f0, b0, i0 := len(l.d.frags), len(l.d.boxes), len(l.d.imgs)
	l.inAtt, l.authorImg, l.titleFrags = true, 0, [2]int{}
	h := l.attachment(b, x, y, w)
	l.inAtt = false
	l.tagFrom(tag, f0, b0, i0)
	if a := l.authorImg; a > 0 {
		l.d.imgs[a-1].comp = -tag // not painted with the card
		l.d.comps[tag-1].author = a
	}
	if t := l.titleFrags; t[1] > t[0] {
		// a YouTube card's title: its own component (AttachmentTitle),
		// drawn by the card inside MessageAttachmentYouTube's tree
		tt := l.addComp(compBox{kind: compTitle})
		for i := t[0]; i < t[1]; i++ {
			l.d.frags[i].comp = tt
		}
		l.d.comps[tag-1].title = tt
	}
	l.d.comps[tag-1].r = image.Rect(x, y, x+w, y+h)
	return max(0, h+cs.extra)
}

// compFile lays out a file card as a component (MessageFile).
func (l *lay) compFile(b *richtext.Block, x, y, w int) int {
	tag := l.addComp(compBox{kind: compFile, att: b})
	cs := l.compState(tag)
	f0, b0, i0 := len(l.d.frags), len(l.d.boxes), len(l.d.imgs)
	l.inAtt = true
	h := l.file(b.File, x, y, w)
	l.inAtt = false
	l.tagFrom(tag, f0, b0, i0)
	l.d.comps[tag-1].r = image.Rect(x, y, x+w, y+h)
	return max(0, h+cs.extra)
}

// tagFrom marks the primitives added since f0/b0/i0 as component tag's.
func (l *lay) tagFrom(tag int16, f0, b0, i0 int) {
	for i := f0; i < len(l.d.frags); i++ {
		l.d.frags[i].comp = tag
	}
	for i := b0; i < len(l.d.boxes); i++ {
		l.d.boxes[i].comp = tag
	}
	for i := i0; i < len(l.d.imgs); i++ {
		l.d.imgs[i].comp = tag
	}
}

// pillRects sets each pill's box from its tagged primitives.
func (d *display) pillRects() {
	if len(d.comps) == 0 {
		return
	}
	add := func(tag int16, r image.Rectangle) {
		c := &d.comps[tag-1]
		if c.kind == compAtt || c.kind == compFile {
			return
		}
		if c.r.Empty() {
			c.r = r
		} else {
			c.r = c.r.Union(r)
		}
	}
	for i := range d.boxes {
		if b := &d.boxes[i]; b.comp != 0 {
			add(b.comp, b.r)
		}
	}
	for i := range d.frags {
		if f := &d.frags[i]; f.comp != 0 {
			add(f.comp, fragRect(f))
		}
	}
}

// layoutComps draws the components over the static content.
func (v *View) layoutComps(gtx layout.Context, st *Style) {
	if cap(v.refs) < len(v.d.comps) {
		v.refs = make([]compRef, len(v.d.comps))
	}
	v.refs = v.refs[:len(v.d.comps)]
	for i := range v.refs {
		v.refs[i] = compRef{v: v, i: i, st: st}
	}
	for i := range v.d.comps {
		c := &v.d.comps[i]
		ref := &v.refs[i]
		if c.kind == compTitle {
			continue // drawn by its card
		}
		t := op.Offset(c.r.Min).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(c.r.Dx(), v.laidW-c.r.Min.X), 1<<20)}
		var d layout.Dimensions
		switch c.kind {
		case compUser:
			d = slackc.Component(g, "MemberLink", ref, (*compRef).userProps, (*compRef).layoutUser)
		case compChannel:
			d = slackc.Component(g, "BaseMrkdwnChannel", ref, (*compRef).channelProps, (*compRef).layoutChannel)
		case compFile:
			d = slackc.Component(g, "MessageFile", ref, (*compRef).fileProps, (*compRef).layoutFile)
		default:
			if c.att.Att != nil && c.att.Att.ServiceName == "Huddle" {
				d = slackc.Component(g, "MessageAttachmentHuddleLink", ref, (*compRef).huddleProps, (*compRef).layoutHuddle)
			} else {
				d = slackc.Component(g, "MessageAttachment", ref, (*compRef).attachmentProps, (*compRef).layoutAttachment)
			}
		}
		t.Pop()
		extra := d.Size.Y - c.r.Dy()
		if c.kind == compUser || c.kind == compChannel {
			extra = d.Size.X - c.r.Dx()
		}
		if cs := &v.cs[i]; cs.extra != extra {
			cs.extra = extra
			v.dirty = true
			gtx.Execute(op.InvalidateCmd{})
		}
	}
}

func (c *compRef) box() *compBox { return &c.v.d.comps[c.i] }

// paint draws the component's own primitives at the current origin.
func (c *compRef) paint(gtx layout.Context, s slackc.Styled) layout.Dimensions {
	b := c.box()
	var fg *color.NRGBA
	// a pill is one color (its slug's); a card's text keeps each part's
	// own (css.go), e.g. its title link's
	if (b.kind == compUser || b.kind == compChannel) && s.Has("color") {
		col := s.Color(color.NRGBA{})
		fg = &col
	}
	t := op.Offset(b.r.Min.Mul(-1)).Push(gtx.Ops)
	c.v.paint(gtx.Ops, c.st, int16(c.i+1), fg)
	t.Pop()
	if b.author > 0 {
		im := &c.v.d.imgs[b.author-1]
		t := op.Offset(im.r.Min.Sub(b.r.Min)).Push(gtx.Ops)
		slackc.Component(gtx, "ConnectedBaseAvatar", c, (*compRef).authorProps, (*compRef).layoutAuthor)
		t.Pop()
	}
	return layout.Dimensions{Size: b.r.Size()}
}

func (c *compRef) authorProps() slackc.Props {
	b := c.box()
	a := b.att.Att
	px := c.v.d.imgs[b.author-1].r.Dx()
	if c.v.laidPx > 0 {
		px = int(float32(px)/c.v.laidPx + 0.5)
	}
	p := slackc.Props{"$key": c.v.opts.TS + "/" + strconv.Itoa(c.i) + "/author", "className": "c-message_attachment__author_icon",
		"size": px, "isInteractive": false, "messageTs": c.v.opts.TS}
	if id := slackUserID(a.AuthorLink); id != "" {
		p["userId"] = id
	}
	return p
}

// layoutAuthor is the card's author icon (its image; the native ignores
// a plugin's userId, the URL comes from the attachment).
func (c *compRef) layoutAuthor(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	im := &c.v.d.imgs[c.box().author-1]
	r := image.Rectangle{Max: im.r.Size()}
	if c.st.Images != nil {
		if img, ok := c.st.Images.Get(im.url, im.px); ok {
			drawImage(gtx.Ops, img, r, im.radii, true)
			return layout.Dimensions{Size: r.Size()}
		}
	}
	fillRadii(gtx.Ops, r, im.radii, c.st.Palette.Placeholder)
	return layout.Dimensions{Size: r.Size()}
}

// styled is paint under class s's box effects (Wrap only when CSS applies).
func (c *compRef) styled(gtx layout.Context, s slackc.Styled) layout.Dimensions {
	if s.Hidden() {
		return layout.Dimensions{}
	}
	if s.Active() {
		return s.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return c.paint(gtx, s) })
	}
	return c.paint(gtx, s)
}

// ---- pills ----

// userProps are MemberLink's: its child is the MemberSlug (the pill), so
// context a MemberLink patch provides reaches the slug.
func (c *compRef) userProps() slackc.Props {
	sp := &c.box().span
	return slackc.Props{"$key": sp.ID, "id": sp.ID, "className": "c-member_slug c-member_slug--link",
		"children": []any{slackc.Child("MemberSlug", c.slugProps(), c.layoutSlug)}}
}

// layoutUser is the user mention link (MemberLink) around its MemberSlug.
func (c *compRef) layoutUser(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "MemberSlug", c, (*compRef).slugProps, (*compRef).layoutSlug)
}

func (c *compRef) slugProps() slackc.Props {
	sp := &c.box().span
	return slackc.Props{"$key": sp.ID, "id": sp.ID, "supressMentionAtSymbol": false}
}

// layoutSlug is the @mention pill (MemberSlug, .c-member_slug); a plugin's
// supressMentionAtSymbol drops the "@" on the next layout.
func (c *compRef) layoutSlug(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p != nil {
		cs := &c.v.cs[c.i]
		if no := p.Bool("supressMentionAtSymbol"); no != cs.noAt {
			cs.noAt = no
			c.v.dirty = true
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	s := memberClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}

func (c *compRef) channelProps() slackc.Props {
	sp := &c.box().span
	name := strings.TrimPrefix(sp.Text, "#")
	return slackc.Props{"$key": sp.ID, "id": sp.ID, "channelName": name, "isPrivate": sp.Lock && !sp.Off, "isMember": !sp.Off,
		"isNonExistent": sp.Off, "isUnknown": name == sp.ID}
}

// layoutChannel is the #channel pill (BaseMrkdwnChannel); a plugin's
// channelName is laid out on the next frame.
func (c *compRef) layoutChannel(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if name, ok := p.Str("channelName"); ok {
		cs := &c.v.cs[c.i]
		if name == strings.TrimPrefix(c.box().span.Text, "#") {
			name = ""
		}
		if name != cs.name {
			cs.name = name
			c.v.dirty = true
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	s := channelClass.Enter(gtx, 0, 0)
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	n := channelNameClass.Enter(gtx, 0, 0)
	defer n.Leave()
	if s.Active() {
		return s.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return c.styled(gtx, n) })
	}
	return c.styled(gtx, n)
}

// ---- attachment cards ----

func (c *compRef) attachmentIndex() int {
	n := 0
	for i := 0; i < c.i; i++ {
		if b := &c.v.d.comps[i]; b.kind == compAtt && b.att.Att != nil && b.att.Att.ServiceName != "Huddle" {
			n++
		}
	}
	return n
}

func (c *compRef) attachmentProps() slackc.Props {
	a := c.box().att.Att
	o := &c.v.opts
	p := slackc.Props{"$key": o.TS + "/" + strconv.Itoa(c.i), "activeChannelId": o.Channel, "messageTs": o.TS, "attachmentIndex": c.attachmentIndex(),
		"blocksContainerContext": "message", "className": "c-message_attachment", "fromUrl": a.FromURL, "originalUrl": a.FromURL,
		"serviceName": a.ServiceName, "serviceIcon": a.ServiceIcon, "authorLink": a.AuthorLink, "authorName": a.AuthorName,
		"text": richtext.PlainText(richtext.Doc{Blocks: a.Text}), "fallback": richtext.SpansText(a.Title)}
	if id := slackUserID(a.AuthorLink); id != "" {
		p["authorId"] = id
	}
	c.mediaInto(p)
	return p
}

// slackUserID extracts U…/W… from a Slack profile link (…/team/U123).
func slackUserID(link string) string {
	i := strings.LastIndex(link, "/team/")
	if i < 0 {
		return ""
	}
	id := link[i+6:]
	if j := strings.IndexAny(id, "/?#"); j >= 0 {
		id = id[:j]
	}
	if len(id) > 1 && (id[0] == 'U' || id[0] == 'W') {
		return id
	}
	return ""
}

func (c *compRef) mediaInto(p slackc.Props) {
	a := c.box().att.Att
	if im := a.Image; im != nil {
		p["imageUrl"], p["imageWidth"], p["imageHeight"] = im.URL, im.W, im.H
	}
	if th := a.Thumb; th != nil {
		p["thumbUrl"], p["thumbWidth"], p["thumbHeight"] = th.URL, th.W, th.H
	}
	if a.VideoURL != "" {
		p["videoHtml"] = `<iframe src="` + a.VideoURL + `"></iframe>`
	}
}

func (c *compRef) hasMedia() bool {
	a := c.box().att.Att
	return a.Image != nil || a.VideoURL != "" || a.IsUnfurl && a.Thumb != nil
}

// layoutAttachment is MessageAttachment; shared messages go through
// BaseMessageAttachmentSlackMessage, media cards through AttachmentMedia.
func (c *compRef) layoutAttachment(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	s := attachmentClass.Enter(gtx, 0, 0)
	defer s.Leave()
	a := c.box().att.Att
	inner := c.layoutCard
	switch {
	case a.IsShare:
		inner = c.layoutShare
	case isYouTube(a):
		inner = c.layoutYouTubeBoundary
	case c.hasMedia():
		inner = c.layoutMediaBoundary
	}
	if s.Hidden() {
		return layout.Dimensions{}
	}
	if s.Active() {
		return s.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return inner(gtx) })
	}
	return inner(gtx)
}

func (c *compRef) layoutCard(gtx layout.Context) layout.Dimensions {
	return c.paint(gtx, slackc.Styled{})
}

func (c *compRef) layoutShare(gtx layout.Context) layout.Dimensions {
	return slackc.Component(gtx, "BaseMessageAttachmentSlackMessage", c, (*compRef).shareProps, (*compRef).layoutShareNative)
}

func (c *compRef) layoutYouTubeBoundary(gtx layout.Context) layout.Dimensions {
	return slackc.Component(gtx, "MessageAttachmentYouTube", c, (*compRef).youTubeProps, (*compRef).layoutYouTube)
}

func (c *compRef) layoutMediaBoundary(gtx layout.Context) layout.Dimensions {
	return slackc.Component(gtx, "AttachmentMedia", c, (*compRef).mediaProps, (*compRef).layoutMedia)
}

func (c *compRef) shareProps() slackc.Props {
	a := c.box().att.Att
	o := &c.v.opts
	return slackc.Props{"$key": o.TS + "/" + strconv.Itoa(c.i), "channelId": a.ShareChannelID, "channelName": a.ShareChannel,
		"isPrivate": false, "isChannelNonExistent": a.ShareChannelID != "" && a.ShareChannel == "",
		"isMessageNonExistent": false, "activeChannelId": o.Channel, "messageTs": o.TS}
}

// layoutShareNative is the shared-message card; a plugin's channelName
// (PrivateChannel's stub name) shows in its "Posted in" line.
func (c *compRef) layoutShareNative(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if name, ok := p.Str("channelName"); ok {
		a := c.box().att.Att
		cs := &c.v.cs[c.i]
		if name == a.ShareChannel {
			name = ""
		}
		if name != cs.share {
			cs.share = name
			cs.private = p.Bool("isPrivate")
			c.v.dirty = true
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	s := shareClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}

func (c *compRef) mediaProps() slackc.Props {
	o := &c.v.opts
	a := c.box().att.Att
	p := slackc.Props{"$key": o.TS + "/" + strconv.Itoa(c.i), "activeChannelId": o.Channel, "messageTs": o.TS,
		"fromUrl": a.FromURL, "originalUrl": a.FromURL, "blocksContainerContext": "message"}
	c.mediaInto(p)
	return p
}

// layoutMedia is AttachmentMedia (ClickToLoad replaces it with its own
// placeholder).
func (c *compRef) layoutMedia(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	s := mediaClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}

func (c *compRef) huddleProps() slackc.Props {
	o := &c.v.opts
	return slackc.Props{"$key": o.TS + "/" + strconv.Itoa(c.i), "huddle": slackc.Props{"channel_id": o.Channel},
		"channelId": o.Channel, "messageTs": o.TS}
}

// layoutHuddle is MessageAttachmentHuddleLink around the join gate (an
// anonymous Slack memo; Clack names it HuddleLinkJoinGate).
func (c *compRef) layoutHuddle(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return slackc.Component(gtx, "HuddleLinkJoinGate", c, (*compRef).huddleProps, (*compRef).layoutHuddleCard)
}

func (c *compRef) layoutHuddleCard(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	s := huddleClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}

// ---- YouTube cards ----

// isYouTube reports a YouTube unfurl (Slack draws MessageAttachmentYouTube).
func isYouTube(a *richtext.Attachment) bool {
	if strings.EqualFold(a.ServiceName, "YouTube") {
		return true
	}
	for _, u := range [...]string{a.FromURL, a.TitleLink} {
		u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
		for _, h := range [...]string{"www.youtube.com/", "youtube.com/", "m.youtube.com/", "music.youtube.com/", "youtu.be/"} {
			if strings.HasPrefix(u, h) {
				return true
			}
		}
	}
	return false
}

// thumbOf is a card's thumbnail URL (the image when it has no thumb).
func thumbOf(a *richtext.Attachment) string {
	if a.Thumb != nil {
		return a.Thumb.URL
	}
	if a.Image != nil {
		return a.Image.URL
	}
	return ""
}

// youTubeProps are MessageAttachmentYouTube's (the source attachment's
// title and thumbnail; the card's title is its AttachmentTitle child).
func (c *compRef) youTubeProps() slackc.Props {
	a := c.box().att.Att
	o := &c.v.opts
	p := slackc.Props{"$key": o.TS + "/" + strconv.Itoa(c.i), "messageTs": o.TS, "activeChannelId": o.Channel,
		"attachmentIndex": c.attachmentIndex(), "title": richtext.SpansText(a.Title), "titleLink": a.TitleLink,
		"fromUrl": a.FromURL, "originalUrl": a.FromURL, "serviceName": a.ServiceName, "serviceIcon": a.ServiceIcon}
	if u := thumbOf(a); u != "" {
		p["thumbUrl"] = u
	}
	if a.VideoURL != "" {
		p["videoHtml"] = `<iframe src="` + a.VideoURL + `"></iframe>`
	}
	if t := c.box().title; t > 0 {
		tr := &c.v.refs[t-1]
		p["children"] = []any{slackc.Child("AttachmentTitle", tr.titleProps(), tr.layoutTitle)}
	}
	return p
}

// layoutYouTube is MessageAttachmentYouTube: the card (through
// AttachmentMedia) with its title (AttachmentTitle) on top. A plugin's
// title and thumbUrl (DeArrow) are laid out on the next frame.
func (c *compRef) layoutYouTube(gtx layout.Context, p slackc.Props) layout.Dimensions {
	b := c.box()
	if p != nil {
		a := b.att.Att
		title, thumb := p.StrOr("title", ""), p.StrOr("thumbUrl", "")
		if title == richtext.SpansText(a.Title) {
			title = ""
		}
		if thumb == thumbOf(a) {
			thumb = ""
		}
		if cs := &c.v.cs[c.i]; cs.title != title || cs.thumb != thumb {
			cs.title, cs.thumb = title, thumb
			c.v.dirty = true
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	d := c.layoutMediaBoundary(gtx)
	if t := b.title; t > 0 {
		tb := &c.v.d.comps[t-1]
		off := op.Offset(tb.r.Min.Sub(b.r.Min)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(tb.r.Dx(), b.r.Max.X-tb.r.Min.X), 1<<20)}
		if e, ok := slackc.PropChildren(p); ok {
			slackc.LayoutElement(g, e)
		} else {
			slackc.Component(g, "AttachmentTitle", &c.v.refs[t-1], (*compRef).titleProps, (*compRef).layoutTitle)
		}
		off.Pop()
	}
	return d
}

// titleProps are AttachmentTitle's (c is the title component).
func (c *compRef) titleProps() slackc.Props {
	card := c.card()
	if card == nil {
		return nil
	}
	a := card.att.Att
	return slackc.Props{"$key": c.v.opts.TS + "/" + strconv.Itoa(c.i), "title": richtext.SpansText(a.Title), "link": a.TitleLink,
		"type": "default", "shouldLinkify": false, "suppressBreaks": true, "emojiSize": "small"}
}

// card is the card a title component belongs to.
func (c *compRef) card() *compBox {
	for i := range c.v.d.comps {
		if b := &c.v.d.comps[i]; b.title == int16(c.i+1) {
			return b
		}
	}
	return nil
}

// layoutTitle is the card's title (AttachmentTitle, .c-message_attachment__title).
func (c *compRef) layoutTitle(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	s := titleClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}

// ---- file cards ----

func (c *compRef) fileProps() slackc.Props {
	f := c.box().att.File
	o := &c.v.opts
	return slackc.Props{"$key": o.TS + "/" + strconv.Itoa(c.i), "file": slackcFile(f), "fileId": f.ID, "messageTs": o.TS,
		"channelId": o.Channel, "className": "c-message_kit__file", "viewContext": "message_pane"}
}

// slackcFile is Slack's file object for MessageFile.
func slackcFile(f *richtext.File) slackc.Props {
	p := slackc.Props{"id": f.ID, "name": f.Name, "title": f.Title, "mimetype": f.Mime, "filetype": f.Type,
		"pretty_type": f.PrettyType, "size": f.Size, "url_private": f.URL, "permalink": f.Permalink}
	if f.Thumb != nil {
		p["thumb_360"] = f.Thumb.URL
	}
	return p
}

// layoutFile is MessageFile: the file card. A plugin returning null
// collapses it (the size it got back is fed into the next layout).
func (c *compRef) layoutFile(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	s := fileClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}
