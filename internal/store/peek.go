package store

import (
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Peek is the store as the resolver of a conversion no view asked for (a
// message converted again because a member or a conversation it names
// changed). Its member reads don't count as hits: they keep nobody in the
// members cache, and a member's next profile change doesn't look drawn
// (core.Event.Seen, which the lists' drawsMember reads, is the member's
// last hit). A member it doesn't hold is asked for, as a view's read does.
type Peek struct{ *Store }

func (p Peek) peekUser(id string) (*slack.User, bool) {
	p.mu.RLock()
	u, known := p.peekLocked(id)
	p.mu.RUnlock()
	p.afterLookup(id, known)
	return u, u != nil
}

func (p Peek) UserName(id string) string { return userName(p.peekUser, id) }

func (p Peek) MemberName(id string) (string, bool) { return p.memberName(p.peekUser, id) }

func (p Peek) MemberRealName(id string) string { return memberRealName(p.peekUser, id) }

func (p Peek) MemberAvatar(id string) string { return p.memberAvatar(p.peekUser, id) }

func (p Peek) UserAvatar(id string) string { return userAvatar(p.peekUser, id) }

func (p Peek) ChannelName(id string) string { return p.channelName(id, false) }

var _ richtext.MemberNamer = Peek{}
