package messages

// A reply also sent to the channel (subtype thread_broadcast) is drawn by
// the channel's list and by its thread pane. Slack draws the two copies as
// two components in two React subtrees, each with its own props:
// BaseMessageList renders MessageWrapper keyed by the ts (boot-shared
// K+Na), and ThreadsFlexpane renders ThreadReply → ThreadReplyGeneric
// (modern.vendor). Clack drew both through one MessageWrapper root keyed by
// the message alone. The two panes' props differ (viewContext, the thread
// pane's $pane), so each frame sent JS the other pane's props and the root
// rendered again, every frame, with nothing changing. Each list is now a
// slackc pane with roots of its own. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestBroadcastReplyInBothPanesIdle(t *testing.T) {
	t.Run("free-running", func(t *testing.T) { checkBroadcastPanes(t, false) })
	// lockstep: JS answers every render before the next frame
	t.Run("lockstep", func(t *testing.T) { checkBroadcastPanes(t, true) })
}

func checkBroadcastPanes(t *testing.T, lockstep bool) {
	app, _, _ := shotsApp(t)
	const ch = "C0BCAST001"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS, replyTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "Launch checklist is up",
		Blocks: shotRich(shotSection("Launch checklist is up"))}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 1, []string{"UBEN00001"}, replyTS
	reply := slack.Message{Type: "message", Subtype: "thread_broadcast", TS: replyTS, ThreadTS: rootTS, User: "UBEN00001",
		Text: "Signed off on the copy, shipping at noon", Blocks: shotRich(shotSection("Signed off on the copy, shipping at noon"))}
	app.S.SetLatest(ch, []slack.Message{root, reply}, false)
	app.S.SetThread(ch, rootTS, []slack.Message{root, reply})

	patch := []string{"MessageWrapper"}
	renders, step := patchedEnv(t, app, passThroughJS(patch), patch...)
	if !lockstep {
		step = nil
	}
	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, ch)
	cv.Preview = true
	tv := NewThreadView(h, ch, rootTS)
	now := time.Now()
	panes := []*votePane{
		{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now, step: step},
		{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now, step: step},
	}
	all := func() {
		for _, p := range panes {
			p.frame()
		}
	}
	settle := func(n int) {
		for i := 0; i < n; i++ {
			all()
			time.Sleep(10 * time.Millisecond)
		}
	}
	// the reply drawn in both panes, and every MessageWrapper root drawing
	// it has its plugin tree
	until(t, 10*time.Second, "the reply drawn in both panes through the plugin's tree", all, func() bool {
		for _, p := range panes {
			if p.s.rows[replyTS] == nil || itemHeight(p.s, replyTS) <= 0 {
				return false
			}
		}
		trees := slackc.KeyedTrees(replyTS)
		return strings.Contains(trees, "MessageWrapper: ") && !strings.Contains(trees, "no tree yet")
	})
	settle(30)

	// nothing changes: no render of the reply's MessageWrapper may reach JS
	mark := renders.mark()
	settle(60)
	n := 0
	for _, r := range renders.since(mark) {
		if r.name == "MessageWrapper" && r.ts == replyTS {
			n++
		}
	}
	if n > 0 {
		t.Errorf("idle for 60 frames, the reply in both panes sent JS %d renders of its MessageWrapper (%s); want 0", n, slackc.KeyedTrees(replyTS))
	}

	// a root per pane, each with its own pane's props
	views := map[uint64]string{}
	for _, r := range renders.since(0) {
		if r.name == "MessageWrapper" && r.ts == replyTS {
			views[r.root] = r.view
		}
	}
	got := map[string]int{}
	for _, v := range views {
		got[v]++
	}
	if len(views) != 2 || got["message_pane"] != 1 || got["threads_flexpane"] != 1 {
		t.Errorf("the reply's MessageWrapper rendered into %d roots with views %v; want one root per pane, message_pane and threads_flexpane", len(views), views)
	}
	if trees := slackc.KeyedTrees(replyTS); strings.Count(trees, "MessageWrapper: ") != 2 {
		t.Errorf("the reply's MessageWrapper instances: %s; want one per pane", trees)
	}
}
