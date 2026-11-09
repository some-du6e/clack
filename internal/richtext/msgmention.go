package richtext

import (
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Message permalinks, 1:1 with Slack's web client (gantry-v2, 2026-11):
//
//   - message_mention elements (a pasted message permalink the composer
//     turned into a slug) render as RichTextSlug with the "message" icon
//     and getMessageSlugAttributes' label: "{author} in #channel",
//     "{author} in 🔒private", "{author}" in a DM ("{author} with {name}"
//     for your own DM message), "{author} with {names}" in a group DM,
//     "Private message" (disabled) when the message can't be shown, and
//     "Unknown author" when the author has no name.
//   - is_msg_unfurl attachments render as MessageAttachmentSlackMessageCard
//     (the forward_message_card layout): avatar, author, compact time,
//     getFooterTextData's context line, the message, its files.

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

// MessageMentionLabel is Slack's message slug label for a message by
// author in channel. lock: the channel is private (Slack draws a lock
// before the name; pre is the label's bytes before the name). off: the
// message can't be shown ("Private message").
func MessageMentionLabel(r Namer, channel, author string) (label string, lock bool, pre int, off bool) {
	kind, known := "public", true
	if ck, ok := r.(ConvKinder); ok {
		kind, known = ck.ConvKind(channel)
	}
	if !known || channel == "" {
		return "Private message", false, 0, true
	}
	name := ""
	if author != "" {
		name = strings.TrimPrefix(r.UserName(author), "@")
	}
	if name == "" || name == author {
		name = "Unknown author"
	}
	conv := strings.TrimPrefix(r.ChannelName(channel), "#")
	switch kind {
	case "im":
		self := ""
		if s, ok := r.(interface{ SelfID() string }); ok {
			self = s.SelfID()
		}
		if author != "" && author == self {
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

// messageMentionSpan converts a message_mention element.
func messageMentionSpan(r Resolver, e *bk) Span {
	label, lock, pre, off := MessageMentionLabel(r, e.ChannelID, e.AuthorID)
	sp := Span{Kind: MessageMention, Text: label, ID: e.MessageTS, Lock: lock, Pre: pre, Off: off}
	if !off {
		sp.URL = SafeURL(e.URL)
	}
	return sp
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
	if bs := parseBlocks(a.Blocks); len(bs) > 0 {
		att.Text = c.blocks(bs)
	}
	for _, mb := range a.MessageBlocks {
		if len(att.Text) == 0 {
			att.Text = append(att.Text, c.blocks(parseBlocks(mb.Message.Blocks))...)
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
