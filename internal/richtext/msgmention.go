package richtext

import (
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Message permalinks, 1:1 with Slack's web client (gantry-v2, 2026-11):
//
//   - message_mention elements (a pasted message permalink the composer
//     turned into a slug) render as RichTextSlug with the "message" icon
//     and getMessageSlugAttributes' label: "{author} in #channel",
//     "{author} in 🔒private", "{author}" in a DM ("{author} with {name}"
//     for your own DM message), "{author} with {names}" in a group DM,
//     "Private message" (disabled) when the message can't be shown, and
//     "Unknown author" when the author has no name. The author is the
//     linked message's own sender, read from the store (MessageSlugger:
//     core/msg_slug.go), not the element's author_id, which only a
//     resolver that can't read messages falls back to.
//   - is_msg_unfurl attachments render as MessageAttachmentSlackMessageCard
//     (the forward_message_card layout): avatar, author, compact time,
//     getFooterTextData's context line, the message, its files. With an
//     author_id the avatar and name are the member's (MemberNamer).
//   - The author's name in both is the member's getPrefCompliantMemberName
//     (client-boot vUCu, via MemberNamer): the display name, or the real
//     name under display_real_names, never the username.

// ConvKinder is an optional Resolver extension: the kind of a
// conversation ("public", "private", "im", "mpim"), ok false when the
// viewer can't see it. Resolvers without it treat every channel as public.
type ConvKinder interface {
	ConvKind(id string) (kind string, ok bool)
}

// Namer is what MessageMentionLabel needs (Resolver and the composer's
// AtomResolver both are one). SelfID and ConvKind are used when present.
type Namer interface {
	UserName(id string) string
	ChannelName(id string) string
}

// SlugMessage is the message a message slug links to, as
// getMessageSlugAttributes (client-boot N1tw) reads it for the label.
type SlugMessage struct {
	// Author is messageAuthorName short of its fallback: the sender
	// getMsgSenderEntity (hVKV) finds, named as
	// getMessageSenderNameFromState (xbeB) names them, or a DM partner as
	// mentions name them (Conv); "" while nothing names them yet (the
	// members fetch's unknown stub has no name): "Unknown author".
	Author string
	// Conv is plainTextChannelName's own name for a DM under the
	// mentions_full_names_only_pref experiment, the partner as mentions
	// name them (N1tw's k); "" for the conversation's name.
	Conv string
	// Self: the viewer sent it (isFromCurrentUser).
	Self bool
}

// MessageSlugger is an optional Resolver extension: the message a slug
// links to, read the way getMessageSlugAttributes reads it, from the
// messages store (wClv getMessageByTimestamp), never from the element:
// its author_id is only what the composer knew when it made the slug. ok
// is false while the store doesn't hold the message (the messages
// UnknownFetchManager's unknown stub while messages.list is out: Za0J)
// and once Slack said there is none (the non-existent stub): both are
// the Unavailable variant, "Private message", disabled.
type MessageSlugger interface {
	SlugMessage(channel, ts string) (SlugMessage, bool)
}

// MessageSlugLabel is getMessageSlugAttributes' label for the message ts
// of channel as ms reads it: MessageMentionLabel's variants, named for
// the linked message's own sender.
func MessageSlugLabel(r Namer, ms MessageSlugger, channel, ts string) (label string, lock bool, pre int, off bool) {
	if channel == "" {
		return "Private message", false, 0, true
	}
	m, ok := ms.SlugMessage(channel, ts)
	if !ok {
		return "Private message", false, 0, true
	}
	return slugLabel(r, channel, m.Author, m.Conv, m.Self)
}

// MessageMentionLabel is Slack's message slug label for a message by
// author in channel, for a resolver that can't read the message itself
// (MessageSlugger): the author is the element's author_id. lock: the
// channel is private (Slack draws a lock before the name; pre is the
// label's bytes before the name). off: the message can't be shown
// ("Private message").
func MessageMentionLabel(r Namer, channel, author string) (label string, lock bool, pre int, off bool) {
	if channel == "" {
		return "Private message", false, 0, true
	}
	// the author: getMessageSenderNameFromState (4ubN), the member's
	// getPrefCompliantMemberName, "Unknown author" when it is empty
	name := ""
	if mn, ok := r.(MemberNamer); ok && author != "" {
		name, _ = mn.MemberName(author)
	} else if author != "" {
		name = strings.TrimPrefix(r.UserName(author), "@")
	}
	if name == author {
		name = ""
	}
	self := ""
	if s, ok := r.(interface{ SelfID() string }); ok {
		self = s.SelfID()
	}
	return slugLabel(r, channel, name, "", author != "" && author == self)
}

// slugLabel is the label's variants for a message in channel whose
// author reads name ("Unknown author" when it is empty): "{author}" in a
// DM ("{author} with {name}" for the viewer's own), "{author} with
// {names}" in a group DM, "{author} in 🔒name" for a private channel,
// "{author} in #name" otherwise, "Private message" for a conversation the
// viewer can't see. conv, when set, is the conversation's name.
func slugLabel(r Namer, channel, name, conv string, fromSelf bool) (label string, lock bool, pre int, off bool) {
	kind, known := "public", true
	if ck, ok := r.(ConvKinder); ok {
		kind, known = ck.ConvKind(channel)
	}
	if !known {
		return "Private message", false, 0, true
	}
	if name == "" {
		name = "Unknown author"
	}
	if conv == "" {
		conv = strings.TrimPrefix(r.ChannelName(channel), "#")
	}
	switch kind {
	case "im":
		if fromSelf {
			return name + " with " + conv, false, 0, false
		}
		return name, false, 0, false
	case "mpim":
		return name + " with " + mpimNames(conv), false, 0, false
	case "private":
		p := name + " in "
		return p + conv, true, len(p), false
	}
	return name + " in #" + conv, false, 0, false
}

// mpimNames turns a group DM's name ("mpdm-ada--bo--cy-1") into the
// member list Slack shows ("ada, bo, cy").
func mpimNames(n string) string {
	s, ok := strings.CutPrefix(n, "mpdm-")
	if !ok {
		return n
	}
	if i := strings.LastIndexByte(s, '-'); i > 0 && strings.Trim(s[i+1:], "0123456789") == "" {
		s = s[:i]
	}
	return strings.Join(strings.Split(s, "--"), ", ")
}

// messageMentionSpan converts a message_mention element: labelled for the
// linked message when the resolver can read it (MessageSlugger: core's
// conversions), else for the element's author_id. The span keeps the
// channel (Chan) and the ts (ID), so what draws it can read the label
// again as it draws (Slack's MessageMention reads it on every render),
// and the URL even when the slug is disabled, which a notification
// writes in its place (messageMentionToText's convertUnknownSlugToUrl);
// a disabled slug isn't a link (richview).
func messageMentionSpan(r Resolver, e *bk) Span {
	var label string
	var lock, off bool
	var pre int
	if ms, ok := r.(MessageSlugger); ok {
		label, lock, pre, off = MessageSlugLabel(r, ms, e.ChannelID, e.MessageTS)
	} else {
		label, lock, pre, off = MessageMentionLabel(r, e.ChannelID, e.AuthorID)
	}
	return Span{Kind: MessageMention, Text: label, ID: e.MessageTS, Chan: e.ChannelID, URL: SafeURL(e.URL),
		Lock: lock, Pre: pre, Off: off}
}

// msgCard fills att for an is_msg_unfurl attachment (Slack's
// MessageAttachmentSlackMessage in card layout).
func (c *builder) msgCard(a *slack.Attachment, att *Attachment) {
	att.MsgCard, att.IsShare = true, true
	att.AuthorID, att.MsgTS = a.AuthorID, strings.Trim(string(a.TS), `"`)
	// getAuthorDisplayName: with an author id, author_name, else the subname
	if a.AuthorID == "" && a.AuthorSubname != "" {
		att.AuthorName = a.AuthorSubname
	} else if att.AuthorName == "" {
		att.AuthorName = a.AuthorSubname
	}
	att.AuthorSubname = ""
	// the card's author is the member (renderCardAvatar,
	// renderCardAuthorName): their name and picture
	if mn, ok := c.r.(MemberNamer); ok && a.AuthorID != "" {
		if n, known := mn.MemberName(a.AuthorID); known {
			att.CardMember, att.CardAuthor, att.CardAvatar = true, n, mn.MemberAvatar(a.AuthorID)
		}
	}
	att.ShareChannelID = a.ChannelID
	att.ShareChannel = strings.TrimPrefix(a.ChannelName, "#")
	if att.ShareChannel == "" && a.ChannelID != "" {
		if n := c.r.ChannelName(a.ChannelID); n != a.ChannelID {
			att.ShareChannel = strings.TrimPrefix(n, "#")
		}
	}
	att.Color, att.Footer, att.FooterIcon = "", nil, ""
	// the message: its blocks (unfurls carry them as blocks, shares as
	// message_blocks), else its mrkdwn text
	if bs := parseBlocks(c.expanded(a.Blocks)); len(bs) > 0 {
		att.Text = c.blocks(bs)
	}
	for _, mb := range a.MessageBlocks {
		if len(att.Text) == 0 {
			att.Text = append(att.Text, c.topBlocks(mb.Message.Blocks)...)
		}
	}
	if len(att.Text) == 0 && a.Text != "" {
		att.Text = FromMrkdwn(c.r, a.Text)
	}
	var linked bool
	att.Context, att.ContextLock, linked = c.cardContext(a, att.ShareChannel)
	if linked {
		att.ContextLink = att.FromURL
	}
}

// cardContext is getFooterTextData: where the linked message is.
func (c *builder) cardContext(a *slack.Attachment, name string) (spans []Span, lock, linked bool) {
	ch := a.ChannelID
	if ch == "" {
		return []Span{{Kind: Text, Text: "From another organization"}}, false, false
	}
	kind, known := "public", true
	if ck, ok := c.r.(ConvKinder); ok {
		kind, known = ck.ConvKind(ch)
	}
	switch {
	case known && (kind == "im" || kind == "mpim"):
		return []Span{{Kind: Text, Text: "Direct Message"}}, false, true
	case !known && name == "":
		// the channel isn't one the viewer can see: Slack's
		// messageIsProbablyForwarded
		return []Span{{Kind: Text, Text: "From a private conversation"}}, false, false
	}
	chSpan := Span{Kind: Channel, Text: firstNonEmpty(name, ch), ID: ch}
	lock = kind == "private"
	switch {
	case a.IsReplyUnfurl:
		return []Span{{Kind: Text, Text: "From a thread in "}, chSpan}, lock, true
	case a.IsThreadRootUnfurl:
		return []Span{{Kind: Text, Text: "Thread in "}, chSpan}, lock, true
	}
	return []Span{{Kind: Text, Text: "Posted in "}, chSpan}, lock, true
}
