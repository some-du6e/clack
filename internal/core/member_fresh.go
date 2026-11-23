package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// How Slack keeps the members it shows current (client-boot DOS4, TruP,
// p7oS; build 35b42de8b870, 2026-11-22). Clack fetched a member only when
// a lookup missed it, and never again: a member who changed their
// picture kept the old one in every row drawn before (Karim, α.143: "two
// different pfps?", one sender's thread broadcast and plain message), and
// a restart drew the offline store's copies for good.
//
//   - getMemberById (DOS4), on every lookup that finds the member, calls
//     maybeRefetch: a member that isn't fresh is fetched again, a fetch
//     that sends each id with the updated stamp of the copy the store
//     holds (fetchObjectsById's updated_ids; users/info answers only the
//     members changed since), and its answer is upserted. Freshness is the
//     session's: after a restart every member is stale until fetched.
//   - What fetches upsert (a lookup's miss, a refetch, user_change) is
//     fresh (markObjectsAsFresh, from the upsert ScqY).
//   - Refetches wait 200 ms after the previous one began and take every
//     id pending by then; none start while the viewer types (2 s after a
//     key) or during boot (pauseRefetchForBoot): a lookup then asks for
//     nothing, and the next one after asks again
//     (member_refetch_after_boot_pause, which would keep them, is off on
//     Hack Club).
//   - On Grid (getFetchManager's isEnterprise branch; Hack Club is in
//     gs_member_subscribe_refusals' treatment, AcknowledgedSubscription-
//     FetchManager) a fetch is also a subscription: flannel pushes
//     user_change only for the members a client subscribed to. A cycle
//     works through the pending ids 100 at a time, one request a second:
//     user_change_subscribe_request for those not held (looksLikeMemberId),
//     waiting for the gateway's answer, then the conditional users/info
//     for all of them. A subscribed member stays fresh while subscribed;
//     one the gateway refused (12: its id cap, after which nothing more is
//     asked; 15: a rate limit, tried once more 10 s later) is fresh for its
//     max age: 1800 s for a member the store doesn't hold or doesn't exist,
//     600 s for an external member or a stranger, else 3600 s, ±20%.
//     Members the store lets go are marked stale (subscribed → held) and
//     the next cycle unsubscribes them first
//     (user_change_unsubscribe_request), 100 at a time.
//   - Off Grid (ConditionalFetchManager) a fetch subscribes nothing and a
//     member is fresh for getMaxAge (the same ages, 1 s for one the store
//     doesn't hold), jittered ±20% when it's marked.
//   - A new connection (Slack's connection count) starts over: nothing is
//     subscribed or fresh, and after a fast reconnect the members that were
//     subscribed are subscribed again and refetched at once.
//
// The control group's SubscriptionFetchManager counts a member fresh only
// while subscribed and marks refused ones subscribed too; Clack runs the
// treatment's rules for every Grid team.

const (
	memberFreshInterval = time.Second            // the cycle's interval
	memberFreshBatch    = slack.MaxUserSubs      // batchMaximum
	memberTypingPause   = 2 * time.Second        // pauseRefetch's timer
	memberRateLimitWait = 10 * time.Second       // _sendSubscriptionRequest's retry
	memberSubTimeout    = 60 * time.Second       // wj(…, 6e4)
	memberFreshFetch    = 65 * time.Second       // the refetch's own limit (FETCH_TIMEOUT_MS)
	gatewayIDCap        = 12                     // gatewayErrorCode: the subscription's id cap
	gatewayRateLimit    = 15                     // gatewayErrorCode: refused for its rate
	memberDebounceFresh = 200 * time.Millisecond // _refetchPendingObjects' delay
)

// getMaxAge's ages (tests shorten them).
var (
	memberMaxAgeKnown   = 3600 * time.Second // a member as such
	memberMaxAgeGone    = 1800 * time.Second // a non-existent (or, subscribed, missing) member
	memberMaxAgeForeign = 600 * time.Second  // an external member or a stranger
	memberMaxAgeMissing = 1 * time.Second    // off Grid: not in the store
)

type memberFresh struct {
	mu          sync.Mutex
	ttl         map[string]time.Time // freshness, as a deadline
	subscribed  map[string]bool
	held        map[string]bool // subscribed, then marked stale: unsubscribed once the store lets them go
	pending     map[string]bool
	keepPending map[string]bool // refetched after a fast reconnect, kept pending once more
	out         map[string]bool // the operation's ids: fresh while it runs
	staleOut    map[string]bool // marked stale while the operation ran
	full        bool            // the gateway refused for its id cap
	conn        uint64          // the connection the state is for
	booting     bool            // pauseRefetchForBoot until bootDone
	typingUntil time.Time
	lastFetch   time.Time // _lastRefetchTimesByTeam
	lastSettled time.Time // _lastSettledTimesByTeam
	notify      chan struct{}
	// asked counts the conditional users/info requests (tests)
	asked int
}

func (m *memberFresh) init() {
	if m.notify == nil {
		m.notify = make(chan struct{}, 1)
		m.ttl, m.subscribed, m.held = map[string]time.Time{}, map[string]bool{}, map[string]bool{}
		m.pending, m.keepPending, m.out, m.staleOut = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
		m.booting = true
	}
}

// freshLocked is isObjectFresh: subscribed, out in the running
// operation, or within its max age.
func (m *memberFresh) freshLocked(id string, now time.Time) bool {
	return m.subscribed[id] || m.out[id] || now.Before(m.ttl[id])
}

func (m *memberFresh) signal() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

// memberHit is maybeRefetch, from every lookup that found the member
// (store.SetMemberHit): one that isn't fresh is fetched again in the next
// cycle, unless refetches are paused (typing, boot), when the hit asks for
// nothing.
func (a *App) memberHit(id string) {
	if !LooksLikeMemberID(id) {
		return
	}
	m := &a.mfresh
	now := time.Now()
	m.mu.Lock()
	m.init()
	if m.booting || now.Before(m.typingUntil) || m.pending[id] || m.freshLocked(id, now) {
		m.mu.Unlock()
		return
	}
	m.pending[id] = true
	m.mu.Unlock()
	m.signal()
}

// MemberRefetchTyping is ConditionalFetchManager.pauseRefetch: a key the
// viewer typed holds refetches back for 2 s.
func (a *App) MemberRefetchTyping() {
	m := &a.mfresh
	m.mu.Lock()
	m.init()
	m.typingUntil = time.Now().Add(memberTypingPause)
	m.mu.Unlock()
}

// memberFreshBootDone is unpauseRefetchAfterBoot.
func (a *App) memberFreshBootDone() {
	m := &a.mfresh
	m.mu.Lock()
	m.init()
	m.booting = false
	m.mu.Unlock()
}

// membersFetched is markObjectsAsFresh for members just upserted: on Grid
// those not subscribed go into the next cycle (to be subscribed, the
// fetch conditional), off Grid each is fresh for its max age.
func (a *App) membersFetched(ids []string) {
	if len(ids) == 0 {
		return
	}
	m := &a.mfresh
	grid := a.memberGrid()
	now := time.Now()
	jit := 1 + (rand.Float64()*0.4 - 0.2)
	queued := false
	m.mu.Lock()
	m.init()
	for _, id := range ids {
		if !LooksLikeMemberID(id) {
			continue
		}
		if !grid {
			m.ttl[id] = now.Add(time.Duration(float64(a.memberMaxAge(id, false)) * jit))
			continue
		}
		if !m.freshLocked(id, now) && !m.pending[id] {
			m.pending[id] = true
			queued = true
		}
	}
	m.mu.Unlock()
	if queued {
		m.signal()
	}
}

// membersStale is markObjectsAsStale (the members cache let them go): a
// subscribed member becomes held, to be unsubscribed, and none is fresh.
func (a *App) membersStale(ids []string) {
	m := &a.mfresh
	m.mu.Lock()
	m.init()
	for _, id := range ids {
		if m.subscribed[id] {
			delete(m.subscribed, id)
			m.held[id] = true
		}
		if m.out[id] {
			m.staleOut[id] = true
		}
		delete(m.ttl, id)
	}
	m.mu.Unlock()
}

// memberFreshConnected is _maybeResetState on a new connection: nothing
// is subscribed or fresh; after a fast reconnect the members that were
// subscribed are refetched (and subscribed) at once.
func (a *App) memberFreshConnected(fast bool) {
	m := &a.mfresh
	if !a.memberGrid() {
		// ConditionalFetchManager (off Grid): the TTLs go only when the
		// freshness epoch moves (the slow connection count: a fast
		// reconnect keeps them); nothing is subscribed
		if fast {
			return
		}
		m.mu.Lock()
		m.init()
		m.conn++
		m.ttl = map[string]time.Time{}
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.init()
	var again []string
	if fast {
		for id := range m.subscribed {
			again = append(again, id)
		}
	}
	m.conn++
	m.subscribed, m.held, m.ttl = map[string]bool{}, map[string]bool{}, map[string]time.Time{}
	m.full = false
	for id := range m.out {
		m.staleOut[id] = true
	}
	for _, id := range again {
		m.keepPending[id] = true
		m.pending[id] = true
	}
	m.mu.Unlock()
	if len(again) > 0 {
		m.signal()
	}
}

// memberGrid is getFetchManager's branch: the subscription manager on
// Enterprise Grid.
func (a *App) memberGrid() bool { return a.C != nil && a.EnterpriseTeam() }

// memberMaxAge is getMaxAge (off Grid) and getUnsubscribedMaxAge (Grid's
// treatment), before the jitter.
func (a *App) memberMaxAge(id string, grid bool) time.Duration {
	su, ok := a.S.RawUser(id)
	switch {
	case !ok && !grid:
		return memberMaxAgeMissing
	case !ok || su == nil || a.S.Nonexistent(id):
		return memberMaxAgeGone
	}
	if su.IsStranger || a.userOf(su).External {
		return memberMaxAgeForeign
	}
	return memberMaxAgeKnown
}

// memberFreshLoop runs the refetches: _refetchPendingObjects' 200 ms
// after the previous one began, then (Grid) the cycle or (off Grid) one
// conditional fetch of every pending id.
func (a *App) memberFreshLoop(ctx context.Context) {
	m := &a.mfresh
	m.mu.Lock()
	m.init()
	notify := m.notify
	m.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
		}
		m.mu.Lock()
		wait := time.Until(m.lastFetch.Add(memberDebounceFresh))
		m.mu.Unlock()
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		m.mu.Lock()
		m.lastFetch = time.Now()
		ids := make([]string, 0, len(m.pending))
		for id := range m.pending {
			ids = append(ids, id)
		}
		m.pending = map[string]bool{}
		conn := m.conn
		m.mu.Unlock()
		if len(ids) == 0 {
			continue
		}
		slices.Sort(ids)
		if a.memberGrid() {
			a.memberCycle(ctx, ids, conn)
		} else {
			a.refetchMembers(ctx, ids)
			a.membersFetched(ids)
		}
		// _updateAfterRefetch: what is still not fresh (a connection
		// that changed meanwhile) goes into the next cycle
		m.mu.Lock()
		now, again := time.Now(), false
		for _, id := range ids {
			if m.keepPending[id] {
				delete(m.keepPending, id)
				continue
			}
			if m.conn != conn && !m.freshLocked(id, now) {
				m.pending[id], again = true, true
			}
		}
		m.mu.Unlock()
		if again {
			m.signal()
		}
	}
}

// memberCycle is AcknowledgedSubscriptionFetchManager's _runCycle for ids
// on connection conn.
func (a *App) memberCycle(ctx context.Context, ids []string, conn uint64) {
	m := &a.mfresh
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	unsubOK := true
	for ctx.Err() == nil {
		m.mu.Lock()
		if m.conn != conn {
			m.mu.Unlock()
			return
		}
		now := time.Now()
		for id := range want {
			// fresh by now, or let go meanwhile (the members cache's pass):
			// a refetch is of copies the store holds, never one asked back
			if _, held := a.S.RawUser(id); !held || m.freshLocked(id, now) {
				delete(want, id)
			}
		}
		var unsub []string
		if unsubOK {
			for id := range m.held {
				if _, ok := a.S.RawUser(id); !ok && !want[id] {
					unsub = append(unsub, id)
				}
			}
			slices.Sort(unsub)
			unsub = unsub[:min(len(unsub), memberFreshBatch)]
		}
		var batch []string
		for id := range want {
			batch = append(batch, id)
		}
		slices.Sort(batch)
		batch = batch[:min(len(batch), memberFreshBatch)]
		wait := time.Until(m.lastSettled.Add(memberFreshInterval))
		m.mu.Unlock()
		if len(unsub) == 0 && len(batch) == 0 {
			return
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait + time.Duration(rand.Float64()*0.1*float64(wait))):
			}
			continue
		}
		if len(unsub) > 0 {
			unsubOK = a.unsubscribeMembers(ctx, unsub, conn)
			continue
		}
		a.fetchMemberBatch(ctx, batch, conn)
		for _, id := range batch {
			delete(want, id)
		}
	}
}

// fetchMemberBatch is _fetchBatch: subscribe the batch's members that
// aren't held (unless the gateway is full), fetch them all conditionally,
// then settle each: subscribed, held again, or fresh for its max age.
func (a *App) fetchMemberBatch(ctx context.Context, ids []string, conn uint64) {
	m := &a.mfresh
	m.mu.Lock()
	var heldIDs, subIDs []string
	for _, id := range ids {
		if m.held[id] {
			heldIDs = append(heldIDs, id)
		} else {
			subIDs = append(subIDs, id)
		}
	}
	try := len(subIDs) > 0 && !m.full
	m.out, m.staleOut = map[string]bool{}, map[string]bool{}
	for _, id := range ids {
		m.out[id] = true
	}
	m.mu.Unlock()
	var subErr error
	if try {
		subErr = a.subscribeRequest(ctx, conn, func(c context.Context) error {
			return a.RTM.SubscribeUsers(c, filterMemberIDs(subIDs))
		})
	}
	a.refetchMembers(ctx, ids)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSettled = time.Now()
	stale := m.staleOut
	m.out, m.staleOut = map[string]bool{}, map[string]bool{}
	if m.conn != conn {
		return
	}
	var ge *slack.GatewayError
	if errors.As(subErr, &ge) && ge.Code == gatewayIDCap {
		m.full = true
	}
	for _, id := range heldIDs {
		if !stale[id] && m.held[id] {
			delete(m.held, id)
			m.subscribed[id] = true
		}
	}
	now := time.Now()
	jit := 1 + (rand.Float64()*0.4 - 0.2)
	for _, id := range subIDs {
		switch {
		case try && subErr == nil && stale[id]:
			m.held[id] = true
		case try && subErr == nil:
			m.subscribed[id] = true
		case !stale[id]: // _markAsFetched
			m.ttl[id] = now.Add(time.Duration(float64(a.memberMaxAge(id, true)) * jit))
		}
	}
}

// unsubscribeMembers is _unsubscribeBatch: false when the gateway refused
// for its rate (no more unsubscribing this cycle).
func (a *App) unsubscribeMembers(ctx context.Context, ids []string, conn uint64) bool {
	err := a.subscribeRequest(ctx, conn, func(c context.Context) error {
		return a.RTM.UnsubscribeUsers(c, ids)
	})
	m := &a.mfresh
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSettled = time.Now()
	var ge *slack.GatewayError
	if errors.As(err, &ge) && ge.Code == gatewayRateLimit {
		return false
	}
	for _, id := range ids {
		delete(m.held, id)
	}
	if err == nil {
		m.full = false
	}
	return true
}

// subscribeRequest is _sendSubscriptionRequest: the request, and once
// more after 10 s when the gateway refused it for its rate and the
// connection is still the one it was for.
func (a *App) subscribeRequest(ctx context.Context, conn uint64, send func(context.Context) error) error {
	if a.RTM == nil {
		return errors.New("core: no realtime connection")
	}
	try := func() error {
		c, cancel := context.WithTimeout(ctx, memberSubTimeout)
		defer cancel()
		return send(c)
	}
	err := try()
	var ge *slack.GatewayError
	if !errors.As(err, &ge) || ge.Code != gatewayRateLimit {
		if err != nil {
			log.Printf("members: subscription request: %v", err)
		}
		return err
	}
	select {
	case <-ctx.Done():
		return err
	case <-time.After(memberRateLimitWait):
	}
	a.mfresh.mu.Lock()
	same := a.mfresh.conn == conn
	a.mfresh.mu.Unlock()
	if !same {
		return err
	}
	return try()
}

func filterMemberIDs(ids []string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if LooksLikeMemberID(id) {
			out = append(out, id)
		}
	}
	return out
}

// refetchMembers is the conditional fetch (fetchAndUpsertMembersById with
// fetchConditionally): users/info with the stamps of the copies the store
// holds; a member that comes back changed is stored and its views drawn
// again ("user", as user_change does).
func (a *App) refetchMembers(ctx context.Context, ids []string) {
	stamps := make(map[string]int64, len(ids))
	for _, id := range ids {
		if su, ok := a.S.RawUser(id); ok && su != nil { // one the store let go isn't asked back
			stamps[id] = su.Updated
		}
	}
	if len(stamps) == 0 {
		return
	}
	a.mfresh.mu.Lock()
	a.mfresh.asked++
	a.mfresh.mu.Unlock()
	fctx, cancel := context.WithTimeout(ctx, memberFreshFetch)
	defer cancel()
	r := a.C.MembersRefetch(fctx, stamps)
	if r.Err != nil {
		log.Printf("members: refetch of %d: %v", len(stamps), r.Err)
	}
	changed := 0
	for _, nu := range r.Users {
		// a copy newer than the answer landed meanwhile (user_change while
		// the request was out): the answer is the older one
		if old, ok := a.S.RawUser(nu.ID); ok && old != nil && old.Updated > nu.Updated {
			continue
		}
		if old, ok := a.S.RawUser(nu.ID); ok && old != nil && sameMember(old, &nu) {
			continue // what the store holds already
		}
		a.putUserLive(nu)
		changed++
	}
	if changed > 0 {
		log.Printf("members: refetched %d: %d changed", len(stamps), changed)
	}
}

// sameMember reports whether two copies of a member say the same
// (presence aside: the store keeps its own).
func sameMember(a, b *slack.User) bool {
	x, y := *a, *b
	x.Presence, y.Presence = "", ""
	ja, err1 := json.Marshal(x)
	jb, err2 := json.Marshal(y)
	return err1 == nil && err2 == nil && string(ja) == string(jb)
}

// MemberRefetches is how many conditional users/info requests the
// freshness manager made (tests).
func (a *App) MemberRefetches() int {
	a.mfresh.mu.Lock()
	defer a.mfresh.mu.Unlock()
	return a.mfresh.asked
}
