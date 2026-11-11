package core

import (
	"context"
	"net/url"
	"sync"
	"time"
)

// Unarchiving from the archived channel's footer (the
// unarchive_channel_banner_polish experiment, boot-render bgYM
// MessagePaneInputDisabled): the footer offers "Unarchive" when
// canUserUnarchiveChannel (Tq8O) says the viewer may, and its link is
// submitUnarchive (6ndL): conversations.unarchive {channel}. Slack's
// channel_unarchive event then brings the composer back.

// PermUnarchiveChannel is permissions/info's UNARCHIVE_CHANNEL.
const PermUnarchiveChannel = "UNARCHIVE_CHANNEL"

type unarchivePerms struct {
	mu      sync.Mutex
	m       map[string]permAt
	loading map[string]bool
}

type permAt struct {
	ok bool
	at time.Time
}

var unarchiveIndex sync.Map // *App → *unarchivePerms

func (a *App) unarchivePerms() *unarchivePerms {
	if v, ok := unarchiveIndex.Load(a); ok {
		return v.(*unarchivePerms)
	}
	v, _ := unarchiveIndex.LoadOrStore(a, &unarchivePerms{m: map[string]permAt{}, loading: map[string]bool{}})
	return v.(*unarchivePerms)
}

// CanUnarchive is canUserUnarchiveChannel without blocking: an archived
// channel (not a DM or group DM) where permissions/info grants
// UNARCHIVE_CHANNEL. Until the permission is known (an hour's cache, like
// the broadcast permissions) it reads false and a load starts.
func (a *App) CanUnarchive(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok {
		c, ok = a.S.ExtConv(ch) // an archived channel outside the sidebar
	}
	if !ok || c.IsIM || c.IsMPIM || !c.IsArchived {
		return false
	}
	pc := a.unarchivePerms()
	pc.mu.Lock()
	e, have := pc.m[ch]
	stale := !have || time.Since(e.at) > broadcastPermsMaxAge
	start := stale && !pc.loading[ch] && a.C != nil
	if start {
		pc.loading[ch] = true
	}
	pc.mu.Unlock()
	if start {
		go func() {
			ctx, cancel := context.WithTimeout(a.bgCtx(), 30*time.Second)
			defer cancel()
			ok, err := a.fetchUnarchivePerm(ctx, ch)
			pc.mu.Lock()
			delete(pc.loading, ch)
			if err == nil {
				pc.m[ch] = permAt{ok: ok, at: time.Now()}
			}
			pc.mu.Unlock()
			if err == nil {
				a.emit(Event{Type: "conv", Channel: ch}) // redraw the footer
			}
		}()
	}
	return have && e.ok
}

func (a *App) fetchUnarchivePerm(ctx context.Context, ch string) (bool, error) {
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": []string{PermUnarchiveChannel}, "channel_id": ch}
	var r struct {
		Allowed []string `json:"allowed"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err != nil {
		return false, err
	}
	for _, p := range r.Allowed {
		if p == PermUnarchiveChannel {
			return true, nil
		}
	}
	return false, nil
}

// Unarchive is submitUnarchive: conversations.unarchive, then the
// channel's info again (what Slack's channel_unarchive event does).
func (a *App) Unarchive(ctx context.Context, ch string) error {
	if err := a.C.Call(ctx, "conversations.unarchive", url.Values{"channel": {ch}}, nil); err != nil {
		return err
	}
	a.refreshConv(ctx, ch)
	return nil
}
