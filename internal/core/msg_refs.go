package core

import (
	"slices"

	"github.com/some-du6e/clack/internal/richtext"
)

// A converted message's Doc has names in it that Slack's components read
// from the store as they render: a file's uploader, a huddle's sentence
// ("Alice and Carol were in a huddle"), a message slug's label ("Alice's
// message in #x"), a work object's people and channels, a channel
// mention, a tab slug's tab title, channel name, icon and membership
// (ChannelTabSlug). Slack's selectors re-render just the components that
// read a member or channel when it changes; a Doc is converted once and
// kept (the UI's message models, taut's view cache), so each conversion
// records what it read, and a change to a member or a conversation is
// converted again only in the messages that read it (RefsChanged).

// MsgRefs are the members and conversations a message's Doc was built
// from: every id its conversion asked the resolver about.
type MsgRefs struct {
	Members, Channels []string
}

// Reads reports whether the conversion read any of members or channels.
func (r *MsgRefs) Reads(members, channels []string) bool {
	for _, id := range members {
		if slices.Contains(r.Members, id) {
			return true
		}
	}
	for _, id := range channels {
		if slices.Contains(r.Channels, id) {
			return true
		}
	}
	return false
}

func (r *MsgRefs) member(id string) {
	if id != "" && !slices.Contains(r.Members, id) {
		r.Members = append(r.Members, id)
	}
}

func (r *MsgRefs) channel(id string) {
	if id != "" && !slices.Contains(r.Channels, id) {
		r.Channels = append(r.Channels, id)
	}
}

// RefsChanged is what ev changed of what conversions read: the member of a
// profile change, the conversation of a channel change or its tabs'
// ("conv", "conv_removed", "tabs"), the ids lookups resolved ("names":
// members and conversations, among bots, canvases and list records, so as
// either). nil for anything else.
func RefsChanged(ev Event) (members, channels []string) {
	switch ev.Type {
	case "user":
		id := ev.UserID
		if id == "" && ev.User != nil {
			id = ev.User.ID
		}
		if id != "" {
			return []string{id}, nil
		}
	case "names":
		return ev.Users, ev.Users
	case "conv", "conv_removed", "tabs":
		id := ev.Channel
		if id == "" && ev.Conv != nil {
			id = ev.Conv.ID
		}
		if id != "" {
			return nil, []string{id}
		}
	}
	return nil, nil
}

// StoredMessage is the store's copy of ch/ts (the timeline's, else a
// thread's) converted now, with the names and conversations as they are
// now. false when the store doesn't hold it.
func (a *App) StoredMessage(ch, ts string) (Message, bool) {
	m, ok := a.liveMessage(ch, ts)
	if !ok {
		return Message{}, false
	}
	return *m, true
}

// refNames is msgNames recording every member and conversation the
// conversion asks about. It overrides only methods the store has, so it
// satisfies exactly the resolver interfaces msgNames does.
type refNames struct {
	msgNames
	refs MsgRefs
}

func (r *refNames) UserName(id string) string {
	r.refs.member(id)
	return r.msgNames.UserName(id)
}

func (r *refNames) MentionName(id string) string {
	r.refs.member(id)
	return r.msgNames.MentionName(id)
}

func (r *refNames) MemberName(id string) (string, bool) {
	r.refs.member(id)
	return r.msgNames.MemberName(id)
}

func (r *refNames) MemberAvatar(id string) string {
	r.refs.member(id)
	return r.msgNames.MemberAvatar(id)
}

func (r *refNames) UserAvatar(id string) string {
	r.refs.member(id)
	return r.msgNames.UserAvatar(id)
}

func (r *refNames) MemberRealName(id string) string {
	r.refs.member(id)
	return r.msgNames.MemberRealName(id)
}

func (r *refNames) ChannelName(id string) string {
	r.refs.channel(id)
	return r.msgNames.ChannelName(id)
}

func (r *refNames) ChannelAccess(id string) richtext.ChannelAccess {
	r.refs.channel(id)
	return r.msgNames.ChannelAccess(id)
}

func (r *refNames) ConvKind(id string) (string, bool) {
	r.refs.channel(id)
	return r.msgNames.ConvKind(id)
}

func (r *refNames) ChannelTabSlug(ch, tab string) (richtext.TabSlug, bool) {
	r.refs.channel(ch)
	return r.msgNames.ChannelTabSlug(ch, tab)
}

func (r *refNames) HuddleTextOpts(ch string) richtext.HuddleTextOpts {
	r.refs.channel(ch)
	return r.msgNames.HuddleTextOpts(ch)
}

func (r *refNames) FileChannel(ch string) (richtext.FileChannel, bool) {
	r.refs.channel(ch)
	return r.msgNames.FileChannel(ch)
}
