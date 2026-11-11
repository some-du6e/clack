package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A streamed agent reply over the realtime socket (fakeslack's
// AgentStream: setStatus, startStream, appendStream ×2, stopStream,
// status cleared, suggested prompts) reaches subscribers as Slack's
// client sees it: the reply with streaming_state in_progress, updates
// until "completed", and the assistant status and prompts.
func TestAgentStreamEvents(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	defer a.Shutdown()
	evs, stop := a.Subscribe()
	defer stop()
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.History(context.Background(), fakeslack.AgentChannelID, ""); err != nil { // the channel's timeline is loaded
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	const ts = "1762520099.000100"
	go fs.PlayAgentStream(ts, 20*time.Millisecond)

	var states []string
	var status []string
	gotPrompts := false
	deadline := time.After(10 * time.Second)
	for !gotPrompts {
		select {
		case ev := <-evs:
			switch ev.Type {
			case "message", "update":
				if ev.TS == ts && ev.Msg != nil {
					states = append(states, ev.Type+":"+ev.Msg.StreamingState)
					if ev.Type == "message" && !ev.Reply {
						t.Error("streamed reply not marked as a thread reply")
					}
				}
			case "assistant_status":
				s, ok := AssistantStatusOf(ev)
				if !ok || s.ThreadTS != fakeslack.AgentThreadTS || s.BotUserID != fakeslack.AgentUserID {
					t.Errorf("status event %+v", s)
				}
				if s.Status != "" {
					if len(s.LoadingMessages) != 2 || s.StatusType != "typing" {
						t.Errorf("status %+v", s)
					}
					if _, ok := a.AssistantStatusFor(fakeslack.AgentChannelID, fakeslack.AgentThreadTS); !ok {
						t.Error("status not kept for the thread")
					}
				}
				status = append(status, s.Status)
			case "suggested_prompts":
				gotPrompts = true
			}
		case <-deadline:
			t.Fatalf("timed out: states %v status %q", states, status)
		}
	}
	want := []string{"message:in_progress", "update:in_progress", "update:in_progress", "update:completed"}
	if len(states) != len(want) {
		t.Fatalf("states %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states %v, want %v", states, want)
		}
	}
	if len(status) != 2 || status[0] != "is thinking…" || status[1] != "" {
		t.Errorf("status sequence %q", status)
	}
	if _, ok := a.AssistantStatusFor(fakeslack.AgentChannelID, fakeslack.AgentThreadTS); ok {
		t.Error("cleared status still kept")
	}
	p, ok := a.SuggestedPromptsFor(fakeslack.AgentChannelID, fakeslack.AgentThreadTS)
	if !ok || p.Title != "Try these" || len(p.Prompts) != 2 || p.Prompts[1].Message != "What should I do next?" {
		t.Errorf("prompts %+v", p)
	}
	// the stream stayed in the thread: nothing landed in the channel timeline
	tl := a.S.Timeline(fakeslack.AgentChannelID)
	if tl == nil {
		t.Fatal("no timeline")
	}
	for _, m := range tl.Msgs {
		if m.TS == ts {
			t.Error("streamed reply upserted into the channel timeline")
		}
	}
}

// agent_member_channel_count: Slack's channel header counts a channel's
// people (membership counts' member_count, from edge users/counts'
// people) with the experiment on; off, or not assigned (Slack's null),
// it counts people and apps (member_count + app_count).
func TestAgentMemberChannelCountFollowsExperiment(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a, ch := fakeBroadcastApp(t, fs, slack.User{})
	a.loadMemberCount(context.Background(), ch) // fakeslack: 1100 people, 12 apps
	count := func() int {
		c, ok := a.Conv(ch)
		if !ok {
			t.Fatal("channel lost")
		}
		return c.NumMembers
	}
	a.setExperiments(map[string]string{"agent_member_channel_count": "on"})
	if n := count(); n != 1100 {
		t.Errorf("on: %d members, want the 1100 people", n)
	}
	a.SetExperimentGroup("agent_member_channel_count", "off")
	if n := count(); n != 1112 {
		t.Errorf("off: %d members, want 1100 people + 12 apps", n)
	}
	a.SetExperimentGroup("agent_member_channel_count", "") // unassigned
	if n := count(); n != 1112 {
		t.Errorf("unassigned: %d members, want people + apps", n)
	}
	a.ClearExperimentGroup("agent_member_channel_count")
	if n := count(); n != 1100 {
		t.Errorf("server group back: %d", n)
	}
	if apps := a.AppCountCached(ch); apps != 12 {
		t.Errorf("app_count %d", apps)
	}
}

// app_thread_nugget_fe / app_nugget_refinement (boot-shared 352G, G2VO):
// an app's status in a thread is the footer's nugget, and the composer
// leaves the app out of its typing line (suppressBotTyping); a message in
// the thread ends it; Slackbot threads get none; without the experiment
// (off or not assigned) there is no nugget and no suppression.
func TestAppThreadNugget(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UAPP", Name: "kyto", IsBot: true, RealName: "Kyto Agent",
		Profile: slack.Profile{RealName: "Kyto Agent", DisplayName: "kyto"}}})
	status := func(bot, st, username string) {
		a.handle(slack.Event{Type: "ai_assistant_status", Raw: raw(map[string]any{"channel_id": "D1", "thread_ts": "1.0",
			"bot_user_id": bot, "status": st, "username": username, "loading_messages": []string{"Reading the thread…", "Writing…"}})})
	}
	a.setExperiments(map[string]string{"app_thread_nugget_fe": "on", "app_nugget_refinement": "on"})
	status("UAPP", "is thinking…", "")
	n, ok := a.AppThreadNugget("D1", "1.0")
	if !ok || n.Name != "Kyto Agent" || !n.Chip || len(n.LoadingMessages) != 2 || n.BotUserID != "UAPP" {
		t.Fatalf("nugget %+v %v", n, ok)
	}
	if bot, ok := a.SuppressBotTyping("D1", "1.0"); !ok || bot != "UAPP" {
		t.Errorf("suppressBotTyping %q %v", bot, ok)
	}
	if _, ok := a.AppThreadNugget("D1", "2.0"); ok {
		t.Error("a nugget in a thread without a status")
	}
	a.SetExperimentGroup("app_nugget_refinement", "off")
	if n, _ := a.AppThreadNugget("D1", "1.0"); n.Chip {
		t.Error("app_nugget_refinement off still draws the chip")
	}
	for _, g := range []string{"off", ""} {
		a.SetExperimentGroup("app_thread_nugget_fe", g)
		if _, ok := a.AppThreadNugget("D1", "1.0"); ok {
			t.Errorf("app_thread_nugget_fe %q: nugget shown", g)
		}
		if _, ok := a.SuppressBotTyping("D1", "1.0"); ok {
			t.Errorf("app_thread_nugget_fe %q: typing suppressed", g)
		}
	}
	a.ClearExperimentGroup("app_thread_nugget_fe")
	a.ClearExperimentGroup("app_nugget_refinement")
	status("UAPP", "is thinking…", "Kyto (beta)") // a username wins
	if n, _ := a.AppThreadNugget("D1", "1.0"); n.Name != "Kyto (beta)" {
		t.Errorf("name %q, want the status's username", n.Name)
	}
	// the app's reply (any message in the thread) ends the status
	a.handle(rtm("message", map[string]any{"channel": "D1", "ts": "3.0", "thread_ts": "1.0", "user": "UAPP", "text": "done"}))
	if _, ok := a.AppThreadNugget("D1", "1.0"); ok {
		t.Error("the nugget outlived the app's reply")
	}
	if _, ok := a.AssistantStatusFor("D1", "1.0"); ok {
		t.Error("the status outlived the app's reply")
	}
	// Slackbot's own agent threads have none (isSlackbotAgentThread)
	status("USLACKBOT", "is thinking…", "")
	if _, ok := a.AppThreadNugget("D1", "1.0"); ok {
		t.Error("a nugget for Slackbot")
	}
}
