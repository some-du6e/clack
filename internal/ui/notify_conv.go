package ui

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/views"
)

// What a notification names, as Slack's web client reads it (CDN build
// f132f0e78468, 2026-11-14; chunks fetched without a session):
//
//   - maybeShowNotification (boot-render) takes the message's channel from
//     the store (gT hasChannel ? OL getChannelById : undefined) and goes on
//     only for a channel the viewer is in (JUrK bl isMemberOfChannel:
//     is_member); any other is reason 2, nothing shown.
//   - getNotificationTitle (boot-shared, growl) names it with
//     getDisplayNameForChannel(channel, {imIncludeAtSign: false})
//     (client-boot): "#name" for a public channel, a private channel's
//     bare name (privateChannelUseHashSign is off by default), a group
//     DM's members, a DM's partner. A channel without a name goes through
//     formatChannelName (ZuZy), whose fallback is "unknown-channel"
//     (message): the id never shows.
//   - getContentForNotification's text (extractTextFromMessage, the
//     message-to-text pass) writes a channel mention with channelToText
//     (client-boot): a channel the store doesn't have (isNonExistent: no
//     object) or a private one the viewer isn't in is "#private-channel"
//     (channel_token); any other is formatChannelName over its
//     getDisplayNameForChannel name, "#name" ("#unknown-channel" without
//     one).
//
// Clack's sidebar model (b.m.Convs) isn't that store: it holds the
// sidebar's rows (no archived conversations, no file channels a section
// doesn't list) and is rebuilt 150 ms after core changes. So a
// conversation core had just learned (a private channel the viewer was
// added to, the conversation core looked up for the message itself, a
// canvas's comment channel) wasn't in it: its notification was titled
// after the sender, and a followed thread's reply wasn't shown. And a
// name the store doesn't know reads as the raw id (store.ChannelName's
// fallback): a mentioned channel nobody looked up yet. A public
// channel's name is usually still known (meta.json keeps the names of
// channels seen in search, browse and mentions); a private channel's
// lives only on its conversation object, and one the viewer isn't in has
// no name to learn (edge channels/info lists it as failed). So
// notifications read core's conversations, and when what they'd name
// isn't named yet they look it up first (core.ResolveConv), and show
// Slack's fallback texts, never the id.

// noteResolveWait bounds the lookups a notification waits for.
const noteResolveWait = 5 * time.Second

// noteResolveMax is how many mentioned channels one notification looks
// up (the rest read "#private-channel" until core's resolver names them).
const noteResolveMax = 8

// noteMembersMax is how many people one notification waits for: one
// users/info batch (EnsureUsers asks 100 at a time), so a message naming
// many people names them all (before α.110, onMessage fetched every
// mention before converting it).
const noteMembersMax = 100

// noteUnknownChannel is formatChannelName's name for a channel without
// one; noteHiddenChannel is channelToText's for a channel the viewer
// can't see.
func noteUnknownChannel() string { return i18n.T("message", "unknown-channel") }
func noteHiddenChannel() string  { return i18n.T("channel_token", "private-channel") }

// noteNamed reports whether c has a name to show: a DM its partner's, a
// channel's not empty and not the raw id the store falls back to.
func noteNamed(c *convInfo) bool {
	if c == nil {
		return false
	}
	n := strings.TrimSpace(c.Name)
	return n != "" && n != c.ID && (c.User == "" || n != c.User)
}

// noteConv is the conversation a notification is about: the sidebar
// model's row when it names it, else core's (every conversation the
// viewer is in, the file channels the sidebar leaves out, the ones
// resolved outside the sidebar). named: its name is known. nil: core
// doesn't know it.
func (b *backend) noteConv(ch string) (c *convInfo, named bool) {
	if c = b.m.conv(ch); c != nil && noteNamed(c) {
		if b.core == nil {
			return c, true
		}
		switch c.Kind {
		case "im", "app":
			// a DM is named for its partner under the viewer's
			// display_real_names (getDisplayNameForChannel's
			// getPrefCompliantMemberNameById), the sidebar's row for their
			// display name
			if n, ok := b.core.S.PrefNameHeld(c.User); ok && n != "" && n != c.Name {
				ci := *c
				ci.Name = n
				return &ci, true
			}
		case "mpim":
			// a group DM is named for its members as the store holds them
			// now (the toast waited for them: noteMembers), not as the
			// sidebar's snapshot did: a closed group DM's members are never
			// read for the sidebar, so its row named only those held then.
			// None known: "" (getMpimDisplayNameAlphabetical leaves out
			// every member it can't name; the title reads "New message in ")
			if v, ok := b.core.Conv(ch); ok {
				ci := *c
				ci.Name, _ = b.noteMpimName(&v)
				return &ci, ci.Name != ""
			}
		}
		return c, true
	}
	if b.core == nil || ch == "" {
		return c, false
	}
	v, ok := b.core.Conv(ch)
	if !ok {
		return c, false
	}
	ci := b.noteInfo(v)
	return ci, noteNamed(ci)
}

// noteInfo is what apply makes of core's conversation, for the fields a
// notification reads: a DM named for its partner under the viewer's
// display_real_names (an app's DM is "app"), a group DM for its members
// (noteMpimName).
func (b *backend) noteInfo(v core.Conv) *convInfo {
	ci := &convInfo{ID: v.ID, Name: v.Name, Kind: v.Kind, User: v.User, Members: v.Members, Muted: v.Muted,
		Archived: v.IsArchived, Preview: v.Preview, File: v.File, FileID: v.FileID, Doc: v.Doc}
	switch v.Kind {
	case "im":
		if u, ok := b.core.User(v.User); ok {
			if u.Name != "" {
				ci.Name = u.Name
			}
			if u.IsBot {
				ci.Kind = "app"
			}
		}
		if n, ok := b.core.S.PrefNameHeld(v.User); ok && n != "" { // getPrefCompliantMemberNameById
			ci.Name = n
		}
		ci.Self = v.User == b.selfID
	case "mpim":
		ci.Name, _ = b.noteMpimName(&v)
	}
	return ci
}

// noteMpimName is getMpimDisplayNameAlphabetical (client-boot), the name
// getDisplayNameForChannel gives a group DM's notification
// (truncateMpimNames off: nothing is cut): its custom name (mpdm_naming),
// else its members' names but the viewer's (isSelf), "Name hidden" for
// one the viewer hid (isUserHidden), the others under the viewer's
// display_real_names (getPrefCompliantMemberName), empty ones left out,
// sorted as Slack's sorter does (en-US, numeric) and joined with ", ".
// The members are read as the store holds them (RawUser: nothing asked;
// the toast waited for them, noteMembers; a lookup here brought a second
// users/info beside the toast's own, and its "names" a pass over
// everything on screen), and one it doesn't hold is left out, as Slack
// leaves out the unknown member's "". ok: something was named.
func (b *backend) noteMpimName(v *core.Conv) (string, bool) {
	if n, ok := b.core.MpdmCustomName(v.ID); ok && n != "" {
		return n, true
	}
	var names []string
	for _, id := range v.Members {
		switch {
		case id == b.selfID:
		case b.core.UserHidden(id):
			names = append(names, i18n.T("channels", "Name hidden"))
		default:
			if n, ok := b.core.S.PrefNameHeld(id); ok && n != "" {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		return "", false
	}
	views.SortNames(names)
	return strings.Join(names, ", "), true
}

// channelToText is Slack's for a channel mention in a notification's
// text (notify_conv.go's header); text is the mention as Clack drew it
// (a mrkdwn label stays when nothing better is known).
func (b *backend) channelToText(id, text string) string {
	if v, ok := b.core.Conv(id); ok {
		if v.Kind == "private" && !v.Member {
			return "#" + noteHiddenChannel()
		}
		ci := b.noteInfo(v)
		if !noteNamed(ci) {
			return "#" + noteUnknownChannel()
		}
		return "#" + strings.TrimPrefix(ci.Name, "#")
	}
	if b.core.S.ChannelAccess(id) == richtext.ChannelHidden {
		return "#" + noteHiddenChannel()
	}
	if n := b.core.S.ChannelName(id); n != "" && n != id { // named in search, browse or a mention's lookup
		return "#" + strings.TrimPrefix(n, "#")
	}
	if t := strings.TrimPrefix(text, "#"); t != "" && t != id && text != richtext.PrivateChannelLabel {
		return "#" + t // <#C…|label>
	}
	return "#" + noteHiddenChannel() // not in the store: isNonExistent
}

// noteSpanText writes a notification's channel mentions (channelToText)
// and its member mentions; the other spans keep their text. A member
// mention is named from the store as the body is written, as Slack's
// showNotification builds it (extractTextFromMessage over the store,
// after ensureModelsArePresent; userToText: noteMentionName, "Hidden
// user" for someone the viewer hid): core announces a message before
// anyone it names is fetched, so its conversion wrote a member the store
// didn't hold yet as the id ("@U07DANA"), and the store's copy, converted
// again once noteResolve fetched them (noteFresh), is there only for a
// conversation whose messages it keeps (no group DM or DM not opened
// lately, no thread it never loaded). One still unknown after the wait,
// or past noteMembersMax, reads as nothing, never the id (the views'
// mentionText), and one with a label of its own keeps the label; an id
// that isn't a member's keeps its text.
func (b *backend) noteSpanText(s richtext.Span) (string, bool) {
	if s.ID == "" || b.core == nil {
		return "", false
	}
	switch s.Kind {
	case richtext.Channel:
		return b.channelToText(s.ID, s.Text), true
	case richtext.MessageMention:
		// messageMentionToText: the label as the store has it now (the
		// notification waited for the linked message and the element's
		// author: noteSlugs, noteMembers), fetching nothing; the link
		// itself when the slug is disabled (convertUnknownSlugToUrl)
		if s.Chan == "" {
			return "", false
		}
		return b.core.MessageSlugText(s.Chan, s.ID, s.URL), true
	case richtext.User:
		if !core.LooksLikeMemberID(s.ID) {
			return "", false
		}
		if b.core.UserHidden(s.ID) {
			// userToText (client-boot DRDu) checks first: a member the
			// viewer hid reads "Hidden user", no @
			return i18n.T("channel_token", "Hidden user"), true
		}
		if n := b.noteMentionName(s.ID); n != "" {
			return "@" + strings.TrimPrefix(n, "@"), true
		}
		// still unknown: the id the conversion fell back to reads as
		// nothing; a mention's own label (<@U…|dana>) stays
		return "", s.Text == "@"+s.ID
	}
	return "", false
}

// noteNames is the resolver a notification's text is converted with
// (NotificationMessageText): getContentForNotification runs after
// ensureModelsArePresent and asks for no one. It names a member the way
// noteSpanText writes the mention (userToText), so a mention reads the
// same whether the toast's body comes from the blocks or from the text.
// A member the toast's wait (noteResolve) brought has their name; one
// still unknown stays unknown, never asked for again.
type noteNames struct {
	*store.Store
	b *backend
}

// MentionName is richtext.MentionNamer: noteMentionName's name for the
// member as the store holds them (the real name when the viewer's
// mentions show real names, else the display name), without a users/info
// ask. A member the viewer hid has none here: noteSpanText writes their
// mention "Hidden user", with no @ (the one answer a span's "@"+name
// can't give), and neither their name nor their id gets in.
func (r noteNames) MentionName(id string) string {
	if r.b.core.UserHidden(id) {
		return ""
	}
	return r.b.noteMentionName(id)
}

// MemberDeleted is richtext.MemberStater for a share_shortcut's bot,
// whose state a notification doesn't read (its title is the text): no
// ask.
func (noteNames) MemberDeleted(string) (bool, bool) { return false, true }

// noteMentionName is userToText's name for a member mention (client-boot
// DRDu; extractTextFromMessage turns useMentionsNamePref on): the real
// name (getMemberRealName) when the viewer's mentions show real names
// (resolveMemberMentionName under core.RealNamesInMentions), else
// getMemberMentionName, the display name, else the real name, as the
// rows name a mention (store.DisplayName: installMemberRead). Not
// display_real_names, which names senders and titles: MessageNamesPref's
// example keeps "@Meg". The member as the store holds it (the toast
// waited for it: nothing asked again); "" for one it doesn't.
func (b *backend) noteMentionName(id string) string {
	u, ok := b.core.S.RawUser(id)
	if !ok || u == nil {
		return ""
	}
	if b.core.RealNamesInMentions() {
		if n := memberRealName(u); n != "" {
			return n
		}
	}
	return store.DisplayName(u)
}

// memberRealName is getMemberRealName (hWCs wg): profile.real_name, else
// real_name.
func memberRealName(u *slack.User) string {
	if u.Profile.RealName != "" {
		return u.Profile.RealName
	}
	return u.RealName
}

// noteChannelKnown reports whether channelToText has what it needs for
// a mention of id: the conversation by name, its name, or that the viewer
// can't see it.
func (b *backend) noteChannelKnown(id string) bool {
	if v, ok := b.core.Conv(id); ok {
		return (v.Kind == "private" && !v.Member) || noteNamed(b.noteInfo(v))
	}
	if b.core.S.ChannelAccess(id) == richtext.ChannelHidden {
		return true
	}
	n := b.core.S.ChannelName(id)
	return n != "" && n != id
}

// noteMentions are the channels d mentions that the store can't name yet
// (at most noteResolveMax): its channel mentions', and its message
// slugs' channels (getMessageMentions adds a slug's channel_id to the
// channels ensureModelsArePresent fetches).
func (b *backend) noteMentions(d richtext.Doc) []string {
	if b.core == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	richtext.PlainTextWith(d, func(s richtext.Span) (string, bool) {
		id := ""
		switch s.Kind {
		case richtext.Channel:
			id = s.ID
		case richtext.MessageMention:
			id = s.Chan
		}
		if id != "" && !seen[id] {
			seen[id] = true
			if len(out) < noteResolveMax && !b.noteChannelKnown(id) {
				out = append(out, id)
			}
		}
		return "", false
	})
	return out
}

// noteSlugs are the messages d's message slugs link to that the store
// doesn't hold (at most noteResolveMax): ensureModelsArePresent's
// messageIds (client-boot Eley te, DHDI), fetched by id before the
// notification writes their labels.
func (b *backend) noteSlugs(d richtext.Doc) []core.MsgKey {
	if b.core == nil {
		return nil
	}
	var out []core.MsgKey
	seen := map[core.MsgKey]bool{}
	richtext.PlainTextWith(d, func(s richtext.Span) (string, bool) {
		if s.Kind != richtext.MessageMention || s.Chan == "" || s.ID == "" {
			return "", false
		}
		k := core.MsgKey{Channel: s.Chan, TS: s.ID}
		if !seen[k] && len(out) < noteResolveMax {
			seen[k] = true
			if _, ok := b.core.S.FindHeld(k.Channel, k.TS); !ok {
				out = append(out, k)
			}
		}
		return "", false
	})
	return out
}

// noteMembers are the people a notification names that the store doesn't
// hold: its sender, its conversation's people (a DM's partner, a group
// DM's members, whose names title it), then the members its text mentions
// and its message slugs' authors (at most noteMembersMax). Core announces a message the moment it
// arrives, its rows asking for the people they draw; the notification
// waits for them, as Slack's showNotification waits for
// ensureModelsArePresent (which adds a DM's user and a group DM's
// members: isMpim && members ? push(...members)).
func (b *backend) noteMembers(m *core.Message) []string {
	if b.core == nil || m == nil {
		return nil
	}
	// the sender and the conversation's people first, the ones the title
	// names, so a cut takes mentions past it rather than them
	var ids []string
	if m.User != "" { // a user-less bot's message names its sender itself
		ids = append(ids, m.User)
	}
	if v, ok := b.core.Conv(m.Channel); ok {
		switch v.Kind {
		case "im":
			ids = append(ids, v.User)
		case "mpim":
			for _, id := range v.Members {
				if id != b.selfID {
					ids = append(ids, id)
				}
			}
		}
	}
	richtext.PlainTextWith(m.Doc, func(s richtext.Span) (string, bool) {
		if s.Kind == richtext.User && s.ID != "" {
			ids = append(ids, s.ID)
		}
		return "", false
	})
	// and its message slugs' author_ids (getMessageMentions' userIds)
	ids = append(ids, core.SlugAuthors(m)...)
	if len(ids) == 0 {
		return nil
	}
	out := b.core.S.MissingUsers(ids)
	if len(out) > noteMembersMax {
		out = out[:noteMembersMax]
	}
	return out
}

// noteFresh is ev with its message converted again from the store's copy
// (core.HeldMessage), now that the people it names are in: core converted
// it before they were, and the body would name a mention by its id.
func (b *backend) noteFresh(ev core.Event) core.Event {
	if b.core == nil || ev.Msg == nil {
		return ev
	}
	if m, ok := b.core.HeldMessage(ev.Channel, ev.TS); ok {
		ev.Msg = &m
	}
	return ev
}

// noteResolve looks ids up (core.ResolveConv: conversations.info for one
// the store doesn't name, which keeps what it learns) and people
// (core.EnsureUsers: users/info) off the UI goroutine, for at most
// noteResolveWait, then runs then on it. Slack's store has a
// notification's channel and people when it builds the notification;
// Clack's may learn them a beat later, so the notification waits for the
// lookups instead of showing ids. The notifications that waited go in the
// order they came among those that wait for a common conversation, member
// or message (noteOrder).
func (b *backend) noteResolve(a *App, ch string, ids, people []string, msgs []core.MsgKey, then func()) {
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	c := b.core
	keys := make([]string, 0, 1+len(ids)+len(people)+len(msgs))
	keys = append(append(append(keys, ch), ids...), people...) // ch: every toast of it in order (noteOrder)
	for _, k := range msgs {
		keys = append(keys, k.Channel+" "+k.TS)
	}
	w := b.notes.add(keys, then)
	go func() {
		ctx, cancel := context.WithTimeout(parent, noteResolveWait)
		defer cancel()
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				c.ResolveConv(ctx, id)
			}(id)
		}
		if len(people) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c.EnsureUsers(ctx, people)
			}()
		}
		if len(msgs) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c.EnsureMessages(ctx, msgs) // the message slugs' messages (noteSlugs)
			}()
		}
		wg.Wait()
		b.notes.done(a, w)
	}()
}

// noteOrder lets the notifications that waited for lookups go in the
// order their messages came, among those of one conversation or waiting
// for a common member or message: Slack's waits share the one fetch per
// model (core's EnsureUsers shares a member's users/info the same way),
// and their continuations run in the order they began, so two messages
// from a new person notify in order; the goroutine each waited on posted
// them in whatever order they woke. A notification with nothing to look
// up goes through it too (later): at once while nothing of its
// conversation waits, else behind it. One that waits for nothing another
// waits for goes as soon as its own lookups answer.
type noteOrder struct {
	mu    sync.Mutex
	waits []*noteWait
	// posted counts, by key, the notifications released to the UI
	// goroutine that haven't run yet: one with nothing to look up waits
	// for them too, or it would show ahead of an earlier one already
	// queued
	posted map[string]int
}

type noteWait struct {
	keys map[string]bool
	done bool
	then func()
}

func (o *noteOrder) add(keys []string, then func()) *noteWait {
	w := &noteWait{keys: make(map[string]bool, len(keys)), then: then}
	for _, k := range keys {
		w.keys[k] = true
	}
	o.mu.Lock()
	o.waits = append(o.waits, w)
	o.mu.Unlock()
	return w
}

// done marks w's lookups answered and posts, in order, every waiting
// notification no earlier one with a key in common holds back. It posts
// them before it lets go of o.mu: two lookups that answer together each
// release a run of notifications, and the later run, posted first, went
// out ahead of the earlier one (Post only queues them).
func (o *noteOrder) done(a *App, w *noteWait) {
	o.mu.Lock()
	defer o.mu.Unlock()
	w.done = true
	o.releaseLocked(a)
}

// releaseLocked posts, in order, every done notification no earlier
// waiting one with a key in common holds back (mu held).
func (o *noteOrder) releaseLocked(a *App) {
	var held []*noteWait // in order: what still waits, or is held back
	for _, x := range o.waits {
		if x.done && !sharesKey(x, held) {
			o.postLocked(a, x)
			continue
		}
		held = append(held, x)
	}
	o.waits = held
}

// postLocked hands x's notification to the UI goroutine, its keys counted
// in posted until it runs (mu held).
func (o *noteOrder) postLocked(a *App, x *noteWait) {
	if o.posted == nil {
		o.posted = map[string]int{}
	}
	for k := range x.keys {
		o.posted[k]++
	}
	then := x.then
	a.Post(func() {
		o.mu.Lock()
		for k := range x.keys {
			if o.posted[k]--; o.posted[k] <= 0 {
				delete(o.posted, k)
			}
		}
		o.mu.Unlock()
		then()
	})
}

// later queues then, a notification of ch with nothing to look up (UI
// goroutine), behind the notifications of ch that wait for their lookups
// or are released and queued, and reports true; with none it queues
// nothing and reports false, and the caller shows it now. Shown at once
// while one waited, a toast went out ahead of an earlier one of its
// conversation still waiting for its sender.
func (o *noteOrder) later(a *App, ch string, then func()) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	busy := o.posted[ch] > 0
	for _, x := range o.waits {
		if busy {
			break
		}
		busy = x.keys[ch]
	}
	if !busy {
		return false
	}
	o.waits = append(o.waits, &noteWait{keys: map[string]bool{ch: true}, done: true, then: then})
	o.releaseLocked(a)
	return true
}

func sharesKey(x *noteWait, ws []*noteWait) bool {
	for _, y := range ws {
		for k := range x.keys {
			if y.keys[k] {
				return true
			}
		}
	}
	return false
}

// noteSends hands the desktop notifications to the system in the order
// they were made, off the caller's goroutine: a goroutine each could hand
// two over in reverse order. One the system takes longer than noteSendCap
// to take doesn't hold the next ones back: the call the system answers
// in milliseconds keeps its place, and a hung one (a notification daemon
// that doesn't answer) holds the queue half a second, not the 3 s it took.
var noteSends struct {
	mu sync.Mutex
	q  []func()
	on bool
}

const noteSendCap = 500 * time.Millisecond

func sendNote(f func()) {
	s := &noteSends
	s.mu.Lock()
	s.q = append(s.q, f)
	start := !s.on
	s.on = true
	s.mu.Unlock()
	if !start {
		return
	}
	go func() {
		for {
			s.mu.Lock()
			if len(s.q) == 0 {
				s.on, s.q = false, nil
				s.mu.Unlock()
				return
			}
			f := s.q[0]
			s.q[0] = nil
			s.q = s.q[1:]
			s.mu.Unlock()
			done := make(chan struct{})
			go func() {
				defer close(done)
				f()
			}()
			select {
			case <-done:
			case <-time.After(noteSendCap):
			}
		}
	}()
}
