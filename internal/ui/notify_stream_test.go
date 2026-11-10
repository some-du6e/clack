package ui

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// defer_notification_3p_streaming (boot-render maybeShowNotification and
// messageChanged): on, an app's message still streaming doesn't notify and
// the update that settles its stream does; off or unassigned, the message
// notifies when it arrives and the settling update doesn't again.
func TestDeferStreamingNotification(t *testing.T) {
	a, ca, _ := fakeApp(t, map[string]any{"mute_sounds": true})
	rec := &recNotifier{}
	a.notify = rec
	msg := func(state string) *core.Message {
		return &core.Message{TS: "1762520099.000300", User: "U0FAKEAGENT1", Name: "Agent", StreamingState: state}
	}
	start := core.Event{Type: "message", Channel: "D0STREAM1", TS: "1762520099.000300", ConvKind: "im", Msg: msg("in_progress")}
	settle := core.Event{Type: "update", Channel: "D0STREAM1", TS: "1762520099.000300", ConvKind: "im", Msg: msg("completed"), StreamDone: true}
	plain := core.Event{Type: "update", Channel: "D0STREAM1", TS: "1762520099.000300", ConvKind: "im", Msg: msg("in_progress")}
	settleWait := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for rec.count() < n && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			a.drain()
		}
		time.Sleep(50 * time.Millisecond) // nothing more comes
		a.drain()
		if got := rec.count(); got != n {
			t.Fatalf("%d notifications, want %d", got, n)
		}
	}
	for _, c := range []struct {
		group                   string
		afterStart, afterSettle int
	}{{"on", 0, 1}, {"off", 1, 1}, {"", 1, 1}} {
		rec.mu.Lock()
		rec.n = 0
		rec.mu.Unlock()
		ca.SetExperimentGroup("defer_notification_3p_streaming", c.group)
		a.data.handle(a, start)
		settleWait(c.afterStart)
		a.data.handle(a, plain) // an append: never notifies
		settleWait(c.afterStart)
		a.data.handle(a, settle)
		settleWait(c.afterSettle)
	}
}
