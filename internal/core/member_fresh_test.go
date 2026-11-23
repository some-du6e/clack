package core

// Members kept current as Slack keeps them (member_fresh.go): on Grid a
// member Clack fetched is subscribed to user_change, and the gateway's
// user_change brings their new picture; a member the gateway wouldn't
// subscribe Clack to is fetched again, conditionally, once its max age
// has passed and a view reads them. Synthetic: the in-repo fake's members.

import (
	"encoding/json"
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
// are the connection's).
func TestMemberSubscriptionsAfterReconnect(t *testing.T) {
	a, fs, _ := rtApp(t)
	id := unheldMember(t, a, fs)
	a.User(id)
	waitFor(t, "the member subscribed", 10*time.Second, func() bool { return fs.UserSubscribed(id) })
	a.mfresh.mu.Lock()
	sub := a.mfresh.subscribed[id]
	a.mfresh.mu.Unlock()
	if !sub {
		t.Fatal("core doesn't count the member subscribed")
	}
	a.RTM.Reconnect("test")
	waitFor(t, "the member subscribed again on the new connection", 15*time.Second, func() bool { return fs.UserSubscribed(id) })
}
