package core

import (
	"context"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Broadcast keywords (@channel, @here, @everyone): who may use them where,
// ported from Slack's web client (build a84662d2d135, 2026-11-08).
//
// Slack never reads the team's who_can_at_channel prefs or a channel's
// properties.at_channel_restricted in the client. It asks the server
// through the permission system: edgeapi permissions/info with
// {user_id, permissions: ["CAN_AT_CHANNEL", "CAN_AT_EVERYONE", …],
// channel_id} answers {allowed: [...]}, cached for an hour (fetchPermissions,
// client-boot qPNw/AcSl). On it sit:
//
//   - canUserAtChannelInChannelById (Mqzw): CAN_AT_CHANNEL in the channel
//     (in a group DM: in any of the viewer's workspaces), and not stopped
//     by shouldStopChannelMentions.
//   - canUserAtEveryoneInChannelById (U42r): the channel is #general and
//     CAN_AT_EVERYONE holds there (on Grid, also not stopped).
//   - getBroadcastKeywordsForUser (4e6T): BKeveryone when the second holds;
//     BKchannel and BKhere for public/private channels and group DMs when
//     the first does. The @ autocomplete offers exactly these unless the
//     caller passes includeAllBroadcastKeywords.
//   - hasProhibitedKeywords (prepareAndSendMessage, boot-shared): a send
//     using a keyword the viewer may not use is refused before it reaches
//     chat.postMessage: Slackbot posts a local "Only visible to you"
//     message explaining why and the draft stays in the composer. Slash
//     commands are run earlier in the same thunk, so they are never
//     checked (that's how relays like at-channel's /channel work).
//
// An unknown permission counts as not allowed, as in Slack (isAllowed of
// a missing object is false).
//
// Not ported: shouldStopChannelMentions (the team pref
// loud_channel_mentions_limit; Clack doesn't load team prefs, and Slack
// treats a missing pref as no limit) and the channel pref enable_at_channel
// (isBroadcastKeywordsEnabled; channel prefs aren't loaded either).

// Slack's permission ids (QxkB/tzxv: the enum's key names).
const (
	PermAtChannel  = "CAN_AT_CHANNEL"
	PermAtEveryone = "CAN_AT_EVERYONE"
)

// Broadcast keyword ids, as Slack's composer marks them on a mention
// (slackmention.id) and the autocomplete row ids.
const (
	KeywordChannel  = "channel"
	KeywordHere     = "here"
	KeywordEveryone = "everyone"
)

// BroadcastPerms are the viewer's broadcast permissions in a conversation.
type BroadcastPerms struct {
	Known      bool // permissions/info answered
	AtChannel  bool // CAN_AT_CHANNEL (@channel and @here)
	AtEveryone bool // CAN_AT_EVERYONE
}

// CanAtChannel is canUserAtChannelInChannelById.
func CanAtChannel(c Conv, p BroadcastPerms) bool {
	return c.Kind != "im" && p.AtChannel
}

// CanAtEveryone is canUserAtEveryoneInChannelById.
func CanAtEveryone(c Conv, p BroadcastPerms) bool {
	return c.IsGeneral && p.AtEveryone
}

// BroadcastKeywords is getBroadcastKeywordsForUser: the keywords the
// viewer may use in c, in Slack's order.
func BroadcastKeywords(c Conv, p BroadcastPerms) []string {
	var out []string
	if CanAtEveryone(c, p) {
		out = append(out, KeywordEveryone)
	}
	switch c.Kind {
	case "channel", "private", "mpim":
		if CanAtChannel(c, p) {
			out = append(out, KeywordChannel, KeywordHere)
		}
	}
	return out
}

// BroadcastSend is what hasProhibitedKeywords looks at.
type BroadcastSend struct {
	Conv     Conv
	Perms    BroadcastPerms
	ThreadTS string   // a reply: no keyword is allowed (Slack's list is empty)
	Used     []string // keywords the message mentions (KeywordHere…)
	// Disabled is the channel pref enable_at_channel turned off.
	Disabled bool
	// Restricted is a guest account (Slack's "Your account is restricted").
	Restricted bool
}

// ProhibitedBroadcast is hasProhibitedKeywords: when the message uses a
// keyword the viewer may not use, it returns Slackbot's explanation
// (mrkdwn, Slack's exact strings) and true.
func ProhibitedBroadcast(s BroadcastSend) (string, bool) {
	enabled := !s.Disabled
	allowed := map[string]bool{}
	if s.ThreadTS == "" && enabled {
		for _, k := range BroadcastKeywords(s.Conv, s.Perms) {
			allowed[k] = true
		}
	}
	used := map[string]bool{}
	for _, k := range s.Used {
		used[k] = true
	}
	if used[KeywordHere] && !allowed[KeywordHere] || used[KeywordChannel] && !allowed[KeywordChannel] {
		kw := "@" + KeywordChannel
		if used[KeywordHere] {
			kw = "@" + KeywordHere
		}
		switch {
		case !enabled:
			return "*" + kw + "* has been disabled in this channel, so your message cannot be sent.", true
		case s.Perms.AtChannel:
			return "Usage of *" + kw + "* is restricted in this large channel.", true
		default:
			return "A Workspace Owner has restricted the use of *" + kw + "* messages.", true
		}
	}
	if used[KeywordEveryone] && !allowed[KeywordEveryone] {
		switch {
		case !enabled:
			return "*@everyone* has been disabled in this channel, so your message cannot be sent.", true
		case s.Restricted:
			return "Your account is restricted, and you cannot send *@everyone* messages.", true
		case s.Perms.AtEveryone:
			return "Usage of *@everyone* is restricted in the large general channel.", true
		default:
			return "A Workspace Owner has restricted the use of *@everyone* messages.", true
		}
	}
	return "", false
}

// ---- permissions/info ----

// Slack's fetch manager keeps permissions for an hour (maxAge 3600 s).
const broadcastPermsMaxAge = time.Hour

type permCache struct {
	mu      sync.Mutex
	m       map[string]permEntry
	loading map[string]chan struct{}
}

type permEntry struct {
	p  BroadcastPerms
	at time.Time
}

var permIndex sync.Map // *App → *permCache

func (a *App) perms() *permCache {
	if v, ok := permIndex.Load(a); ok {
		return v.(*permCache)
	}
	v, _ := permIndex.LoadOrStore(a, &permCache{m: map[string]permEntry{}, loading: map[string]chan struct{}{}})
	return v.(*permCache)
}

// BroadcastPerms returns the cached broadcast permissions for ch without
// blocking (Known=false until permissions/info answered) and starts a
// load when they are missing or stale, like Slack's selectors do.
func (a *App) BroadcastPerms(ch string) BroadcastPerms {
	pc := a.perms()
	pc.mu.Lock()
	e, ok := pc.m[ch]
	pc.mu.Unlock()
	if !ok || time.Since(e.at) > broadcastPermsMaxAge {
		go func() { _, _ = a.LoadBroadcastPerms(a.bg, ch) }()
	}
	return e.p
}

// LoadBroadcastPerms returns ch's broadcast permissions, asking
// permissions/info when the cache has none (or they're an hour old).
// Concurrent callers share one request.
func (a *App) LoadBroadcastPerms(ctx context.Context, ch string) (BroadcastPerms, error) {
	pc := a.perms()
	for {
		pc.mu.Lock()
		if e, ok := pc.m[ch]; ok && time.Since(e.at) <= broadcastPermsMaxAge {
			pc.mu.Unlock()
			return e.p, nil
		}
		if wait, busy := pc.loading[ch]; busy {
			pc.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return BroadcastPerms{}, ctx.Err()
			}
		}
		done := make(chan struct{})
		pc.loading[ch] = done
		pc.mu.Unlock()

		p, err := a.fetchBroadcastPerms(ctx, ch)
		pc.mu.Lock()
		delete(pc.loading, ch)
		if err == nil {
			pc.m[ch] = permEntry{p: p, at: time.Now()}
		}
		pc.mu.Unlock()
		close(done)
		return p, err
	}
}

// fetchBroadcastPerms is Slack's fetchPermissions for the two broadcast
// permissions: in the channel, or (group DMs: Slack's
// getPermissionForAnyWorkspace) across the viewer's workspace.
func (a *App) fetchBroadcastPerms(ctx context.Context, ch string) (BroadcastPerms, error) {
	kind := a.kindOf(ch)
	if kind == "im" {
		return BroadcastPerms{Known: true}, nil // no keyword is ever offered in a DM
	}
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": []string{PermAtChannel, PermAtEveryone}}
	team := a.C.Creds.Team
	perTeam := kind == "mpim" && a.C.Creds.EnterpriseID != "" && team != ""
	switch {
	case perTeam:
		body["team_ids"] = []string{team}
	case kind == "mpim":
		body["as_admin"] = true // Slack: neither a channel nor teams
	default:
		body["channel_id"] = ch
	}
	var r struct {
		Allowed        []string            `json:"allowed"`
		AllowedPerTeam map[string][]string `json:"allowed_per_team"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err != nil {
		return BroadcastPerms{}, err
	}
	allowed := map[string]bool{}
	for _, p := range r.Allowed {
		allowed[p] = true
	}
	if perTeam {
		allowed = map[string]bool{}
		for _, ps := range r.AllowedPerTeam {
			for _, p := range ps {
				allowed[p] = true
			}
		}
	}
	return BroadcastPerms{Known: true, AtChannel: allowed[PermAtChannel], AtEveryone: allowed[PermAtEveryone]}, nil
}

// BroadcastKeywordsFor is getBroadcastKeywordsForUser for a known
// conversation, from the cached permissions (non-blocking).
func (a *App) BroadcastKeywordsFor(ch string) []string {
	c, ok := a.Conv(ch)
	if !ok {
		return nil
	}
	return BroadcastKeywords(c, a.BroadcastPerms(ch))
}

// LocalEphemeral adds a Slackbot message only the viewer sees ("Only
// visible to you") to ch, or to the thread threadTS, on this client
// alone: Slack's addEphemeralMessage({temporary: true}), which the send
// checks use to say why a message wasn't sent. It returns the local ts.
func (a *App) LocalEphemeral(ch, threadTS, text string) string {
	ts := a.pendingTS(ch, threadTS)
	m := slack.Message{Type: "message", TS: ts, User: "USLACKBOT", Text: text, ThreadTS: threadTS}
	ix := a.Interactive()
	ix.mu.Lock()
	ix.ephem[ch+"/"+ts] = threadTS
	ix.mu.Unlock()
	a.putLocal(ch, m)
	v := a.message(ch, &m)
	a.emit(Event{Type: "message", Channel: ch, TS: ts, ThreadTS: threadTS, UserID: m.User, Msg: &v,
		Reply: !isTopLevel(&m), ConvKind: a.kindOf(ch)})
	ix.emit(ViewEvent{Kind: "ephemeral", Channel: ch, TS: ts})
	return ts
}
