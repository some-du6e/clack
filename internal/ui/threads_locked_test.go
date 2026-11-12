package ui

// Locked threads (docs/THREADS-AUDIT.md "Locked thread"; joinpane.go
// threadLockedCopy, core/thread_lock.go) on internal/fakeslack's
// #th-audit: ThreadFooter's MessageInputLockedThreadRoadblock in place of
// the reply box, its two strings (feat_flag_message_hide_replies), the
// socket's message_locked turning it off and on, and its place before the
// join pane. Synthetic only.

import (
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

const (
	lockedCopy        = "Replies have been turned off for this thread."
	lockedHideReplies = "Replies and reactions have been turned off for this thread."
)

// roadblockOf is a conversation composer's roadblock message.
func roadblockOf(c *convo) string {
	if c == nil || c.comp == nil {
		return ""
	}
	if r, ok := c.comp.(interface{ RoadblockMessage() string }); ok {
		return r.RoadblockMessage()
	}
	return ""
}

// openAuditThread opens root in #th-audit and waits for its replies.
func (e *perfEnv) openAuditThread(root string) *convo {
	ch := fakeslack.ThreadsAuditChannel
	e.a.OpenThread(ch, root)
	e.until("the thread", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && tv.ContentHeight() > 0
	})
	return e.a.convoFor(ch, root)
}

func TestThreadsAuditLocked(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch := fakeslack.ThreadsAuditChannel
	e.openChannel(ch)
	tc := e.openAuditThread(fakeslack.ThreadsAuditLocked)
	e.until("the locked roadblock", 10*time.Second, func() bool { return roadblockOf(tc) == lockedCopy })
	if got := roadblockOf(e.a.convoFor(ch, "")); got != "" {
		t.Errorf("the channel's composer is blocked too: %q", got)
	}
	svc := e.a.Core()
	if !svc.ThreadLocked(ch, fakeslack.ThreadsAuditLocked) {
		t.Fatal("core doesn't see the root's is_locked")
	}

	// useThreadLockedCopy: "on" says reactions are off too; other groups don't
	svc.SetExperimentGroup(core.ExpHideReplies, "on")
	e.until("the hide-replies copy", 5*time.Second, func() bool { return roadblockOf(tc) == lockedHideReplies })
	svc.SetExperimentGroup(core.ExpHideReplies, "off")
	e.until("the plain copy again", 5*time.Second, func() bool { return roadblockOf(tc) == lockedCopy })
	svc.ClearExperimentGroup(core.ExpHideReplies)

	// ThreadFooter checks the lock before nonMember's join pane
	if ci := e.a.convOf(ch); ci != nil {
		ci.Preview = true
		if f := e.a.footerFor(tc); f != footNone {
			t.Errorf("a locked thread in a channel the viewer isn't in shows footer %d, Slack the locked roadblock", f)
		}
		if f := e.a.footerFor(e.a.convoFor(ch, "")); f != footPreview {
			t.Errorf("the channel itself shows footer %d, want the join pane", f)
		}
		ci.Preview = false
	}

	// message_locked over the socket (perfEnv starts without realtime):
	// unlocked, the reply box is back; locked, it goes
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
	e.fs.PushThreadLocked(ch, fakeslack.ThreadsAuditLocked, false)
	e.until("unlocked", 10*time.Second, func() bool { return roadblockOf(tc) == "" })
	other := e.openAuditThread(fakeslack.ThreadsAuditRead)
	e.frame()
	if got := roadblockOf(other); got != "" {
		t.Fatalf("an unlocked thread has a roadblock: %q", got)
	}
	e.fs.PushThreadLocked(ch, fakeslack.ThreadsAuditRead, true)
	e.until("locked by message_locked", 10*time.Second, func() bool { return roadblockOf(other) == lockedCopy })
}

// The locked thread's pane, light and dark, both strings
// (THREADS_AUDIT_SHOTS_DIR: threads-audit-locked-*.png).
func TestThreadsAuditLockedShots(t *testing.T) {
	if os.Getenv("THREADS_AUDIT_SHOTS_DIR") == "" {
		t.Skip("THREADS_AUDIT_SHOTS_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := threadsAuditEnv(t, dark)
			ch := fakeslack.ThreadsAuditChannel
			e.openChannel(ch)
			tc := e.openAuditThread(fakeslack.ThreadsAuditLocked)
			e.until("the locked roadblock", 10*time.Second, func() bool { return roadblockOf(tc) == lockedCopy })
			e.settle(time.Second)
			e.threadsAuditShot("threads-audit-locked-" + mode)
			e.a.Core().SetExperimentGroup(core.ExpHideReplies, "on")
			e.until("the hide-replies copy", 5*time.Second, func() bool { return roadblockOf(tc) == lockedHideReplies })
			e.settle(time.Second)
			e.threadsAuditShot("threads-audit-locked-hide-replies-" + mode)
			e.a.Core().ClearExperimentGroup(core.ExpHideReplies)
		})
	}
}
