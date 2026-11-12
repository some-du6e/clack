package ui

// Mod+J in the thread pane (docs/THREADS-AUDIT.md "j"): ThreadsFlexpane's
// onKeyDown takes j with the desktop modifier while the key focus is in
// the pane, and jumps to the first unread reply or announces "No unread
// messages". On internal/fakeslack's #th-audit (synthetic).

import (
	"reflect"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

func TestThreadsAuditModJ(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditLong // not followed: nothing unread to jump to
	e.openChannel(ch)
	e.a.OpenThread(ch, root)
	e.until("the thread", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && tv.ContentHeight() >= 0 && len(tv.Channel()) > 0
	})
	tv := e.threadView(ch, root)
	// the key focus outside the pane: Mod+J isn't the thread's
	e.key("J", shortcut.Mod())
	if got := tv.Announced(); len(got) != 0 {
		t.Fatalf("Mod+J outside the pane announced %q", got)
	}
	e.a.FocusThreadComposer(ch, root)
	e.settle(300 * time.Millisecond)
	e.key("J", shortcut.Mod())
	e.until("the announcement", 5*time.Second, func() bool { return len(tv.Announced()) > 0 })
	if got := tv.Announced(); !reflect.DeepEqual(got, []string{"No unread messages"}) {
		t.Errorf("announced %q, want \"No unread messages\"", got)
	}
}
