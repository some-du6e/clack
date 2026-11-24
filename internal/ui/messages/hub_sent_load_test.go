package messages

// A send confirmed while a reload of its conversation's list is out:
// core swaps the pending copy for Slack's and says so ("sent"), and the
// reload's answer, read before the swap, mustn't undo it. Before the swap
// was kept for the answer (hub.apply's "sent", noteLive) the list took
// the answer as it was: with neither copy, or with the pending copy core
// had let go, until the next reload. A member lookup's answer ("names")
// reloads the open channel, so on TestMessageLatency's busy socket (and
// TestTautMessageLatency's) the second send's row drew 0.73 s late, at
// the next lookup's reload, its echo with it. Core's side is played here
// as core plays it (the store's copies, then the events). Synthetic
// history on the fake.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestSentAcrossALoad(t *testing.T) {
	for _, c := range []struct {
		name string
		ch   string
		// the reload's answer read with the pending copy in core's window
		// (else before the send went in)
		pendingRead bool
	}{
		{"the answer read before the send", "C0SENTLD01", false},
		{"the answer read with the pending copy", "C0SENTLD02", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _ := guardEnv(t, c.ch, nil, nil)
			app := e.h.app
			now := time.Now()
			localTS := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)   // the local clock's
			slackTS := fmt.Sprintf("%d.%06d", now.Unix()+1, now.Nanosecond()/1000) // Slack's
			const text, cid = "a send across a reload", "5e9d0c1a-0000-4000-8000-00000000c0de"
			pending := slack.Message{Type: "message", TS: localTS, User: fakeslack.SelfID, Text: text, ClientMsgID: cid}
			// core's send: the pending copy in the window, then its "message"
			send := func() {
				app.S.UpsertLocal(c.ch, pending)
				pm := core.Message{TS: localTS, Channel: c.ch, User: fakeslack.SelfID, Text: text, Pending: true}
				e.v.s.hb.apply(core.Event{Type: "message", Channel: c.ch, TS: localTS, Msg: &pm, EmitSeq: app.EmitSeq()})
			}
			if c.pendingRead {
				send()
			}
			// a reload out, held once its answer (core's window as it is now)
			// is in, as HoldLoad holds one, saying when it is
			read, gate := make(chan struct{}), make(chan struct{})
			var once sync.Once
			hold := func(m *model) {
				if m.ch == c.ch && m.threadTS == "" && !m.win {
					once.Do(func() { close(read) })
					<-gate
				}
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
			e.v.s.hb.refresh(e.v.s.m)
			select {
			case <-read:
			case <-time.After(10 * time.Second):
				t.Fatalf("the reload's answer never came: %s", e.state())
			}
			if !c.pendingRead {
				send()
			}
			// Slack's echo confirms it (confirmPending): the copies swapped in
			// the window, then "sent"
			app.S.Remove(c.ch, localTS)
			confirmed := pending
			confirmed.TS = slackTS
			app.S.UpsertLocal(c.ch, confirmed)
			cm := core.Message{TS: slackTS, Channel: c.ch, User: fakeslack.SelfID, Text: text}
			e.v.s.hb.apply(core.Event{Type: "sent", Channel: c.ch, TS: slackTS, ReplacesTS: localTS, Msg: &cm, EmitSeq: app.EmitSeq()})
			release()
			e.await(t, "the reload landed", func() bool { return !e.hcLoading() })
			e.frames(100 * time.Millisecond)
			got := e.v.LoadedTS()
			if !hcHas(got, slackTS) {
				t.Errorf("after the reload out across the send the list doesn't hold Slack's copy of it (%s)", slackTS)
			}
			if hcHas(got, localTS) {
				t.Errorf("after the reload out across the send the list holds its pending copy (%s), which core let go", localTS)
			}
			if !t.Failed() {
				e.await(t, "the confirmed send drawn", func() bool { return e.drawn(slackTS) })
			}
		})
	}
}
