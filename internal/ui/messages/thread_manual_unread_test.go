package messages

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// threadsManuallyMarkedUnread in the thread pane (ThreadsFlexpane, eTbq):
// its checkUnreads marks a thread read only while the viewer hasn't
// marked it unread, and showing the thread (componentDidMount, a thread
// switch) lets the mark go, so the pane then marks it read.
func TestThreadsAuditManualUnreadLikeSlack(t *testing.T) {
	app, fs := auditEnv(t)
	root := fakeslack.ThreadsAuditUnread
	ch := fakeslack.ThreadsAuditChannel
	tv, host, frame := auditPane(t, app, root, false, 700)
	newest := fakeslack.ThreadsAuditReply(root, 5)
	readMark := func() bool {
		for _, m := range fs.ThreadsAuditMarks() {
			if m.TS == newest {
				return true
			}
		}
		return false
	}
	if err := app.MarkThreadUnreadFrom(context.Background(), ch, root, fakeslack.ThreadsAuditReply(root, 3), core.MarkedInThreadsView); err != nil {
		t.Fatal(err)
	}
	host.attention.Store(true)
	for end := time.Now().Add(700 * time.Millisecond); time.Now().Before(end); {
		frame()
		time.Sleep(20 * time.Millisecond)
	}
	if readMark() {
		t.Fatalf("the pane marked the thread read while the viewer had it marked unread: %v", fs.ThreadsAuditMarks())
	}
	tv.Reopen()
	if o := app.ThreadMarkedUnread(ch, root); o != "" {
		t.Errorf("the pane showed the thread again and left it marked unread (%s)", o)
	}
	until(t, 10*time.Second, "the read mark once the pane showed the thread", frame, readMark)
}
