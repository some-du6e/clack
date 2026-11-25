package core

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Who is or isn't a member of a channel, as Slack's web client keeps it
// (web client 66e72b57ade1, 2026-11-24). The membership store (client-boot
// DMmC, persisted) holds {isKnown, isMember} per channel per member:
//
//   - getCachedChannelMembershipInfoForUser (9UBA Ql) reads it; with
//     nothing held, an IM's partner and the viewer read as members of the
//     IM (and are stored so).
//   - getChannelMembershipInfoForUser (9UBA Sv), a check's read
//     (removeCommand's canUserKickMemberFromChannelWithReason, cxgb ZM),
//     asks for a member it holds nothing for
//     (fetchAndUpsertChannelMembershipForUsers) and answers {isKnown:
//     false} meanwhile: not a member.
//   - fetchAndUpsertChannelMembershipForUsers (Tkab R) stores the members
//     asked for as {isKnown: false} at once, so a read while the ask runs
//     doesn't ask again; edge channels/membership {channel, users,
//     as_admin} answers them (mVvn: the asks of the next 200 ms gathered
//     per channel, 100 users a call, a member missing from the answer
//     failing them all), stored only for those still held; a failure
//     drops them, and the next read asks again.
//   - getChannelMembershipInfoForUsers (pfZb z) asks for the members not
//     known yet (never an IM's partner or the viewer) and answers them
//     all. The member autocomplete calls it for its rows in a channel
//     (prepSuggestions), so a member picked there is known by the time a
//     command names them.
//
// Clack holds it in memory only. as_admin is false: Clack's client is
// never Slack's admin team or org view.

// MemberInfo is one member's membership of a channel as the store holds
// it: Known false while nobody knows.
type MemberInfo struct{ Known, Member bool }

type membershipStore struct {
	mu sync.Mutex
	m  map[string]map[string]MemberInfo // channel → member
	// pending are the asks of the 200 ms window, per channel (mVvn's w)
	pending map[string]*memBatch
	timer   *time.Timer
}

// memBatch is one channel's gathered asks and their answer.
type memBatch struct {
	users []string
	done  chan struct{}
	got   map[string]bool
	err   error
}

// memBatchWait is mVvn's debounce: the asks of 200 ms go together.
var memBatchWait = 200 * time.Millisecond

var membershipIndex = newPerApp() // *App → *membershipStore

func (a *App) memberships() *membershipStore {
	if v, ok := membershipIndex.Load(a); ok {
		return v.(*membershipStore)
	}
	v, _ := membershipIndex.LoadOrStore(a, &membershipStore{m: map[string]map[string]MemberInfo{}, pending: map[string]*memBatch{}})
	return v.(*membershipStore)
}

// put is upsertChannelMembershipInfoForUsers (DMmC g9).
func (ms *membershipStore) put(ch string, infos map[string]MemberInfo) {
	if ms.m[ch] == nil {
		ms.m[ch] = map[string]MemberInfo{}
	}
	for u, i := range infos {
		ms.m[ch][u] = i
	}
}

// cachedMembership is getCachedChannelMembershipInfoForUser (9UBA Ql):
// false when the store holds nothing for user in ch.
func (a *App) cachedMembership(ch, user string) (MemberInfo, bool) {
	if ch == "" || user == "" {
		return MemberInfo{}, false
	}
	ms := a.memberships()
	ms.mu.Lock()
	info, ok := ms.m[ch][user]
	ms.mu.Unlock()
	if ok {
		return info, true
	}
	if c, _ := a.S.Conv(ch); c != nil && c.IsIM && (user == c.User || user == a.S.SelfID()) {
		info = MemberInfo{Known: true, Member: true}
		ms.mu.Lock()
		ms.put(ch, map[string]MemberInfo{user: info})
		ms.mu.Unlock()
		return info, true
	}
	return MemberInfo{}, false
}

// membershipOf is getChannelMembershipInfoForUser (9UBA Sv): the held
// answer, else an ask in the background and {isKnown: false}.
func (a *App) membershipOf(ch, user string) MemberInfo {
	if info, ok := a.cachedMembership(ch, user); ok {
		return info
	}
	if ch != "" && user != "" {
		go func() { _ = a.fetchMembership(a.bg, ch, []string{user}) }()
	}
	return MemberInfo{}
}

// ChannelMembershipInfo is getChannelMembershipInfoForUsers (pfZb z): the
// membership of users in ch, asking for the ones the store doesn't know
// yet, an IM's partner and the viewer never. A failed ask leaves those
// out.
func (a *App) ChannelMembershipInfo(ctx context.Context, ch string, users []string) map[string]MemberInfo {
	out := map[string]MemberInfo{}
	var ask []string
	ms := a.memberships()
	ms.mu.Lock()
	for _, u := range users {
		if i, ok := ms.m[ch][u]; ok && i.Known { // wr: isKnown
			out[u] = i
		} else if !slices.Contains(ask, u) {
			ask = append(ask, u)
		}
	}
	ms.mu.Unlock()
	if c, _ := a.S.Conv(ch); c != nil && c.IsIM {
		for _, u := range []string{c.User, a.S.SelfID()} {
			if i := slices.Index(ask, u); i >= 0 {
				ask = slices.Delete(ask, i, i+1)
				if info, ok := a.cachedMembership(ch, u); ok {
					out[u] = info
				}
			}
		}
	}
	if len(ask) == 0 {
		return out
	}
	if a.fetchMembership(ctx, ch, ask) == nil {
		ms.mu.Lock()
		for _, u := range ask {
			if i, ok := ms.m[ch][u]; ok {
				out[u] = i
			}
		}
		ms.mu.Unlock()
	}
	return out
}

// fetchMembership is fetchAndUpsertChannelMembershipForUsers (Tkab R):
// users stored as not known at once, then the gathered ask
// (sendMembershipAsks) stores its answer whoever waits for it; the
// error is the ask's (or ctx's, which stops only the wait).
func (a *App) fetchMembership(ctx context.Context, ch string, users []string) error {
	ms := a.memberships()
	ms.mu.Lock()
	loading := make(map[string]MemberInfo, len(users))
	for _, u := range users {
		loading[u] = MemberInfo{} // vUsS l
	}
	ms.put(ch, loading)
	b := ms.pending[ch]
	if b == nil {
		b = &memBatch{done: make(chan struct{})}
		ms.pending[ch] = b
	}
	for _, u := range users {
		if !slices.Contains(b.users, u) {
			b.users = append(b.users, u)
		}
	}
	if ms.timer != nil {
		ms.timer.Stop()
	}
	ms.timer = time.AfterFunc(memBatchWait, a.sendMembershipAsks)
	ms.mu.Unlock()
	select {
	case <-b.done:
		return b.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sendMembershipAsks is fetchChannelMembershipForPendingChannels (mVvn):
// every channel's gathered asks, 100 users a call. An answer is stored
// for the members still held (upsertFetchedChannelMembershipInfoForUsers,
// DMmC rU); a failure drops them all (clearChannelMembershipInfoForUsers,
// DMmC KG).
func (a *App) sendMembershipAsks() {
	ms := a.memberships()
	ms.mu.Lock()
	batches := ms.pending
	ms.pending = map[string]*memBatch{}
	ms.mu.Unlock()
	for ch, b := range batches {
		go func() {
			got, err := a.askMembership(a.bg, ch, b.users)
			ms.mu.Lock()
			for _, u := range b.users {
				_, held := ms.m[ch][u]
				switch {
				case err != nil:
					delete(ms.m[ch], u)
				case held:
					ms.m[ch][u] = MemberInfo{Known: true, Member: got[u]}
				}
			}
			b.got, b.err = got, err
			ms.mu.Unlock()
			close(b.done)
		}()
	}
}

// askMembership is fetchChannelMembershipForChannel (mVvn): edge
// channels/membership for users, a hundred at a time.
func (a *App) askMembership(ctx context.Context, ch string, users []string) (map[string]bool, error) {
	got := map[string]bool{}
	for part := range slices.Chunk(users, 100) {
		m, err := a.ChannelMembership(ctx, ch, part)
		if err != nil {
			return nil, err
		}
		for u, in := range m {
			got[u] = in
		}
	}
	// "Flannel did not return membership status of {channel} for some
	// users": the ones asked about that the answer left out
	var missing []string
	for _, u := range users {
		if _, ok := got[u]; !ok {
			missing = append(missing, u)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("channels/membership did not answer for %v in %s", missing, ch)
	}
	return got, nil
}

// noteMembership stores what a write learned (a kick: not a member).
func (a *App) noteMembership(ch, user string, member bool) {
	ms := a.memberships()
	ms.mu.Lock()
	ms.put(ch, map[string]MemberInfo{user: {Known: true, Member: member}})
	ms.mu.Unlock()
}
