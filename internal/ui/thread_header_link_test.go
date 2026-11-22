package ui

// Karim (2026-11-22, α.136 on Windows, a thread opened from Activity):
// "i cant click up there?" on the header's "Thread  # largest-thread-on-
// slack". Slack's threads Header (client-boot-shared O+gL) wraps the name
// in a Link whose href is the root's permalink (mapStateToProps:
// getMessage(channel, threadTs), the messages store's copy of the root,
// wherever the pane's slice of replies is, then getPermalinkForMessage),
// so the name is a link as soon as the root is known; a click opens the
// channel at the root. Clack linked it only while the root was in the
// slice the pane drew, and a thread opened at a reply far from its root
// (a mention in the middle of Karim's 1,078,423-reply thread) never has
// it there. Synthetic: fakeslack's huge thread.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestThreadHeaderLinkAwayFromRoot(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	th := fs.AddHugeThread("largest-thread-fixture", 100000, 40)
	ch, root := th.Channel, th.Root
	at := fs.HugeThreadReplyTS(50000)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.a.Navigate("activity")
	e.settle(600 * time.Millisecond)
	e.a.OpenThreadAt(ch, root, at)
	e.until("the reply drawn in the pane, the header naming the channel", 20*time.Second, func() bool {
		tv := e.threadView(ch, root)
		if tv == nil || !paneHas(tv, at) {
			return false
		}
		_, _, ok := tv.HeaderName()
		return ok
	})
	e.settle(300 * time.Millisecond)
	tv := e.threadView(ch, root)
	if paneHas(tv, root) {
		t.Fatal("the pane drew the root: the slice isn't away from it, so this proves nothing")
	}
	sub, r, _ := tv.HeaderName()
	if !sub.Link {
		t.Fatalf("the header's %q isn't a link with the pane's slice away from the root (Slack's Header links the root's permalink from the store's copy)", sub.Text)
	}
	e.clickAt(r.Min.Add(r.Size().Div(2)), time.Second)
	want := Route("c/" + ch + "/m/" + root)
	e.until("the click opens the channel at the root", 5*time.Second, func() bool { return e.a.router.Current() == want })
	if s := e.a.secondary; s.kind != secThread || s.channel != ch || s.ts != root {
		t.Errorf("the thread pane didn't stay open (secondary %v %s/%s)", s.kind, s.channel, s.ts)
	}
}
