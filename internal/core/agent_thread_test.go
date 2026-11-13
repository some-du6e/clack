package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Karim's agent thread (2026-11-12): in a DM with an agent app, its
// replies (a "working..." placeholder that a chat.update turns into the
// answer, and a footer message per turn: a context line and feedback
// buttons) showed in Activity as @ rows, the rows' previews were the
// context line, and the open thread pane kept "working..." after the
// answer came. fakeslack's AgentThread fixture plays that thread.

func agentThreadApp(t *testing.T) (*App, *fakeslack.Server, *eventLog) {
	t.Helper()
	sz := fakeslack.Small
	sz.AgentThread = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	l := &eventLog{}
	go func() {
		for ev := range evs {
			l.add(ev)
		}
	}()
	return a, fs, l
}

func replyText(msgs []Message, ts string) (string, bool) {
	for i := range msgs {
		if msgs[i].TS == ts {
			return msgs[i].Text, true
		}
	}
	return "", false
}

// The agent's replies mention no one: the server's thread_reply entries
// (one per reply) never reach Activity, only the thread's thread_v2
// bundle does, and none counts as a mention. The bundle's message (the
// footer) previews as its text, not its context line.
func TestAgentThreadActivityNoMentions(t *testing.T) {
	a, fs, log := agentThreadApp(t)
	if _, err := a.Replies(context.Background(), fakeslack.AgentDMID, fakeslack.AgentDMRootTS); err != nil {
		t.Fatal(err)
	}
	before := a.Count(fakeslack.AgentDMID).Mentions
	for _, ts := range []string{fakeslack.AgentDMReply1TS, fakeslack.AgentDMFooter1TS, fakeslack.AgentDMAnswerTS} {
		for _, ev := range fakeslack.AgentDMActivity(ts) {
			fs.Push(ev)
		}
	}
	fs.PlayAgentEdit(0)
	for _, ev := range fakeslack.AgentDMActivity(fakeslack.AgentDMFooter2TS) {
		fs.Push(ev)
	}
	log.wait(t, "the thread item for the second footer", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "activity" && ev.Activity != nil && ev.Activity.Type == "thread_v2" && ev.Activity.Msg != nil &&
				ev.Activity.Msg.TS == fakeslack.AgentDMFooter2TS {
				return true
			}
		}
		return false
	})
	flush(t, fs, log)
	var last *ActivityItem
	for _, ev := range log.all() {
		switch {
		case ev.Type == "activity" && ev.Activity != nil:
			if ev.Activity.Type != "thread_v2" {
				t.Errorf("a realtime %q item reached Activity (an @ row)", ev.Activity.Type)
			}
			last = ev.Activity
		case ev.Type == "message" && ev.Channel == fakeslack.AgentDMID:
			if ev.Mention || (ev.Msg != nil && a.MentionsViewerInReply(ev.Msg)) {
				t.Errorf("the agent's reply %s counts as a mention", ev.TS)
			}
		}
	}
	if got := a.Count(fakeslack.AgentDMID).Mentions; got != before {
		t.Errorf("mentions %d → %d for replies without a mention", before, got)
	}
	if last == nil || last.Msg == nil {
		t.Fatal("no thread item with its message")
	}
	doc, ok := a.PreviewDoc(last.Msg)
	if !ok {
		t.Fatal("the footer has no preview")
	}
	if got := richtext.PlainText(doc); got != "done in under a second" {
		t.Errorf("the thread row previews %q, want the message's text", got)
	}
}

// The answer's chat.update lands while the thread's first replies fetch
// is out (its answer read the thread before): the cached thread, and what
// Replies returns, keep the edited answer and the footer posted
// meanwhile, not the answer's "working...".
func TestAgentThreadFetchKeepsLiveEdit(t *testing.T) {
	a, fs, log := agentThreadApp(t)
	fs.SetFault("conversations.replies", fakeslack.Fault{ReplyDelay: 1500 * time.Millisecond, Times: 1})
	type res struct {
		msgs []Message
		err  error
	}
	done := make(chan res, 1)
	go func() {
		msgs, err := a.Replies(context.Background(), fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
		done <- res{msgs, err}
	}()
	dl := time.Now().Add(10 * time.Second)
	for fs.AgentDMRepliesRead() == 0 {
		if time.Now().After(dl) {
			t.Fatal("conversations.replies never read the thread")
		}
		time.Sleep(5 * time.Millisecond)
	}
	fs.PlayAgentEdit(0)
	log.wait(t, "the footer's message", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "message" && ev.TS == fakeslack.AgentDMFooter2TS {
				return true
			}
		}
		return false
	})
	var r res
	select {
	case r = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Replies never answered")
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	if txt, _ := replyText(r.msgs, fakeslack.AgentDMAnswerTS); txt != "pong" {
		t.Errorf("Replies has the answer as %q, want the edit (pong)", txt)
	}
	if _, ok := replyText(r.msgs, fakeslack.AgentDMFooter2TS); !ok {
		t.Error("Replies lost the footer posted while it was out")
	}
	cached, _ := a.S.Thread(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
	for _, m := range cached {
		if m.TS == fakeslack.AgentDMAnswerTS && (m.Text != "pong" || m.ThreadTS != fakeslack.AgentDMRootTS) {
			t.Errorf("the cached answer is %q in thread %q", m.Text, m.ThreadTS)
		}
	}
	// a later fetch (the server has the edit by then) changes nothing
	msgs, err := a.RefreshReplies(context.Background(), fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
	if err != nil {
		t.Fatal(err)
	}
	if txt, _ := replyText(msgs, fakeslack.AgentDMAnswerTS); txt != "pong" {
		t.Errorf("after a refetch the answer is %q", txt)
	}
}

// A message_changed whose copy has no thread_ts keeps the reply in its
// thread (Slack keeps a thread's replies apart from the message objects):
// the update names the thread, and the DM's own timeline never gets it.
func TestAgentThreadEditKeepsThread(t *testing.T) {
	a, fs, log := agentThreadApp(t)
	if _, err := a.Replies(context.Background(), fakeslack.AgentDMID, fakeslack.AgentDMRootTS); err != nil {
		t.Fatal(err)
	}
	if _, err := a.History(context.Background(), fakeslack.AgentDMID, ""); err != nil {
		t.Fatal(err)
	}
	ans := fs.AgentDMAnswer(time.Now())
	ans.ThreadTS, ans.ParentUser = "", ""
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": fakeslack.AgentDMID, "hidden": true,
		"message": ans, "event_ts": ans.TS})
	log.wait(t, "the answer's update", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "update" && ev.TS == fakeslack.AgentDMAnswerTS {
				return true
			}
		}
		return false
	})
	for _, ev := range log.all() {
		if ev.Type == "update" && ev.TS == fakeslack.AgentDMAnswerTS {
			if ev.ThreadTS != fakeslack.AgentDMRootTS || ev.Msg == nil || ev.Msg.ThreadTS != fakeslack.AgentDMRootTS {
				t.Errorf("the update left the thread: %q / %+v", ev.ThreadTS, ev.Msg)
			}
			if ev.Msg != nil && ev.Msg.Text != "pong" {
				t.Errorf("the update's text %q", ev.Msg.Text)
			}
		}
	}
	if tl := a.S.Timeline(fakeslack.AgentDMID); tl != nil {
		for _, m := range tl.Msgs {
			if m.TS == fakeslack.AgentDMAnswerTS {
				t.Error("the reply joined the DM's timeline")
			}
		}
	}
}

// The edited answer is an app's message its bot edited: Slack shows no
// "(edited)" (shouldDisplayAsEdited), in the thread or anywhere else.
func TestAgentAnswerNoEditedLabel(t *testing.T) {
	a, fs, _ := agentThreadApp(t)
	ans := fs.AgentDMAnswer(time.Now())
	m := a.message(fakeslack.AgentDMID, &ans)
	if !m.Edited || m.DisplaysEdited() {
		t.Errorf("edited %v, shows (edited) %v; want an edited message without the label", m.Edited, m.DisplaysEdited())
	}
	if strings.TrimSpace(richtext.PlainText(m.Doc)) != "pong" {
		t.Errorf("the answer's body %q", richtext.PlainText(m.Doc))
	}
	own := slack.Message{TS: "1.000100", User: fakeslack.SelfID, Text: "typo fixed", Edited: &slack.Edited{User: fakeslack.SelfID, TS: "2.000000"}}
	if mm := a.message("C1", &own); !mm.DisplaysEdited() {
		t.Error("a person's edited message lost its (edited)")
	}
}
