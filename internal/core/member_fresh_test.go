package core

// Members kept current as Slack keeps them (member_fresh.go): on Grid a
// member Clack fetched is subscribed to user_change, and the gateway's
// user_change brings their new picture; a member the gateway wouldn't
// subscribe Clack to is fetched again, conditionally, once its max age
// has passed and a view reads them. Synthetic: the in-repo fake's members.

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

const freshPic = "https://avatars.invalid/fresh-picture-72.png"

// unheldMember is a member of the fake the client hasn't stored.
func unheldMember(t *testing.T, a *App, fs *fakeslack.Server) string {
	t.Helper()
	for _, id := range fs.UserIDs() {
		if _, held := a.S.RawUser(id); !held && id != fakeslack.SelfID && strings.HasPrefix(id, "U") {
			return id
		}
	}
	t.Fatal("every member of the fake is stored")
	return ""
}

func newPicture(u *slack.User) {
	u.Profile.Image24, u.Profile.Image32, u.Profile.Image48, u.Profile.Image72 = freshPic, freshPic, freshPic, freshPic
	u.Profile.Image192, u.Profile.Image512 = freshPic, freshPic
	u.Profile.AvatarHash = "f0f0f0f0f0f0"
}

// A member a view looked up is fetched and, on Grid, subscribed
// (user_change_subscribe_request, at most 100 ids, the gateway's answer
// waited for); their new picture then arrives as user_change.
func TestMemberSubscribedFollowsUserChange(t *testing.T) {
	a, fs, _ := rtApp(t)
	id := unheldMember(t, a, fs)
	a.User(id) // a row draws them: the miss is fetched
	waitFor(t, "the member fetched and subscribed", 10*time.Second, func() bool {
		_, held := a.S.RawUser(id)
		return held && fs.UserSubscribed(id)
	})
	for _, f := range fs.UserSubscriptions() {
		if n := strings.Count(f, ",") + 1; strings.HasPrefix(f, "subscribe:") && n > slack.MaxUserSubs {
			t.Errorf("a subscribe request with %d ids; Slack's batches hold at most %d", n, slack.MaxUserSubs)
		}
	}
	before, _ := a.User(id)
	if _, ok := fs.EditMember(id, newPicture); !ok {
		t.Fatal("the fake has no such member")
	}
	if !fs.PushUserChange(id) {
		t.Fatal("user_change went to no subscriber")
	}
	waitFor(t, "the new picture", 10*time.Second, func() bool {
		u, _ := a.User(id)
		return u.Avatar != before.Avatar && strings.Contains(u.Avatar, "fresh-picture")
	})
}

// The gateway refuses the subscription (12, its id cap): the member is
// fresh for its max age, then the next lookup fetches them again with the
// stored copy's updated stamp, and their new picture comes back.
func TestMemberRefetchedPastMaxAgeLikeSlack(t *testing.T) {
	old := memberMaxAgeKnown
	memberMaxAgeKnown = 400 * time.Millisecond
	t.Cleanup(func() { memberMaxAgeKnown = old })
	a, fs, _ := rtApp(t)
	fs.SetUserSubCap(1) // nothing more fits
	var mu sync.Mutex
	stamps := map[string][]int64{}
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			var up map[string]int64
			_ = json.Unmarshal(body["updated_ids"], &up)
			mu.Lock()
			for k, v := range up {
				stamps[k] = append(stamps[k], v)
			}
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })
	id := unheldMember(t, a, fs)
	a.User(id)
	waitFor(t, "the member fetched", 10*time.Second, func() bool { _, held := a.S.RawUser(id); return held })
	time.Sleep(1500 * time.Millisecond) // the cycle's subscription, refused, and its conditional fetch
	if fs.UserSubscribed(id) {
		t.Fatal("the member was subscribed past the cap: the case tests nothing")
	}
	held, _ := a.S.RawUser(id)
	stored := held.Updated
	if stored == 0 {
		t.Fatal("the stored member has no updated stamp")
	}
	before, _ := a.User(id)
	if _, ok := fs.EditMember(id, newPicture); !ok {
		t.Fatal("the fake has no such member")
	}
	waitFor(t, "the new picture from a refetch", 10*time.Second, func() bool {
		u, _ := a.User(id) // a view drawing them: the hit past the max age refetches
		return u.Avatar != before.Avatar && strings.Contains(u.Avatar, "fresh-picture")
	})
	mu.Lock()
	defer mu.Unlock()
	conditional := false
	for _, st := range stamps[id] {
		conditional = conditional || st == stored
	}
	if !conditional {
		t.Errorf("users/info asked for %s with stamps %v; Slack's refetch sends the stored copy's %d", id, stamps[id], stored)
	}
}

// A new connection starts over: after a fast reconnect the members that
// were subscribed are subscribed again on it (the gateway's subscriptions
// are the connection's). The fake has a subscription as its request
// comes; core counts it once the gateway has answered and the refetch
// after it is in (subscribeMembers), so the test waits for both. Read
// right after the fake's, core's count was sometimes still out ("core
// doesn't count the member subscribed", msg-latency-thread's push ci
// 45388429533); MF_SLOW_REFETCH holds that refetch 300 ms (slowRefetch).
func TestMemberSubscriptionsAfterReconnect(t *testing.T) {
	a, fs, _ := rtApp(t)
	slowRefetch(fs)
	id := unheldMember(t, a, fs)
	a.User(id)
	waitFor(t, "the member subscribed, on the fake and in core", 10*time.Second, func() bool {
		if !fs.UserSubscribed(id) {
			return false
		}
		a.mfresh.mu.Lock()
		defer a.mfresh.mu.Unlock()
		return a.mfresh.subscribed[id]
	})
	// what the old connection was sent, and which one it was: the
	// subscription after the reconnect must be a new request on a new
	// connection (the fake's subscribed set is the last connection's until
	// the new one opens)
	sent := len(fs.UserSubscriptions())
	a.mfresh.mu.Lock()
	conn := a.mfresh.conn
	a.mfresh.mu.Unlock()
	// a fast reconnect: the gateway's reconnect_url, then the socket drops
	// (Reconnect itself forgets the URL: a slow one)
	fs.Push(map[string]any{"type": "reconnect_url", "url": "ws" + strings.TrimPrefix(fs.URL, "http") + "ws?fast=1"})
	time.Sleep(100 * time.Millisecond) // handled before the drop
	fs.DropSockets()
	waitFor(t, "a new connection", 15*time.Second, func() bool {
		a.mfresh.mu.Lock()
		defer a.mfresh.mu.Unlock()
		return a.mfresh.conn != conn
	})
	if !a.RTM.LastConnectFast() {
		t.Fatal("the reconnect wasn't fast (no reconnect_url dialled): the case tests nothing")
	}
	waitFor(t, "the member subscribed again on the new connection", 15*time.Second, func() bool {
		for _, f := range fs.UserSubscriptions()[sent:] {
			if ids, ok := strings.CutPrefix(f, "subscribe:"); ok && slices.Contains(strings.Split(ids, ","), id) {
				return fs.UserSubscribed(id)
			}
		}
		return false
	})
}

// A subscription request waits for the connection's hello, as Slack's
// sendMessage waits for the socket to be connected: a frame sent before
// it isn't taken (no answer comes), and the member was left unsubscribed.
func TestMemberSubscribeWaitsForHello(t *testing.T) {
	a, fs, _ := rtApp(t)
	release := fs.HoldHello()
	t.Cleanup(release)
	ws := fs.CallCount("ws")
	a.RTM.Reconnect("test")
	// the fake counts the dial at its handler's entry: the client's side
	// (its conn, its hello gate) is set once it's connected
	waitFor(t, "the new connection open, its hello held", 15*time.Second, func() bool { return fs.CallCount("ws") > ws && a.RTM.Connected() })
	id := unheldMember(t, a, fs)
	a.User(id) // a row draws them: fetched, then a subscription asked for
	waitFor(t, "the member fetched", 10*time.Second, func() bool { _, held := a.S.RawUser(id); return held })
	time.Sleep(2 * time.Second) // the cycle's interval: its request is due
	if n := fs.EarlyFrames(); n != 0 {
		t.Errorf("%d subscription requests went out before the connection's hello", n)
	}
	release()
	waitFor(t, "the member subscribed once the connection said hello", 15*time.Second, func() bool { return fs.UserSubscribed(id) })
	if n := fs.EarlyFrames(); n != 0 {
		t.Errorf("%d subscription requests went out before the connection's hello", n)
	}
	// the request that waited sent nothing (ErrHelloWaited): the member
	// is asked for once, on the connection now up
	time.Sleep(2500 * time.Millisecond) // a cycle more, were it to ask again
	asks := 0
	for _, f := range fs.UserSubscriptions() {
		if ids, ok := strings.CutPrefix(f, "subscribe:"); ok && slices.Contains(strings.Split(ids, ","), id) {
			asks++
		}
	}
	if asks != 1 {
		t.Errorf("the member was asked for in %d subscribe requests, want one", asks)
	}
}

// A refetch's answer older than the copy the store took meanwhile (a
// user_change while the request was out) is the older one: the store
// keeps the newer copy.
func TestMemberRefetchKeepsNewerCopy(t *testing.T) {
	const livePic = "https://avatars.invalid/live-picture-72.png"
	a, fs, _ := rtApp(t)
	fs.SetUserSubCap(1) // nothing more fits: refetches, no user_change
	id := unheldMember(t, a, fs)
	a.User(id)
	waitFor(t, "the member fetched", 10*time.Second, func() bool { _, held := a.S.RawUser(id); return held })
	time.Sleep(1500 * time.Millisecond) // the cycle's refused subscription and its fetch, done
	edited, ok := fs.EditMember(id, newPicture)
	if !ok {
		t.Fatal("the fake has no such member")
	}
	var once sync.Once
	newer := false
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		var up map[string]int64
		if method == "edge/users/info" && json.Unmarshal(body["updated_ids"], &up) == nil {
			if _, asked := up[id]; asked {
				once.Do(func() {
					// user_change lands while users/info is out: newer than
					// what the answer will carry
					su, _ := a.S.RawUser(id)
					nu := *su
					nu.Profile.Image72, nu.Profile.Image48 = livePic, livePic
					nu.Updated = edited.Updated + 1000
					a.putUserLive(nu)
					newer = true
				})
			}
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })
	a.refetchMembers(context.Background(), []string{id})
	if !newer {
		t.Fatal("users/info wasn't asked for the member: the case tests nothing")
	}
	if su, _ := a.S.RawUser(id); su.Profile.Image72 != livePic {
		t.Errorf("the store holds %q (updated %d) after the refetch; the copy that came while it was out (%q, updated %d) is the newer one",
			su.Profile.Image72, su.Updated, livePic, edited.Updated+1000)
	}
}

// Off Grid (ConditionalFetchManager) a member's freshness goes only when
// the freshness epoch moves: a fast reconnect keeps it, a slow one
// doesn't.
func TestMemberFreshnessKeptOnFastReconnectOffGrid(t *testing.T) {
	a := &App{} // no client: off Grid
	const id = "U0FRESH001"
	a.mfresh.mu.Lock()
	a.mfresh.init()
	a.mfresh.ttl[id] = time.Now().Add(time.Hour)
	a.mfresh.mu.Unlock()
	fresh := func() bool {
		a.mfresh.mu.Lock()
		defer a.mfresh.mu.Unlock()
		return a.mfresh.freshLocked(id, time.Now())
	}
	a.memberFreshConnected(true)
	if !fresh() {
		t.Error("a fast reconnect off Grid made the member stale; Slack's p7oS keeps the TTLs while the epoch stays")
	}
	a.memberFreshConnected(false)
	if fresh() {
		t.Error("a slow reconnect off Grid kept the member fresh")
	}
}

// slowRefetch holds the fake's users/info 300 ms with MF_SLOW_REFETCH set
// (flake-check member-subs): a member's refetch after its subscription
// then comes in well after the fake has the subscription.
func slowRefetch(fs *fakeslack.Server) {
	if os.Getenv("MF_SLOW_REFETCH") != "" {
		fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 300 * time.Millisecond})
	}
}
