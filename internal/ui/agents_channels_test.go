package ui

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

// Channel details under agents_in_channels_improvements (views/
// details_agents.go) against fakeslack's ChannelAgents fixture:
// #agents-lab has people, an agent and two apps; #no-agents people only.

func channelAgentsEnv(t *testing.T, group string) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ChannelAgents = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("PREVIEW_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.a.Core().SetExperimentGroup(core.ExpAgentsInChannelsImprovements, group)
	return e, fs
}

func openAgentsDetails(t *testing.T, e *perfEnv, ch string) *views.ChannelDetails {
	t.Helper()
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(300 * time.Millisecond)
	return topDetails(t, e)
}

// darkShot saves name and name-dark.
func (e *perfEnv) darkShot(name string) {
	e.shot(name)
	e.a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.shot(name + "-dark")
	e.a.setDark(false)
	e.settle(200 * time.Millisecond)
}

// On: Members is people only with "Add people", "Find people" and KcBz's
// filter (no Agents & apps); Agents & apps is ReworkedAgentsAppsTab:
// the add row and the channel's agents and apps, the search's in-channel
// and out-of-channel sections, and the invite modal scoped to agents and
// apps, whose Add invites them.
func TestChannelDetailsAgentsImprovements(t *testing.T) {
	e, fs := channelAgentsEnv(t, "on")
	ch := fakeslack.ChannelAgentsID
	d := openAgentsDetails(t, e, ch)

	// Members (e5)
	d.ShowTab("Members")
	e.until("people-only users/list", 10*time.Second, func() bool {
		return slices.Contains(fs.UserListFilters(), "everyone AND NOT bots AND NOT apps") && len(d.MembersShown()) > 0
	})
	e.settle(400 * time.Millisecond)
	if got := d.MembersPlaceholder(); got != "Find people" {
		t.Errorf("placeholder %q, want Find people", got)
	}
	if got := d.MembersAddLabel(); got != "Add people" {
		t.Errorf("add row %q, want Add people", got)
	}
	labels := d.MembersFilterLabels()
	if want := []string{"All", "Channel Managers", "Members", "Guests"}; !slices.Equal(labels, want) {
		t.Errorf("filter %q, want %q", labels, want)
	}
	for _, id := range d.MembersShown() {
		if id == fakeslack.ChannelAgentID || id == fakeslack.ChannelAppID || id == fakeslack.AppUserID {
			t.Errorf("Members lists the bot %s", id)
		}
	}
	e.darkShot("agents-io-members")
	d.SelectMembersFilter(3) // Guests
	e.settle(200 * time.Millisecond)
	if got := d.MembersShown(); !slices.Equal(got, []string{fakeslack.ChannelGuestID}) {
		t.Errorf("Guests %v, want only the guest", got)
	}
	e.shot("agents-io-members-guests")
	d.SelectMembersFilter(2) // Members: not the guest or the frontline worker
	e.settle(200 * time.Millisecond)
	if got := d.MembersShown(); slices.Contains(got, fakeslack.ChannelGuestID) || slices.Contains(got, fakeslack.ChannelFrontID) || len(got) == 0 {
		t.Errorf("Members filter %v", got)
	}
	d.SelectMembersFilter(0)

	// frontline_users adds Frontline
	e.a.Core().SetExperimentGroup("frontline_users", "on")
	e.settle(100 * time.Millisecond)
	if labels := d.MembersFilterLabels(); len(labels) != 5 || labels[4] != "Frontline" {
		t.Errorf("filter with frontline_users %q", labels)
	}
	d.SelectMembersFilter(4)
	e.settle(200 * time.Millisecond)
	if got := d.MembersShown(); !slices.Equal(got, []string{fakeslack.ChannelFrontID}) {
		t.Errorf("Frontline %v", got)
	}
	d.SelectMembersFilter(0)
	e.a.Core().SetExperimentGroup("frontline_users", "")

	// Agents & apps (ReworkedAgentsAppsTab)
	d.ShowTab("Agents & apps")
	e.until("the agents roster", 10*time.Second, func() bool { e.frame(); return d.AgentsRosterLoaded() })
	e.settle(400 * time.Millisecond)
	if !d.AgentsReworked() {
		t.Fatal("not the reworked tab")
	}
	want := []string{"add", "member:" + fakeslack.ChannelAgentID, "member:" + fakeslack.ChannelAppID, "member:" + fakeslack.AppUserID}
	if got := d.AgentRosterKinds(); !slices.Equal(got, want) {
		t.Errorf("roster %q, want %q", got, want)
	}
	if d.AgentsCTAShown() {
		t.Error("the call to action shows beside agents")
	}
	e.darkShot("agents-io-apps")

	// search: in the channel, then outside it
	d.SetAgentsQuery("build")
	e.settle(400 * time.Millisecond)
	if got := d.AgentRosterKinds(); !slices.Equal(got, []string{"add", "in-heading", "member:" + fakeslack.ChannelAppID}) {
		t.Errorf("search build %q", got)
	}
	d.SetAgentsQuery("triage")
	e.until("out-of-channel results", 10*time.Second, func() bool {
		e.frame()
		return slices.Contains(d.AgentRosterKinds(), "out:"+fakeslack.OutsideAgentID)
	})
	if got := d.AgentRosterKinds(); !slices.Equal(got, []string{"add", "in-heading", "no-matches", "out-heading", "out:" + fakeslack.OutsideAgentID}) {
		t.Errorf("search triage %q", got)
	}
	e.settle(300 * time.Millisecond)
	e.darkShot("agents-io-apps-search")
	d.SetAgentsQuery("nobody-matches-this")
	e.settle(600 * time.Millisecond)
	if got := d.AgentRosterKinds(); len(got) != 0 {
		t.Errorf("no matches: %q", got)
	}
	e.shot("agents-io-apps-search-empty")
	d.SetAgentsQuery("")
	e.settle(200 * time.Millisecond)

	// the invite modal
	d.PressAgents("add")
	e.settle(300 * time.Millisecond)
	if !d.AgentInviteOpen() {
		t.Fatal("Add agents and apps opened no invite modal")
	}
	e.darkShot("agents-io-invite")
	d.SetAgentInviteQuery("poll")
	e.settle(800 * time.Millisecond)
	e.shot("agents-io-invite-results")
	var poll core.User
	e.until("Poll Maker in the store", 10*time.Second, func() bool {
		u, ok := e.a.Core().User(fakeslack.OutsideAppID)
		poll = u
		return ok
	})
	d.AddPerson(poll)
	d.SetAgentInviteQuery("")
	e.settle(300 * time.Millisecond)
	e.shot("agents-io-invite-picked")
	d.PressAgents("invite")
	e.until("conversations.invite", 10*time.Second, func() bool {
		e.frame()
		return slices.Contains(fs.ChannelInvites(ch), fakeslack.OutsideAppID)
	})
	e.until("the modal closes", 5*time.Second, func() bool { e.frame(); return !d.AgentInviteOpen() })
	e.until("the roster has Poll Maker", 10*time.Second, func() bool {
		e.frame()
		return slices.Contains(d.AgentRosterIDs(), fakeslack.OutsideAppID)
	})

	// Explore more agents and apps: the apps browser
	d.PressAgents("add")
	e.settle(200 * time.Millisecond)
	d.PressAgents("explore")
	e.settle(300 * time.Millisecond)
	if _, ok := topOverlay[*popups.AppsBrowser](e.a); !ok {
		t.Error("Explore more opened no apps browser")
	}
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)
}

// No agents or apps: the roster sizes to its add row and the call to
// action follows; Browse Agents and Apps opens the apps browser.
func TestChannelDetailsAgentsEmpty(t *testing.T) {
	e, _ := channelAgentsEnv(t, "on")
	d := openAgentsDetails(t, e, fakeslack.ChannelNoAgentsID)
	d.ShowTab("Agents & apps")
	e.until("the agents roster", 10*time.Second, func() bool { e.frame(); return d.AgentsRosterLoaded() })
	e.until("the counts", 10*time.Second, func() bool { e.frame(); return d.AgentsCTAShown() })
	if got := d.AgentRosterKinds(); !slices.Equal(got, []string{"add"}) {
		t.Errorf("roster %q, want the add row only", got)
	}
	e.settle(1500 * time.Millisecond) // the CTA's illustration
	e.darkShot("agents-io-apps-empty")
	d.PressAgents("browse")
	e.settle(300 * time.Millisecond)
	if _, ok := topOverlay[*popups.AppsBrowser](e.a); !ok {
		t.Error("Browse Agents and Apps opened no apps browser")
	}
}

// Off (and unassigned): today's tabs. Members keeps "Find people or
// agents", "Add people or agents" and its All / People / Agents select;
// Agents & apps is the field group (ChannelApps).
func TestChannelDetailsAgentsOff(t *testing.T) {
	for _, g := range []string{"off", ""} {
		t.Run("group="+g, func(t *testing.T) {
			e, fs := channelAgentsEnv(t, g)
			d := openAgentsDetails(t, e, fakeslack.ChannelAgentsID)
			d.ShowTab("Members")
			e.settle(500 * time.Millisecond)
			if got := d.MembersPlaceholder(); got != "Find people or agents" {
				t.Errorf("placeholder %q", got)
			}
			if got := d.MembersAddLabel(); got != "Add people or agents" {
				t.Errorf("add row %q", got)
			}
			if got := strings.Join(d.MembersFilterLabels(), "|"); got != "All|People|Agents" {
				t.Errorf("filter %q", got)
			}
			d.ShowTab("Agents & apps")
			e.until("apps users/list", 10*time.Second, func() bool { return slices.Contains(fs.UserListFilters(), "apps") })
			e.settle(500 * time.Millisecond)
			if d.AgentsReworked() || d.AgentsRosterLoaded() {
				t.Error("the reworked tab with the experiment off")
			}
			if g == "off" {
				e.shot("agents-off-apps")
			}
		})
	}
}
