package ui

// Karim, 2026-11-22 (α.143): one sender drawn with two pictures. Slack's
// members go stale and are fetched again (core/member_fresh.go): after a
// restart nothing is fresh, so the first view to draw a member the
// offline store held asks users/info for them with that copy's updated
// stamp, and a picture they changed while Clack was closed replaces the
// old one in every row. Clack drew the offline store's copy for good.
// Synthetic: the in-repo fake's members.

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

const restartPic = "https://avatars.invalid/changed-while-closed-72.png"

func TestAvatarAfterRestartLikeSlack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := freezeHome(t)
	ch := fs.ChannelIDs()[0]

	// session 1: a member with a full profile posted before it started
	// (a quiet post after the boot shows only on the 20 s poll); their
	// row draws them
	poster := fakeslack.ProfileID
	ts, _ := fs.PostQuiet(ch, poster, "a synthetic message before the restart")
	e := startEnv(t, fs, home)
	e.waitReady()
	e.openChannel(ch)
	e.toLatest()
	rowAvatar := func(e *perfEnv) string {
		if cv := e.channelView(ch); cv != nil {
			for _, s := range cv.Senders() {
				if s.TS == ts {
					return s.Avatar
				}
			}
		}
		return ""
	}
	e.until("the poster's row", 20*time.Second, func() bool { return rowAvatar(e) != "" })
	before := rowAvatar(e)
	<-e.a.data.stop()
	e.shutdown()

	// while Clack is closed they change their picture
	if _, ok := fs.EditMember(poster, func(u *slack.User) {
		u.Profile.Image24, u.Profile.Image32, u.Profile.Image48, u.Profile.Image72 = restartPic, restartPic, restartPic, restartPic
		u.Profile.Image192, u.Profile.Image512 = restartPic, restartPic
		u.Profile.AvatarHash = "a1a1a1a1a1a1"
	}); !ok {
		t.Fatal("the fake has no such member")
	}
	var mu sync.Mutex
	var stamps []int64
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			var up map[string]int64
			_ = json.Unmarshal(body["updated_ids"], &up)
			if st, ok := up[poster]; ok {
				mu.Lock()
				stamps = append(stamps, st)
				mu.Unlock()
			}
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })

	// session 2: the offline store's copy, then the member as they are
	e2 := startEnv(t, fs, home)
	e2.waitReady()
	if u, ok := e2.a.Core().S.RawUser(poster); !ok || u.Updated != fakeslack.FakeUpdated {
		t.Fatalf("the restart doesn't start from the offline store's copy of %s (%v, %+v): the case tests nothing", poster, ok, u)
	}
	e2.openChannel(ch)
	e2.toLatest()
	e2.until("the poster's new picture", 20*time.Second, func() bool {
		av := rowAvatar(e2)
		return av != "" && av != before && strings.Contains(av, "changed-while-closed")
	})
	mu.Lock()
	defer mu.Unlock()
	conditional := false
	for _, st := range stamps {
		conditional = conditional || st == fakeslack.FakeUpdated
	}
	if !conditional {
		t.Errorf("users/info asked for %s with stamps %v; Slack's refetch sends the held copy's updated (%d)", poster, stamps, fakeslack.FakeUpdated)
	}
}
