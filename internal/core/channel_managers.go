package core

// The channel managers store and the tab permission check that reads it,
// as Slack's web client wires them (build 859cadc0e0ab, read from the
// public CDN without a cookie; docs/CHANNELS-AUDIT.md "Pins tab and
// bookmark folders"):
//
//   - The store (client-boot hRtB, storeKey "channelManagers",
//     persistenceEnabled) keeps each channel's manager ids, replaced by
//     setChannelManagers. Only fetchAndUpsertChannelManagers (boot-shared
//     evGV: admin.roles.entity.listAssignments {entity_id}, reason
//     "fetch-channel-managers", the users of role Rl0A) writes it. Its
//     callers are the views that show managers: the channel details view
//     on a paid team (client-channel-details-view), About's Managed by
//     (BaseAboutChannelModal), the Channel Managers modal, the posting
//     permissions while who_can_post is channel_managers (TrGd), the app
//     profile card and Slackbot's channel settings. The Pins and folder
//     tabs fetch nothing: their check reads what the store holds, and the
//     store outlives a restart. Clack keeps it next to the account's
//     cache, and core.App.Managers (every one of those reads in Clack)
//     fills it.
//   - useCanEditTabs (+n5q) is mwX5 Ln for the channel:
//     no for an unknown conversation, a 1:1 DM with a deactivated person
//     (OePe K), a conversation the viewer isn't in (JUrK bl) or can't post
//     in (o/IU c). With who_can_manage_tabs channel_managers: yes on a free
//     team (not isPaidTeam) for the channel's creator or a workspace admin
//     (IJC0 M, Yyn8 j); otherwise yes only for a channel manager (uKU9 O:
//     the store) or someone who may edit the channel's managers (SxxJ $).
//     With no who_can_manage_tabs on Enterprise Grid (GmTx k): the
//     MANAGE_CHANNEL_TABS grant, the org's for a group DM (QaSy yI), else
//     the channel's (QaSy WB). Otherwise yes.
//   - canUserEditChannelManagers (SxxJ B): a known conversation that
//     isn't a DM or group DM, not on a free plan (mAu1 tc); the viewer
//     acting as a workspace admin in it (nARn x → 7RIn S4) or a member
//     with the ASSIGN_CHANNEL_MANAGER grant.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// PermAssignChannelManager is the manager grant (QxkB's permission enum
// key); PermManageChannelTabs is chdetails.go's.
const PermAssignChannelManager = "ASSIGN_CHANNEL_MANAGER"

const channelManagersFile = "channel_managers.json"

type chManagersState struct {
	mu     sync.Mutex
	loaded bool
	m      map[string][]string
}

var chManagersIndex sync.Map // *App → *chManagersState

func (a *App) chManagers() *chManagersState {
	v, _ := chManagersIndex.LoadOrStore(a, &chManagersState{m: map[string][]string{}})
	return v.(*chManagersState)
}

func (a *App) channelManagersPath() string {
	dir := a.S.CacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, channelManagersFile)
}

// loadChannelManagersLocked reads the persisted store once. st.mu is held.
func (a *App) loadChannelManagersLocked(st *chManagersState) {
	if st.loaded {
		return
	}
	st.loaded = true
	p := a.channelManagersPath()
	if p == "" || a.S.Dropped() {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var m map[string][]string
	if json.Unmarshal(b, &m) == nil {
		for k, v := range m {
			if _, ok := st.m[k]; !ok {
				st.m[k] = v
			}
		}
	}
}

// setChannelManagers is setChannelManagers (hRtB CP): ch's managers
// replace what the store had, and the store is written to disk.
func (a *App) setChannelManagers(ch string, ids []string) {
	st := a.chManagers()
	st.mu.Lock()
	a.loadChannelManagersLocked(st)
	if slices.Equal(st.m[ch], ids) {
		st.mu.Unlock()
		return
	}
	st.m[ch] = slices.Clone(ids)
	b, err := json.Marshal(st.m)
	p := a.channelManagersPath()
	st.mu.Unlock()
	if err == nil && p != "" && !a.S.Dropped() {
		_ = os.WriteFile(p, b, 0o600)
	}
	a.emit(Event{Type: "permissions", Channel: ch})
}

// ChannelManagerIDs is the store's managers for ch (n7Cz t); nil when no
// read has filled it.
func (a *App) ChannelManagerIDs(ch string) []string {
	st := a.chManagers()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadChannelManagersLocked(st)
	return slices.Clone(st.m[ch])
}

// isChannelManager is isUserChannelManagerOfChannel (uKU9 O).
func (a *App) isChannelManager(ch string) bool {
	return slices.Contains(a.ChannelManagerIDs(ch), a.S.SelfID())
}

// selfWorkspaceAdmin is isWorkspaceAdmin (Yyn8 j) for the viewer.
func (a *App) selfWorkspaceAdmin() bool {
	if u, ok := a.S.User(a.S.SelfID()); ok {
		return u.IsAdmin
	}
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.Self.IsAdmin
}

// canEditChannelManagers is canUserEditChannelManagers (SxxJ B).
func (a *App) canEditChannelManagers(ch string, member bool) bool {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return false
		}
	}
	if c.IsIM || c.IsMPIM || a.OnFreePlan() {
		return false
	}
	asAdmin := a.selfIsWorkspaceAdmin() // nARn x: canUserActAsWorkspaceAdmin in the channel
	if !asAdmin && !member {
		return false
	}
	return a.ChannelGrant(ch, PermAssignChannelManager) || asAdmin
}

// CanEditTabs is useCanEditTabs (+n5q → mwX5 Ln): who may add, rename,
// reorder and remove the channel's tabs and folders. It is the one check
// every tab editor reads (the tab bar's menu and drags, Manage tabs, the
// folders, Pins). It never blocks: the grant and the managers come from
// their stores, and a "permissions" event follows their answers, so a
// view that reads it each frame updates when they land (Slack's hook
// re-renders the same way).
func (a *App) CanEditTabs(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return false
		}
	}
	if c.IsIM { // OePe K: a DM with a deactivated person
		if u, ok := a.S.User(c.User); ok && u.Deleted {
			return false
		}
	}
	member := a.isMemberConv(c)
	if !member || c.IsArchived || !a.PostingFor(c).CanPost() { // JUrK bl, o/IU c (read-only, thread-only, archived)
		return false
	}
	who := ParseChannelProps(c.Properties).WhoCanManageTabs
	switch {
	case who == "channel_managers":
		if !a.paidTeam() && (c.Creator == a.S.SelfID() || a.selfWorkspaceAdmin()) {
			return true
		}
		return a.isChannelManager(ch) || a.canEditChannelManagers(ch, member)
	case who == "" && a.IsEnterprise():
		if c.IsMPIM {
			return a.TeamGrant(PermManageChannelTabs)
		}
		return a.ChannelGrant(ch, PermManageChannelTabs)
	}
	return true
}
