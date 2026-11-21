package core

import (
	"context"
	"time"
)

// A channel's member counts after someone joins or leaves it. Slack
// (build 2c203245ea9b) handles member_joined_channel and
// member_left_channel for anyone, not just the viewer, in boot-render's
// batchProcessChannelMembers: per channel it dispatches
// updateAndInvalidateMembershipCounts (boot-shared UaE3), which marks the
// channel's membershipCounts stale (client-boot CeSm markObjectsAsStale,
// a ConditionalFetchManager over storeKey "membershipCounts": only a
// count fetched before turns stale). Nothing is fetched then. Every
// reader of the count, getMembershipCountsByChannelId (1Ti7 q8), calls
// maybeRefetch (CeSm ez), so the next draw of a stale count refetches
// edge users/counts (fetchAndUpsertMembershipCountsForChannel, I+V5),
// throttled per channel to one call per 2 s, leading and trailing (CeSm
// l). The readers are the channel header (People) and channel details'
// tab strip ("Members N", about-channel-modal's memberCount: WDcq vC,
// counts.member_count).
//
// Here: the event marks the count stale and says so ("member_count_stale"),
// the views that draw the count call RefreshMemberCount, and the refetch
// lands as loadMemberCount's "member_count" event.

// memberCountThrottle is CeSm's per-channel throttle wait.
const memberCountThrottle = 2 * time.Second

// countThrottle is one channel's throttle state.
type countThrottle struct {
	last     time.Time // the last refetch started
	trailing bool      // a trailing refetch is scheduled
}

// membershipChanged is a member_joined_channel or member_left_channel in
// ch: a count fetched before is stale now, and its readers hear of it.
func (a *App) membershipChanged(ch string) {
	if ch == "" {
		return
	}
	a.mu.Lock()
	_, had := a.memberN[ch]
	if had {
		if a.memberStale == nil {
			a.memberStale = map[string]bool{}
		}
		a.memberStale[ch] = true
	}
	a.mu.Unlock()
	if had {
		a.emit(Event{Type: "member_count_stale", Channel: ch})
	}
}

// MemberCountStale reports whether ch's count was marked stale by a join
// or a leave and hasn't been refetched since.
func (a *App) MemberCountStale(ch string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.memberStale[ch]
}

// RefreshMemberCount is maybeRefetch for a view drawing ch's count: a
// stale count is refetched (users/counts) at most once per 2 s per
// channel, the call that comes too soon running when the 2 s are up; a
// fresh one is left alone. The answer arrives as a "member_count" event
// when the count changed.
func (a *App) RefreshMemberCount(ch string) {
	if ch == "" || a.bg.Err() != nil {
		return
	}
	a.mu.Lock()
	if !a.memberStale[ch] {
		a.mu.Unlock()
		return
	}
	if a.countThr == nil {
		a.countThr = map[string]*countThrottle{}
	}
	t := a.countThr[ch]
	if t == nil {
		t = &countThrottle{}
		a.countThr[ch] = t
	}
	if t.trailing { // the trailing call is queued already
		a.mu.Unlock()
		return
	}
	wait := memberCountThrottle - time.Since(t.last)
	if wait <= 0 {
		t.last = time.Now()
		a.mu.Unlock()
		a.refetchMemberCount(ch)
		return
	}
	t.trailing = true
	a.mu.Unlock()
	time.AfterFunc(wait, func() {
		a.mu.Lock()
		t.trailing = false
		stale := a.memberStale[ch]
		if stale {
			t.last = time.Now()
		}
		a.mu.Unlock()
		if stale {
			a.refetchMemberCount(ch)
		}
	})
}

// refetchMemberCount reads ch's counts again under core's background
// context. The count is fresh from the read's start: a join during it
// marks it stale again, so the next draw reads once more.
func (a *App) refetchMemberCount(ch string) {
	a.mu.Lock()
	delete(a.memberStale, ch)
	a.mu.Unlock()
	a.sideRead(ch, "users/counts (stale)", func(ctx context.Context) {
		if err := a.loadMemberCountNow(ctx, ch); err != nil {
			a.mu.Lock()
			if a.memberStale == nil {
				a.memberStale = map[string]bool{}
			}
			a.memberStale[ch] = true // still stale: the next draw tries again
			a.mu.Unlock()
		}
	})
}

// MemberCountsCached is the counts of ch's last users/counts read, ok
// false before one answered.
func (a *App) MemberCountsCached(ch string) (MemberCounts, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mc, ok := a.memberMC[ch]
	return mc, ok
}
