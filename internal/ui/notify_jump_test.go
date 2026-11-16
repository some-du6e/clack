package ui

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// A notification's click lands on its message (Karim, α.89 macOS: "the
// thing to hop to the desired message when clicking a notif doesn't
// work"): the webapp's notificationClicked is jumpToMessage({channelId,
// ts, threadTs}), so a message's conversation opens started at the
// message with it highlighted (toMessage: startTs, highlightTs), and a
// reply's thread opens at the reply, highlighted, with the root
// highlighted in the conversation beside it (toThread's replyTs and
// highlightRoot; jumpToThreadReply). The click goes through the macOS
// notifier's routing (notify_mac.go, a fake Notification Center) from
// another conversation. Synthetic: fakeslack's thread header fixtures.

// channelView is the conversation's message list.
func (e *perfEnv) channelView(ch string) *messages.ChannelView {
	if c := e.a.convos[ch+"/"]; c != nil {
		if cv, ok := c.list.(channelList); ok {
			return cv.ChannelView
		}
	}
	return nil
}

func TestNotificationClickJumpsToMessage(t *testing.T) {
	e := threadHeaderEnv(t, false)
	a := e.a
	cases := e.fs.ThreadHeaderCases()
	pub, other := cases[0].Channel, cases[1].Channel
	root, reply := fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	fc := newFakeCenter()
	a.notify = newMacNotifier(fc)
	shown := 0
	click := func(ts, thread string) {
		t.Helper()
		n := a.messageNote(pub, ts, thread)
		n.Title, n.Body = "New message in #th-launch-notes", "Ship it"
		_ = a.notify.(noteNotifier).NotifyNote(n)
		shown++
		u := fc.waitShown(t, a, shown)
		fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "click", Index: -1})
	}
	elsewhere := func() {
		t.Helper()
		a.Navigate("c/" + other)
		e.until("the other conversation", 10*time.Second, func() bool { return a.router.Current() == Route("c/"+other) })
		e.settle(300 * time.Millisecond)
	}

	// a message: its conversation at the message, highlighted
	elsewhere()
	click(root, "")
	if r := a.router.Current(); r != Route("c/"+pub+"/m/"+root) {
		t.Fatalf("a click on a message's notification opened %q, want the message (c/%s/m/%s)", r, pub, root)
	}
	e.until("the message placed and highlighted", 10*time.Second, func() bool {
		cv := e.channelView(pub)
		return cv != nil && cv.JumpPending() == "" && cv.Highlighted() == root
	})

	// a reply: its thread at the reply, highlighted; the root highlighted
	// in the conversation
	elsewhere()
	click(reply, root)
	if r := a.router.Current(); r != Route("c/"+pub+"/t/"+root) {
		t.Fatalf("a click on a reply's notification opened %q, want its thread", r)
	}
	if s := a.secondary; s.kind != secThread || s.channel != pub || s.ts != root || s.at != reply {
		t.Fatalf("the thread pane: %v %s/%s at %q, want the thread at the reply", s.kind, s.channel, s.ts, s.at)
	}
	e.until("the reply placed and highlighted", 10*time.Second, func() bool {
		tv := e.threadView(pub, root)
		return tv != nil && tv.JumpPending() == "" && tv.Highlighted() == reply
	})
	if cv := e.channelView(pub); cv == nil || cv.Highlighted() != root {
		t.Errorf("the conversation beside the thread doesn't highlight the root (highlightRoot)")
	}

	// a notification id alone (no channel in the payload): the id's route
	a.Navigate("c/" + other)
	e.frame()
	a.notificationClicked("", "", "", buildNotificationID(storeTeam(a.Core()), noteTypeMessage, messageNotificationPayload(pub, reply, root)...))
	if s := a.secondary; a.router.Current() != Route("c/"+pub+"/t/"+root) || s.kind != secThread || s.at != reply {
		t.Errorf("the id's route opened %q (thread pane at %q), want the thread at the reply", a.router.Current(), s.at)
	}
}
