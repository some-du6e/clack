package messages

// A message row's rich text allows check lists' checkboxes: RichTextList
// draws them where s3Wf's sS allows (a message, a thread, a Slackbot
// message), and every row is one of those. Without the option the row's
// check lists would turn back into bullets (richview TestCheckListLayout
// holds the drawing, TestCheckListInAttachments the attachments').

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestRowAllowsCheckLists(t *testing.T) {
	host := coreHost{th: theme.New(false)}
	e := entry{rev: 1, m: core.Message{TS: "1762520000.000100", Channel: "C0CHECKS", User: "U0ANA0001"}}
	r := newRow(&e)
	r.rc = &rowCtx{h: host, selfID: "USELF"}
	if !r.rvOpts().CheckLists {
		t.Error("a message row's rich text doesn't allow check lists' checkboxes")
	}
}
