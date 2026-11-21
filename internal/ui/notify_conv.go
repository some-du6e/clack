package ui

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
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
// many people names them all (on ui, onMessage fetched every mention
// before converting it).
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
		if c.Kind != "mpim" {
			return c, true
		}
		// a group DM is named for its members as the store holds them now
		// (the toast waited for them: noteMembers), not as the sidebar's
		// snapshot did: a closed group DM's members are never read for
		// the sidebar, so its row named only those held then
		// (RawUser: nothing asked; a lookup here brought a second
		// users/info beside the toast's own, and its "names" a pass over
		// everything on screen for people nothing draws)
		if v, ok := b.core.Conv(ch); ok {
			ci := *c
			ci.Name = b.mpimNameBy(&v, b.selfID, b.core.RawUser)
			return &ci, noteNamed(&ci)
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

// noteSpanText writes a notification's channel mentions (channelToText)
// and its member mentions; the other spans keep their text. A member
// mention is named from the store as the body is written, as Slack's
// showNotification builds it (extractTextFromMessage over the store,
// after ensureModelsArePresent), under the viewer's name pref
// (store.MemberName: display_real_names, hidden_users): core announces a
// message before anyone it names is fetched, so its conversion wrote a
// member the store didn't hold yet as the id ("@U07DANA"), and the
// store's copy, converted again once noteResolve fetched them
// (noteFresh), is there only for a conversation whose messages it keeps
// (no group DM or DM not opened lately, no thread it never loaded). One
// still unknown after the wait, or past noteMembersMax, reads as nothing,
// never the id (the views' mentionText), and one with a label of its own
// keeps the label; an id that isn't a member's keeps its text.
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
		if n, ok := b.core.S.MemberNameHeld(s.ID); ok && n != "" { // waited for: nothing asked again
			return "@" + strings.TrimPrefix(n, "@"), true
		}
		// still unknown: the id the conversion fell back to reads as
		// nothing; a mention's own label (<@U…|dana>) stays
		return "", s.Text == "@"+s.ID
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
// hold: its sender, the members its text mentions, and its conversation's
// people, a DM's partner and a group DM's members, whose names title it
// (at most noteMembersMax). Core announces a message the moment it
// arrives, its rows asking for the people they draw; the notification
// waits for them, as Slack's showNotification waits for
// ensureModelsArePresent (which adds a DM's user and a group DM's
// members: isMpim && members ? push(...members)).
func (b *backend) noteMembers(m *core.Message) []string {
	if b.core == nil || m == nil {
		return nil
	}
	var ids []string
	if m.User != "" { // a user-less bot's message names its sender itself
		ids = append(ids, m.User)
	}
	richtext.PlainTextWith(m.Doc, func(s richtext.Span) (string, bool) {
		if s.Kind == richtext.User && s.ID != "" {
			ids = append(ids, s.ID)
		}
		return "", false
	})
	// and its message slugs' author_ids (getMessageMentions' userIds)
	ids = append(ids, core.SlugAuthors(m)...)
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
// lookups instead of showing ids.
func (b *backend) noteResolve(a *App, ids, people []string, msgs []core.MsgKey, then func()) {
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	c := b.core
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
		a.Post(then)
	}()
}
