package ui

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/messages"
)

// sharedJumpLanded checks TestSharedMessageOpensItsReply's jump past its
// route: the conversation flashes the thread's root (highlightRoot), and
// the pane draws the linked reply unflashed (the flexpane's sync: the
// primary already showed the reply's channel). The rows' Highlight is
// this branch's; a red commit before it stubs this file.
func sharedJumpLanded(t *testing.T, e *perfEnv, ch, root, reply string) {
	t.Helper()
	e.waitDrawn(t, "the thread's root flashing in the conversation", 10*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, root) })
	e.waitDrawn(t, "the linked reply in the pane", 10*time.Second, "", ch, root, func(rows []messages.SenderView) bool { return drewRow(rows, reply) })
	if lit(e.paneRows("", ch, root), reply) {
		t.Error("the linked reply flashed in the pane: the primary already showed its channel, so the root flashes in the conversation instead")
	}
}
