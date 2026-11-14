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
	if s.Kind != richtext.Channel || s.ID == "" || b.core == nil {
		return "", false
	}
	return b.channelToText(s.ID, s.Text), true
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
		wg.Wait()
		a.Post(then)
	}()
}
