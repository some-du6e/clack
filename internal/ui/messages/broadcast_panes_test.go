package messages

// A reply also sent to the channel (subtype thread_broadcast) is drawn by
// the channel's list and by its thread pane. Slack draws the two copies
// in two React subtrees, each with components of its own and its own
// props: BaseMessageList renders MessageWrapper → Message (boot-shared
// K+Na, EXGU), and ThreadsFlexpane renders ThreadReply →
// ThreadReplyGeneric (modern.vendor); both draw MessageBackground under
// them. Clack keyed a patched component's root by the message alone, so
// the two panes shared one: their props differ (viewContext, the thread
// pane's $pane), each frame sent JS the other pane's, and the root
// rendered again every frame with nothing changing. Each list is now a
// slackc pane with roots of its own: the channel and the thread, and each
// of them again in the split view. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"image"
	"slices"
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

	// MessageBackground: what both of Slack's panes draw for the reply
	// (Clack's thread rows also draw MessageWrapper, which Slack's thread
	// pane doesn't: see docs/PLUGINS-taut.md)
	patch := []string{"MessageBackground"}
	renders, step := patchedEnv(t, app, passThroughJS(patch), patch...)
	if !lockstep {
		step = nil
	}
	h := coreHost{app: app, th: theme.New(false)}
	cv, sv := NewChannelView(h, ch), NewChannelViewIn(h, ch, "split")
	cv.Preview, sv.Preview = true, true
	tv, stv := NewThreadView(h, ch, rootTS), NewThreadViewIn(h, ch, rootTS, "split")
	now := time.Now()
	panes := []*votePane{
		{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now, step: step},
		{name: "channel in the split view", s: sv.s, v: sv, size: image.Pt(1100, 700), now: &now, step: step},
		{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now, step: step},
		{name: "thread in the split view", s: stv.s, v: stv, size: image.Pt(520, 700), now: &now, step: step},
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
	// the reply drawn in every list, and every MessageBackground root
	// drawing it has its plugin tree
	until(t, 10*time.Second, "the reply drawn in every list through the plugin's tree", all, func() bool {
		for _, p := range panes {
			if p.s.rows[replyTS] == nil || itemHeight(p.s, replyTS) <= 0 {
				return false
			}
		}
		trees := slackc.KeyedTrees(replyTS)
		return strings.Contains(trees, "MessageBackground: ") && !strings.Contains(trees, "no tree yet")
	})
	settle(30)

	// nothing changes: no render of the reply's MessageBackground may reach JS
	mark := renders.mark()
	settle(60)
	n := 0
	for _, r := range renders.since(mark) {
		if r.name == "MessageBackground" && r.ts == replyTS {
			n++
		}
	}
	if n > 0 {
		t.Errorf("idle for 60 frames, the reply in four lists sent JS %d renders of its MessageBackground (%s); want 0", n, slackc.KeyedTrees(replyTS))
	}

	// a root per list, each with its own list's props
	views := map[uint64]string{}
	for _, r := range renders.since(0) {
		if r.name == "MessageBackground" && r.ts == replyTS {
			views[r.root] = r.view
		}
	}
	got := map[string]int{}
	for _, v := range views {
		got[v]++
	}
	if len(views) != 4 || got["message_pane"] != 2 || got["threads_flexpane"] != 2 {
		t.Errorf("the reply's MessageBackground rendered into %d roots with views %v; want one root per list: two message_pane, two threads_flexpane", len(views), views)
	}
	// each in its own list's pane (dropping the channel's or the thread's
	// slackc.Pane call shares one root between that kind's two lists)
	want := []string{cv.PaneID(), sv.PaneID(), tv.PaneID(), stv.PaneID()}
	slices.Sort(want)
	if got := slackc.KeyedPanes("MessageBackground", replyTS); !slices.Equal(got, want) {
		t.Errorf("the reply's MessageBackground instances are in panes %q; want one in each list's: %q", got, want)
	}
}
