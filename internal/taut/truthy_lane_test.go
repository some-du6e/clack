package taut

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

// A message menu's pick (CallTruthy: Slick's onSelectAction) is the user's
// input: it runs on the user-blocking lane, ahead of the work queued
// before it, as Call and CallEvent do. On the plain lane it waited for the
// runtime's whole backlog.
func TestCallTruthyAheadOfBacklog(t *testing.T) {
	h := newHarness(t)
	var id float64
	h.jsInto(`return T.encode(() => "taken").$fn`, &id)
	r := h.rt()
	const each, depth = 10 * time.Millisecond, 40
	for i := 0; i < depth; i++ {
		r.rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
	}
	t0 := time.Now()
	done := make(chan time.Duration, 1)
	h.tt.Bridge().CallTruthy(int64(id), []byte("[]"), func(truthy, threw bool, err error) {
		if err != nil || !truthy || threw {
			t.Errorf("the pick answered truthy %v threw %v err %v, want truthy", truthy, threw, err)
		}
		done <- time.Since(t0)
	})
	select {
	case d := <-done:
		if limit := depth * each / 2; d > limit {
			t.Errorf("the pick answered %v after it was made, %v of JS work queued before it (want under %v): it waited for the backlog", d.Round(time.Millisecond), depth*each, limit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the pick never answered")
	}
	_ = r.rt.Do(func(*jsrt.Ctx) error { return nil })
}
