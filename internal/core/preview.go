package core

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Channel previews, ported from Slack's web client (build a84662d2d135,
// 2026-11-08; webpack ids in client-boot and gantry-v2-async-client-boot-
// {render,shared}):
//
//   - a conversation is a preview (client-boot Opi6 isPreview) when it is a
//     channel or private channel, not archived, and the viewer isn't a
//     member. MessagePaneInput then draws JoinChannelPane (boot-shared UY14)
//     instead of the composer.
//   - the selected conversation is always a sidebar candidate and its row
//     is never filtered out (boot-render lJBI: `e.id===currentlySelectedId
//     → addChannel`, and the row filter returns true for it first), so a
//     previewed channel shows a row while it's open and drops out when the
//     viewer leaves it.
//   - Join calls conversations.join; "Join…" under temp_join_channel adds
//     expiration_ts (boot-shared uFAp: end of today, +48h, +1 week), and
//     the channel then sits in the Temporary channels section
//     (temporaryChannelStore, filled from channel_joined's
//     temp_membership_expiration_ts).

// TempJoin is a "Join…" duration (Slack's JoinChannelPane menu).
type TempJoin string

const (
	TempToday   TempJoin = "today"
	TempTwoDays TempJoin = "two_days"
	TempWeek    TempJoin = "one_week"
)

// TempJoinExpiry is uFAp's getTemporaryChannelJoinTimestampFromDuration:
// the end of today (local time), 48 hours, or a week from now.
func TempJoinExpiry(d TempJoin, now time.Time) int64 {
	switch d {
	case TempToday:
		y, m, day := now.Date()
		return time.Date(y, m, day, 23, 59, 59, 0, now.Location()).Unix()
	case TempTwoDays:
		return now.Add(48 * time.Hour).Unix()
	case TempWeek:
		return now.AddDate(0, 0, 7).Unix()
	}
	return 0
}

// TempJoinEnabled reports Slack's temp_join_channel experiment
// (+7BQ isTemporaryJoinChannelsEnabled).
func (a *App) TempJoinEnabled() bool { return a.experiment("temp_join_channel") == "on" }

// isPreviewConv is Opi6's isPreview for a stored conversation: a channel
// the viewer isn't in (outside the sidebar's member set), not archived.
func (a *App) isPreviewConv(c *slack.Conversation) bool {
	if c == nil || c.IsIM || c.IsMPIM || c.IsArchived || c.IsMember {
		return false
	}
	_, member := a.S.Conv(c.ID)
	return !member
}

// SetSelected tells core which conversation is open ("" for none), Slack's
// currentlySelectedId: it is always a sidebar candidate and its row is
// never hidden. A change that moves a row in or out resnapshots the
// sidebar ("sidebar_changed").
func (a *App) SetSelected(id string) {
	a.mu.Lock()
	old := a.selected
	a.selected = id
	a.mu.Unlock()
	if old == id {
		return
	}
	if a.selectionShapesSidebar(old) || a.selectionShapesSidebar(id) {
		a.emit(Event{Type: "sidebar_changed"})
	}
}

func (a *App) selectedID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selected
}

// selectionShapesSidebar reports whether selecting id can change what the
// sidebar shows: it's outside the member set (a preview), a DM the rules
// would otherwise drop, or a row a section filter could hide.
func (a *App) selectionShapesSidebar(id string) bool {
	if id == "" {
		return false
	}
	c, ok := a.S.Conv(id)
	if !ok {
		_, ext := a.S.ExtConv(id)
		return ext
	}
	if c.IsIM || c.IsMPIM {
		if !a.dmCandidate(id) {
			return true
		}
	}
	a.S.RLock()
	p := a.S.Prefs
	muted := a.S.Muted[id]
	a.S.RUnlock()
	return muted || sidebarBehavior(p) != "" || c.Dormant()
}

// selectedExt returns the selected conversation when it's known only from
// ResolveConv (a previewed channel), which the sidebar then lists too.
func (a *App) selectedExt() (*slack.Conversation, bool) {
	id := a.selectedID()
	if id == "" {
		return nil, false
	}
	if _, ok := a.S.Conv(id); ok {
		return nil, false
	}
	c, ok := a.S.ExtConv(id)
	if !ok || c.IsIM {
		return nil, false
	}
	return c, true
}

// sidebarConv looks a sidebar row up: the member set, or the selected
// conversation outside it.
func (a *App) sidebarConv(id string) (*slack.Conversation, bool) {
	if c, ok := a.S.Conv(id); ok {
		return c, true
	}
	if c, ok := a.selectedExt(); ok && c.ID == id {
		return c, true
	}
	return nil, false
}

// JoinFor joins a channel for a while (Slack's "Join…" menu): the server
// ends the membership at expiration_ts, and until then the channel sits in
// the Temporary channels section. A zero d is a plain Join.
func (a *App) JoinFor(ctx context.Context, ch string, d TempJoin) (Conv, error) {
	exp := TempJoinExpiry(d, time.Now())
	if exp == 0 {
		return a.Join(ctx, ch)
	}
	return a.join(ctx, ch, exp)
}

// setTemp records (exp > 0) or clears (exp == 0) a temporary membership.
func (a *App) setTemp(ch string, exp int64) {
	a.mu.Lock()
	if exp > 0 {
		if a.temp == nil {
			a.temp = map[string]int64{}
		}
		a.temp[ch] = exp
	} else {
		delete(a.temp, ch)
	}
	a.mu.Unlock()
}

// tempChannels returns the temporary memberships that haven't expired.
func (a *App) tempChannels(now time.Time) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.temp) == 0 {
		return nil
	}
	out := map[string]bool{}
	for id, exp := range a.temp {
		if exp > now.Unix() {
			out[id] = true
		}
	}
	return out
}

// TempExpiry returns when a temporary membership ends (0: not temporary).
func (a *App) TempExpiry(ch string) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.temp[ch]
}

func joinParams(ch string, exp int64) url.Values {
	p := url.Values{"channel": {ch}}
	if exp > 0 {
		p.Set("expiration_ts", strconv.FormatInt(exp, 10))
	}
	return p
}

// dropMembership takes ch out of the member set (left, archived, deleted)
// but keeps it known as a non-member, so the conversation still open
// turns into a preview (or the archived bar) instead of losing its header.
func (a *App) dropMembership(ch string, archived bool) {
	a.S.Lock()
	c, ok := a.S.Convs[ch]
	delete(a.S.Convs, ch)
	a.S.Unlock()
	a.setTemp(ch, 0)
	if !ok || c.IsIM || c.IsMPIM {
		return
	}
	cp := *c
	cp.IsMember = false
	if archived {
		cp.IsArchived = true
	}
	a.S.PutExtConv(cp)
}
