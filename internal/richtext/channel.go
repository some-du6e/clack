package richtext

import "strings"

// Channel mentions follow Slack's BaseMrkdwnChannel (build a84662d2d135,
// gantry-v2-async-client-boot-shared lH2I) and BaseMissingChannel
// (client-boot 1x8o):
//
//   - a channel the viewer can see is a link "#name"; a private one shows
//     the lock-filled icon and its name without "#";
//   - a channel the viewer can't see (private, or on a workspace they're
//     not part of; the store has no channel object, isNonExistent) is not
//     a link: with a fallback name (<#C123|name>) it reads "#name", without
//     one Slack draws the "Private channel" chip with the lock icon and
//     the tooltip "You don't have access to this channel (ex. it's
//     private or in a workspace you're not a part of), so its name isn't
//     shown".
//
// Off marks those non-links; Lock the lock icon.

// ChannelAccess is how the viewer sees a mentioned channel.
type ChannelAccess uint8

const (
	ChannelUnresolved ChannelAccess = iota // not looked up yet (shown by id or fallback, still a link)
	ChannelVisible                         // a channel the viewer can see
	ChannelPrivate                         // a private channel the viewer is in
	ChannelHidden                          // the viewer can't see it
)

// PrivateChannelLabel is BaseMissingChannel's Nonexistent label.
const PrivateChannelLabel = "Private channel"

// ChannelAccessResolver is an optional Resolver method.
type ChannelAccessResolver interface {
	ChannelAccess(id string) ChannelAccess
}

// channelSpan is a channel mention; label is mrkdwn's fallback name.
func channelSpan(r Resolver, id, label string) Span {
	acc := ChannelUnresolved
	if a, ok := r.(ChannelAccessResolver); ok {
		acc = a.ChannelAccess(id)
	}
	label = strings.TrimPrefix(label, "#")
	switch acc {
	case ChannelHidden:
		if label != "" {
			return Span{Kind: Channel, Text: "#" + label, ID: id, Off: true}
		}
		return Span{Kind: Channel, Text: PrivateChannelLabel, ID: id, Off: true, Lock: true}
	case ChannelPrivate:
		name := strings.TrimPrefix(firstNonEmpty(r.ChannelName(id), label, id), "#")
		return Span{Kind: Channel, Text: name, ID: id, Lock: true}
	}
	name := label
	if name == "" {
		name = r.ChannelName(id)
	}
	if name == "" {
		name = id
	}
	return Span{Kind: Channel, Text: "#" + strings.TrimPrefix(name, "#"), ID: id}
}
