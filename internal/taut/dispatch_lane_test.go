package taut

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

// A dispatch queued behind a busy runtime keeps its native until its JS
// has run, whatever the caller's deadline: a notification that waited
// past its 30 s context behind the backlog never showed.
func TestDispatchSeqOutlivesItsContext(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.redux.patchThunk('showNotification', (orig) => (...a) => orig(...a)); return 1`)
	eventually(t, 2*time.Second, "showNotification patched", func() bool { return h.tt.ThunkPatched("showNotification") })
	r := h.rt()
	for i := 0; i < 60; i++ {
		r.rt.Post(func(*jsrt.Ctx) { time.Sleep(10 * time.Millisecond) })
	}
	ran := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	DispatchSeq(ctx, h.tt, "showNotification", map[string]any{"title": "a DM"}, func(context.Context, map[string]any) (any, error) {
		ran <- struct{}{}
		return nil, nil
	}, nil)
	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Error("the native never ran: its dispatch's context ended while the JS was behind")
	}
}

// The send checks' grace (a check an interrupted task left pending
// forever) covers the checks only: a patched send that takes its time
// after them (an Owl reply waiting for its modal) isn't dispatched a
// second time.
func TestCheckThenDispatchGraceCoversTheChecksOnly(t *testing.T) {
	defer func(g time.Duration) { sendCheckGrace = g }(sendCheckGrace)
	sendCheckGrace = 2 * time.Second
	h := newHarness(t)
	// a task the watchdog interrupted earlier in the session
	h.js(`setTimeout(() => { for (;;) {} }, 0); return 1`)
	r := h.rt()
	eventually(t, 30*time.Second, "an interrupted task", func() bool { return r.rt.Interrupts() > 0 })
	h.js(`globalThis.__runs = 0
		api.composer.addSendCheck(() => true)
		globalThis.__un = api.redux.patchThunk('prepareAndSendMessage', (orig) => (...a) => async () => {
			globalThis.__runs++
			await new Promise((r) => setTimeout(r, 4500))
			return 'sent as Owl'
		})
		return 1`)
	eventually(t, 2*time.Second, "the check and the patch", func() bool {
		return h.tt.HasSendChecks() && h.tt.ThunkPatched("prepareAndSendMessage")
	})
	var natives atomic.Int32
	done := make(chan bool, 1)
	CheckThenDispatch(context.Background(), h.tt, ComposerContext{Kind: "channel", ChannelID: chGen}, "prepareAndSendMessage",
		map[string]any{"channelId": chGen, "text": "anonymously"},
		func(context.Context, map[string]any) (any, error) { natives.Add(1); return nil, nil },
		func(sent bool, _ any, err error) { done <- sent && err == nil })
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the send never finished")
	}
	raw := h.js(`return globalThis.__runs`)
	var runs int
	_ = json.Unmarshal(raw, &runs)
	if runs != 1 {
		t.Errorf("the patched send ran %d times, want once: the grace dispatched it again while it was running", runs)
	}
	if n := natives.Load(); n != 0 {
		t.Errorf("the native ran %d times; the patch never calls it", n)
	}
}
