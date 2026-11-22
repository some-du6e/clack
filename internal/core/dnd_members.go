package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Other members' Do Not Disturb, as Slack's dndV2 store keeps it (client-
// boot Z/ON, build 35b42de8b870). A presence indicator reads
// getDndStatusByMemberId (d4YN Vd), which calls the conditional fetch
// manager's maybeRefetch: a member it hasn't fetched, or fetched more than
// maxAge (600 s) ago, goes to fetchAndUpsertDndForUsers, which asks
// dnd.teamInfo for up to 30 members a request (Glrq Rs: users=a,b,…) and
// keeps each one's next_dnd_start_ts / next_dnd_end_ts. isInDnd (5nPr a)
// is then start ≤ now < end, a 1 in either meaning no schedule; the
// answer carries no snooze for others. dnd_updated_user marks the member
// stale (markObjectsAsStale), so the next read asks again. The viewer's
// own DND stays dnd.info's (dnd.go).

// memberDNDMaxAge is dndV2's maxAge for a member it isn't subscribed to.
const memberDNDMaxAge = 600 * time.Second

// memberDNDBatch is fetchAndUpsertDndForUsers' batch size.
const memberDNDBatch = 30

type memberDND struct {
	start, end int64
	at         time.Time // when dnd.teamInfo answered; zero once marked stale
}

type memberDNDState struct {
	mu       sync.Mutex
	m        map[string]memberDND
	inflight map[string]bool
}

// memberInDND is isInDnd over one member's schedule.
func memberInDND(d memberDND, now time.Time) bool {
	if d.start == 1 || d.end == 1 {
		return false
	}
	t := now.Unix()
	return d.start <= t && d.end > t
}

// MemberInDND reports whether id's notifications are paused at now, as
// BasePresence's isDnd reads it (bvIE z → getDndStatusByMemberId): the
// viewer's own from dnd.info, anyone else's from the last dnd.teamInfo
// answer (false until one comes).
func (a *App) MemberInDND(id string, now time.Time) bool {
	if id == "" {
		return false
	}
	if id == a.S.SelfID() {
		a.dnd.mu.Lock()
		defer a.dnd.mu.Unlock()
		return a.dnd.known && a.dnd.status.Active(now)
	}
	a.mdnd.mu.Lock()
	defer a.mdnd.mu.Unlock()
	d, ok := a.mdnd.m[id]
	return ok && memberInDND(d, now)
}

// ClaimMemberDND reports whether a read of id's DND should fetch it now
// (maybeRefetch: never fetched, older than maxAge or marked stale, and
// not already being asked for) and, when it should, marks it asked for,
// so the caller's LoadMemberDND is the only one. The viewer's own is
// dnd.go's.
func (a *App) ClaimMemberDND(id string, now time.Time) bool {
	if !a.memberDNDWanted(id) {
		return false
	}
	a.mdnd.mu.Lock()
	defer a.mdnd.mu.Unlock()
	if a.mdnd.inflight[id] {
		return false
	}
	d, ok := a.mdnd.m[id]
	if ok && !d.at.IsZero() && now.Sub(d.at) < memberDNDMaxAge {
		return false
	}
	if a.mdnd.inflight == nil {
		a.mdnd.inflight = map[string]bool{}
	}
	a.mdnd.inflight[id] = true
	return true
}

// LoadMemberDND is fetchAndUpsertDndForUsers for others: the members
// Clack holds that aren't bots or deactivated, 30 a request to
// dnd.teamInfo, each answer kept. Off the UI goroutine.
func (a *App) LoadMemberDND(ctx context.Context, ids []string) error {
	var want []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !a.memberDNDWanted(id) {
			continue
		}
		seen[id] = true
		want = append(want, id)
	}
	if len(want) == 0 {
		return nil
	}
	a.mdnd.mu.Lock()
	if a.mdnd.inflight == nil {
		a.mdnd.inflight = map[string]bool{}
	}
	for _, id := range want {
		a.mdnd.inflight[id] = true
	}
	a.mdnd.mu.Unlock()
	defer func() {
		a.mdnd.mu.Lock()
		for _, id := range want {
			delete(a.mdnd.inflight, id)
		}
		a.mdnd.mu.Unlock()
	}()
	var firstErr error
	for i := 0; i < len(want); i += memberDNDBatch {
		batch := want[i:min(len(want), i+memberDNDBatch)]
		var out struct {
			Users map[string]struct {
				Start json.Number `json:"next_dnd_start_ts"`
				End   json.Number `json:"next_dnd_end_ts"`
			} `json:"users"`
		}
		err := a.C.Call(ctx, "dnd.teamInfo", url.Values{"users": {strings.Join(batch, ",")}}, &out)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		now := time.Now()
		a.mdnd.mu.Lock()
		if a.mdnd.m == nil {
			a.mdnd.m = map[string]memberDND{}
		}
		for id, v := range out.Users {
			s, _ := v.Start.Int64()
			e, _ := v.End.Int64()
			a.mdnd.m[id] = memberDND{start: s, end: e, at: now}
		}
		// every member asked is fresh now, whatever the answer held (the
		// conditional fetch manager's _updateAfterRefetch, p7oS): one the
		// answer left out, or a failed request, keeps the schedule already
		// held (none if there was none) until maxAge passes, so an open
		// card doesn't ask again every round trip
		for _, id := range batch {
			d, ok := a.mdnd.m[id]
			if !ok {
				d = memberDND{start: 1, end: 1}
			}
			d.at = now
			a.mdnd.m[id] = d
		}
		a.mdnd.mu.Unlock()
	}
	return firstErr
}

// memberDNDWanted is fetchAndUpsertDndForUsers' filter: someone else,
// member-shaped, held, not a bot and not deactivated.
func (a *App) memberDNDWanted(id string) bool {
	if id == "" || id == "USLACKBOT" || id == a.S.SelfID() || !LooksLikeMemberID(id) {
		return false
	}
	u, ok := a.S.User(id)
	return ok && !u.IsBot && !u.Deleted
}

// onDNDUpdatedUser is dndUpdated for someone else: the member's entry is
// marked stale (the schedule stays until the refetch answers).
func (a *App) onDNDUpdatedUser(raw json.RawMessage) {
	var ev struct {
		User string `json:"user"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.User == "" {
		return
	}
	a.mdnd.mu.Lock()
	if d, ok := a.mdnd.m[ev.User]; ok {
		d.at = time.Time{}
		a.mdnd.m[ev.User] = d
	}
	a.mdnd.mu.Unlock()
}

// memberDNDReconnected is the dndV2 manager's markStaleAfterSlowReconnect
// (the conditional fetch manager's default): after a slow (full)
// reconnect every member's DND is stale, so the next read asks again; the
// schedules stay until it answers.
func (a *App) memberDNDReconnected() {
	a.mdnd.mu.Lock()
	for id, d := range a.mdnd.m {
		d.at = time.Time{}
		a.mdnd.m[id] = d
	}
	a.mdnd.mu.Unlock()
}
