package taut

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// The patched-view cache the UI reads during layout (MessageView,
// InjectedMessages): Karim's freeze reports of 2026-11-15 had the UI
// goroutine waiting on its lock while a fetch decoded a 124 KB bucket once
// per message, and decoding and converting the same views every frame.

// docID tells conversions apart: a cached conversion hands out the same
// blocks, a new one new blocks.
func docID(m core.Message) uintptr { return reflect.ValueOf(m.Doc.Blocks).Pointer() }

func rawEvent(typ, subtype, ch string, body map[string]any) slack.Event {
	body["type"] = typ
	if subtype != "" {
		body["subtype"] = subtype
	}
	if ch != "" {
		body["channel"] = ch
	}
	raw, _ := json.Marshal(body)
	cj, _ := json.Marshal(ch)
	return slack.Event{Type: typ, Subtype: subtype, Channel: cj, Raw: raw}
}

func TestViewCacheNoUIWaitWhileJSRuns(t *testing.T) {
	h := newHarness(t)
	// a long channel: 400 messages of about 1 KB, a 400 KB bucket
	const n = 400
	msgs := make([]slack.Message, n)
	for i := range msgs {
		msgs[i] = slack.Message{Type: "message", TS: fmt.Sprintf("1700001%03d.000100", i), User: "UALICE",
			Text: fmt.Sprintf("message %d %s", i, strings.Repeat("filler words ", 80))}
	}
	h.app.S.SetLatest(chAnn, msgs, false)
	// a messages patch whose every bucket read can be made slow
	h.js(`globalThis.__slow = 0
		globalThis.__un = T.redux.patchSlice('messages', (ch, b) => {
			if (globalThis.__slow) { const t0 = Date.now(); while (Date.now() - t0 < globalThis.__slow) {} }
			return b
		})
		return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	// a frame: every row asks for its view
	frame := func() (worst time.Duration, ok int) {
		for i := range msgs {
			t0 := time.Now()
			if _, got := h.tt.MessageView(chAnn, msgs[i].TS); got {
				ok++
			}
			worst = max(worst, time.Since(t0))
		}
		return worst, ok
	}
	eventually(t, 20*time.Second, "every message's view", func() bool { _, ok := frame(); return ok == n })

	h.js(`globalThis.__slow = 300; return 1`)
	f0 := h.tt.ui.views.fetches.Load()
	h.tt.mirror.invalidate("messages", chAnn) // all 400 stale: refreshed by bucket reads of 300 ms each
	var worst time.Duration
	frames := 0
	for dl := time.Now().Add(1500 * time.Millisecond); time.Now().Before(dl); frames++ {
		w, ok := frame()
		if ok != n {
			t.Fatalf("frame %d: %d of %d views while they refreshed (the last value must stay)", frames, ok, n)
		}
		worst = max(worst, w)
		time.Sleep(4 * time.Millisecond)
	}
	if worst > 30*time.Millisecond {
		t.Errorf("a MessageView call took %v while JS was busy: the UI waited on the cache", worst)
	}
	// each stale key asked of JS once per invalidation (the mirror marks
	// twice), not once per frame that missed while JS was busy
	if b := h.tt.ui.views.fetches.Load() - f0; b > 4 {
		t.Errorf("%d fetch batches over %d frames", b, frames)
	}
}

func TestMessageViewConvertedOnceUntilChanged(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	const ts = "1700000001.000100"
	var m core.Message
	eventually(t, 3*time.Second, "the view", func() bool {
		var ok bool
		m, ok = h.tt.MessageView(chGen, ts)
		return ok
	})
	if m.Text != "the *** fair is today" {
		t.Fatalf("view %q", m.Text)
	}
	f0 := h.tt.ui.views.fetches.Load()
	for i := 0; i < 50; i++ { // frames with nothing changed
		again, ok := h.tt.MessageView(chGen, ts)
		if !ok || docID(again) != docID(m) {
			t.Fatalf("frame %d: an unchanged view was converted again (ok %v)", i, ok)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if f := h.tt.ui.views.fetches.Load(); f != f0 {
		t.Errorf("%d fetches for an unchanged view", f-f0)
	}

	// an edit: core's event, the mirror, a new value from JS, converted once
	h.app.Inject(rawEvent("message", "message_changed", chGen, map[string]any{"ts": "1700000099.000100",
		"message": map[string]any{"type": "message", "ts": ts, "user": "UALICE", "text": "the job fair moved to friday",
			"edited": map[string]any{"user": "UALICE", "ts": "1700000099.000100"}}}))
	eventually(t, 3*time.Second, "the edited view", func() bool {
		m2, _ := h.tt.MessageView(chGen, ts)
		return m2.Text == "the *** fair moved to friday"
	})

	// a reaction: the message's state in core
	h.app.Inject(rawEvent("reaction_added", "", "", map[string]any{"user": "UBOB", "reaction": "eyes",
		"item": map[string]any{"type": "message", "channel": chGen, "ts": ts}}))
	eventually(t, 3*time.Second, "the reaction", func() bool {
		m3, _ := h.tt.MessageView(chGen, ts)
		for _, r := range m3.Reactions {
			if r.Name == "eyes" {
				return true
			}
		}
		return false
	})

	// what the conversion reads besides the value: the viewer's highlight
	// words (the JS value doesn't change)
	before, _ := h.tt.MessageView(chGen, ts)
	if highlighted(before, "friday") {
		t.Fatal("highlighted before any keyword")
	}
	h.app.S.Lock()
	p, muted := h.app.S.Prefs, h.app.S.Muted
	h.app.S.Unlock()
	p.Keywords = []string{"friday"}
	h.app.S.SetPrefs(p, muted)
	h.tt.mirror.onEvent(core.Event{Type: "prefs"})
	after, _ := h.tt.MessageView(chGen, ts)
	if !highlighted(after, "friday") {
		t.Errorf("a new highlight word didn't reach the cached view")
	}
	// a caller whose copy just changed gets a new conversion
	if fresh, _ := h.tt.MessageViewChanged(chGen, ts); docID(fresh) == docID(after) {
		t.Errorf("MessageViewChanged handed out the cached conversion")
	}
}

func highlighted(m core.Message, word string) bool {
	var walk func(bs []richtext.Block) bool
	walk = func(bs []richtext.Block) bool {
		for _, b := range bs {
			for _, sp := range b.Spans {
				if sp.Style&richtext.Highlight != 0 && strings.Contains(sp.Text, word) {
					return true
				}
			}
			if walk(b.Children) {
				return true
			}
		}
		return false
	}
	return walk(m.Doc.Blocks)
}

func TestInjectedMessagesDecodedOnce(t *testing.T) {
	h := newHarness(t)
	const ts = "1700000005.000500"
	h.js(`globalThis.__un = api.messages.injectMessages(() => [{type: 'message', channel: '` + chGen + `', ts: '` + ts + `', user: 'UBOB', text: 'kept by a logger'}])
		return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	var got []core.Message
	eventually(t, 3*time.Second, "the injected message", func() bool {
		got = h.tt.InjectedMessages(chGen)
		return len(got) == 1
	})
	if got[0].TS != ts || got[0].Text != "kept by a logger" {
		t.Fatalf("injected %+v", got[0])
	}
	f0 := h.tt.ui.views.fetches.Load()
	for i := 0; i < 50; i++ {
		again := h.tt.InjectedMessages(chGen)
		if len(again) != 1 || docID(again[0]) != docID(got[0]) {
			t.Fatalf("call %d: the bucket was decoded or converted again", i)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if f := h.tt.ui.views.fetches.Load(); f != f0 {
		t.Errorf("%d fetches with nothing changed", f-f0)
	}
	// core gets the message (the real copy of a pending send, say): no
	// longer injected, before any refetch
	h.app.S.Upsert(chGen, slack.Message{Type: "message", TS: ts, User: "UBOB", Text: "kept by a logger"})
	if again := h.tt.InjectedMessages(chGen); len(again) != 0 {
		t.Errorf("a message core has came back as injected: %d", len(again))
	}
}

// TestViewCacheStaleWhileInFlight: a value asked of JS before an
// invalidation and delivered after it is stale, so the change shows.
func TestViewCacheStaleWhileInFlight(t *testing.T) {
	h := newHarness(t)
	v := &h.tt.ui.views
	k := viewKey{"messages", chGen, "1700000001.000100"}
	v.mu.Lock()
	g := v.groupLocked(viewSK{k.slice, k.key})
	asked := v.seq // the worker takes the batch
	v.mu.Unlock()
	v.stale("messages", chGen) // the change lands while JS answers
	v.mu.Lock()
	e := &viewEntry{raw: json.RawMessage(`{}`), ver: asked}
	g.subs[k.sub] = e
	stale := v.staleLocked(e, g, k.slice)
	v.mu.Unlock()
	if !stale {
		t.Fatal("a value asked for before an invalidation counts as fresh")
	}
	v.mu.Lock()
	e.ver = v.seq
	fresh := !v.staleLocked(e, g, k.slice)
	v.mu.Unlock()
	if !fresh {
		t.Fatal("a value asked for after the last invalidation counts as stale")
	}
	// whole-slice and staleAll marks
	v.stale("messages", "")
	v.mu.Lock()
	s1 := v.staleLocked(e, g, k.slice)
	e.ver = v.seq
	v.mu.Unlock()
	v.staleAll()
	v.mu.Lock()
	s2 := v.staleLocked(e, g, k.slice)
	v.mu.Unlock()
	if !s1 || !s2 {
		t.Fatalf("slice-wide %v, staleAll %v", s1, s2)
	}
}
