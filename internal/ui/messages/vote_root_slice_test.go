package messages

// Karim's setup (11-15, 11-17), as the round-5 re-review traced it:
// ShowRealUser patches the messages slice and leaves the vote bot's
// message as it is, so the vote message's channel row draws its source.
// Karim opens its thread within a JS round trip of a vote. The thread
// pane's root, a new row, took JS's copy of the revision before on its
// first read (a vote fewer than the channel's), and its blocks went into
// the message's one Block Kit host: the channel row's buttons found no
// element and went, and at each later vote one pane's buttons went for a
// JS round trip while the panes disagreed on the count. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestVoteRootInBothPanesUnderASlicePatch(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: voteChan, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
	app.S.PutBot(voteBotID, store.BotInfo{Name: "Vote Bot", Icon: shotAvatar(voteBotID)})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS, replyTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	// root is the vote message at revision n (n-1 upvotes), the root of a
	// thread with one reply; made once per revision (its edit's time is
	// the clock's)
	revs := map[int]slack.Message{}
	root := func(n int) slack.Message {
		if m, ok := revs[n]; ok {
			return m
		}
		m := voteMsg(rootTS, rootTS, n, n-1, 0)
		m.ReplyCount, m.ReplyUsers, m.LatestReply = 1, []string{"UBEN00001"}, replyTS
		revs[n] = m
		return m
	}
	reply := slack.Message{Type: "message", TS: replyTS, ThreadTS: rootTS, User: "UBEN00001", Text: "voted", Blocks: shotRich(shotSection("voted"))}
	app.S.SetLatest(voteChan, []slack.Message{root(1)}, false)
	app.S.SetThread(voteChan, rootTS, []slack.Message{root(1), reply})
	patchedEnv(t, app, unchangedPatchJS)
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })

	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, voteChan)
	cv.Preview = true
	now := time.Now()
	chPane := &votePane{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now}
	panes := []*votePane{chPane}
	all := func() {
		for _, p := range panes {
			p.frame()
		}
	}
	// draws reports whether p's row of the vote message draws revision n's
	// two buttons
	draws := func(p *votePane, n int) bool {
		_, _, ok := p.buttons(rootTS, fmt.Sprintf("u%d", n), fmt.Sprintf("d%d", n))
		return ok
	}
	until(t, 10*time.Second, "the channel row's buttons", all, func() bool { return draws(chPane, 1) })
	for i := 0; i < 30; i++ { // its view read, and settled
		all()
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := tautOf().MessageView(voteChan, rootTS); !ok {
		t.Fatal("the vote message has no view: the test reads none")
	}

	// a vote while JS is busy: the channel row draws its new source
	rt, err := tautOf().JS()
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	rt.Post(func(*jsrt.Ctx) { <-release })
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	voteChanged(app, root(2))
	until(t, 5*time.Second, "the channel row on revision 2", all, func() bool { return draws(chPane, 2) })

	// Karim opens the thread now: its root's entry in taut's views is
	// still JS's copy of revision 1
	tv := NewThreadView(h, voteChan, rootTS)
	thPane := &votePane{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now}
	panes = append(panes, thPane)

	var bad []string
	note := func(s string) {
		if len(bad) < 12 {
			bad = append(bad, s)
		}
	}
	// every frame from the pane's first drawn root on: both panes draw
	// revision n's buttons
	check := func(what string, n, frames int) {
		t.Helper()
		started := false
		for i := 0; i < frames; i++ {
			all()
			if !started {
				if thPane.s.rows[rootTS] == nil || itemHeight(thPane.s, rootTS) <= 0 {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				started = true
			}
			for _, p := range panes {
				if !draws(p, n) {
					r := p.s.rows[rootTS]
					txt, viewed := "", false
					if r != nil {
						txt, viewed = rowText(r), r.viewed
					}
					note(fmt.Sprintf("%s, frame %d: the %s pane doesn't draw revision %d's buttons (viewed %v, %q)", what, i, p.name, n, viewed, txt))
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !started {
			t.Fatalf("%s: the thread pane never drew its root", what)
		}
	}
	check("the thread opened while JS is held", 2, 60)

	// JS answers: the root's view of revision 2 draws as its source
	close(release)
	released = true
	until(t, 10*time.Second, "JS's view of revision 2 in both panes", all, func() bool {
		mv, ok := tautOf().MessageView(voteChan, rootTS)
		return ok && mv.EditedTS == root(2).Edited.TS
	})
	for i := 0; i < 30; i++ {
		all()
		time.Sleep(5 * time.Millisecond)
	}
	for _, p := range panes {
		if r := p.s.rows[rootTS]; r == nil || r.viewed {
			t.Errorf("the %s pane's root draws the plugins' view once it is ready; the patch leaves the message as it is", p.name)
		}
	}

	// votes with JS free-running: one event updates both panes' models,
	// and from the frame both draw it they stay on it
	for n := 3; n <= 5; n++ {
		voteChanged(app, root(n))
		until(t, 5*time.Second, fmt.Sprintf("revision %d in both panes", n), all, func() bool { return draws(chPane, n) && draws(thPane, n) })
		check(fmt.Sprintf("after vote %d", n), n, 40)
	}
	for _, b := range bad {
		t.Error(b)
	}
}
