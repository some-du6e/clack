package ui

// The hub review's fixes (2026-11-22), each against internal/fakeslack:
// the carousel asks the host for its frames (no op.InvalidateCmd in the
// middle of layout), a forked Ignore modal's rows act on their own
// invite, Accept holds its flow (Slack's loading button), and the
// smaller ones: a failed re-read after a revoke, Filter by's menu drawn
// once, Load More listing a repeated team once.

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The carousel's next quote (7 s on) and its 1 s slide: the router never
// holds a wake-up the carousel executed mid-layout (it would replay the
// unread input of what lays out after it: the org page, overlay scrims),
// and the host's timer is out for the next quote instead.
func TestSlackConnectCarouselAsksHostForFrames(t *testing.T) {
	e, _ := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{DMEligible: true})
	})
	v := hubTab(t, e, 3, (*views.Directories).SCExternalLoaded)
	v.SCScrollToEnd()
	e.until("the carousel", 10*time.Second, func() bool {
		next, _, _ := v.SCQuoteDue()
		return next.Year() > 1
	})
	e.frame()
	e.r.WakeupTime() // the latch, from before
	e.frame()
	next, asked, pending := v.SCQuoteDue()
	if w, ok := e.r.WakeupTime(); ok && w.Equal(next) {
		t.Errorf("the carousel executed InvalidateCmd{At: %v} mid-layout", w)
	}
	if !pending || !asked.Equal(next) {
		t.Errorf("the next quote's frame: asked %v (pending %v), want the host's timer for %v", asked, pending, next)
	}
	// the slide: each frame asks the host, none executes an immediate wake-up
	v.SCNextQuote(time.Now())
	e.frame()
	e.r.WakeupTime()
	for i := 0; i < 5; i++ {
		before := time.Now()
		e.frame()
		if w, ok := e.r.WakeupTime(); ok && !w.After(before) {
			t.Fatalf("frame %d of the slide executed an immediate InvalidateCmd (at %v)", i, w)
		}
	}
}

// Two forked Ignore modals: the older one's "Ignore this invitation" acts
// on its own invite once the newer one is closed (the rows used to call
// the host's latest).
func TestSlackConnectForkedIgnoreActsOnItsInvite(t *testing.T) {
	now := time.Now().Unix()
	invs := []fakeslack.HubInvite{
		{ID: "I0RECVA001", Sig: "sa", Kind: "channel", Direction: "incoming", Status: "sent", ChannelName: "partner-launch",
			DateCreated: now - 600, DateInvalid: now + 86400*14, InviterID: fakeslack.ExternalID, InviterName: "Sam Partner",
			TeamID: fakeslack.ExternalTeamID, TeamName: "Partner Co", TeamVerified: true},
		{ID: "I0RECVB001", Sig: "sb", Kind: "im", Direction: "incoming", Status: "sent",
			DateCreated: now - 300, DateInvalid: now + 86400*14, InviterID: "U0OTHERX01", InviterName: "Riley Other",
			TeamID: "T0OTHER001", TeamName: "Other Org"},
	}
	e, fs := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Invites: invs})
	})
	v := hubTab(t, e, 4, (*views.Directories).SCInvitesLoaded)
	v.SCInviteAct("I0RECVA001", core.InviteActIgnore)
	e.frame()
	v.SCInviteAct("I0RECVB001", core.InviteActIgnore)
	e.frame()
	var forks []*popups.Modal
	for _, o := range e.a.overlays {
		if m, ok := o.(*popups.Modal); ok && !m.Dismissed() && m.Title() == "What would you like to do?" {
			forks = append(forks, m)
		}
	}
	if len(forks) != 2 {
		t.Fatalf("%d forked modals, want 2", len(forks))
	}
	forks[1].Dismiss() // Esc on the newer one
	e.frame()
	if !v.SCForkedRowPress(forks[0], 0) {
		t.Fatal("the older forked modal isn't open")
	}
	e.frame()
	m := e.topModal("Ignore this invitation from Sam Partner?")
	if m == nil {
		t.Fatal("the older modal's Ignore this invitation didn't open its own invite's modal")
	}
	if !m.Press("Ignore Invitation") {
		t.Fatal("no Ignore Invitation")
	}
	e.until("the ignore", 10*time.Second, func() bool {
		return len(fs.SlackConnectForms("conversations.ignoreInvite"))+len(fs.SlackConnectForms("sharedInvites.ignoreInvite")) > 0
	})
	if fms := fs.SlackConnectForms("sharedInvites.ignoreInvite"); len(fms) > 0 {
		t.Errorf("ignored the newer invite: sharedInvites.ignoreInvite %v", fms)
	}
	if fms := fs.SlackConnectForms("conversations.ignoreInvite"); len(fms) != 1 || fms[0].Get("invite") != "I0RECVA001" {
		t.Errorf("conversations.ignoreInvite %v, want I0RECVA001 once", fms)
	}
}

// A double click on a verified team's DM invite: one canAccept, one
// accept, the button loading while the flow runs (Slack's
// SlackConnectAcceptDmInviteAction), not a refused second accept.
func TestSlackConnectAcceptInFlight(t *testing.T) {
	now := time.Now().Unix()
	inv := fakeslack.HubInvite{ID: "I0RECVDM01", Sig: fakeslack.ActivityInviteSig, Kind: "im", Direction: "incoming", Status: "sent",
		DateCreated: now - 300, DateInvalid: now + 86400*14, InviterID: fakeslack.ExternalID, InviterName: "Sam Partner",
		TeamID: fakeslack.ExternalTeamID, TeamName: "Partner Co", TeamVerified: true}
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	e, fs := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Invites: []fakeslack.HubInvite{inv}})
		fs.SetHook(func(method string, form url.Values) (any, bool) {
			if method == "sharedInvites.canAccept" {
				<-release
			}
			return nil, false
		})
	})
	v := hubTab(t, e, 4, (*views.Directories).SCInvitesLoaded)
	v.SCInviteAct(inv.ID, core.InviteActAcceptDM)
	e.frame()
	if !v.SCInviteBusy(inv.ID) {
		t.Error("Accept isn't loading while its flow runs")
	}
	v.SCInviteAct(inv.ID, core.InviteActAcceptDM) // the second click
	e.frame()
	once.Do(func() { close(release) })
	count := func(m string) int { return len(fs.SlackConnectForms(m)) }
	e.until("the accept", 10*time.Second, func() bool { return count("sharedInvites.accept") > 0 && !v.SCInviteBusy(inv.ID) })
	e.settle(500 * time.Millisecond)
	if a, b := count("sharedInvites.canAccept"), count("sharedInvites.accept"); a != 1 || b != 1 {
		t.Errorf("canAccept %d, accept %d; want one each", a, b)
	}
}

// A revoke whose outgoing re-read fails still shows the invite revoked.
func TestSlackConnectRevokeRereadFails(t *testing.T) {
	e, fs := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Invites: hubInvites(), DMEligible: true})
		fs.SetHook(func(method string, form url.Values) (any, bool) {
			if method == "connectInvites.list" && form.Get("direction") == "outgoing" {
				return json.RawMessage(`{"ok":false,"error":"fatal_error"}`), true
			}
			return nil, false
		})
	})
	v := hubTab(t, e, 4, (*views.Directories).SCInvitesLoaded)
	v.SCInviteAct("I0SENTMAIL", core.InviteActRevoke)
	e.frame()
	m := e.topModal("Revoke this invitation?")
	if m == nil || !m.Press("Revoke") {
		t.Fatal("no revoke modal")
	}
	e.until("the revoked card", 10*time.Second, func() bool {
		inv, _ := fs.HubInviteNow("I0SENTMAIL")
		r := v.SCInviteRows()
		i := slices.IndexFunc(r, func(s string) bool { return strings.HasPrefix(s, "I0SENTMAIL:") })
		return inv.Status == "revoked" && i >= 0 && !strings.HasSuffix(r[i], ":revoke")
	})
}

// Filter by's menu is drawn once (the select drew its own and the row
// another).
func TestSlackConnectFilterMenuOnce(t *testing.T) {
	e, _ := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Invites: hubInvites(), DMEligible: true})
	})
	v := hubTab(t, e, 4, (*views.Directories).SCInvitesLoaded)
	v.SCInviteFilterOpen()
	var mu sync.Mutex
	n := 0
	theme.DrawHook = func(s string) {
		if s == "Show all" {
			mu.Lock()
			n++
			mu.Unlock()
		}
	}
	e.frame()
	theme.DrawHook = nil
	mu.Lock()
	defer mu.Unlock()
	if n != 2 {
		t.Errorf(`"Show all" drawn %d times with the menu open; want 2 (the select and one menu row)`, n)
	}
}

// Load More with a page that repeats a team lists it once.
func TestSlackConnectLoadMoreDedupes(t *testing.T) {
	team := func(id, name string) map[string]any {
		return map[string]any{"id": id, "name": name, "icon": map[string]any{"image_default": true}}
	}
	e, _ := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{DMEligible: true})
		fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
			if method != "edge/externalTeams/list" {
				return nil, false
			}
			var filter, marker string
			_ = json.Unmarshal(body["filter"], &filter)
			_ = json.Unmarshal(body["marker"], &marker)
			switch {
			case filter != "available":
				return map[string]any{"ok": true, "results": []any{}}, true
			case marker == "":
				return map[string]any{"ok": true, "results": []any{team("T0DUPE0001", "Dupe One"), team("T0DUPE0002", "Dupe Two")}, "next_marker": "p2"}, true
			}
			return map[string]any{"ok": true, "results": []any{team("T0DUPE0002", "Dupe Two"), team("T0DUPE0003", "Dupe Three")}}, true
		})
	})
	v := hubTab(t, e, 3, (*views.Directories).SCExternalLoaded)
	v.SCExternalLoadMore()
	e.until("the second page", 10*time.Second, func() bool {
		return v.SCExternalLoaded() && slices.Contains(v.SCExternalRows(), "available-T0DUPE0003")
	})
	n := 0
	for _, r := range v.SCExternalRows() {
		if r == "available-T0DUPE0002" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a team both pages list is shown %d times: %v", n, v.SCExternalRows())
	}
}
