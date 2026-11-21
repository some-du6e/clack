package core

import (
	"encoding/json"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A converted message's Doc has names in it that Slack's components read
// from the store as they render: a file's uploader, a huddle's sentence
// ("Alice and Carol were in a huddle"), a message slug's label ("Alice's
// message in #x", "karim with Bob" for a DM), a work object's people and
// channels, a channel mention, a tab slug's tab title, channel name, icon
// and membership (ChannelTabSlug), a canvas or list record's title.
// Slack's selectors re-render just the components that read a member or
// a channel when it changes; a Doc is converted once and kept (the UI's
// message models, taut's view cache), so each conversion records what it
// read, and a change to a member, a conversation, a file or a message a
// slug links to is converted again only in the messages that read it
// (RefsChanged). User mentions aren't recorded: richview draws their
// names from the store as it lays them out (MemberRead), the Doc's text
// is only its fallback. A message slug is both: richview reads its label
// as it draws it (SlugRead, msg_slug.go), and the Doc's label, read from
// the store at conversion, follows the linked message's answer.

// MsgRefs are the members, conversations, files, bots and emoji a
// message's conversion was built from: every id (and emoji name) it
// asked the resolver or the store about. Messages are the messages its
// message slugs link to, as "channel/ts" (MsgKey.String: msg_slug.go).
type MsgRefs struct {
	Members, Channels, Files, Bots, Emoji []string
	Messages                              []string
}

// RefChange is what an event changed of what conversions read
// (RefsChanged).
type RefChange struct {
	Members, Channels, Files, Bots, Emoji []string
	Messages                              []string // as "channel/ts"
	// AnyMember: how messages name every member changed (the
	// display_real_names or hidden_users pref)
	AnyMember bool
	// All: something every conversion reads (the highlight words,
	// emoji_mode, experiment groups, the viewer's user groups)
	All bool
}

// Empty reports whether c changes nothing a conversion read.
func (c RefChange) Empty() bool {
	return !c.All && !c.AnyMember && len(c.Members)+len(c.Channels)+len(c.Files)+len(c.Bots)+len(c.Emoji)+len(c.Messages) == 0
}

// IDs are c's ids, every kind together (they never collide: U…/W…,
// C…/G…/D…, F…, B…; an emoji's name as ":name"; a message as
// "channel/ts"), as RefIDs gives a conversion's.
func (c RefChange) IDs() []string {
	return refIDs(c.Members, c.Channels, c.Files, c.Bots, c.Emoji, c.Messages)
}

// RefIDs are r's ids, every kind together, as RefChange.IDs gives a
// change's.
func (r *MsgRefs) RefIDs() []string {
	return refIDs(r.Members, r.Channels, r.Files, r.Bots, r.Emoji, r.Messages)
}

func refIDs(members, channels, files, bots, emoji, msgs []string) []string {
	out := make([]string, 0, len(members)+len(channels)+len(files)+len(bots)+len(emoji)+len(msgs))
	for _, l := range [][]string{members, channels, files, bots, msgs} {
		for _, id := range l {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	for _, name := range emoji {
		if id := ":" + name; !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Reads reports whether the conversion read anything c changed.
func (r *MsgRefs) Reads(c RefChange) bool {
	if c.All || c.AnyMember && len(r.Members) > 0 {
		return true
	}
	for _, p := range [...]struct{ read, changed []string }{{r.Members, c.Members}, {r.Channels, c.Channels},
		{r.Files, c.Files}, {r.Bots, c.Bots}, {r.Emoji, c.Emoji}, {r.Messages, c.Messages}} {
		for _, id := range p.changed {
			if slices.Contains(p.read, id) {
				return true
			}
		}
	}
	return false
}

func addRef(l *[]string, id string) {
	if id != "" && !slices.Contains(*l, id) {
		*l = append(*l, id)
	}
}

// RefsChanged is what ev changed of what conversions read: the member of
// a profile change, the conversation of a channel change or its tabs'
// ("conv", "conv_removed", "tabs"), the ids lookups resolved ("names":
// members, conversations, bots, canvases and lists, and the messages
// message slugs asked for, so as any kind; with none, the viewer's user
// groups, which every mention's highlight follows), a deleted message a
// slug links to ("delete"), an emoji added or removed ("emoji_changed"),
// a canvas or a list whose content changed ("canvas", "list",
// "list_record_info"),
// every member's name under a names pref and every message under one
// every Doc reads ("prefs": NamePrefs, DocPrefs), the experiment groups
// (a huddle's sentence, the emoji set). A profile change that left the
// member's names and picture alone (a status, a huddle) and a channel
// event that changed nothing its messages name (a topic, a bookmark)
// change nothing (Event.SameNames).
func RefsChanged(ev Event) RefChange {
	switch ev.Type {
	case "user":
		id := ev.UserID
		if id == "" && ev.User != nil {
			id = ev.User.ID
		}
		if id != "" && !ev.SameNames {
			return RefChange{Members: []string{id}}
		}
	case "names":
		if len(ev.Users) == 0 {
			return RefChange{All: true}
		}
		return RefChange{Members: ev.Users, Channels: ev.Users, Files: ev.Users, Bots: ev.Users, Messages: ev.Users}
	case "delete":
		if ev.Channel != "" && ev.TS != "" {
			return RefChange{Messages: []string{MsgKey{ev.Channel, ev.TS}.String()}}
		}
	case "conv", "conv_removed", "tabs":
		id := ev.Channel
		if id == "" && ev.Conv != nil {
			id = ev.Conv.ID
		}
		if id != "" && !ev.SameNames {
			return RefChange{Channels: []string{id}}
		}
	case "emoji_changed":
		names := ev.Users // a removal's
		if ev.Name != "" {
			names = append(names[:len(names):len(names)], ev.Name)
		}
		if len(names) == 0 {
			return RefChange{All: true} // a removal that named none: any message's emoji
		}
		return RefChange{Emoji: names}
	case "canvas", "list", "list_record_info":
		if ev.ID != "" {
			return RefChange{Files: []string{ev.ID}}
		}
	case "experiments":
		return RefChange{All: true}
	case "prefs":
		return RefChange{All: ev.DocPrefs, AnyMember: ev.NamePrefs && !ev.DocPrefs}
	}
	return RefChange{}
}

// StoredMessage is the store's copy of ch/ts (the timeline's, else a
// thread's) converted now, with the names and conversations as they are
// now, its reads not counting as a view's (store.Peek). false when the
// store doesn't hold it.
func (a *App) StoredMessage(ch, ts string) (Message, bool) {
	m, ok := a.S.Find(ch, ts)
	if !ok {
		return Message{}, false
	}
	return a.messageWith(ch, &m, true), true
}

// refNames is msgNames recording every member, conversation and file the
// conversion asks about. It overrides only methods the store has, so it
// satisfies exactly the resolver interfaces msgNames does. With peek its
// member reads go through store.Peek.
type refNames struct {
	msgNames
	app  *App
	peek bool
	refs MsgRefs
}

func (r *refNames) member(id string) { addRef(&r.refs.Members, id) }
func (r *refNames) file(id string)   { addRef(&r.refs.Files, id) }

// BotName records the bot (an attachment's, a workflow's), whose name a
// "names" event may resolve later.
func (r *refNames) BotName(id string) string {
	addRef(&r.refs.Bots, id)
	return r.msgNames.BotName(id)
}

// Emoji records the emoji's name, known or not: an emoji added later
// ("emoji_changed") draws in the messages that used it. And the names an
// alias resolves through: a change to its target (removed, its picture
// replaced) draws in them too.
func (r *refNames) Emoji(name string) (string, string, bool) {
	addRef(&r.refs.Emoji, name)
	if r.app != nil && r.app.S != nil {
		for _, n := range r.app.S.CustomEmoji.AliasChain(name) {
			addRef(&r.refs.Emoji, n)
		}
	}
	return r.msgNames.Emoji(name)
}

// channel records conversation id, and for a DM its partner, whose name
// is the DM's (Store.ChannelName).
func (r *refNames) channel(id string) {
	addRef(&r.refs.Channels, id)
	c, ok := r.Conv(id)
	if !ok {
		c, ok = r.ExtConv(id)
	}
	if ok && c.IsIM {
		r.member(c.User)
	}
}

func (r *refNames) UserName(id string) string {
	r.member(id)
	if r.peek {
		return store.Peek{Store: r.Store}.UserName(id)
	}
	return r.msgNames.UserName(id)
}

func (r *refNames) MemberName(id string) (string, bool) {
	r.member(id)
	if r.peek {
		return store.Peek{Store: r.Store}.MemberName(id)
	}
	return r.msgNames.MemberName(id)
}

func (r *refNames) MemberAvatar(id string) string {
	r.member(id)
	if r.peek {
		return store.Peek{Store: r.Store}.MemberAvatar(id)
	}
	return r.msgNames.MemberAvatar(id)
}

func (r *refNames) UserAvatar(id string) string {
	r.member(id)
	if r.peek {
		return store.Peek{Store: r.Store}.UserAvatar(id)
	}
	return r.msgNames.UserAvatar(id)
}

func (r *refNames) MemberRealName(id string) string {
	r.member(id)
	if r.peek {
		return store.Peek{Store: r.Store}.MemberRealName(id)
	}
	return r.msgNames.MemberRealName(id)
}

func (r *refNames) ChannelName(id string) string {
	r.channel(id)
	if r.peek {
		return store.Peek{Store: r.Store}.ChannelName(id)
	}
	return r.msgNames.ChannelName(id)
}

func (r *refNames) ChannelAccess(id string) richtext.ChannelAccess {
	r.channel(id)
	return r.msgNames.ChannelAccess(id)
}

func (r *refNames) ConvKind(id string) (string, bool) {
	r.channel(id)
	return r.msgNames.ConvKind(id)
}

// ChannelTabSlug records the tab's channel and the file a canvas or list
// tab is titled by.
func (r *refNames) ChannelTabSlug(ch, tab string) (richtext.TabSlug, bool) {
	r.channel(ch)
	c, ok := r.Conv(ch)
	if !ok {
		c, ok = r.ExtConv(ch)
	}
	if ok {
		if t, found := findSlugTab(c.Properties, tab); found {
			r.file(t.Data.FileID)
		}
	}
	if r.app == nil {
		return r.msgNames.ChannelTabSlug(ch, tab)
	}
	name := r.app.S.ChannelName
	if r.peek {
		name = store.Peek{Store: r.Store}.ChannelName
	}
	return r.app.channelTabSlug(ch, tab, name)
}

func (r *refNames) HuddleTextOpts(ch string) richtext.HuddleTextOpts {
	r.channel(ch)
	return r.msgNames.HuddleTextOpts(ch)
}

func (r *refNames) FileChannel(ch string) (richtext.FileChannel, bool) {
	r.channel(ch)
	return r.msgNames.FileChannel(ch)
}

func (r *refNames) CanvasRef(fileID string) (richtext.CanvasRef, bool) {
	r.file(fileID)
	return r.msgNames.CanvasRef(fileID)
}

func (r *refNames) CanvasSection(fileID, sectionID string) (json.RawMessage, bool) {
	r.file(fileID)
	return r.msgNames.CanvasSection(fileID, sectionID)
}

func (r *refNames) CanvasPreview(fileID string) ([]richtext.Block, bool) {
	r.file(fileID)
	return r.msgNames.CanvasPreview(fileID)
}

func (r *refNames) ListRecordName(fileID, recordID string) (json.RawMessage, string, bool) {
	r.file(fileID)
	return r.msgNames.ListRecordName(fileID, recordID)
}

// ---- which events change what conversions read ----

// docState is what conversions read of members, conversations and the
// prefs, as the last event about each left it (noteDocChange,
// noteUserDoc): a change that reached the store without an event (a
// member a search or the composer's @ list stored anew, prefs a boot
// installed) shows at the next event about it. The members' was the
// store's copy just before the event, so the event after a silent
// rename looked like a status change, and Docs converted before kept the
// old name.
type docState struct {
	mu       sync.Mutex
	users    map[string]uint64 // member id → userDocFP
	convs    map[string]uint64 // conversation id → convDocFP
	prefs    string            // namePrefsKey as of the last install or prefs event
	docPrefs string            // docPrefsKey, the same
	// the keys moved at an install since the last prefs event: the next
	// one says so (installs emit none themselves)
	namesMoved, docMoved bool
}

// maxDocUsers bounds docState.users: past it the members start over (each
// one's next event converts again what names them).
const maxDocUsers = 100000

// noteUserDoc records member u's names fingerprint as of a profile
// event, reporting whether it's the one the member's last event left
// (SameNames: no conversion reads anything new of them).
func (a *App) noteUserDoc(u *slack.User) (same bool) {
	fp := userDocFP(u)
	a.docs.mu.Lock()
	defer a.docs.mu.Unlock()
	old, seen := a.docs.users[u.ID]
	if a.docs.users == nil || len(a.docs.users) >= maxDocUsers && !seen {
		a.docs.users = map[string]uint64{}
	}
	a.docs.users[u.ID] = fp
	return seen && old == fp
}

// notePrefsInstalled notes prefs just installed (users.prefs.get, the
// cache, a write): a name or Doc key they moved is reported by the next
// prefs event, whenever it comes.
func (a *App) notePrefsInstalled() {
	k, d := a.namePrefsKey(), a.docPrefsKey()
	a.docs.mu.Lock()
	if k != a.docs.prefs {
		a.docs.prefs, a.docs.namesMoved = k, true
	}
	if d != a.docs.docPrefs {
		a.docs.docPrefs, a.docs.docMoved = d, true
	}
	a.docs.mu.Unlock()
}

// prefsMoved reports whether an install moved a key since the last prefs
// event.
func (a *App) prefsMoved() bool {
	a.docs.mu.Lock()
	defer a.docs.mu.Unlock()
	return a.docs.namesMoved || a.docs.docMoved
}

// noteDocChange marks ev with what it changed of what message Docs read:
// a conv, conv_removed or tabs event that left a conversation's Doc
// fingerprint alone (a topic, a purpose, a bookmark, a tab edit Slack
// hasn't confirmed yet) is SameNames; a prefs event that changed how
// messages name people (display_real_names, hidden_users) is NamePrefs,
// and one that changed what every Doc reads (the highlight words,
// emoji_mode) DocPrefs. Core emits these from many places; emit asks here
// for each.
func (a *App) noteDocChange(ev *Event) {
	switch ev.Type {
	case "conv", "conv_removed", "tabs":
		id := ev.Channel
		if id == "" && ev.Conv != nil {
			id = ev.Conv.ID
		}
		if id == "" {
			return
		}
		fp := a.convDocFP(id)
		a.docs.mu.Lock()
		old, seen := a.docs.convs[id]
		if a.docs.convs == nil {
			a.docs.convs = map[string]uint64{}
		}
		a.docs.convs[id] = fp
		a.docs.mu.Unlock()
		ev.SameNames = seen && old == fp
	case "prefs":
		k, d := a.namePrefsKey(), a.docPrefsKey()
		a.docs.mu.Lock()
		ev.NamePrefs = a.docs.namesMoved || k != a.docs.prefs
		ev.DocPrefs = a.docs.docMoved || d != a.docs.docPrefs
		a.docs.prefs, a.docs.docPrefs = k, d
		a.docs.namesMoved, a.docs.docMoved = false, false
		a.docs.mu.Unlock()
	}
}

// convDocFP is a fingerprint of what message Docs read of conversation
// id: its name, kind, privacy, archive state, membership, DM partner and
// properties (tabs, record and file channel data).
func (a *App) convDocFP(id string) uint64 {
	c, ok := a.S.Conv(id)
	if !ok {
		if c, ok = a.S.ExtConv(id); !ok {
			return 0
		}
	}
	h := fnv.New64a()
	b := make([]byte, 0, 128+len(c.Properties))
	b = append(b, c.Name...)
	b = append(b, 0)
	b = append(b, c.User...)
	b = append(b, 0)
	for _, f := range [...]bool{c.IsChannel, c.IsGroup, c.IsIM, c.IsMPIM, c.IsPrivate, c.IsArchived, a.isMemberConv(c)} {
		b = strconv.AppendBool(b, f)
	}
	b = append(b, c.Properties...)
	_, _ = h.Write(b)
	return h.Sum64() | 1 // never 0: 0 is a conversation the store doesn't hold
}

// namePrefsKey is what of the prefs message Docs name people by.
func (a *App) namePrefsKey() string {
	return strconv.FormatBool(a.Prefs().DisplayRealNames) + "|" + a.PrefString("hidden_users")
}

// docPrefsKey is what of the prefs every message's Doc reads: the
// highlight words (richtext's highlighter) and emoji_mode (its emoji
// texter).
func (a *App) docPrefsKey() string {
	return strconv.FormatBool(a.S.EmojiAsText()) + "|" + strings.Join(a.S.HighlightWords(), "\x00")
}

// userDocFP is a fingerprint of what message Docs read of member u: how
// they name and picture them (Store.MemberName, UserName, MemberRealName,
// MemberAvatar, UserAvatar): the username, both real names, the display
// name, the pictures, the avatar hash and the team.
func userDocFP(u *slack.User) uint64 {
	h := fnv.New64a()
	p := &u.Profile
	for _, f := range [...]string{u.Name, u.RealName, p.DisplayName, p.RealName, p.Image48, p.Image72, p.Image192, p.Image512, p.AvatarHash, u.TeamID} {
		_, _ = h.Write([]byte(f))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}
