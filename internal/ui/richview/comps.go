package richview

// Slack component boundaries inside a message body (docs/PLUGINS-
// components.md §1.2, §1.6): mention pills (MemberLink, BaseMrkdwnChannel)
// and attachment cards (MessageAttachment, BaseMessageAttachmentSlackMessage,
// AttachmentMedia, MessageAttachmentHuddleLink, the huddle join gate).
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
)

// compMode is the current layout mode: one atomic load per name while no
// plugin patches anything, plus the CSS engine's empty flag.
func compMode() uint8 {
	css := !slackc.Styles.Empty()
	var m uint8
	if css || slackc.Patched("MemberLink") || slackc.Patched("BaseMrkdwnChannel") {
		m |= modePills
	}
	if css || slackc.Patched("MessageAttachment") || slackc.Patched("BaseMessageAttachmentSlackMessage") ||
		slackc.Patched("AttachmentMedia") || slackc.Patched("MessageAttachmentHuddleLink") || slackc.Patched("HuddleLinkJoinGate") {
		m |= modeAtts
	}
	return m
}

type compKind uint8

const (
	compUser compKind = iota
	compChannel
	compAtt
)

// compBox is one component in the display list; primitives with comp ==
// its index+1 belong to it.
type compBox struct {
	kind compKind
	span richtext.Span   // pills
	att  *richtext.Block // cards
	r    image.Rectangle // its box in View coordinates
}

// compState is what a component's plugin tree told the layout, kept
// across relayouts of the same doc (index = comp order).
type compState struct {
	extra   int    // px the plugin's tree is wider (pills) / taller (cards) than the native
	name    string // BaseMrkdwnChannel: the plugin's channelName
	share   string // BaseMessageAttachmentSlackMessage: the plugin's channelName
	private bool   // … and its isPrivate
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
	if cs.share != "" && b.Att != nil {
		bb, a := *b, *b.Att
		a.ShareChannel = cs.share
		bb.Att = &a
		b = &bb
	}
	f0, b0, i0 := len(l.d.frags), len(l.d.boxes), len(l.d.imgs)
	l.inAtt = true
	h := l.attachment(b, x, y, w)
	l.inAtt = false
	for i := f0; i < len(l.d.frags); i++ {
		l.d.frags[i].comp = tag
	}
	for i := b0; i < len(l.d.boxes); i++ {
		l.d.boxes[i].comp = tag
	}
	for i := i0; i < len(l.d.imgs); i++ {
		l.d.imgs[i].comp = tag
	}
	l.d.comps[tag-1].r = image.Rect(x, y, x+w, y+h)
	return max(0, h+cs.extra)
}

// pillRects sets each pill's box from its tagged primitives.
func (d *display) pillRects() {
	if len(d.comps) == 0 {
		return
	}
	add := func(tag int16, r image.Rectangle) {
		c := &d.comps[tag-1]
		if c.kind == compAtt {
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
	for i := range v.d.comps {
		c := &v.d.comps[i]
		ref := &v.refs[i]
		ref.v, ref.i, ref.st = v, i, st
		t := op.Offset(c.r.Min).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(c.r.Dx(), v.laidW-c.r.Min.X), 1<<20)}
		var d layout.Dimensions
		switch c.kind {
		case compUser:
			d = slackc.Component(g, "MemberLink", ref, (*compRef).userProps, (*compRef).layoutUser)
		case compChannel:
			d = slackc.Component(g, "BaseMrkdwnChannel", ref, (*compRef).channelProps, (*compRef).layoutChannel)
		default:
			if c.att.Att != nil && c.att.Att.ServiceName == "Huddle" {
				d = slackc.Component(g, "MessageAttachmentHuddleLink", ref, (*compRef).huddleProps, (*compRef).layoutHuddle)
			} else {
				d = slackc.Component(g, "MessageAttachment", ref, (*compRef).attachmentProps, (*compRef).layoutAttachment)
			}
		}
		t.Pop()
		extra := d.Size.Y - c.r.Dy()
		if c.kind != compAtt {
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
	if s.Has("color") {
		col := s.Color(color.NRGBA{})
		fg = &col
	}
	t := op.Offset(b.r.Min.Mul(-1)).Push(gtx.Ops)
	c.v.paint(gtx.Ops, c.st, int16(c.i+1), fg)
	t.Pop()
	return layout.Dimensions{Size: b.r.Size()}
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

func (c *compRef) userProps() slackc.Props {
	sp := &c.box().span
	return slackc.Props{"$key": sp.ID, "id": sp.ID, "className": "c-member_slug c-member_slug--link",
		"children": strings.TrimPrefix(sp.Text, "@")}
}

// layoutUser is the user mention pill (MemberLink).
func (c *compRef) layoutUser(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	s := memberClass.Enter(gtx, 0, 0)
	defer s.Leave()
	return c.styled(gtx, s)
}

func (c *compRef) channelProps() slackc.Props {
	sp := &c.box().span
	name := strings.TrimPrefix(sp.Text, "#")
	return slackc.Props{"$key": sp.ID, "id": sp.ID, "channelName": name, "isPrivate": false, "isMember": true,
		"isNonExistent": false, "isUnknown": name == sp.ID}
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
