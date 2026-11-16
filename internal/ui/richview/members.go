package richview

import (
	"strings"
	"sync/atomic"
)

// User mentions name the member as plugins present it. Slack's mention
// (the rich text renderer's user element, MemberSlug inside MemberLink)
// reads the member from the store when it renders, so a plugin's patch
// over the members slice (Taut's and Slick's Nicknames) changes what every
// mention says; Clack resolves "@name" when it parses a message, so the
// layout asks MemberLabel for each mention instead, and lays the doc out
// again when a label it used changes.

// MemberLabel returns the name a user mention shows as plugins present
// member id (Slack's display rule over the patched member), ok false when
// no plugin changes it. The ui root installs it while a plugin runtime is
// attached; it must be cheap and never block (taut's patched views).
type MemberLabel func(id string) (name string, ok bool)

var memberLabel atomic.Pointer[MemberLabel]

// SetMemberLabel installs (or with nil removes) the label hook.
func SetMemberLabel(f MemberLabel) {
	if f == nil {
		memberLabel.Store(nil)
		return
	}
	memberLabel.Store(&f)
}

func memberLabelFn() MemberLabel {
	if p := memberLabel.Load(); p != nil {
		return *p
	}
	return nil
}

// MemberRead is Slack's getMemberById as a user mention renders
// (MemberSlug): the member's display name, or what the store has instead
// (MemberLoading while it doesn't hold the member, in which case the
// lookup asks for it: the members cache's UnknownFetchManager), and the
// layout runs again once it reads differently. Core builds message docs
// without asking for the members they mention (core/lazy_users.go), so a
// mention is fetched when a view draws it, not when its message loads;
// each read also counts as an access for the members cache, as Slack's
// render does. The ui root installs it; it must be cheap and never block.
type MemberRead func(id string) (name string, miss MemberMiss)

// MemberMiss is a mention's member when the store can't name it, as
// MemberSlug tells them apart (vrX/ isUnknownMember, isNonExistentMember):
// both draw BaseMissingMember (FToV), MissingText's bar, never the id.
type MemberMiss uint8

const (
	MemberLoaded  MemberMiss = iota // the store holds it: its name
	MemberLoading                   // the unknown stub: users/info is due or running
	MemberGone                      // users/info didn't return it (a non-existent member)
)

var memberRead atomic.Pointer[MemberRead]

// SetMemberRead installs (or with nil removes) the member read.
func SetMemberRead(f MemberRead) {
	if f == nil {
		memberRead.Store(nil)
		return
	}
	memberRead.Store(&f)
}

func memberReadFn() MemberRead {
	if p := memberRead.Load(); p != nil {
		return *p
	}
	return nil
}

// nameUse is a user mention a layout drew and the name it used ("" for
// the parsed text), or what the store had instead (miss) and whether the
// mention drew BaseMissingMember's bar for it (bar).
type nameUse struct {
	id, name string
	miss     MemberMiss
	bar      bool
}

// mentionName is what a user mention names now: the plugin's label when
// a plugin changes the member, else the member's name as the store has it
// (MemberRead, which asks for a missing one), else "" (the parsed text).
// miss is set when the store can't name the member yet (or at all).
func mentionName(id string) (string, MemberMiss) {
	if f := memberLabelFn(); f != nil {
		if n, ok := f(id); ok {
			return n, MemberLoaded
		}
	}
	if f := memberReadFn(); f != nil {
		n, miss := f(id)
		if miss != MemberLoaded {
			return "", miss
		}
		return strings.TrimPrefix(n, "@"), MemberLoaded
	}
	return "", MemberLoaded
}

// mentionText is a user mention's text: "@" and mentionName, else the
// parsed text; miss (no text) when the member is still loading or doesn't
// exist. Each use is recorded so namesChanged can tell when to lay out
// again.
func (l *lay) mentionText(id, parsed string) (string, MemberMiss) {
	if id == "" || memberLabelFn() == nil && memberReadFn() == nil {
		return parsed, MemberLoaded
	}
	n, miss := mentionName(id)
	l.d.names = append(l.d.names, nameUse{id: id, name: n, miss: miss})
	if miss != MemberLoaded {
		return "", miss
	}
	if n == "" {
		return parsed, MemberLoaded
	}
	return "@" + n, MemberLoaded
}

// namesChanged reports a mention the last layout drew that reads
// differently now (a nickname set or cleared, the member loaded, users/info
// saying there is none). It reads every drawn mention's member on each
// frame, as Slack's mentions read theirs on every render.
func (v *View) namesChanged() bool {
	changed := false
	for _, u := range v.d.names {
		if n, miss := mentionName(u.id); n != u.name || miss != u.miss {
			changed = true // the rest are still read: each is an access
		}
	}
	return changed
}

// MissingMembers lists the user mentions the last layout drew as
// BaseMissingMember's bar (their members still loading, or not found), in
// order (tests).
func (v *View) MissingMembers() []string {
	var out []string
	for _, u := range v.d.names {
		if u.bar {
			out = append(out, u.id)
		}
	}
	return out
}
