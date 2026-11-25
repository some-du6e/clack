package messages

// A reply confirmed while its thread pane's load is out: the pane's first,
// or its slice reloading after a history change cleared it (both a
// thread model that hasn't loaded). Core swaps the pending copy for
// Slack's and says so ("sent"), and the load's answer, read before the
// swap, mustn't lose it. The model held neither copy and didn't reach
// Slack's (it hadn't loaded), so the swap was recorded as the pending
// copy gone and nothing else: an answer holding the pending copy lost it
// and gained nothing, and one read before the send had neither, until the
// slice was read again (up to the 30 s refresh). The swap is recorded
// whole now (noteReplace), and the answer takes Slack's copy where it
// holds the pending one or reaches it. A broadcast reply takes both lists,
// the channel's reloading too. Core's side is played as core plays it
// (send.go's putLocal, confirmPending: the store's copies, then the
// events). Synthetic data, as broadcast_panes_test.go's.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestSentAcrossAThreadLoad(t *testing.T) {
	for _, c := range []struct {
		name string
		ch   string
		// the answers read with the pending copy in the store (else before
		// the send went in)
		pendingRead bool
		// a reply also sent to the channel (thread_broadcast): the channel's
		// list holds it too, and its reload is out across the send as well
		broadcast bool
	}{
		{"the pane's answer read before the send", "C0SENTTH01", false, false},
		{"the pane's answer read with the pending copy", "C0SENTTH02", true, false},
		{"a broadcast reply, both answers read with the pending copy", "C0SENTTH03", true, true},
		{"a broadcast reply, both answers read before the send", "C0SENTTH04", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, _, _ := shotsApp(t)
			app.S.PutConv(slack.Conversation{ID: c.ch, Name: "friday-plans", IsChannel: true, IsMember: true, NumMembers: 12})
			base := time.Now().Add(-time.Hour).Unix()
			rootTS, replyTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
			root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "The plan for Friday is up"}
			root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 1, []string{"UBEN00001"}, replyTS
			reply := slack.Message{Type: "message", TS: replyTS, ThreadTS: rootTS, User: "UBEN00001", Text: "Works for me"}
			app.S.SetLatest(c.ch, []slack.Message{root}, false)
			app.S.SetThread(c.ch, rootTS, []slack.Message{root, reply})

			now := time.Now()
			localTS := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)   // the local clock's
			slackTS := fmt.Sprintf("%d.%06d", now.Unix()+1, now.Nanosecond()/1000) // Slack's
			const text, cid = "a reply across the pane's load", "5e9d0c1a-0000-4000-8000-00000000c0df"
			subtype := ""
			if c.broadcast {
				subtype = "thread_broadcast"
			}
			pending := slack.Message{Type: "message", Subtype: subtype, TS: localTS, ThreadTS: rootTS, User: fakeslack.SelfID, Text: text, ClientMsgID: cid}
			h := &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}

			var cv *ChannelView
			var crt *routed
			if c.broadcast {
				cv = NewChannelView(h, c.ch)
				crt = newRouted(cv, 900, 700)
				until(t, 10*time.Second, "the channel's list loaded", crt.frame, func() bool { return hcHas(cv.LoadedTS(), rootTS) })
			}
			// the loads out (the pane's first, and the channel's reload), each
			// held once its answer is in, as HoldLoad holds one
			want := 1
			if c.broadcast {
				want = 2
			}
			var mu sync.Mutex
			read, gate := make(chan struct{}), make(chan struct{})
			seen := map[*model]bool{}
			hold := func(m *model) {
				if m.ch != c.ch || m.win || m.threadTS != rootTS && (m.threadTS != "" || !c.broadcast) {
					return
				}
				mu.Lock()
				if !seen[m] {
					seen[m] = true
					if len(seen) == want {
						close(read)
					}
				}
				mu.Unlock()
				<-gate
			}
			loadLanded.Store(&hold)
			var released sync.Once
			release := func() {
				released.Do(func() {
					loadLanded.CompareAndSwap(&hold, nil)
					close(gate)
				})
			}
			defer release()

			// core's send (putLocal): the pending copy in the thread (and,
			// broadcast, in the channel's window), then its "message", which
			// no list hears before one is made (the pane's first load reads
			// the copy from the store)
			send := func() {
				if c.broadcast {
					app.S.UpsertLocal(c.ch, pending)
				}
				app.S.AddReplyLocal(c.ch, pending)
				pm := core.Message{TS: localTS, Channel: c.ch, ThreadTS: rootTS, Subtype: subtype, Broadcast: c.broadcast, User: fakeslack.SelfID, Text: text, Pending: true}
				if hb := hubOf(app); hb != nil {
					hb.apply(core.Event{Type: "message", Channel: c.ch, TS: localTS, Msg: &pm, EmitSeq: app.EmitSeq()})
				}
			}
			if c.pendingRead {
				send()
			}
			tv := NewThreadView(h, c.ch, rootTS)
			trt := newRouted(tv, 520, 700)
			if c.broadcast {
				hubOf(app).refresh(cv.s.m)
			}
			answered := false
			for dl := time.Now().Add(10 * time.Second); !answered && time.Now().Before(dl); {
				trt.frame()
				select {
				case <-read:
					answered = true
				case <-time.After(15 * time.Millisecond):
				}
			}
			if !answered {
				mu.Lock()
				n := len(seen)
				mu.Unlock()
				t.Fatalf("the loads' answers never came (%d of %d in)", n, want)
			}
			if !c.pendingRead {
				send()
			}
			// Slack's echo confirms it (confirmPending): the copies swapped in
			// the store, then "sent"
			app.S.Remove(c.ch, localTS)
			confirmed := pending
			confirmed.TS = slackTS
			app.S.NoteAdded(slackTS)
			if c.broadcast {
				app.S.UpsertLocal(c.ch, confirmed)
			}
			app.S.AddReplyLocal(c.ch, confirmed)
			cm := core.Message{TS: slackTS, Channel: c.ch, ThreadTS: rootTS, Subtype: subtype, Broadcast: c.broadcast, User: fakeslack.SelfID, Text: text}
			hubOf(app).apply(core.Event{Type: "sent", Channel: c.ch, TS: slackTS, ThreadTS: rootTS, ReplacesTS: localTS, Msg: &cm, Reply: true, EmitSeq: app.EmitSeq()})
			release()

			landed := func(m *model) bool {
				m.mu.Lock()
				defer m.mu.Unlock()
				return m.loaded && !m.loading
			}
			frames := func() {
				trt.frame()
				if crt != nil {
					crt.frame()
				}
			}
			until(t, 10*time.Second, "the loads landed", frames, func() bool {
				return landed(tv.s.m) && (cv == nil || landed(cv.s.m))
			})
			for k := 0; k < 5; k++ {
				frames()
			}
			check := func(list string, s *listState) {
				t.Helper()
				var got []string
				for i := range s.snap.msgs {
					got = append(got, s.snap.msgs[i].m.TS)
				}
				if !hcHas(got, slackTS) {
					t.Errorf("after its load out across the reply's send the %s doesn't hold Slack's copy of it (%s): %v", list, slackTS, got)
				}
				if hcHas(got, localTS) {
					t.Errorf("after its load out across the reply's send the %s holds the pending copy (%s), which core let go: %v", list, localTS, got)
				}
			}
			check("thread pane", tv.s)
			if cv != nil {
				check("channel's list", cv.s)
			}
		})
	}
}
