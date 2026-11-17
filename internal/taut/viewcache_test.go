package taut

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
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
	h.tt.mirror.onEvent(core.Event{Type: "prefs", DocPrefs: true}) // as core marks it (noteDocChange)
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

// cached is the entry under messages/ch/ts and whether it's fresh, read
// without asking for it (entry() would ask again, which an idle window
// never does).
func cached(v *viewCache, ch, ts string) (*viewEntry, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	g := v.groups[viewSK{"messages", ch}]
	if g == nil {
		return nil, false
	}
	e := g.subs[ts]
	return e, e != nil && !v.staleLocked(e, g, "messages")
}

// The review's interleaving (perf-plugin-ui-thread, 2026-11-16): an edit
// is invalidated while JS is busy; the fetch it starts is queued ahead of
// the mirror's "invalidate" post, so JS answers with the old bytes after
// the mirror marked the view stale again, and the frame that mark asked
// for was turned away because the key was in flight. The worker must ask
// again by itself, and redraw once the new value is in: an idle window
// gets no other frame.
func TestViewCacheAsksAgainWhenInvalidatedInFlight(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	const ts = "1700000001.000100"
	eventually(t, 3*time.Second, "the view", func() bool { _, ok := h.tt.MessageView(chGen, ts); return ok })
	v := &h.tt.ui.views

	// the JS thread busy until released
	block := make(chan struct{})
	h.rt().rt.Post(func(*jsrt.Ctx) { <-block })
	// the edit lands in core, with nothing told yet
	h.app.S.Mutate(chGen, ts, func(m *slack.Message) { m.Text = "the job fair moved to friday" })
	v.stale("messages", chGen)  // the mirror's first mark
	h.tt.MessageView(chGen, ts) // the frame for core's event: asked, queued behind the busy task
	eventually(t, 2*time.Second, "the key in flight", func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.flying[viewKey{"messages", chGen, ts}]
	})
	time.Sleep(30 * time.Millisecond) // its views call is on the JS queue
	// the mirror: stale, then post "invalidate" to JS (behind the views
	// call), mark it stale again and redraw
	h.tt.mirror.invalidate("messages", chGen)
	time.Sleep(50 * time.Millisecond)
	h.tt.MessageView(chGen, ts) // the redraw it asked for: the key is in flight
	n0 := h.ui.invalidations.Load()
	close(block) // JS: the views call (old value), then the invalidate

	// no more frames: the cache must get the edit by itself
	deadline := time.Now().Add(5 * time.Second)
	for {
		e, fresh := cached(v, chGen, ts)
		if e != nil && e.sm != nil && e.sm.Text == "the *** fair moved to friday" && fresh {
			break
		}
		if time.Now().After(deadline) {
			text := "(none)"
			if e != nil && e.sm != nil {
				text = e.sm.Text
			}
			t.Fatalf("after the in-flight answer the view is %q (fresh %v): it was never asked for again", text, fresh)
		}
		time.Sleep(20 * time.Millisecond)
	}
	eventually(t, 2*time.Second, "a redraw for the new value", func() bool { return h.ui.invalidations.Load() > n0 })
}

// A batch JS didn't answer in time is asked again a moment later, without
// waiting for a frame (an idle window might never draw one).
func TestViewCacheRetriesWhatJSDidntAnswer(t *testing.T) {
	if raceOn {
		t.Skip("bounds wall-clock time around a 3 s timeout")
	}
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	const ts = "1700000001.000100"
	v := &h.tt.ui.views
	block := make(chan struct{})
	h.rt().rt.Post(func(*jsrt.Ctx) { <-block }) // past the views call's 3 s timeout
	h.tt.MessageView(chGen, ts)                 // asked once: the call times out
	time.Sleep(3500 * time.Millisecond)
	close(block)
	eventually(t, 5*time.Second, "the view after JS came back", func() bool {
		e, fresh := cached(v, chGen, ts)
		return e != nil && e.sm != nil && fresh
	})
}

// Conversion generations: a bump redraws when a conversion it outdates is
// cached and on screen (the UI may have converted with the old generation
// first); "names" bumps the conversions that read its ids (with none, the
// viewer's user groups: every mention's highlight), and "experiments"
// every message (a huddle message's sentence and the emoji set follow
// experiment groups).
func TestConvBumpRedraws(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	const ts = "1700000001.000100"
	eventually(t, 3*time.Second, "the view", func() bool { _, ok := h.tt.MessageView(chGen, ts); return ok })
	v := &h.tt.ui.views

	n0 := h.ui.invalidations.Load()
	v.bumpConv(chGen, ts) // its message changed in core
	if h.ui.invalidations.Load() == n0 {
		t.Error("no redraw after the cached conversion's message changed")
	}
	n1 := h.ui.invalidations.Load()
	h.tt.mirror.onEvent(core.Event{Type: "names", Users: []string{"B0BOT"}}) // a bot no conversion read
	if h.ui.invalidations.Load() != n1 {
		t.Error("the name of a bot no conversion read redrew")
	}
	h.tt.mirror.onEvent(core.Event{Type: "names"}) // the viewer's user groups
	if h.ui.invalidations.Load() == n1 {
		t.Error("no redraw after the viewer's user groups changed")
	}
	g0 := v.convGen(chGen, ts)
	h.tt.mirror.onEvent(core.Event{Type: "experiments"})
	if v.convGen(chGen, ts) == g0 {
		t.Error("an experiments event left the conversions as they were")
	}
	g1 := v.convGen(chGen, ts)
	h.tt.mirror.onEvent(core.Event{Type: "typing", Channel: chGen, UserID: "UBOB"})
	if v.convGen(chGen, ts) != g1 {
		t.Error("a typing event outdated the conversions")
	}
}

// What a message view holds: the decoded message and its conversion, not
// the JSON as well; what core's cache pass lets go goes from here too; and
// the cache is bounded by bytes, not only by entries.
func TestViewCacheMemory(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	tss := []string{"1700000001.000100", "1700000002.000200", "1700000003.000300"}
	eventually(t, 3*time.Second, "the views", func() bool {
		for _, ts := range tss {
			if _, ok := h.tt.MessageView(chGen, ts); !ok {
				return false
			}
		}
		return true
	})
	v := &h.tt.ui.views
	for _, ts := range tss {
		e, _ := cached(v, chGen, ts)
		if e == nil || e.raw != nil || e.sum == 0 || e.rawLen == 0 || e.sm == nil {
			t.Fatalf("%s: raw kept %v, sum %x, %d bytes, decoded %v", ts, e != nil && e.raw != nil, e.sum, e.rawLen, e != nil && e.sm != nil)
		}
	}
	// a refetch with the same bytes keeps the conversion
	before, _ := h.tt.MessageView(chGen, tss[0])
	v.stale("messages", chGen)
	h.tt.MessageView(chGen, tss[0])
	eventually(t, 3*time.Second, "the refetch", func() bool { _, fresh := cached(v, chGen, tss[0]); return fresh })
	if after, _ := h.tt.MessageView(chGen, tss[0]); docID(after) != docID(before) {
		t.Error("a refetch of the same value converted it again")
	}

	// core's cache pass kept chGen from its second message on
	h.app.S.Remove(chGen, tss[0])
	h.tt.mirror.onEvent(core.Event{Type: "evicted", Channel: chGen, TS: tss[1]})
	if e, _ := cached(v, chGen, tss[0]); e != nil {
		t.Error("the evicted message's view stayed")
	}
	if e, _ := cached(v, chGen, tss[1]); e == nil {
		t.Error("a message the pass kept lost its view")
	}
	v.mu.Lock()
	n, b := v.n, v.bytes
	v.mu.Unlock()
	if n < 2 || b <= 0 {
		t.Errorf("entries %d, bytes %d after the eviction", n, b)
	}

	// past the byte budget, everything goes when the next answer lands
	v.mu.Lock()
	v.maxBytes = 1
	v.mu.Unlock()
	v.stale("messages", chGen)
	h.tt.MessageView(chGen, tss[2])
	eventually(t, 3*time.Second, "the cache dropped", func() bool {
		e, _ := cached(v, chGen, tss[1])
		return e == nil
	})
}

// Profile changes for members no conversion read, and count or badge
// events, cost nothing: no conversion generation moves, nothing converts
// again, nothing redraws (a busy workspace sends user_change many times a
// second; review of 2026-11-16). So do a channel's details and its member
// count (core's member_count, once per channel on its first open when
// users/counts lands) and tabs of a channel no conversion read: they carry
// a channel but no message, which noteConv would otherwise take as a
// change to every message. A member a conversion read (a mention, a
// file's owner) converts that message again, and only that one, whether a
// frame drew them or not (core.Message.Refs).
func TestUserEventsForUndrawnMembersCostNothing(t *testing.T) {
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
	v := &h.tt.ui.views
	time.Sleep(100 * time.Millisecond) // the view's fetch settled
	g0, n0 := v.convGen(chGen, ts), h.ui.invalidations.Load()
	long := time.Now().Add(-time.Minute)
	for i := 0; i < 200; i++ {
		h.tt.mirror.noteConv(core.Event{Type: "user", UserID: "UBOB", Seen: long}) // drawn a minute ago
		h.tt.mirror.noteConv(core.Event{Type: "user", UserID: "UALICE"})           // never drawn
		for _, typ := range []string{"activity_badges", "counts", "poll", "client_online", "realtime", "tabs", "bookmarks"} {
			h.tt.mirror.noteConv(core.Event{Type: typ})
		}
		h.tt.mirror.noteConv(core.Event{Type: "conv", Channel: chGen})
		h.tt.mirror.noteConv(core.Event{Type: "member_count", Channel: chGen, Conv: &core.Conv{ID: chGen, NumMembers: 42}})
	}
	if g := v.convGen(chGen, ts); g != g0 {
		t.Errorf("the conversion generation moved (%d → %d)", g0, g)
	}
	if n := h.ui.invalidations.Load(); n != n0 {
		t.Errorf("%d redraws for members nobody drew and badge events", n-n0)
	}
	if again, _ := h.tt.MessageView(chGen, ts); docID(again) != docID(m) {
		t.Error("the view was converted again")
	}

	// a member a conversion read (a file Bob owns), drawn a minute ago:
	// that conversion is made again and redraws, the others stay; a
	// status change of his converts nothing
	const fileTS = "1700000004.000400"
	h.app.S.SetLatest(chAnn, []slack.Message{{Type: "message", TS: fileTS, User: "UALICE", Text: "the job board",
		Files: []slack.File{{ID: "F0BOARD01", Name: "board.pdf", Title: "Job board", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Mode: "hosted", User: "UBOB"}}}}, false)
	h.tt.mirror.invalidate("messages", chAnn)
	var fm core.Message
	eventually(t, 3*time.Second, "the file message's view", func() bool {
		var ok bool
		fm, ok = h.tt.MessageView(chAnn, fileTS)
		return ok && slices.Contains(fm.Refs.Members, "UBOB")
	})
	fg := v.convGen(chAnn, fileTS)
	n1 := h.ui.invalidations.Load()
	h.tt.mirror.noteConv(core.Event{Type: "user", UserID: "UBOB", Seen: long, SameNames: true})
	if v.convGen(chAnn, fileTS) != fg || h.ui.invalidations.Load() != n1 {
		t.Error("a status change of a member a conversion names converted it again or redrew")
	}
	h.tt.mirror.noteConv(core.Event{Type: "user", UserID: "UBOB", Seen: long})
	if v.convGen(chAnn, fileTS) == fg {
		t.Error("the change of a member a conversion named left it as it was")
	}
	if g := v.convGen(chGen, ts); g != g0 {
		t.Errorf("a conversion that named nobody changed moved (%d → %d)", g0, g)
	}
	if h.ui.invalidations.Load() == n1 {
		t.Error("no redraw for the change of a member a cached conversion named")
	}
	if again, _ := h.tt.MessageView(chAnn, fileTS); docID(again) == docID(fm) {
		t.Error("the conversion that named the member wasn't made again")
	}

	// a pref no conversion reads converts nothing; a names pref
	// (display_real_names, hidden_users) converts again what named a
	// member and not the rest (every prefs event converted every message)
	fg2, n3 := v.convGen(chAnn, fileTS), h.ui.invalidations.Load()
	h.tt.mirror.noteConv(core.Event{Type: "prefs"})
	if v.convGen(chAnn, fileTS) != fg2 || v.convGen(chGen, ts) != g0 || h.ui.invalidations.Load() != n3 {
		t.Error("a pref no conversion reads converted messages again or redrew")
	}
	h.tt.mirror.noteConv(core.Event{Type: "prefs", NamePrefs: true})
	if v.convGen(chAnn, fileTS) == fg2 {
		t.Error("a names pref left the conversion that named a member as it was")
	}
	if g := v.convGen(chGen, ts); g != g0 {
		t.Errorf("a names pref converted again a message that named nobody (%d → %d)", g0, g)
	}
	if h.ui.invalidations.Load() == n3 {
		t.Error("no redraw for a names pref")
	}

	// a conversion no view holds (its view went): a change to what it
	// read redraws nothing, and the index lets it go
	var slot atomic.Pointer[viewConv]
	sm := slack.Message{Type: "message", TS: "1700000005.000500", User: "UBOB", Text: "a file",
		Files: []slack.File{{ID: "F0LOOSE01", Name: "loose.pdf", Title: "Loose", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Mode: "hosted", User: "UALICE"}}}
	first := v.conv(&slot, chGen, &sm, false)
	if !slices.Contains(first.msg.Refs.Members, "UALICE") {
		t.Fatalf("the conversion of a file Alice owns read %+v; want UALICE", first.msg.Refs)
	}
	n2 := h.ui.invalidations.Load()
	h.tt.mirror.noteConv(core.Event{Type: "user", UserID: "UALICE", Seen: long})
	if v.convGen(chGen, sm.TS) == first.gen {
		t.Error("the change of a member a conversion named left its generation")
	}
	if n := h.ui.invalidations.Load(); n != n2 {
		t.Errorf("%d redraws for a member only uncached conversions named", n-n2)
	}
	v.gmu.Lock()
	_, indexed := v.refOf[viewSK{chGen, sm.TS}]
	v.gmu.Unlock()
	if indexed {
		t.Error("the index kept a conversion no view holds")
	}
}

// indexRefs, by itself: a conversion begun (convGenAt) before a change to
// what it read (bumpChange) is stale when it's indexed, one begun after
// isn't. (TestChangeDuringFirstConversion has a real conversion race it.)
func TestIndexRefsStaleness(t *testing.T) {
	h := newHarness(t)
	v := &h.tt.ui.views
	k := viewSK{chGen, "1700000009.000900"}
	_, at := v.convGenAt(k.slice, k.key) // the conversion begins
	v.bumpChange(core.RefChange{Members: []string{"UBOB"}})
	g0 := v.convGen(k.slice, k.key)
	if !v.indexRefs(k, core.MsgRefs{Members: []string{"UBOB"}}, at) {
		t.Error("a conversion under way as a member it read changed wasn't stale")
	}
	if v.convGen(k.slice, k.key) == g0 {
		t.Error("its generation stayed: it would show the old name")
	}
	_, at2 := v.convGenAt(k.slice, k.key) // one begun after the change
	if v.indexRefs(k, core.MsgRefs{Members: []string{"UBOB"}}, at2) {
		t.Error("a conversion begun after the change was stale")
	}

	// a names pref under way: one that read a member is stale, one that
	// read none isn't
	k2 := viewSK{chGen, "1700000009.000950"}
	_, at3 := v.convGenAt(k2.slice, k2.key)
	v.bumpChange(core.RefChange{AnyMember: true})
	if !v.indexRefs(k2, core.MsgRefs{Members: []string{"UCARA"}}, at3) {
		t.Error("a conversion under way as a names pref changed wasn't stale")
	}
	if v.indexRefs(viewSK{chGen, "1700000009.000960"}, core.MsgRefs{Channels: []string{chAnn}}, at3) {
		t.Error("a conversion that read no member was stale after a names pref")
	}
}

// Core's cache pass trims a channel but keeps a thread on screen whole:
// the thread's views stay (they flashed to unpatched text until JS
// answered again), and only messages core let go lose theirs.
func TestEvictionKeepsWhatCoreKept(t *testing.T) {
	h := newHarness(t)
	const root, r1, r2 = "1700000001.000100", "1700000001.500000", "1700000001.600000"
	const gone = "1700000002.000200"
	h.app.S.SetThread(chGen, root, []slack.Message{
		{Type: "message", TS: root, User: "UALICE", Text: "the job fair is today", ThreadTS: root, ReplyCount: 2},
		{Type: "message", TS: r1, User: "UBOB", Text: "a job reply", ThreadTS: root},
		{Type: "message", TS: r2, User: "UALICE", Text: "another job reply", ThreadTS: root},
	})
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	all := []string{root, r1, r2, gone}
	eventually(t, 3*time.Second, "the views", func() bool {
		for _, ts := range all {
			if _, ok := h.tt.MessageView(chGen, ts); !ok {
				return false
			}
		}
		return true
	})
	v := &h.tt.ui.views
	h.app.S.Remove(chGen, gone) // what the pass let go
	// kept from the channel's newest message on: older than the thread
	h.tt.mirror.onEvent(core.Event{Type: "evicted", Channel: chGen, TS: "1700000003.000300"})
	for _, ts := range []string{root, r1, r2} {
		if e, _ := cached(v, chGen, ts); e == nil {
			t.Errorf("%s: the kept thread's view was dropped", ts)
		}
	}
	if e, _ := cached(v, chGen, gone); e != nil {
		t.Error("the view of a message core let go stayed")
	}
}

// drop takes the members it names and leaves the rest.
func TestViewCacheDropMembers(t *testing.T) {
	h := newHarness(t)
	v := &h.tt.ui.views
	v.mu.Lock()
	for i := 0; i < 50; i++ {
		g := v.groupLocked(viewSK{"members", fmt.Sprintf("U%03d", i)})
		g.subs[""] = &viewEntry{raw: json.RawMessage(`{}`), size: 10}
		v.n++
		v.bytes += 10
	}
	v.mu.Unlock()
	v.drop("members", "U001", "U002", "U049", "UNOPE")
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, id := range []string{"U001", "U002", "U049"} {
		if v.groups[viewSK{"members", id}] != nil {
			t.Errorf("%s stayed", id)
		}
	}
	if v.groups[viewSK{"members", "U000"}] == nil || v.n != 47 || v.bytes != 470 {
		t.Errorf("the rest: U000 kept %v, %d entries, %d bytes", v.groups[viewSK{"members", "U000"}] != nil, v.n, v.bytes)
	}
}
