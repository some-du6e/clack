package ui

// `clack demo` end to end, headless: the real UI and core over the demo
// session (internal/demo: fakeslack on loopback with its realtime socket,
// the curated workspace and the bots), the way cmd/clack/demo.go wires
// them. Synthetic only: nothing here talks to Slack. With CLACK_DEMO_SHOTS
// set (the demo workflow) each step is saved as a PNG, light and dark.

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/demo"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

const demoTestBanner = "Quillfeather Paper Co. and everyone in it are made up. Nothing here connects to Slack."

// demoEnv starts a demo session and the UI on it (live updates on).
func demoEnv(t *testing.T, opts demo.Options) (*perfEnv, *demo.Session) {
	t.Helper()
	s, err := demo.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "ui.json"), []byte(`{"realtime":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &perfEnv{tb: t, fs: s.FS, home: home, size: image.Pt(1440, 900), now: time.Now(), start: time.Now()}
	images := imgcache.New(s.Fetch, "", nil)
	e.a = New(Options{ConfigDir: home, Started: e.start, Demo: demoTestBanner}, images)
	c, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(home, "cache")
	e.a.Attach(core.New(c, store.New(cache)), cache)
	images.SetInvalidate(func() {})
	e.a.th.Images = images
	if installHooks != nil {
		installHooks(e.a)
	}
	e.a.data.start(e.a)
	if os.Getenv("CLACK_DEMO_SHOTS") != "" {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	t.Cleanup(e.shutdown)
	e.waitReady()
	return e, s
}

// demoMsg is core's copy of a message (nil until it has it).
func (e *perfEnv) demoMsg(ch, ts string) *slack.Message {
	if tl := e.a.data.core.S.Timeline(ch); tl != nil {
		for i := range tl.Msgs {
			if tl.Msgs[i].TS == ts {
				m := tl.Msgs[i]
				return &m
			}
		}
	}
	return nil
}

func (e *perfEnv) demoShot(t *testing.T, name string) {
	e.settle(400 * time.Millisecond)
	e.shotTo(t, os.Getenv("CLACK_DEMO_SHOTS"), name)
}

// TestDemoSmoke: the sidebar, a channel's backstory, a bot answering a
// posted message in its thread, a Block Kit poll button's round trip and
// the code channel.
func TestDemoSmoke(t *testing.T) {
	e, s := demoEnv(t, demo.Options{Quiet: true, Speed: 40, Seed: 7})
	a := e.a
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// sidebar: Slack's code channels section, the custom sections, channels and DMs
	e.until("the demo sidebar", 30*time.Second, func() bool {
		return e.sidebarHas(demo.GeneralID) && e.sidebarHas(demo.SalesID) && e.sidebarHas("D0DEMOGORD") && e.sidebarHas(demo.CodeID)
	})
	names := map[string]bool{}
	for _, sec := range a.model().Sections {
		names[sec.Name] = true
	}
	for _, want := range []string{"Sales floor", "Committees"} {
		if !names[want] {
			t.Errorf("no %q section in the sidebar: %v", want, names)
		}
	}
	if got := a.teamName(); got != demo.TeamName {
		t.Errorf("team name %q, want %q", got, demo.TeamName)
	}
	if !strings.Contains(a.windowTitle(), "(Demo)") {
		t.Errorf("window title %q doesn't say (Demo)", a.windowTitle())
	}

	// a channel: #general's backstory, Gordon's posts among it
	e.openChannel(demo.GeneralID)
	e.until("#general's history", 20*time.Second, func() bool { return e.loadedMessages(demo.GeneralID) >= 10 })
	gordon := false
	for _, m := range e.a.data.core.S.Timeline(demo.GeneralID).Msgs {
		gordon = gordon || m.User == "U0DEMOGORD"
	}
	if !gordon {
		t.Error("no message from Gordon in #general")
	}
	e.demoShot(t, "demo-general")

	// a bot reply: post the way the composer does; someone answers in the
	// thread, and the client learns about it over the socket
	sent, err := a.data.core.Send(ctx, core.SendOpts{Channel: demo.ITHelpID, Text: "hi! the printer says PC LOAD PAPER again, can anyone help?"})
	if err != nil {
		t.Fatal(err)
	}
	e.openChannel(demo.ITHelpID)
	e.until("a bot reply in the thread", 30*time.Second, func() bool {
		m := e.demoMsg(demo.ITHelpID, sent.TS)
		return m != nil && m.ReplyCount > 0
	})
	reply := s.World.Thread(demo.ITHelpID, sent.TS)
	if len(reply) == 0 || reply[0].User == demo.SelfID || reply[0].User == "" {
		t.Fatalf("thread replies %+v", reply)
	}
	a.OpenThread(demo.ITHelpID, sent.TS)
	e.until("the reply in the open thread", 20*time.Second, func() bool {
		th, _ := a.data.core.S.Thread(demo.ITHelpID, sent.TS)
		return len(th) > 1
	})
	e.demoShot(t, "demo-thread-reply")

	// Block Kit: Pulse's poll, a Vote button through the client's
	// blocks.actions path, the rewritten message back over the socket
	pollTS := s.World.PostPoll(demo.LunchID)
	e.openChannel(demo.LunchID)
	e.until("the poll in #lunch-orders", 20*time.Second, func() bool { return e.demoMsg(demo.LunchID, pollTS) != nil })
	poll := e.demoMsg(demo.LunchID, pollTS)
	var blocks []struct {
		BlockID   string `json:"block_id"`
		Accessory *struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		} `json:"accessory"`
	}
	if err := json.Unmarshal(poll.Blocks, &blocks); err != nil {
		t.Fatal(err)
	}
	var blockID, value string
	for _, b := range blocks {
		if b.Accessory != nil && b.Accessory.ActionID == demo.PollVoteAction {
			blockID, value = b.BlockID, b.Accessory.Value
			break
		}
	}
	if value == "" {
		t.Fatalf("no Vote button in the poll: %s", poll.Blocks)
	}
	e.demoShot(t, "demo-poll")
	action, _ := json.Marshal(map[string]any{"type": "button", "action_id": demo.PollVoteAction, "block_id": blockID, "value": value,
		"text": map[string]any{"type": "plain_text", "text": "Vote"}})
	if _, err := a.data.core.Interactive().BlockActionSend(ctx, core.BlockAction{ServiceID: poll.BotID, AppID: demo.PulseAppID,
		Container: core.ActionContainer{Type: "message", ChannelID: demo.LunchID, MessageTS: pollTS}, Action: action}); err != nil {
		t.Fatalf("blocks.actions: %v", err)
	}
	if v := s.World.PollVotes(demo.LunchID, pollTS)[value]; len(v) != 1 || v[0] != demo.SelfID {
		t.Fatalf("votes for %s: %v", value, v)
	}
	e.until("the poll rewritten with your vote", 20*time.Second, func() bool {
		m := e.demoMsg(demo.LunchID, pollTS)
		return m != nil && strings.Contains(string(m.Blocks), "Voted")
	})
	e.demoShot(t, "demo-poll-voted")

	// the code channel: Inkwell's session, its diff and its PR
	e.openChannel(demo.CodeID)
	e.until("the code channel", 20*time.Second, func() bool {
		ci := a.model().conv(demo.CodeID)
		return ci != nil && ci.Code != nil && e.loadedMessages(demo.CodeID) > 0
	})
	if cc := a.data.core.CodeChannelOf(demo.CodeID); cc == nil {
		t.Fatal("core has no code channel for #inkwell-order-portal")
	}
	d, err := a.data.core.CodeChannelDiff(ctx, demo.CodeID)
	if err != nil || d == nil || len(d.Files) == 0 {
		t.Fatalf("the code channel's diff: %+v %v", d, err)
	}
	if pr, err := a.data.core.CodeChannelPullRequest(ctx, demo.CodeID, demo.DemoPRURL); err != nil || pr == nil {
		t.Fatalf("the code channel's PR: %+v %v", pr, err)
	}
	e.demoShot(t, "demo-code-channel")
}

// TestDemoShots: the demo as it starts, light and dark, with the bots
// running (CLACK_DEMO_SHOTS only).
func TestDemoShots(t *testing.T) {
	if os.Getenv("CLACK_DEMO_SHOTS") == "" {
		t.Skip("CLACK_DEMO_SHOTS not set")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, s := demoEnv(t, demo.Options{Quiet: true, Speed: 40, Seed: 11})
			if dark {
				e.a.setDark(true)
			}
			e.until("the demo sidebar", 30*time.Second, func() bool { return e.sidebarHas(demo.GeneralID) && e.sidebarHas(demo.CodeID) })
			for _, c := range []struct{ id, name string }{
				{demo.GeneralID, "general"}, {demo.SalesID, "sales-leaderboard"}, {demo.PartyID, "party-planning-crew"},
				{demo.RandomID, "random"}, {"D0DEMOGORD", "dm-gordon"}, {demo.GroupDMID, "group-dm"},
			} {
				e.openChannel(c.id)
				e.until(c.name+" history", 20*time.Second, func() bool { return e.loadedMessages(c.id) > 0 })
				e.demoShot(t, "demo-"+mode+"-"+c.name)
			}
			// Inkwell streaming in the code channel
			e.openChannel(demo.CodeID)
			e.until("code channel history", 20*time.Second, func() bool { return e.loadedMessages(demo.CodeID) > 0 })
			s.World.RunAgent(demo.CodeID, "", "can you add a test for 100 reams?")
			e.demoShot(t, "demo-"+mode+"-code-channel")
			// a poll, an incoming huddle, Activity
			ts := s.World.PostPoll(demo.LunchID)
			e.openChannel(demo.LunchID)
			e.until("the poll", 20*time.Second, func() bool { return e.demoMsg(demo.LunchID, ts) != nil })
			e.demoShot(t, "demo-"+mode+"-poll")
			s.World.RingHuddle("gordon")
			e.settle(time.Second)
			e.demoShot(t, "demo-"+mode+"-huddle-ring")
			e.a.Navigate("activity")
			e.demoShot(t, "demo-"+mode+"-activity")
		})
	}
}
