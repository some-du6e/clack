package core

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
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
// Two more inputs, both read at send time:
//
//   - shouldStopChannelMentions (uXk7, called with allowToAdmins): only
//     on Grid (bootData.page_needs_enterprise), in any conversation but a
//     DM, when the channel's member_count (membershipCounts: edge
//     users/counts `people`) is at least the team pref
//     loud_channel_mentions_limit (missing or 0: no limit). Workspace
//     admins (org admins, for org-shared channels) are exempt. A stopped
//     channel takes @channel/@here (and on Grid @everyone) away, and
//     hasProhibitedKeywords then says "Usage of *@channel* is restricted
//     in this large channel." (the viewer has CAN_AT_CHANNEL but may not
//     use it here).
//   - isBroadcastKeywordsEnabled (sZmW): the channel pref
//     enable_at_channel. Slack's sendDraftMessage fetches it before every
//     send (channels.prefs.get {channel_id, pref_name}), but only where
//     it can apply (vzHP: not a DM or group DM, and the team is on Plus or
//     higher, Grid included, or the channel is #general). An empty value
//     means enabled (lodash isEmpty), else its .enabled decides. One pref
//     covers both keywords: the channel settings dialog writes
//     enable_at_here and enable_at_channel together, and the send check
//     only reads enable_at_channel, naming @here when the message used it
//     ("*@here* has been disabled in this channel, so your message cannot
//     be sent.").
//
// Then the confirmation ("shouty rooster", shouldWarnBeforeBroadcast,
// 8wpw): see BroadcastWarn.

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
	// Stopped is shouldStopChannelMentions: a large channel on Grid
	// (StopChannelMentions). It is worked out when the perms are read,
	// not cached with them.
	Stopped bool
}

// CanAtChannel is canUserAtChannelInChannelById.
func CanAtChannel(c Conv, p BroadcastPerms) bool {
	return c.Kind != "im" && p.AtChannel && !p.Stopped
}

// CanAtEveryone is canUserAtEveryoneInChannelById (Stopped is only ever
// set on Grid, where Slack checks it).
func CanAtEveryone(c Conv, p BroadcastPerms) bool {
	return c.IsGeneral && p.AtEveryone && !p.Stopped
}

// MentionLimit is what shouldStopChannelMentions reads.
type MentionLimit struct {
	Grid    bool // bootData.page_needs_enterprise
	Limit   int  // team pref loud_channel_mentions_limit (0: none)
	Members int  // membershipCounts member_count: people (edge users/counts)
	// Admin: the viewer acts as an admin of the channel's workspace (of
	// the org, for an org-shared channel); allowToAdmins exempts them.
	Admin bool
}

// StopChannelMentions is shouldStopChannelMentions(state, id,
// {allowToAdmins: true}).
func StopChannelMentions(c Conv, l MentionLimit) bool {
	if !l.Grid || c.ID == "" || c.Kind == "im" || l.Limit <= 0 { // `limit || Infinity`
		return false
	}
	return l.Members >= l.Limit && !l.Admin
}

// BroadcastWarn is what shouldWarnBeforeBroadcast (8wpw) reads.
type BroadcastWarn struct {
	Conv          Conv
	Used          []string // keywords the message uses
	CanAtChannel  bool     // CanAtChannel (permission and not stopped)
	CanAtEveryone bool
	Enabled       bool   // isBroadcastKeywordsEnabled
	Pref          string // team pref warn_before_at_channel: never | always | once | daily
	LastSeen      int64  // user pref last_seen_at_channel_warning (ms; 0: never)
	Members       int    // membershipCounts member_count
	Now           time.Time
}

// ShouldWarnBroadcast is shouldWarnBeforeBroadcast: a top-level send
// using a keyword the viewer may use, in a conversation of more than 5
// people, asks first ("Notify N people?") when warn_before_at_channel
// says so: "always", "once" (the user never saw the warning) or "daily"
// (not yet today, local time). "never" or a missing pref: no warning.
// A keyword the viewer may not use never warns (hasProhibitedKeywords
// refuses it later), except @everyone outside #general, which Slack
// doesn't look at here.
func ShouldWarnBroadcast(w BroadcastWarn) bool {
	used := map[string]bool{}
	for _, k := range w.Used {
		used[k] = true
	}
	everyone, channel, here := used[KeywordEveryone], used[KeywordChannel], used[KeywordHere]
	if !everyone && !channel && !here {
		return false
	}
	if w.Conv.ID == "" || w.Conv.Kind == "im" {
		return false
	}
	if (channel || here) && (!w.CanAtChannel || !w.Enabled) {
		return false
	}
	if w.Conv.IsGeneral && everyone && (!w.CanAtEveryone || !w.Enabled) {
		return false
	}
	seen := w.LastSeen != 0
	now := w.Now
	if now.IsZero() {
		now = time.Now()
	}
	seenToday := false
	if seen {
		y1, m1, d1 := time.UnixMilli(w.LastSeen).In(now.Location()).Date()
		y2, m2, d2 := now.Date()
		seenToday = y1 == y2 && m1 == m2 && d1 == d2
	}
	switch w.Pref {
	case "always":
	case "once":
		if seen {
			return false
		}
	case "daily":
		if seenToday {
			return false
		}
	default: // "never", missing
		return false
	}
	return w.Members > 5
}

// WarnKeyword is the keyword openBroadcastKeywordWarningDialog names:
// @everyone over @channel over the first one used.
func WarnKeyword(used []string) string {
	k := ""
	if len(used) > 0 {
		k = used[0]
	}
	if slices.Contains(used, KeywordChannel) {
		k = KeywordChannel
	}
	if slices.Contains(used, KeywordEveryone) {
		k = KeywordEveryone
	}
	return k
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
	mu       sync.Mutex
	m        map[string]permEntry
	loading  map[string]chan struct{}
	counting map[string]bool // member counts loading for the stop
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
	v, _ := permIndex.LoadOrStore(a, &permCache{m: map[string]permEntry{}, loading: map[string]chan struct{}{}, counting: map[string]bool{}})
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
		go func() { _, _ = a.loadPerms(a.bg, ch) }()
	}
	p := e.p
	p.Stopped = a.stopped(nil, ch)
	return p
}

// LoadBroadcastPerms returns ch's broadcast permissions, asking
// permissions/info when the cache has none (or they're an hour old).
// Concurrent callers share one request. Stopped is worked out too, after
// loading the member count when the stop needs it.
func (a *App) LoadBroadcastPerms(ctx context.Context, ch string) (BroadcastPerms, error) {
	p, err := a.loadPerms(ctx, ch)
	if err == nil {
		p.Stopped = a.stopped(ctx, ch)
	}
	return p, err
}

// stopped is StopChannelMentions for ch. With ctx nil it never blocks: a
// missing member count is loaded in the background and counts as 0.
func (a *App) stopped(ctx context.Context, ch string) bool {
	c, ok := a.Conv(ch)
	if !ok {
		return false
	}
	l := MentionLimit{Grid: a.C.Creds.EnterpriseID != ""}
	l.Limit, _ = a.TeamPrefInt("loud_channel_mentions_limit")
	if !l.Grid || l.Limit <= 0 || c.Kind == "im" {
		return false
	}
	n, known := a.MemberCountCached(ch)
	if !known {
		if ctx != nil {
			a.loadMemberCount(ctx, ch)
			n, _ = a.MemberCountCached(ch)
		} else {
			a.loadCountSoon(ch)
		}
	}
	l.Members = n
	l.Admin = a.mentionAdmin(ch)
	return StopChannelMentions(c, l)
}

// loadCountSoon loads ch's member count in the background, once at a
// time.
func (a *App) loadCountSoon(ch string) {
	pc := a.perms()
	pc.mu.Lock()
	busy := pc.counting[ch]
	pc.counting[ch] = true
	pc.mu.Unlock()
	if busy {
		return
	}
	go func() {
		a.loadMemberCount(a.bg, ch)
		pc.mu.Lock()
		delete(pc.counting, ch)
		pc.mu.Unlock()
	}()
}

// mentionAdmin is allowToAdmins' test: an org admin for an org-shared
// channel, else an admin of the workspace (or the org).
func (a *App) mentionAdmin(ch string) bool {
	self, ok := a.S.User(a.S.SelfID())
	if !ok {
		return false
	}
	org := self.EnterpriseUser != nil && self.EnterpriseUser.IsAdmin
	if c, ok := a.S.Conv(ch); ok && c.IsOrgShared {
		return org
	}
	return self.IsAdmin || self.IsOwner || org
}

func (a *App) loadPerms(ctx context.Context, ch string) (BroadcastPerms, error) {
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

// ---- channels.prefs.get enable_at_channel ----

// LoadBroadcastEnabled is isBroadcastKeywordsEnabled after
// sendDraftMessage's fetch: channels.prefs.get {channel_id, pref_name:
// "enable_at_channel"}. DMs and group DMs are always enabled (Slack
// doesn't ask). Slack also skips the fetch for teams below Plus outside
// #general; Clack can't tell a team's plan (team.info has none), so it
// asks anyway: a team that can't set the pref has none, which is
// enabled. A failed lookup counts as enabled.
func (a *App) LoadBroadcastEnabled(ctx context.Context, ch string) bool {
	if k := a.kindOf(ch); k == "im" || k == "mpim" {
		return true
	}
	var r struct {
		PrefValue json.RawMessage `json:"pref_value"`
	}
	if err := a.C.Call(ctx, "channels.prefs.get", url.Values{"channel_id": {ch}, "pref_name": {"enable_at_channel"}}, &r); err != nil {
		return true
	}
	return BroadcastPrefEnabled(r.PrefValue)
}

// BroadcastPrefEnabled reads an enable_at_channel value as Slack does:
// isEmpty(value) || value?.enabled.
func BroadcastPrefEnabled(v json.RawMessage) bool {
	var x any
	if len(v) == 0 || json.Unmarshal(v, &x) != nil {
		return true
	}
	switch x := x.(type) {
	case nil, bool, float64: // lodash isEmpty: true for null, booleans and numbers
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		if len(x) == 0 {
			return true
		}
		switch e := x["enabled"].(type) { // JS truthiness
		case bool:
			return e
		case string:
			return e != ""
		case float64:
			return e != 0
		case nil:
			return false
		default:
			return true
		}
	}
	return true
}

// ---- the "Notify N people?" confirmation ----

// BroadcastWarning reports whether a top-level send using the keywords
// used should ask first (ShouldWarnBroadcast), loading what the check
// reads: the team pref first (nothing else is fetched when it never
// warns), then the permissions and the member count.
func (a *App) BroadcastWarning(ctx context.Context, ch string, used []string, enabled bool) bool {
	pref := a.TeamPrefString("warn_before_at_channel")
	if pref == "" || pref == "never" || len(used) == 0 {
		return false
	}
	c, ok := a.Conv(ch)
	if !ok || c.Kind == "im" {
		return false
	}
	p, _ := a.LoadBroadcastPerms(ctx, ch)
	n, known := a.MemberCountCached(ch)
	if !known {
		a.loadMemberCount(ctx, ch)
		n, _ = a.MemberCountCached(ch)
	}
	return ShouldWarnBroadcast(BroadcastWarn{Conv: c, Used: used, CanAtChannel: CanAtChannel(c, p),
		CanAtEveryone: CanAtEveryone(c, p), Enabled: enabled, Pref: pref,
		LastSeen: int64(a.PrefInt("last_seen_at_channel_warning", 0)), Members: n})
}

// MarkBroadcastWarningSeen is the dialog's componentDidMount write:
// last_seen_at_channel_warning = Date.now() (ms), which "once" and
// "daily" read.
func (a *App) MarkBroadcastWarningSeen(ctx context.Context) error {
	return a.SetPref(ctx, "last_seen_at_channel_warning", int(time.Now().UnixMilli()))
}

// BroadcastAudience is the dialog's fetchConversationInfo({channel,
// timezoneCount: true, displayCounts: true}): conversations.info's
// channel.display_counts.display_counts (members) and
// channel.timezone_count.
func (a *App) BroadcastAudience(ctx context.Context, ch string) (members, timezones int, err error) {
	var r struct {
		Channel struct {
			TimezoneCount int `json:"timezone_count"`
			DisplayCounts struct {
				DisplayCounts int `json:"display_counts"`
			} `json:"display_counts"`
		} `json:"channel"`
	}
	err = a.C.Call(ctx, "conversations.info", url.Values{"channel": {ch}, "timezone_count": {"true"}, "display_counts": {"true"}}, &r)
	return r.Channel.DisplayCounts.DisplayCounts, r.Channel.TimezoneCount, err
}

// BroadcastWarningAdmin is the dialog's isCurrentUserAdmin and
// adminSettingsUrl (the team URL + "admin/settings#permissions"): admins
// get "Pssst, admins… You can control this warning in Workspace
// settings."
func (a *App) BroadcastWarningAdmin() (admin bool, settingsURL string) {
	self, ok := a.S.User(a.S.SelfID())
	if ok {
		admin = self.IsAdmin || self.IsOwner || (self.EnterpriseUser != nil && self.EnterpriseUser.IsAdmin)
	}
	if u := a.C.Creds.TeamURL; u != "" {
		settingsURL = strings.TrimSuffix(u, "/") + "/admin/settings#permissions"
	}
	return admin, settingsURL
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
