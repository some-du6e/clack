package core

import (
	"context"
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
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

// usersOut is the members users/info has out, EnsureUsers' and the
// unknown member fetcher's, each with a channel closed once its answer is
// in. Slack has one members UnknownFetchManager per team (0mwJ, made in
// DOS4 H): a member is asked for once at a time, and whoever wants one
// already out waits for that answer. During boot Boot's EnsureUsers (the
// DM rows' people, the viewer) and the fetcher (the sidebar's lookups of
// the same people) asked users/info for them twice.
type usersOut struct {
	mu  sync.Mutex
	out map[string]chan struct{}
}

// claim splits ids into those the caller asks for now, out under done
// until it releases them, and the answers to wait for of those another
// request has out.
func (o *usersOut) claim(ids []string) (ask []string, wait []chan struct{}, done chan struct{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, id := range ids {
		if ch, ok := o.out[id]; ok {
			if ch != done && !slices.Contains(wait, ch) { // done: ids named twice
				wait = append(wait, ch)
			}
			continue
		}
		if done == nil {
			done = make(chan struct{})
			if o.out == nil {
				o.out = map[string]chan struct{}{}
			}
		}
		o.out[id] = done
		ask = append(ask, id)
	}
	return ask, wait, done
}

// release ends claim's ask: its answer is in (stored), or the request
// gave up.
func (o *usersOut) release(ask []string, done chan struct{}) {
	if done == nil {
		return
	}
	o.mu.Lock()
	for _, id := range ask {
		if o.out[id] == done {
			delete(o.out, id)
		}
	}
	o.mu.Unlock()
	close(done)
}

// waitUsers waits for the answers claim handed back (false: ctx ended
// first).
func waitUsers(ctx context.Context, wait []chan struct{}) bool {
	for _, ch := range wait {
		select {
		case <-ch:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// fetchUnknownMembers is one fetchUnknownsForTeam: fetchMembersForTeamId
// with fetchWithRetries, then the upserts, then "names" for the views.
func (a *App) fetchUnknownMembers(ctx context.Context, ids []string) {
	t0 := time.Now()
	// fetchMembersForTeamId: not the ones the store holds by now, nor the
	// ones another request has out (EnsureUsers'): this waits for that
	// answer instead
	miss := a.S.MissingUsers(ids)
	ask, wait, done := a.usersOut.claim(miss)
	var users, missing, failed int
	func() {
		defer a.usersOut.release(ask, done) // before the wait: two requests never wait on each other
		if len(ask) == 0 {
			return
		}
		a.usersAsked.Add(int64(len(ask)))
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
			a.S.PutAbsentUsers(r.Users)         // upsertKnown
			a.S.PutNonexistent(r.Missing)       // upsertNonExistent
			a.S.ForgetMissing('u', r.Failed...) // asked again at the next lookup
			users, missing, failed = len(r.Users), len(r.Missing), len(r.Failed)
			if r.Err != nil {
				log.Printf("members: users/info: %v", r.Err)
			}
			break
		}
	}()
	if len(wait) > 0 {
		if !waitUsers(ctx, wait) {
			return
		}
		// what that request didn't get (it failed, or isn't the fetcher:
		// EnsureUsers marks nobody non-existent) is asked for again at the
		// next lookup, as this one's failures are
		var lost []string
		for _, id := range miss {
			if slices.Contains(ask, id) {
				continue
			}
			if _, ok := a.S.RawUser(id); !ok && !a.S.Nonexistent(id) {
				lost = append(lost, id)
			}
		}
		a.S.ForgetMissing('u', lost...)
	}
	// the rows that drew unknown members draw them again (the answer's
	// upserts re-render Slack's), and so do ones the answer doesn't
	// change: an id the store got meanwhile (another request's) drew
	// unknown until now
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
// look it up again, which asks users/info for it. The sidebar's people
// stay even when there are more than 500 of them
// (store.EvictMembersKeeping).
func (a *App) EvictMembers() []string {
	t0 := time.Now()
	before, gone := a.S.MembersStats()
	keep := slices.Concat(a.sidebarPeopleNow(), a.badgePeopleNow())
	a.touchSidebarMembers(keep)
	ids := a.S.RunMembersPass(t0, keep...)
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

// touchSidebarMembers records a hit on the sidebar's people (ids, from
// sidebarPeopleNow). Slack's sidebar sections (useCalculateSidebarSections)
// read them through getMemberById on every store change, so they are
// always among the last found when a pass sorts them. Clack reads them at
// each snapshot, which may be long before the pass.
func (a *App) touchSidebarMembers(ids []string) {
	for _, id := range ids {
		a.S.TouchMember(id)
	}
}

// sidebarPeople are lJBI's imAndMpimMembers at the last snapshot: the
// partner of every DM the sections list and the members of every group
// DM they list. Slack's useCalculateSidebarSections (boot-shared lJBI,
// build 2c0d02ca130f) reads them with getMemberById (gIKW nv) on every
// store change, for its candidates: open DMs and group DMs, and closed
// ones with unreads, a draft or a huddle, before any section's own
// filter. Closed DMs aren't candidates and are never read, so their
// partners aren't kept and aren't asked for. Its deleted-member test
// (deletedMembers, gIKW G) is a selector over every member, which
// records nothing.
type sidebarPeople struct {
	mu  sync.Mutex
	ids []string
}

func (a *App) setSidebarPeople(ids []string) {
	a.sbPeople.mu.Lock()
	a.sbPeople.ids = ids
	a.sbPeople.mu.Unlock()
}

// readBadgePeople is getMemberById for the people Slack's DM badges read.
// rHEx's DUy0 G and 6Z4k Sm look up the member of every unread, unmuted
// IM on each store change (gIKW nv: a hit, or a miss that Gx04's
// UnknownFetchManager fetches, in one request with the others pending:
// memberLoop here), all but Slackbot's own IM, which needs no member.
// Clack's sidebar reads only the DMs it lists, so an unread IM it doesn't
// (a system user's, or a closed DM a counts answer made unread before the
// next snapshot) kept the badge skips of a person while its member was
// unknown: an app's DM badged the DMs tab. Clack reads them when it builds
// a snapshot and when client.counts answers (readBadgePerson for a
// message or a mark), so "names" follows a fetch and the next snapshot has the right
// skips; a miss is asked once (store noteMissing), and nothing here runs
// per frame. The ids stay through the members pass with the sidebar's
// people (EvictMembers), as Slack's lookups keep them among the members
// found last. Archived IMs aren't read: Slack's counts skip them (ZdmU).
func (a *App) readBadgePeople() {
	a.S.RLock()
	var ids []string
	for id, c := range a.S.Convs {
		if !c.IsIM || c.IsArchived || c.User == "" || c.User == SlackbotUserID || a.S.Muted[id] {
			continue
		}
		if e := a.S.Counts[id]; e != nil && (e.HasUnreads || e.MentionCount > 0) {
			ids = append(ids, c.User)
		}
	}
	a.S.RUnlock()
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for _, id := range ids {
		a.S.User(id) // a hit, or a miss memberLoop fetches
	}
	a.bdPeople.mu.Lock()
	a.bdPeople.ids = ids
	a.bdPeople.mu.Unlock()
}

// readBadgePerson is readBadgePeople's lookup for one IM: the one a
// message is about to make unread, or one a mark from another client left
// unread (im_marked). For a message it runs before the message's own
// EnsureUsers, whose users/info answer would leave the member known with
// no "names": recorded first, the miss's fetch emits "names" either way
// (fetchUnknownMembers names what the store holds by then).
func (a *App) readBadgePerson(ch string) {
	a.S.RLock()
	c, ok := a.S.Convs[ch]
	read := ok && c.IsIM && !c.IsArchived && c.User != "" && c.User != SlackbotUserID && !a.S.Muted[ch]
	var user string
	if read {
		user = c.User
	}
	a.S.RUnlock()
	if !read {
		return
	}
	a.S.User(user)
	a.bdPeople.mu.Lock()
	if !slices.Contains(a.bdPeople.ids, user) {
		a.bdPeople.ids = append(slices.Clone(a.bdPeople.ids), user)
	}
	a.bdPeople.mu.Unlock()
}

// badgePeopleNow is readBadgePeople's last ids (nil before a read).
func (a *App) badgePeopleNow() []string {
	a.bdPeople.mu.Lock()
	defer a.bdPeople.mu.Unlock()
	return a.bdPeople.ids
}

// sidebarPeopleNow is the last snapshot's sidebarPeople (nil before one).
func (a *App) sidebarPeopleNow() []string {
	a.sbPeople.mu.Lock()
	defer a.sbPeople.mu.Unlock()
	return a.sbPeople.ids
}

// sidebarUsers is the snapshot's Users: the viewer and every DM's
// partner. The people of the DMs and group DMs the sections list are
// read with getMemberById, which records a hit or asks users/info for a
// miss ("names" follows), and are remembered as sidebarPeople; any other
// DM's partner is read as the store holds it (RawUser).
func (a *App) sidebarUsers(convs []Conv, secs []Section, self string) map[string]User {
	listed := map[string]bool{}
	for _, s := range secs {
		for _, l := range [][]string{s.All, s.Channels} {
			for _, id := range l {
				listed[id] = true
			}
		}
	}
	out := map[string]User{}
	if u, ok := a.User(self); ok {
		out[self] = u
	}
	var people []string
	seen := map[string]bool{}
	for _, c := range convs {
		if !listed[c.ID] {
			continue
		}
		switch c.Kind {
		case "im":
			if c.User != "" && !seen[c.User] {
				seen[c.User] = true
				people = append(people, c.User)
				if u, ok := a.User(c.User); ok {
					out[c.User] = u
				}
			}
		case "mpim":
			for _, m := range c.Members {
				if m != "" && !seen[m] {
					seen[m] = true
					people = append(people, m)
					a.S.User(m)
				}
			}
		}
	}
	a.setSidebarPeople(people)
	for _, c := range convs {
		if c.Kind != "im" || c.User == "" {
			continue
		}
		if _, ok := out[c.User]; ok {
			continue
		}
		if u, ok := a.RawUser(c.User); ok {
			out[c.User] = u
		}
	}
	return out
}

// bootDMPeople is Boot's list of the DM rows' people for users/info: the
// viewer and the partners of the DMs the sidebar lists. Those are the
// open ones (open: sidebar.dms' flags; nil reads core's own, the boot
// data's on a warm start, or every DM while nothing is known, when the
// sidebar lists them all) and closed ones with unreads in the counts the
// store holds by then (a warm start's, or client.counts' if it answered
// first). A closed DM a draft or a huddle keeps, or one with unreads a
// cold start learns of later, is named one users/info later, by the
// first snapshot's lookup (sidebarUsers).
func (a *App) bootDMPeople(convs []slack.Conversation, open map[string]bool) []string {
	ids := []string{a.C.Creds.UserID}
	for _, c := range convs {
		if !c.IsIM || c.User == "" {
			continue
		}
		var row bool
		if open != nil {
			row = open[c.ID]
		} else {
			row = a.dmOpen(c.ID)
		}
		if !row {
			e, ok := a.S.Count(c.ID)
			row = ok && (e.HasUnreads || e.MentionCount > 0)
		}
		if row {
			ids = append(ids, c.User)
		}
	}
	return ids
}

// rawUserName is Store.UserName without the lookup's bookkeeping.
func (a *App) rawUserName(id string) string {
	u, _ := a.S.RawUser(id)
	return store.DisplayName(u)
}

// membersOnline is mG45 isOnline for the members pass: clientOnline (the
// machine has a network, online.go) and serviceOnline (the Slack API is
// up: Slack sets it false when calls fail and true again at api.test;
// Clack's nearest is a session that wasn't ended).
func (a *App) membersOnline() bool {
	return a.ClientOnline() && !a.C.SignedOut()
}
