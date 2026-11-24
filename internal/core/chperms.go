package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
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
//   - canUserSetTopicForChannelWithReasonById (boot-shared iJOl, was
//     xjwB): no for an unknown conversation, a Slack Connect DM or group
//     DM, an archived one; yes with SET_CHANNEL_TOPIC; otherwise no unless
//     this is a 1:1 DM the viewer is in, can post to, and isn't a guest
//     (restricted or ultra restricted) in.
//   - canUserSetPurpose (boot-shared GvU9 s, was uJL7): public and private
//     channels only, not archived; yes with SET_CHANNEL_PURPOSE, else no.
//
// Both used to apply the second rule only under
// channel_topic_purpose_setting_restrict_to_channel_managers (falling back
// to membership, posting and guest checks without it). Slack dropped the
// experiment and kept its on path for everyone (build 8f19be21ed7e, the
// 2026-11-21 drift).

// The grants (QxkB's permission enum keys).
const (
	PermSetChannelTopic   = "SET_CHANNEL_TOPIC"
	PermSetChannelPurpose = "SET_CHANNEL_PURPOSE"
)

// chPermSet is what one permissions/info call asks for: both grants, the
// way the header's topic and the details' Topic and Description read them
// together, and About's "Send emails to this channel" grant
// (showChannelEmailAddress, chdetails_email.go).
// The tabs' check (useCanEditTabs, channel_managers.go) reads
// MANAGE_CHANNEL_TABS and ASSIGN_CHANNEL_MANAGER from the same store, the
// roster's Remove from channel (canUserKickFromChannel, exp_wave10.go)
// the kick grants, and /rename's and /archive's isSlashCommandEnabled
// checks (slash_enabled.go) RENAME_CHANNEL and ARCHIVE_CHANNEL.
var chPermSet = []string{PermSetChannelTopic, PermSetChannelPurpose, PermCreateChannelEmailAddresses,
	PermManageChannelTabs, PermAssignChannelManager, PermRemoveFromChannel, PermRemoveFromPrivateChannel,
	PermManagePublicChannels, PermManagePrivateChannels, PermRenameChannel, PermArchiveChannel}

const chPermMaxAge = time.Hour

type chPermCache struct {
	mu sync.Mutex
	m  map[string]chPermEntry
	// loading: the asks in flight, each channel closed when it answers
	loading map[string]chan struct{}
}

type chPermEntry struct {
	allowed map[string]bool
	at      time.Time
}

var chPermIndex = newPerApp() // *App → *chPermCache

func (a *App) chPerms() *chPermCache {
	if v, ok := chPermIndex.Load(a); ok {
		return v.(*chPermCache)
	}
	v, _ := chPermIndex.LoadOrStore(a, &chPermCache{m: map[string]chPermEntry{}, loading: map[string]chan struct{}{}})
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
	start := stale && pc.loading[ch] == nil
	if start {
		pc.loading[ch] = make(chan struct{})
	}
	pc.mu.Unlock()
	if start {
		go a.loadChannelGrants(a.bg, ch)
	}
	return e.allowed[perm]
}

// LoadChannelGrants waits for ch's grants ("" for the team's): it asks
// permissions/info when the cache has no answer for ch (or an hour-old
// one), or waits for the ask in flight, for as long as ctx allows; the
// error is the ask's or ctx's. Slack's channel views fill its permission
// store when the channel opens, and every check reads that store without
// waiting (a miss reads as not allowed); a caller that must know before
// its first read waits here.
func (a *App) LoadChannelGrants(ctx context.Context, ch string) error {
	pc := a.chPerms()
	pc.mu.Lock()
	e, ok := pc.m[ch]
	if ok && time.Since(e.at) <= chPermMaxAge {
		pc.mu.Unlock()
		return nil
	}
	done := pc.loading[ch]
	if done == nil {
		done = make(chan struct{})
		pc.loading[ch] = done
		go a.loadChannelGrants(a.bg, ch)
	}
	pc.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	pc.mu.Lock()
	_, ok = pc.m[ch]
	pc.mu.Unlock()
	if !ok {
		return errors.New("core: permissions/info failed for " + ch)
	}
	return nil
}

func (a *App) loadChannelGrants(ctx context.Context, ch string) {
	pc := a.chPerms()
	allowed, err := a.fetchChannelGrants(ctx, ch)
	pc.mu.Lock()
	done := pc.loading[ch]
	delete(pc.loading, ch)
	if err == nil {
		pc.m[ch] = chPermEntry{allowed: allowed, at: time.Now()}
	}
	pc.mu.Unlock()
	if done != nil {
		close(done)
	}
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
	c, _ := a.S.Conv(ch)
	return a.topicRefusal(c, func(p string) bool { return a.ChannelGrant(ch, p) }) == ""
}

// topicRefusal is canUserSetTopicForChannelWithReasonById's reason (iJOl
// X; getChannelTopicError's codes, MgZJ), "" when the viewer may set c's
// topic; grant reads SET_CHANNEL_TOPIC.
func (a *App) topicRefusal(c *slack.Conversation, grant func(string) bool) string {
	switch {
	case c == nil:
		return "method_not_supported_for_channel_type" // CM
	case (c.IsIM || c.IsMPIM) && c.IsExtShared:
		return "method_not_supported_for_dm" // Jr
	case c.IsArchived:
		return "user_is_restricted" // oP
	case grant(PermSetChannelTopic):
		return ""
	case !c.IsIM:
		return "user_is_restricted"
	}
	// a DM is always the viewer's (crlq bl reads is_member, which Slack's
	// DM objects carry)
	if a.PostingFor(c).CanPost() && !a.viewerRestricted() {
		return ""
	}
	return "user_is_restricted"
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
	}
	return a.ChannelGrant(ch, PermSetChannelPurpose)
}
