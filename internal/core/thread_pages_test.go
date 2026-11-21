package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// hugeApp boots core against the fake with a thread of n synthetic
// replies (fakeslack huge_thread.go).
func hugeApp(t *testing.T, n int) (*App, *fakeslack.Server, fakeslack.HugeThread) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	th := fs.AddHugeThread("largest-thread-fixture", n, 233)
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.History(context.Background(), th.Channel, ""); err != nil { // the channel's copy of the root
		t.Fatal(err)
	}
	return a, fs, th
}

// repliesReq is what of a conversations.replies request the client
// decides: its bounds, its limit, whether it is inclusive, its cursor.
func repliesReq(v url.Values) string {
	var b strings.Builder
	for _, k := range []string{"oldest", "latest", "limit", "inclusive", "cursor"} {
		if x := v.Get(k); x != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(k + "=" + x)
		}
	}
	return b.String()
}

// TestThreadOpensLikeSlack: opening a thread of 1,078,423 replies reads
// what Slack's thread pane reads (requestReplies, gOq3, build
// 35b42de8b870): from the root, one conversations.replies {oldest: root,
// inclusive: true, limit: 28}, the root and its first 28 replies (the
// server counts only replies: live Slack answers 29 messages), the start
// reached and the end not; at a reply in the middle, two at once
// ({latest: reply} and {oldest: reply}, 28 each) and a slice of 55 that
// reaches neither end and leaves the root out (the store keeps its copy).
// loadPre and loadPost page from the slice's ends, and the pages merge
// into it. Nothing reads the whole thread.
func TestThreadOpensLikeSlack(t *testing.T) {
	const n = 1078423
	a, fs, th := hugeApp(t, n)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, root := th.Channel, th.Root

	if got := a.ThreadStartTs(ch, root, "", "", false); got != root {
		t.Fatalf("an unfollowed thread starts at %q, want the root", got)
	}
	sl, err := a.ThreadAt(ctx, ch, root, root)
	if err != nil {
		t.Fatal(err)
	}
	reqs := fs.HugeThreadRequests()
	if len(reqs) != 1 || repliesReq(reqs[0]) != "oldest="+root+" limit=28 inclusive=true" {
		t.Fatalf("the open from the root asked %d: %v", len(reqs), reqs)
	}
	if len(sl.Msgs) != 29 || sl.Msgs[0].TS != root || sl.Msgs[1].TS != fs.HugeThreadReplyTS(1) || sl.Msgs[28].TS != fs.HugeThreadReplyTS(28) {
		t.Fatalf("the slice from the root: %d messages", len(sl.Msgs))
	}
	if !sl.ReachedStart || sl.ReachedEnd {
		t.Errorf("from the root: reachedStart %v reachedEnd %v, want true false", sl.ReachedStart, sl.ReachedEnd)
	}
	if sl.Root == nil || sl.Root.ReplyCount != n {
		t.Errorf("rootMsg: %+v", sl.Root)
	}
	// held now: the same open reads nothing
	if _, err := a.ThreadAt(ctx, ch, root, root); err != nil || len(fs.HugeThreadRequests()) != 1 {
		t.Errorf("a held slice was read again (%d requests, %v)", len(fs.HugeThreadRequests()), err)
	}
	// loadPost: the page after the slice's end, merged into it
	if err := a.ThreadNewer(ctx, ch, root, sl.End); err != nil {
		t.Fatal(err)
	}
	reqs = fs.HugeThreadRequests()
	if len(reqs) != 2 || repliesReq(reqs[1]) != "oldest="+fs.HugeThreadReplyTS(28)+" limit=28 inclusive=true" {
		t.Fatalf("loadPost asked %v", reqs[1:])
	}
	if sl = a.ThreadSliceAt(ch, root, root); len(sl.Msgs) != 56 || sl.Msgs[55].TS != fs.HugeThreadReplyTS(55) {
		t.Fatalf("after loadPost the slice has %d messages", len(sl.Msgs))
	}

	// a reply in the middle: two reads around it
	mid := n / 2
	x := fs.HugeThreadReplyTS(mid)
	before := len(fs.HugeThreadRequests())
	sl, err = a.ThreadAt(ctx, ch, root, x)
	if err != nil {
		t.Fatal(err)
	}
	reqs = fs.HugeThreadRequests()[before:]
	got := map[string]bool{}
	for _, r := range reqs {
		got[repliesReq(r)] = true
	}
	if len(reqs) != 2 || !got["latest="+x+" limit=28 inclusive=true"] || !got["oldest="+x+" limit=28 inclusive=true"] {
		t.Fatalf("the open at reply %d asked %v", mid, reqs)
	}
	if len(sl.Msgs) != 55 || sl.Msgs[0].TS != fs.HugeThreadReplyTS(mid-27) || sl.Msgs[54].TS != fs.HugeThreadReplyTS(mid+27) {
		t.Fatalf("the slice around reply %d: %d messages", mid, len(sl.Msgs))
	}
	if sl.ReachedStart || sl.ReachedEnd || sl.Root == nil {
		t.Errorf("around a reply: reachedStart %v reachedEnd %v, rootMsg %v", sl.ReachedStart, sl.ReachedEnd, sl.Root != nil)
	}
	// loadPre: the page before it
	if err := a.ThreadOlder(ctx, ch, root, sl.Start); err != nil {
		t.Fatal(err)
	}
	reqs = fs.HugeThreadRequests()
	if r := reqs[len(reqs)-1]; repliesReq(r) != "latest="+fs.HugeThreadReplyTS(mid-27)+" limit=28 inclusive=true" {
		t.Fatalf("loadPre asked %v", r)
	}
	if sl = a.ThreadSliceAt(ch, root, x); len(sl.Msgs) != 82 || sl.Msgs[0].TS != fs.HugeThreadReplyTS(mid-54) {
		t.Fatalf("after loadPre the slice has %d messages, from %q", len(sl.Msgs), sl.Msgs[0].TS)
	}
	// the slice from the root is still its own
	if s0 := a.ThreadSliceAt(ch, root, root); len(s0.Msgs) != 56 {
		t.Errorf("the root's slice: %d messages", len(s0.Msgs))
	}
	if all := len(fs.HugeThreadRequests()); all != 5 {
		t.Errorf("%d replies reads in all, want 5 (the root's page, loadPost, two around the reply, loadPre)", all)
	}
	// the messages store holds only what was read: the root and 55+82 replies
	if held, _ := a.S.Thread(ch, root); len(held) != 1+55+82 {
		t.Errorf("the store holds %d of the thread", len(held))
	}
	// closing the pane in a public channel the viewer isn't in lets it go
	// (conversations.info names it, which the history's side read asks)
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, ok := a.Conv(ch); ok && c.Kind == "channel" && !c.Member {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("core never knew the channel as a public one the viewer isn't in")
		}
	}
	if !a.CloseThread(ch, root) || a.S.ThreadHeld(ch, root) {
		t.Error("the closed thread's history stayed")
	}
}

// TestThreadNewestPageLikeSlack: with threads_perf_quality on, a pane
// that starts at the root's latest_reply (a followed thread read to its
// end) reads the newest page alone, {latest: latest_reply}, and the slice
// reaches the end.
func TestThreadNewestPageLikeSlack(t *testing.T) {
	a, fs, th := hugeApp(t, 5000)
	a.SetExperimentGroup("threads_perf_quality", "on")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sl, err := a.ThreadAt(ctx, th.Channel, th.Root, th.Latest)
	if err != nil {
		t.Fatal(err)
	}
	reqs := fs.HugeThreadRequests()
	if len(reqs) != 1 || repliesReq(reqs[0]) != "latest="+th.Latest+" limit=28 inclusive=true" {
		t.Fatalf("the newest page asked %v", reqs)
	}
	if len(sl.Msgs) != 28 || sl.Msgs[27].TS != th.Latest || !sl.ReachedEnd || sl.ReachedStart {
		t.Fatalf("the newest page: %d messages, reachedEnd %v reachedStart %v", len(sl.Msgs), sl.ReachedEnd, sl.ReachedStart)
	}
}
