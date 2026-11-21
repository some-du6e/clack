package taut

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
)

// sustainedBacklog keeps the plugin runtime about depth tasks of each
// behind until the test ends: what patched rows re-rendering for a busy
// channel's messages leave it.
func sustainedBacklog(t *testing.T, r *runtime, each time.Duration, depth int) {
	t.Helper()
	for i := 0; i < depth; i++ {
		r.rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
	}
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(each):
				r.rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		bg.Wait()
		_ = r.rt.Do(func(*jsrt.Ctx) error { return nil })
	})
}

// injectedText is the text of ch's one injected message ("" for none).
func injectedText(h *harness, ch string) string {
	got := h.tt.InjectedMessages(ch)
	if len(got) != 1 {
		return ""
	}
	return got[0].Text
}

// A whole message bucket (Message Logger's injected messages) is read
// behind a busy runtime in about one queue trip, not one per chunk: its
// chunks go to JS together and run back to back. Each waited behind the
// backlog in turn, so a 300-message bucket took 13 trips, and Message
// Logger's deleted copy came back seconds late.
func TestBucketReadBehindBacklog(t *testing.T) {
	h := newHarness(t)
	const n = 300
	msgs := make([]slack.Message, n)
	for i := range msgs {
		msgs[i] = slack.Message{Type: "message", TS: fmt.Sprintf("1700002%03d.000100", i), User: "UALICE", Text: fmt.Sprintf("message %d", i)}
	}
	h.app.S.SetLatest(chAnn, msgs, false)
	h.js(`globalThis.__injText = 'first'
		globalThis.__un = api.messages.injectMessages(() => [{type: 'message', channel: '` + chAnn + `', ts: '1700003000.000100', user: 'UBOB', text: globalThis.__injText}])
		return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	eventually(t, 10*time.Second, "the injected message", func() bool { return injectedText(h, chAnn) == "first" })
	// the plugin's messages change (Message Logger's log): its provider
	// runs again at the next read (api.redux.refresh bumps patchVersion)
	h.js(`globalThis.__injText = 'second'; api.redux.refresh(); return 1`)

	const each, depth = 10 * time.Millisecond, 50
	sustainedBacklog(t, h.rt(), each, depth)
	t0 := time.Now()
	h.tt.mirror.invalidate("messages", chAnn) // a deletion: the bucket is read again
	for injectedText(h, chAnn) != "second" {
		if time.Since(t0) > 20*time.Second {
			t.Fatal("the bucket never came back")
		}
		time.Sleep(5 * time.Millisecond)
	}
	d := time.Since(t0)
	t.Logf("a %d-entry bucket came back %v after it went stale, the runtime %v behind", n+1, d.Round(time.Millisecond), depth*each)
	if limit := 4 * depth * each; d > limit {
		t.Errorf("a %d-entry bucket took %v behind a %v backlog (want under %v): its chunks waited behind it one after another", n+1, d.Round(time.Millisecond), depth*each, limit)
	}
}

// The chunk edges: a bucket of exactly one chunk and of one more, read
// whole together with single messages of it; an empty bucket.
func TestBucketReadChunkEdges(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.injectMessages(() => ['` + chAnn + `', '` + chGen + `'].map((ch) => ({type: 'message', channel: ch, ts: '1700009000.000100', user: 'UBOB', text: 'injected in ' + ch})))
		return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	for _, tc := range []struct {
		ch string
		n  int // messages core holds; the bucket holds one more
	}{{chAnn, viewChunk - 1}, {chGen, viewChunk}} {
		msgs := make([]slack.Message, tc.n)
		for i := range msgs {
			msgs[i] = slack.Message{Type: "message", TS: fmt.Sprintf("1700004%03d.000100", i), User: "UALICE", Text: fmt.Sprintf("%s %d", tc.ch, i)}
		}
		h.app.S.SetLatest(tc.ch, msgs, false)
		h.tt.mirror.invalidate("messages", tc.ch)
		eventually(t, 10*time.Second, fmt.Sprintf("%s's injected message (a bucket of %d)", tc.ch, tc.n+1), func() bool {
			return injectedText(h, tc.ch) == "injected in "+tc.ch
		})
		for _, i := range []int{0, tc.n - 1} {
			ts := msgs[i].TS
			eventually(t, 10*time.Second, fmt.Sprintf("%s's message %d", tc.ch, i), func() bool {
				m, ok := h.tt.MessageView(tc.ch, ts)
				return ok && m.Text == msgs[i].Text
			})
		}
	}
	const empty = "C0EMPTY1"
	h.app.S.SetLatest(empty, nil, false)
	h.tt.mirror.invalidate("messages", empty)
	for i := 0; i < 20; i++ {
		if got := h.tt.InjectedMessages(empty); len(got) != 0 {
			t.Fatalf("an empty bucket injected %d messages", len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
