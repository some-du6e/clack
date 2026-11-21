package views

// The Members tab of channel details after someone joins or leaves, and
// its rows' names, over fakeslack (realtime on, the roster fixture in
// fakeslack/roster.go):
//
//   - Slack's member_joined_channel / member_left_channel handler marks
//     the channel's membershipCounts stale for anyone's join or leave
//     (boot-render batchProcessChannelMembers → UaE3), and the tab
//     strip's "Members N" (about-channel-modal memberCount, WDcq vC)
//     reads it through 1Ti7 q8, whose maybeRefetch reads users/counts
//     again, once per 2 s per channel (CeSm). So the count follows the
//     join while the dialog is open.
//   - The list is IaDetailsPopoverMembers' own fetch (VSHD eq): it keeps
//     the people it fetched while the tab is open, and the Tabs component
//     (client-boot tt5m) renders only the selected tab, so leaving the
//     tab and coming back mounts the list afresh and fetches it again.
//   - The row's names are getPrefCompliantMemberName (VRXO sE) and
//     getMemberSecondaryName (hWCs Mu): under display_real_names the real
//     name leads and the display name follows.

import (
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// newMembersLiveEnv opens channel details for fakeslack's views channel
// with members on its roster, on the Members tab, once the tab's people
// and counts loaded.
func newMembersLiveEnv(t *testing.T, members ...string) (*cdTestEnv, *fakeslack.Server) {
	t.Helper()
	app, fs := vkFake(t, nil)
	fs.SetRoster(fakeslack.ViewsChannel, members...)
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &cdTestHost{app: app, th: th}
	e := &cdTestEnv{t: t, h: h}
	e.d = NewChannelDetails(h, fakeslack.ViewsChannel)
	e.until("info", e.loaded)
	e.d.ShowTab("Members")
	e.until("the members", func() bool { return len(e.d.MembersShown()) == len(members) })
	e.until("the counts", func() bool { _, ok, _ := e.d.counts.get(); return ok })
	return e, fs
}

// memberTabCount is the count the tab strip drew after "Members" ("" for
// none).
func memberTabCount(e *cdTestEnv) string {
	strs := cdDrawnStrings(e)
	for i, s := range strs {
		if s != "Members" || i+1 >= len(strs) {
			continue
		}
		if _, err := strconv.Atoi(strs[i+1]); err == nil {
			return strs[i+1]
		}
	}
	return ""
}

// cdDrawnStrings lays out a frame and returns every string it drew.
func cdDrawnStrings(e *cdTestEnv) []string {
	var mu sync.Mutex
	var out []string
	prev := theme.DrawHook
	theme.DrawHook = func(s string) {
		mu.Lock()
		out = append(out, s)
		mu.Unlock()
	}
	e.frame()
	theme.DrawHook = prev
	return out
}

// untilCount waits for the tab strip to draw "Members want".
func untilCount(e *cdTestEnv, want string) {
	e.t.Helper()
	got := ""
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got = memberTabCount(e); got == want {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("the tab strip draws Members %q, want %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Someone joins while the Members tab is open: the tab's count follows
// at once; someone leaves: it follows again. A burst of joins reads
// users/counts at most twice (leading and trailing, 2 s apart).
func TestMembersTabCountFollowsJoinsLikeSlack(t *testing.T) {
	e, fs := newMembersLiveEnv(t, fakeslack.SelfID, fakeslack.ProfileID)
	untilCount(e, "2")

	fs.JoinChannel(fakeslack.ViewsChannel, fakeslack.AdminID)
	untilCount(e, "3")
	if got := e.d.MembersShown(); len(got) != 2 {
		t.Errorf("the open tab's rows %v changed with the join: Slack's list keeps what it fetched", got)
	}

	fs.LeaveChannel(fakeslack.ViewsChannel, fakeslack.ProfileID)
	untilCount(e, "2")

	time.Sleep(2100 * time.Millisecond) // past the throttle window
	before := fs.CountsAsked(fakeslack.ViewsChannel)
	ids := fs.UserIDs()
	for _, id := range ids[1:4] {
		fs.JoinChannel(fakeslack.ViewsChannel, id)
	}
	untilCount(e, "5")
	time.Sleep(2500 * time.Millisecond) // the trailing read, if any, is done
	if n := fs.CountsAsked(fakeslack.ViewsChannel) - before; n > 2 {
		t.Errorf("three joins read users/counts %d times, want at most 2 (2 s throttle, leading and trailing)", n)
	}
	if got := memberTabCount(e); got != "5" {
		t.Errorf("after the burst the tab strip draws Members %q, want 5", got)
	}
}

// The list keeps its rows while the tab is open; leaving the tab and
// coming back fetches it again, with the joined member in it and the left
// one gone.
func TestMembersTabListRemountsLikeSlack(t *testing.T) {
	e, fs := newMembersLiveEnv(t, fakeslack.SelfID, fakeslack.ProfileID)
	fs.JoinChannel(fakeslack.ViewsChannel, fakeslack.AdminID)
	fs.LeaveChannel(fakeslack.ViewsChannel, fakeslack.ProfileID)
	time.Sleep(300 * time.Millisecond)
	e.frame()
	if got := e.d.MembersShown(); !slices.Contains(got, fakeslack.ProfileID) || slices.Contains(got, fakeslack.AdminID) {
		t.Errorf("the open tab's rows %v, want the ones it fetched (Slack's list doesn't follow the events)", got)
	}
	e.d.ShowTab("About")
	e.frame()
	e.d.ShowTab("Members")
	e.until("the list fetched again", func() bool {
		got := e.d.MembersShown()
		return slices.Contains(got, fakeslack.AdminID) && !slices.Contains(got, fakeslack.ProfileID)
	})
}

// A member's row names under the viewer's display_real_names pref: the
// real name first, then the display name. Without it the display name
// leads and the real name follows (also when the two are equal: Slack
// draws "clod ○ clod").
func TestMembersTabNamesFollowDisplayRealNames(t *testing.T) {
	e, _ := newMembersLiveEnv(t, fakeslack.SelfID, fakeslack.ProfileID)
	order := func() (primary, secondary int) {
		strs := cdDrawnStrings(e)
		return slices.Index(strs, "robin"), slices.Index(strs, "Robin Example")
	}
	if d, r := order(); d < 0 || r < 0 || d > r {
		t.Errorf("display_real_names off: robin at %d, Robin Example at %d, want the display name first", d, r)
	}
	e.h.app.S.Lock()
	e.h.app.S.Prefs.DisplayRealNames = true
	e.h.app.S.Unlock()
	if d, r := order(); d < 0 || r < 0 || r > d {
		t.Errorf("display_real_names on: robin at %d, Robin Example at %d, want the real name first", d, r)
	}

	for _, c := range []struct {
		name, display, real string
		set                 bool
		prim, secd          string
	}{
		{"equal names", "clod", "clod", true, "clod", "clod"},
		{"real name only", "Robin Example", "Robin Example", false, "Robin Example", ""},
	} {
		u := memberNamesUser(c.display, c.real, c.set)
		for _, real := range []bool{false, true} {
			p, s := memberNamesFor(u, real)
			if p != c.prim || s != c.secd {
				t.Errorf("%s (real names %v): %q / %q, want %q / %q", c.name, real, p, s, c.prim, c.secd)
			}
		}
	}
}

// memberNamesUser is a member with these names, as core builds one
// (Name is the display name, else the real name).
func memberNamesUser(display, real string, set bool) core.User {
	u := core.User{ID: "U0TESTNAME1", Name: real, RealName: real, DisplaySet: set}
	if set {
		u.Name, u.DisplayName = display, display
	}
	return u
}
