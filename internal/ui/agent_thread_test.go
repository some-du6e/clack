package ui

// An agent app's answer in the open thread pane, against internal/
// fakeslack's AgentThread fixture (synthetic; nothing talks to Slack).
// Karim, 2026-11-12: the pane kept the agent's "working..." placeholder
// after its chat.update made it the answer (Activity, fetched later,
// already showed it), then flickered. Slack's message_changed replaces
// the message and every view of it redraws in place: the row turns into
// the answer once and never shows the placeholder again, with no
// "(edited)" (an app's message), and the footer (the context line and
// the 👍/👎 buttons) follows. With THREAD_HEADER_SHOTS_DIR set (the
// thread-header workflow) the pane is saved, light and dark.

import (
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func agentThreadEnv(t *testing.T, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.AgentThread = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("THREAD_HEADER_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	svc := e.a.Core()
	evs, stop := svc.Subscribe()
	defer stop()
	svc.SetRealtime(true)
	dl := time.After(10 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
	return e
}

// agentAnswerRows runs frames while the answer changes, recording what
// its row draws each frame, until the footer's row is drawn too.
func (e *perfEnv) agentAnswerRows(what string) []string {
	t := e.tb
	t.Helper()
	var seen []string
	dl := time.Now().Add(20 * time.Second)
	for {
		e.frame()
		tv := e.threadView(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
		if tv != nil {
			if txt, edited, ok := tv.RowText(fakeslack.AgentDMAnswerTS); ok {
				if edited {
					t.Errorf("%s: the app's answer draws (edited)", what)
				}
				if len(seen) == 0 || seen[len(seen)-1] != txt {
					seen = append(seen, txt)
				}
			} else if len(seen) > 0 {
				seen = append(seen, "<gone>")
			}
			if _, _, ok := tv.RowText(fakeslack.AgentDMFooter2TS); ok && len(seen) > 0 && seen[len(seen)-1] == "pong" {
				return seen
			}
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: the answer's row drew %q, want it to end at pong with the footer under it", what, seen)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// checkAnswerRows: the placeholder, then the answer, once.
func checkAnswerRows(t *testing.T, what string, seen []string) {
	t.Helper()
	pong := -1
	for i, s := range seen {
		switch {
		case s == "pong" && pong < 0:
			pong = i
		case pong >= 0 && s != "pong":
			t.Errorf("%s: after the answer the row drew %q again (rows %q)", what, s, seen)
		}
	}
	for _, s := range seen[:max(pong, 0)] {
		if s != "working..." {
			t.Errorf("%s: before the answer the row drew %q (rows %q)", what, s, seen)
		}
	}
}

func TestAgentThreadPaneAnswer(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := agentThreadEnv(t, dark)
			e.openChannel(fakeslack.AgentDMID)
			e.a.OpenThread(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
			e.until("the placeholder in the pane", 20*time.Second, func() bool {
				tv := e.threadView(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
				if tv == nil {
					return false
				}
				txt, _, ok := tv.RowText(fakeslack.AgentDMAnswerTS)
				return ok && txt == "working..."
			})
			e.threadHeaderShot("agent-thread-working-" + mode)
			go e.fs.PlayAgentEdit(400 * time.Millisecond)
			seen := e.agentAnswerRows("open pane")
			checkAnswerRows(t, "open pane", seen)
			e.threadHeaderShot("agent-thread-answer-" + mode)
		})
	}
}

// The edit lands while the pane's first replies fetch is out (its answer
// read the thread before the edit): the pane still ends on the answer
// and never goes back to the placeholder.
func TestAgentThreadPaneEditDuringLoad(t *testing.T) {
	e := agentThreadEnv(t, false)
	e.openChannel(fakeslack.AgentDMID)
	e.settle(300 * time.Millisecond)
	e.fs.SetFault("conversations.replies", fakeslack.Fault{ReplyDelay: 1500 * time.Millisecond, Times: 1})
	e.a.OpenThread(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
	e.until("the replies request read the thread", 20*time.Second, func() bool { return e.fs.AgentDMRepliesRead() > 0 })
	go e.fs.PlayAgentEdit(0)
	seen := e.agentAnswerRows("edit during the load")
	checkAnswerRows(t, "edit during the load", seen)
	e.settle(2 * time.Second) // a refresh after the load keeps it too
	if txt, _, _ := e.threadView(fakeslack.AgentDMID, fakeslack.AgentDMRootTS).RowText(fakeslack.AgentDMAnswerTS); txt != "pong" {
		t.Errorf("after settling the answer draws %q", txt)
	}
}

// The pane's header in the agent app's DM (polish-1113: Karim's
// "Thread gorkie" was with Activity in the primary view, the agent-thread
// shots with the DM there). Slack's ThreadSubtitle hides the name when
// the primary view is the thread's own conversation, which an app DM is
// (EnsureAppViewType gives it a Channel view): "Thread" alone. Opened
// from Activity it names the app's bot user by its real name (no display
// name, the way users.info answers an app's bot user).
func TestAgentThreadHeader(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := agentThreadEnv(t, dark)
			e.openChannel(fakeslack.AgentDMID)
			e.a.OpenThread(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
			e.until("the pane in the DM", 20*time.Second, func() bool {
				return e.threadView(fakeslack.AgentDMID, fakeslack.AgentDMRootTS) != nil
			})
			e.settle(800 * time.Millisecond)
			if sub, _, ok := e.threadView(fakeslack.AgentDMID, fakeslack.AgentDMRootTS).HeaderName(); ok {
				t.Errorf("in the app's DM the header names %+v (Slack hides it)", sub)
			}
			e.threadHeaderShot("agent-thread-header-dm-" + mode)

			e.a.Navigate("activity")
			e.a.OpenThread(fakeslack.AgentDMID, fakeslack.AgentDMRootTS)
			var got messages.HeaderSubtitle
			var shown bool
			e.until("the header names the app from Activity", 20*time.Second, func() bool {
				if tv := e.threadView(fakeslack.AgentDMID, fakeslack.AgentDMRootTS); tv != nil {
					got, _, shown = tv.HeaderName()
					if shown && got.Text == "" {
						t.Fatalf("the header drew %+v (a placeholder)", got)
					}
				}
				return shown && got.Text == "Pip" && got.Icon == "" && got.Link
			})
			e.threadHeaderShot("agent-thread-header-activity-" + mode)
		})
	}
}
