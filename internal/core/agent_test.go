package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
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
