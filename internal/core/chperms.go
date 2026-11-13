package core

import (
	"context"
	"sync"
	"time"
)

// Channel-scoped role grants, read the way Slack's permission store
// answers them (client-boot build 4d0c78085232, 2026-11-10):
// getPermissionForChannel (QaSy WB) returns what fetchPermissions (qPNw:
// edge permissions/info {user_id, permissions, channel_id}, no channel_id
// for a 1:1 DM) stored, and asks for it when the store has none (AcSl's
// fetch, kept an hour); until it answers the permission isn't allowed
// (T63e Jr: isAllowed of a missing object is false).
//
// On them sit the topic and description checks:
//
//   - canUserSetTopicForChannelWithReasonById (boot-shared xjwB): no for an
//     unknown conversation, a Slack Connect DM or group DM, an archived
//     one; yes with SET_CHANNEL_TOPIC; otherwise no when
//     channel_topic_purpose_setting_restrict_to_channel_managers is on and
//     this isn't a 1:1 DM, or the viewer isn't a member, can't post, or is
//     a guest (restricted or ultra restricted); else yes.
//   - canUserSetPurpose (boot-shared uJL7 s): public and private channels
//     only, not archived; yes with SET_CHANNEL_PURPOSE; otherwise no when
//     the experiment is on (for every channel), or for a non-member, a
//     viewer who can't post, or a guest; else yes.

// The grants (QxkB's permission enum keys).
const (
	PermSetChannelTopic   = "SET_CHANNEL_TOPIC"
	PermSetChannelPurpose = "SET_CHANNEL_PURPOSE"
)

// ExpRestrictTopicPurpose limits topic and description edits to people
// with the grants (channel managers, admins).
const ExpRestrictTopicPurpose = "channel_topic_purpose_setting_restrict_to_channel_managers"

// chPermSet is what one permissions/info call asks for: both grants, the
// way the header's topic and the details' Topic and Description read them
// together, and About's "Send emails to this channel" grant
// (showChannelEmailAddress, chdetails_email.go).
// The tabs' check (useCanEditTabs, channel_managers.go) reads
// MANAGE_CHANNEL_TABS and ASSIGN_CHANNEL_MANAGER from the same store.
var chPermSet = []string{PermSetChannelTopic, PermSetChannelPurpose, PermCreateChannelEmailAddresses,
	PermManageChannelTabs, PermAssignChannelManager}

const chPermMaxAge = time.Hour

type chPermCache struct {
	mu      sync.Mutex
	m       map[string]chPermEntry
	loading map[string]bool
}

type chPermEntry struct {
	allowed map[string]bool
	at      time.Time
}

var chPermIndex sync.Map // *App → *chPermCache

func (a *App) chPerms() *chPermCache {
	if v, ok := chPermIndex.Load(a); ok {
		return v.(*chPermCache)
	}
	v, _ := chPermIndex.LoadOrStore(a, &chPermCache{m: map[string]chPermEntry{}, loading: map[string]bool{}})
	return v.(*chPermCache)
}

// ChannelGrant is getPermissionForChannel(ch, perm).isAllowed without
// blocking: false until permissions/info answered, which it starts when
// the cache has nothing for ch (or an hour-old answer). The answer emits
// a "permissions" event for ch so views redraw.
func (a *App) ChannelGrant(ch, perm string) bool {
	if ch == "" {
		return false
	}
	return a.grant(ch, perm)
}

// TeamGrant is getPermissionForAnyWorkspace(perm).isAllowed (QaSy yI)
// the same way: permissions/info without a channel.
func (a *App) TeamGrant(perm string) bool { return a.grant("", perm) }

func (a *App) grant(ch, perm string) bool {
	pc := a.chPerms()
	pc.mu.Lock()
	e, ok := pc.m[ch]
	stale := !ok || time.Since(e.at) > chPermMaxAge
	start := stale && !pc.loading[ch]
	if start {
		pc.loading[ch] = true
	}
	pc.mu.Unlock()
	if start {
		go a.loadChannelGrants(a.bg, ch)
	}
	return e.allowed[perm]
}

func (a *App) loadChannelGrants(ctx context.Context, ch string) {
	pc := a.chPerms()
	allowed, err := a.fetchChannelGrants(ctx, ch)
	pc.mu.Lock()
	delete(pc.loading, ch)
	if err == nil {
		pc.m[ch] = chPermEntry{allowed: allowed, at: time.Now()}
	}
	pc.mu.Unlock()
	if err == nil {
		a.emit(Event{Type: "permissions", Channel: ch})
	}
}

// fetchChannelGrants is fetchPermissions({channelId, permissions}).
func (a *App) fetchChannelGrants(ctx context.Context, ch string) (map[string]bool, error) {
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": chPermSet}
	if ch != "" && a.kindOf(ch) != "im" { // Slack leaves channel_id off for a 1:1 DM (aK5v LI)
		body["channel_id"] = ch
	}
	var r struct {
		Allowed []string `json:"allowed"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range r.Allowed {
		out[p] = true
	}
	return out, nil
}

// viewerRestricted is QLH5 sV || AB: a guest (restricted or ultra
// restricted).
func (a *App) viewerRestricted() bool {
	if u, ok := a.S.User(a.S.SelfID()); ok {
		return u.IsRestricted || u.IsUltraRestricted
	}
	return false
}

// CanSetTopic is canUserSetTopicForChannelWithReasonById(ch).ok.
func (a *App) CanSetTopic(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok {
		return false
	}
	switch {
	case (c.IsIM || c.IsMPIM) && c.IsExtShared:
		return false
	case c.IsArchived:
		return false
	case a.ChannelGrant(ch, PermSetChannelTopic):
		return true
	case a.experiment(ExpRestrictTopicPurpose) == "on" && !c.IsIM:
		return false
	}
	// a DM is always the viewer's (JUrK bl reads is_member, which Slack's
	// DM objects carry)
	member := c.IsMember || c.IsIM || c.IsMPIM
	return member && a.PostingFor(c).CanPost() && !a.viewerRestricted()
}

// CanSetPurpose is canUserSetPurpose(ch).ok (uJL7 s).
func (a *App) CanSetPurpose(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || c.IsIM || c.IsMPIM { // gXDR V: public or private channels only
		return false
	}
	switch {
	case c.IsArchived:
		return false
	case a.ChannelGrant(ch, PermSetChannelPurpose):
		return true
	case a.experiment(ExpRestrictTopicPurpose) == "on":
		return false
	}
	return c.IsMember && a.PostingFor(c).CanPost() && !a.viewerRestricted()
}
