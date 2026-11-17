package ui

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
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
// notification reads: a DM named for its partner (an app's DM is "app"),
// a group DM for its members.
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
		ci.Self = v.User == b.selfID
	case "mpim":
		ci.Name = b.mpimName(&v, b.selfID)
	}
	return ci
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

// noteSpanText writes a notification's channel mentions (channelToText);
// the other spans keep their text.
func (b *backend) noteSpanText(s richtext.Span) (string, bool) {
	if s.ID == "" || b.core == nil {
		return "", false
	}
	switch s.Kind {
	case richtext.Channel:
		return b.channelToText(s.ID, s.Text), true
	case richtext.User:
		// the member's name as the store has it now: the doc may come from
		// the socket's copy, converted before their lookup answered (a
		// message no window holds: noteFresh), and showed the raw id
		if u, ok := b.core.S.RawUser(s.ID); ok {
			if name := store.DisplayName(u); name != "" {
				return "@" + strings.TrimPrefix(name, "@"), true
			}
		}
	}
	return "", false
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
// (at most noteResolveMax).
func (b *backend) noteMentions(d richtext.Doc) []string {
	if b.core == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	richtext.PlainTextWith(d, func(s richtext.Span) (string, bool) {
		if s.Kind == richtext.Channel && s.ID != "" && !seen[s.ID] {
			seen[s.ID] = true
			if len(out) < noteResolveMax && !b.noteChannelKnown(s.ID) {
				out = append(out, s.ID)
			}
		}
		return "", false
	})
	return out
}

// noteResolve looks ids up (core.ResolveConv: conversations.info for one
// the store doesn't name, which keeps what it learns) off the UI
// goroutine, for at most noteResolveWait, then runs then on it. Slack's
// store has a notification's channel when it builds the notification;
// Clack's may learn it a beat later, so the notification waits for the
// lookup instead of showing the id.
func (b *backend) noteResolve(a *App, ids []string, then func()) {
	b.noteResolveWith(a, ids, nil, then)
}

// noteResolveWith is noteResolve that also fetches people (users/info for
// members the store doesn't hold), in the same wait: Slack's
// showNotification awaits ensureModelsArePresent (gaJC) for the
// message's sender and the people and channels it names, for at most
// 5 s, before it builds the notification. The message itself drew
// without them (core onMessage).
func (b *backend) noteResolveWith(a *App, ids, people []string, then func()) {
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	c := b.core
	w := b.notes.add(append(append([]string(nil), ids...), people...), then)
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
		wg.Wait()
		b.notes.done(a, w)
	}()
}

// noteOrder lets the notifications that waited for lookups go in the
// order their messages came, among those waiting for a common member or
// conversation: Slack's waits share the one fetch per model (core's
// EnsureUsers shares a member's users/info the same way), and their
// continuations run in the order they began, so two messages from a new
// person notify in order. One that waits for nobody another waits for
// goes as soon as its own lookups answer.
type noteOrder struct {
	mu    sync.Mutex
	waits []*noteWait
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
// notification no earlier one with a key in common holds back.
func (o *noteOrder) done(a *App, w *noteWait) {
	o.mu.Lock()
	w.done = true
	var ready []func()
	var held []*noteWait // in order: what is still waiting, or held back
	for _, x := range o.waits {
		if x.done && !sharesKey(x, held) {
			ready = append(ready, x.then)
			continue
		}
		held = append(held, x)
	}
	o.waits = held
	o.mu.Unlock()
	for _, f := range ready {
		a.Post(f)
	}
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

// noteMembers are the members a message's notification names that the
// store doesn't hold yet (read without asking): its sender and the
// people its text mentions (at most noteResolveMax). Slack's
// ensureModelsArePresent fetches them before the notification shows.
func (b *backend) noteMembers(m *core.Message) []string {
	if b.core == nil || m == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	want := func(id string) {
		if id == "" || seen[id] || len(out) >= noteResolveMax || !core.LooksLikeMemberID(id) {
			return
		}
		seen[id] = true
		if _, ok := b.core.S.RawUser(id); !ok && !b.core.S.Nonexistent(id) {
			out = append(out, id)
		}
	}
	if m.Name == "" { // a bot's username or a resolved sender needs no lookup
		want(m.User)
	}
	richtext.PlainTextWith(m.Doc, func(s richtext.Span) (string, bool) {
		if s.Kind == richtext.User {
			want(s.ID)
		}
		return "", false
	})
	return out
}

// noteFresh is ev with its message converted again from core's copy, as
// the notification is built (after its people came in, when it waited):
// the mentions' names, which the conversion the socket's event made
// before users/info answered couldn't read, or a member fetched since.
// MessageNow falls back to the socket's own copy, as the message is in no
// window or thread for a conversation nobody opened or a reply in a
// thread that isn't cached: most notifications.
func (b *backend) noteFresh(ev core.Event) core.Event {
	if b.core == nil || ev.Msg == nil {
		return ev
	}
	if m, ok := b.core.MessageNow(ev.Channel, ev.TS); ok {
		msg := *ev.Msg
		msg.Doc = m.Doc
		if msg.Name == "" {
			msg.Name = m.Name
		}
		ev.Msg = &msg
	}
	return ev
}

// noteSends hands the desktop notifications to the system in the order
// they were made, off the caller's goroutine: a goroutine each could hand
// two over in reverse order. One the system takes longer than noteSendCap
// to take doesn't hold the next ones back.
var noteSends struct {
	mu sync.Mutex
	q  []func()
	on bool
}

const noteSendCap = 3 * time.Second

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
