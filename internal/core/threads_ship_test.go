package core

// The threads-ship review's blockers in core (wf_15e6e96d-4f5; findings
// 2 and 5 of /root/clack-ops/next-briefs/threads-ship-findings.md): a
// thread the viewer follows opens at its last read when no window holds
// its root, and a poll with the socket away brings the pane's rows the
// edits, deletions and reactions it missed. Synthetic data only.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// shipApp is a booted app over a fake whose threads answer a page at a
// time as live Slack does, with no socket, and a channel the viewer is
// in with nothing unread (boot's prefetch reads no window of it).
func shipApp(t *testing.T) (*App, *fakeslack.Server, string) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.PagedReplies = true
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range fs.ChannelIDs()[1:] {
		if c := a.Count(id); !c.Unread && c.Mentions == 0 {
			return a, fs, id
		}
	}
	t.Fatal("no read channel in the fixture")
	return nil, nil, ""
}

// shipThread posts a synthetic thread of n replies in ch, unpushed: the
// root age old, a reply a second after it; reply(i) is reply i's ts.
func shipThread(fs *fakeslack.Server, ch string, n int, age time.Duration) (root string, reply func(int) string) {
	base := time.Now().Add(-age).Unix()
	root = fmt.Sprintf("%d.000100", base)
	us := fs.UserIDs()
	fs.PostAtQuiet(ch, root, us[1], "synthetic: a thread for the threads-ship tests")
	reply = func(i int) string { return fmt.Sprintf("%d.000100", base+int64(i)) }
	for i := 1; i <= n; i++ {
		fs.ReplyAtQuiet(ch, root, reply(i), us[1+i%3], fmt.Sprintf("synthetic reply %d", i))
	}
	return root, reply
}

// TestThreadStartTsFollowedWithoutRoot (finding 5): a pane opened with no
// reply asked for (Pins, Drafts, Activity's Open button, a file's thread,
// canvas comments) starts a thread the viewer follows at its last read,
// as Slack's withOriginalLastRead reads the subscription by the thread's
// key (boot-shared l6n6 over client-boot YFI6 getSubscriptionState),
// with no window or cached thread holding the root: the subscription the
// store knows by key, else the one a copy of the root fetched by id
// carries. The code before looked for the root in the windows and cached
// threads only, and started such a thread at its root: no "New" divider,
// and nothing marked read until the reader paged past the last read.
func TestThreadStartTsFollowedWithoutRoot(t *testing.T) {
	a, fs, ch := shipApp(t)
	ctx := context.Background()

	// known by its key alone (a thread_subscribed event, the Threads view):
	// no copy of the root anywhere
	root1, reply1 := shipThread(fs, ch, 40, 3*time.Hour)
	a.S.SetThreadSub(ch, root1, func(s *store.ThreadSub) { s.Subscribed, s.LastRead = true, reply1(30) })
	if _, held := a.S.FindHeld(ch, root1); held {
		t.Fatal("the store holds a copy of the first root: the case needs none")
	}
	if got := a.ThreadStartTs(ch, root1, "", "", false); got != reply1(30) {
		t.Errorf("a followed thread known by its subscription starts at %s, want its last read %s (the root is %s)", got, reply1(30), root1)
	}

	// a copy of the root fetched by id (Activity, Pins and Drafts hold
	// theirs so), the subscription on it
	root2, reply2 := shipThread(fs, ch, 40, 2*time.Hour)
	if !fs.FollowQuiet(ch, root2, reply2(35)) {
		t.Fatal("the fake has no second root")
	}
	if got := a.hydrate(ctx, map[string][]string{ch: {root2}}); got[ch+"/"+root2].TS != root2 {
		t.Fatalf("messages.list didn't bring the second root: %v", got)
	}
	if _, inWindow := a.S.Find(ch, root2); inWindow {
		t.Fatal("a window or cached thread holds the second root: the case needs a fetched copy only")
	}
	if got := a.ThreadStartTs(ch, root2, "", "", false); got != reply2(35) {
		t.Errorf("a followed thread whose root is a fetched copy starts at %s, want its last read %s (the root is %s)", got, reply2(35), root2)
	}

	// one the viewer doesn't follow still starts at its root
	root3, _ := shipThread(fs, ch, 5, time.Hour)
	if got := a.ThreadStartTs(ch, root3, "", "", false); got != root3 {
		t.Errorf("a thread nobody follows starts at %s, want its root %s", got, root3)
	}
}

// TestThreadPollRefreshesPaneRows (finding 2): with the socket down or
// off, a poll reads the replies the thread's pane has on screen again, a
// page of 28 at a time as loadPost reads (fetchReplies {oldest}), so an
// edit, a deletion and a reaction made meanwhile reach the pane within a
// poll, as each replies read of Slack's brings the changes in its range
// (its cached_latest_updates answer lists the changed and the deleted).
// The code before read only past the end of a thread whose history had
// reached it, so a pane opened at the root of a long thread kept an
// edited reply's old text and a deleted reply for the rest of the
// session.
func TestThreadPollRefreshesPaneRows(t *testing.T) {
	a, fs, ch := shipApp(t)
	ctx := context.Background()
	root, reply := shipThread(fs, ch, 60, 2*time.Hour)
	if _, err := a.ThreadAt(ctx, ch, root, root); err != nil { // the pane opens at the root
		t.Fatal(err)
	}
	if sl := a.ThreadSliceAt(ch, root, root); sl.ReachedEnd || len(sl.Msgs) < 20 {
		t.Fatalf("the first page: %d messages, reachedEnd %v; want the root's page of a longer thread", len(sl.Msgs), sl.ReachedEnd)
	}
	noteThreadRows(a, ch, root, reply(1), reply(20)) // what the pane draws on screen
	us := fs.UserIDs()
	if !fs.EditReplyQuiet(ch, root, reply(3), "synthetic reply 3, edited") || !fs.DeleteReplyQuiet(ch, root, reply(5)) ||
		!fs.ReactReplyQuiet(ch, root, reply(4), "eyes", us[2]) {
		t.Fatal("the fake didn't take the changes")
	}
	a.pollOnce(ctx)
	byTS := map[string]Message{}
	for _, m := range a.ThreadSliceAt(ch, root, root).Msgs {
		byTS[m.TS] = m
	}
	if m, ok := byTS[reply(3)]; !ok || m.Text != "synthetic reply 3, edited" {
		t.Errorf("after the poll reply 3 reads %q (held %v), want the edit", m.Text, ok)
	}
	if _, ok := byTS[reply(5)]; ok {
		t.Error("after the poll the pane's slice still holds reply 5, deleted meanwhile")
	}
	if m := byTS[reply(4)]; len(m.Reactions) == 0 {
		t.Error("after the poll reply 4 has no reaction, added meanwhile")
	}
	if _, ok := byTS[reply(6)]; !ok {
		t.Error("after the poll the pane's slice lost reply 6, which nobody deleted")
	}
}
