package ui

// Channels with agents and apps (core/channel_agents.go, joincard.go,
// agent_count.go) against internal/fakeslack with Size.PreviewAgents:
// #lounge-preview has 4,199 people and 11 agents and apps, an agent and
// an app wrote its two newest messages, and edge users/list's "apps"
// filter lists them and one more app. Each experiment is forced the way
// the Experiments plugins override it. Synthetic only; with
// PREVIEW_SHOTS_DIR set (the exp-channels workflow) the tests save light
// and dark screenshots.

import (
	"image"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func agentsEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.PreviewAgents = true
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
	return e, fs
}

func rectMid(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }

// The revamped JoinChannelPane under channel_join_experience_with_agents:
// the facepile has the channel's people and the two agents that posted,
// the caption names two people and two agents and counts the rest, the
// Join split button is the only action (no Details) and its caret opens
// "Join…"; with the experiment off (or unassigned, as on the test
// account) the classic pane is back.
func TestJoinCardWithAgents(t *testing.T) {
	e, _ := agentsEnv(t)
	svc := e.a.Core()
	svc.SetExperimentGroup(core.ExpChannelJoinWithAgents, "on")
	ch := fakeslack.PreviewChannelID
	e.openChannel(ch)
	c := e.a.convoFor(ch, "")
	e.until("join card", 20*time.Second, func() bool { return e.a.footerFor(c) == footPreview && c.jp != nil && !c.jp.joinRect.Empty() })
	var d joinCardInfo
	e.until("the card's people and agents", 20*time.Second, func() bool {
		ci := e.a.convOf(ch)
		if ci == nil {
			return false
		}
		d = e.a.joinCardData(ch, ci)
		e.frame()
		return d.settled && len(d.agentFaces) == 2 && len(d.people) == 3
	})
	if want := []string{fakeslack.PreviewAgentID, fakeslack.PreviewApp1ID}; !slices.Equal(d.agentFaces, want) {
		t.Errorf("agent faces %v, want %v (the agents seen active, newest first)", d.agentFaces, want)
	}
	if want := []string{fakeslack.PreviewAgentID, fakeslack.PreviewApp1ID, fakeslack.PreviewApp2ID}; !slices.Equal(d.agentNames, want) {
		t.Errorf("agent names %v, want %v", d.agentNames, want)
	}
	if d.memberN != fakeslack.PreviewPeople || d.agentN != fakeslack.PreviewApps {
		t.Errorf("counts %d people, %d agents and apps", d.memberN, d.agentN)
	}
	members, agents := e.a.joinCaptions(d)
	if want := "Standup Agent, Changelog, and 9 other agents or apps are also here."; agents != want {
		t.Errorf("agents caption %q, want %q", agents, want)
	}
	names := e.a.memberNames(d.people)
	if len(names) < 2 {
		t.Fatalf("member names %v", names)
	}
	if want := names[0] + ", " + names[1] + ", and 4,197 more people are here."; members != want {
		t.Errorf("members caption %q, want %q", members, want)
	}
	if !c.jp.detailsRect.Empty() {
		t.Error("the revamped card has a Details button")
	}
	if c.jp.caretRect.Empty() {
		t.Error("no Join… caret on a public channel (OtvL Y)")
	}
	e.settle(300 * time.Millisecond)
	e.shot("join-card-agents")

	// the caret's menu: "Join…" over the three durations
	e.click(rectMid(c.jp.caretRect))
	e.settle(200 * time.Millisecond)
	m, ok := topOverlay[*popups.Menu](e.a)
	if !ok {
		t.Fatal("the caret opened no menu")
	}
	if got := strings.Join(m.Labels(), "|"); got != "[Join…]|Just for today|For 48 hours|For 1 week" {
		t.Errorf("menu %q", got)
	}
	e.shot("join-card-agents-menu")
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)

	e.a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.shot("join-card-agents-dark")
	e.a.setDark(false)

	// a short count names everyone: "A, B, and C are also here."
	d2 := d
	d2.agentN = 3
	if _, ag := e.a.joinCaptions(d2); ag != "Standup Agent, Changelog, and Triage Helper are also here." {
		t.Errorf("all named: %q", ag)
	}
	d2.agentNames, d2.agentN = d.agentNames[:1], 1
	if _, ag := e.a.joinCaptions(d2); ag != "Standup Agent is also here." {
		t.Errorf("one: %q", ag)
	}

	for _, g := range []string{"off", ""} {
		svc.SetExperimentGroup(core.ExpChannelJoinWithAgents, g)
		e.settle(100 * time.Millisecond)
		if c.jp.detailsRect.Empty() {
			t.Errorf("%q: no classic pane (Details)", g)
		}
	}
	e.shot("join-card-classic")
}

// AgentCountButton under channel_header_with_agents: the ai-agents glyph
// and the channel's app count after the avatar stack; hovering fetches
// the agents and apps for "Includes …"; a click opens the details on
// Agents & apps. Off or unassigned: no button.
func TestHeaderAgentCount(t *testing.T) {
	e, _ := agentsEnv(t)
	svc := e.a.Core()
	svc.SetExperimentGroup(core.ExpChannelHeaderWithAgents, "on")
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.until("agent count", 20*time.Second, func() bool {
		e.frame()
		n, ok := svc.AgentAppCount(ch)
		return ok && n == 12 && !e.a.agentCountRect.Empty()
	})
	if !e.a.showAgentCount(ch, e.a.convOf(ch)) {
		t.Fatal("showAgentCount false with the experiment on")
	}
	e.settle(300 * time.Millisecond)
	e.shot("header-agent-count")

	// hover: the fetch, then the tooltip names the apps
	p := rectMid(e.a.agentCountRect)
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	e.until("the agents fetched", 10*time.Second, func() bool {
		e.move(f32.Pt(float32(p.X), float32(p.Y)))
		st := e.a.chAgents[ch]
		return st != nil && st.done && len(st.ids) > 0
	})
	lines := e.a.agentCountTip(ch, e.a.chAgents[ch])
	if len(lines) != 2 || lines[0].Text != "View all agents & apps of this channel" || !lines[1].Subtitle || lines[1].Text != "Includes Deploy Bot" {
		t.Errorf("tooltip %+v", lines)
	}
	e.settle(700 * time.Millisecond)
	e.shot("header-agent-count-tooltip")
	e.a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.shot("header-agent-count-dark")
	e.a.setDark(false)

	// a click opens Agents & apps
	e.click(p)
	e.settle(300 * time.Millisecond)
	if tab := topDetails(t, e).Tab(); tab != "Agents & apps" {
		t.Errorf("details opened on %q", tab)
	}
	for e.a.dismissTop() {
	}

	// the previewed channel counts its own 11
	e.openChannel(fakeslack.PreviewChannelID)
	e.until("preview's count", 20*time.Second, func() bool {
		e.frame()
		n, ok := svc.AgentAppCount(fakeslack.PreviewChannelID)
		return ok && n == fakeslack.PreviewApps
	})
	e.settle(300 * time.Millisecond)
	e.shot("header-agent-count-preview")

	e.openChannel(ch)
	for _, g := range []string{"off", ""} {
		svc.SetExperimentGroup(core.ExpChannelHeaderWithAgents, g)
		e.frame()
		if e.a.showAgentCount(ch, e.a.convOf(ch)) {
			t.Errorf("%q: agent count shown", g)
		}
	}
}
