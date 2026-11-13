package core

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's members cache (store/members.go) and how Slack fetches a member
// a lookup missed: the UnknownFetchManager behind getMemberById
// (client-boot Gx04, made in gIKW with fetch: fetchMembersForTeamId(team,
// ids, "UnknownFetchManager"); build 0e695dd18302fa08, 2026-11-12).
//
//   - fetchObjectWithId hands back an unknown member at once (no names,
//     the unknown avatar: what a row draws until the answer) and, unless
//     a fetch is in flight, starts fetchUnknownsForTeam, which waits until
//     DEBOUNCE_INTERVAL (200 ms) has passed since the previous fetch began
//     (none: the next tick), then asks for every id pending by then (no
//     batch limit for members). Ids missed while a fetch is in flight
//     wait for it and go in the next one: one request at a time.
//   - fetchMembersForTeamId leaves out ids the store holds by then and
//     calls fetchRawMembersById, which is users/info over flannel
//     (fetchMembersById: chunks of 100, two at a time, pending_ids asked
//     again: slack.MembersInfo).
//   - fetchWithRetries gives the fetch 65 s and tries a timed-out one up
//     to three times.
//   - What came back is upserted (the members' rows draw again); ids it
//     didn't return become non-existent members (upsertNonExistent), so
//     they aren't asked for again until the cache evicts them.
//
// Slack also treats a fetch that failed outright as "not returned"
// (fetchMembersForTeamId catches it and answers with no members), and its
// members subscription refreshes them when the socket comes back. Clack
// has no such refresh, so a request that failed (the network, a timeout,
// an error answer) leaves its ids to be asked again at the next lookup,
// as the channel name resolver does; the client's backoff paces repeats.
//
// The reason fetchMembersForTeamId must be given ("UnknownFetchManager")
// and flannel's own (flannel-call:users/info) only label Slack's logs:
// users/info carries no reason.
const (
	memberDebounce = 200 * time.Millisecond // DEBOUNCE_INTERVAL
	memberTimeout  = 65 * time.Second       // FETCH_TIMEOUT_MS
	memberAttempts = 3                      // MAX_FETCH_ATTEMPTS
)

// memberTick stands in for the timer Slack's first fetch after a quiet
// spell waits on (a zero delay: it runs once the render that missed the
// members is done), so one frame's misses go in one request.
var memberTick = 16 * time.Millisecond

// memberLoop is the UnknownFetchManager's fetchUnknownsForTeam loop.
func (a *App) memberLoop(ctx context.Context) {
	notify := a.S.MissingUsersNotify()
	var last time.Time // lastFetchTimesByTeam
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
		}
		wait := max(time.Until(last.Add(memberDebounce)), memberTick)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		ids := a.S.TakeMissingUsers()
		if len(ids) == 0 {
			continue
		}
		last = time.Now()
		a.fetchUnknownMembers(ctx, ids)
	}
}

// fetchUnknownMembers is one fetchUnknownsForTeam: fetchMembersForTeamId
// with fetchWithRetries, then the upserts, then "names" for the views.
func (a *App) fetchUnknownMembers(ctx context.Context, ids []string) {
	t0 := time.Now()
	ask := a.S.MissingUsers(ids) // fetchMembersForTeamId: not the ones the store holds by now
	var users, missing, failed int
	if len(ask) > 0 {
		for attempt := 1; ; attempt++ {
			fctx, cancel := context.WithTimeout(ctx, memberTimeout)
			r := a.C.MembersInfo(fctx, ask)
			timedOut := errors.Is(fctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
			cancel()
			if timedOut {
				// the timeout rejects the fetch: what it got is dropped
				if attempt < memberAttempts {
					log.Printf("members: users/info for %d timed out (%d); retrying", len(ask), attempt)
					continue
				}
				r = slack.MembersResult{Failed: ask, Err: fctx.Err()}
			}
			a.S.PutUsers(r.Users)
			a.S.PutNonexistent(r.Missing)       // upsertNonExistent
			a.S.ForgetMissing('u', r.Failed...) // asked again at the next lookup
			users, missing, failed = len(r.Users), len(r.Missing), len(r.Failed)
			if r.Err != nil {
				log.Printf("members: users/info: %v", r.Err)
			}
			break
		}
	}
	// the rows that drew unknown members draw them again (the answer's
	// upserts re-render Slack's), and so do ones the answer doesn't
	// change: an id the store got meanwhile drew unknown until now
	var named []string
	for _, id := range ids {
		if _, ok := a.S.RawUser(id); ok || a.S.Nonexistent(id) {
			named = append(named, id)
		}
	}
	if len(ask) > 0 || failed > 0 {
		log.Printf("members: %d missed, %d asked: %d found, %d not found, %d failed in %s",
			len(ids), len(ask), users, missing, failed, time.Since(t0).Round(time.Millisecond))
	}
	if len(named) > 0 {
		a.emit(Event{Type: "names", Users: named})
	}
}

// membersLoop runs the members cache's pass when a lookup (or the cache
// loop) found it due: Slack's idle callback.
func (a *App) membersLoop(ctx context.Context) {
	due := a.S.MembersDue()
	for {
		select {
		case <-ctx.Done():
			return
		case <-due:
		}
		a.EvictMembers()
	}
}

// AfterMembersEvict, when set, is called after a members pass let members
// go, with how many (cmd/clack collects then, as after a message pass).
var AfterMembersEvict func(members int)

// EvictMembers runs the members cache's pass now (Slack's evict(): the
// store keeps the viewer and the 500 members found most recently once it
// holds 700) and returns the ids it removed. Views drawing one of them
// look it up again, which asks users/info for it.
func (a *App) EvictMembers() []string {
	t0 := time.Now()
	before, gone := a.S.MembersStats()
	a.touchSidebarMembers()
	ids := a.S.RunMembersPass(t0)
	if len(ids) > 0 || before+gone >= 700 {
		after, gone2 := a.S.MembersStats()
		log.Printf("core: members cache: %d members and %d not found; %d went, %d and %d left (%s)",
			before, gone, len(ids), after, gone2, time.Since(t0).Round(time.Millisecond))
	}
	if len(ids) > 0 {
		a.emit(Event{Type: "members_evicted", Users: ids})
		if f := AfterMembersEvict; f != nil {
			f(len(ids))
		}
	}
	return ids
}

// touchSidebarMembers records a hit on the members the sidebar shows (the
// viewer, DM partners, group DMs' members): Slack's sidebar sections
// (useCalculateSidebarSections, which reads each DM's member to hide
// deactivated people and apps) run on every store change, so its people
// are always among the last found when a pass sorts them. Clack's sidebar
// rules read them at each snapshot, which may be long before the pass.
func (a *App) touchSidebarMembers() {
	var ids []string
	a.S.RLock()
	for _, c := range a.S.Convs {
		switch {
		case c.IsIM && c.User != "":
			ids = append(ids, c.User)
		case c.IsMPIM:
			ids = append(ids, c.Members...)
		}
	}
	a.S.RUnlock()
	for _, id := range ids {
		a.S.TouchMember(id)
	}
}

// membersOnline is mG45 isOnline for the members pass: clientOnline (the
// machine has a network, online.go) and serviceOnline (the Slack API is
// up: Slack sets it false when calls fail and true again at api.test;
// Clack's nearest is a session that wasn't ended).
func (a *App) membersOnline() bool {
	return a.ClientOnline() && !a.C.SignedOut()
}
