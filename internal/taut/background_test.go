package taut

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
)

// Background traffic on a busy workspace (messages in channels opened
// earlier, profile changes of people no row draws, badge and thread
// events) used to cost a frame and JS work each: Karim's window drew at
// 10% GPU on Windows while nothing on it changed.

// patchedViews patches the messages slice and reads the general channel's
// first message's view and the DM's, so both have conversions.
func patchedViews(t *testing.T, h *harness) (genTS, dmTS string) {
	t.Helper()
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/job/g, '***')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	genTS, dmTS = "1700000001.000100", "1700000010.000100"
	eventually(t, 3*time.Second, "the views", func() bool {
		_, a := h.tt.MessageView(chGen, genTS)
		_, b := h.tt.MessageView(dmAlice, dmTS)
		return a && b
	})
	return genTS, dmTS
}

// A change to a view the UI hasn't read lately (a channel left, so no row
// reads it) marks it stale and draws nothing; one the UI reads still
// redraws. Every message, update or delete in any channel opened this
// session redrew the main window.
func TestOffScreenViewsDrawNothing(t *testing.T) {
	h := newHarness(t)
	v := &h.tt.ui.views
	v.mu.Lock()
	v.onScreen = int64(100 * time.Millisecond) // the window in short, for the test
	v.mu.Unlock()
	genTS, dmTS := patchedViews(t, h)
	// the UI moves on to the DM: only its views are read from now on
	time.Sleep(150 * time.Millisecond)
	for i := 0; i < 3; i++ {
		h.tt.MessageView(dmAlice, dmTS)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // the worker done with them
	n0, ep0 := h.ui.invalidations.Load(), h.tt.ViewEpoch()
	h.tt.mirror.onEvent(core.Event{Type: "update", Channel: chGen, TS: genTS})
	h.tt.mirror.onEvent(core.Event{Type: "message", Channel: chGen, TS: "1700000099.000100"})
	h.tt.mirror.onEvent(core.Event{Type: "delete", Channel: chGen, TS: "1700000003.000300"})
	time.Sleep(80 * time.Millisecond) // past send's 8 ms flush
	if n := h.ui.invalidations.Load() - n0; n != 0 {
		t.Errorf("events in a channel no row reads now redrew the window %d times", n)
	}
	// nor do the rows on screen read their views again for them
	// (ViewEpoch): every event in any channel had every row read all its
	// views again in the next frame
	if ep := h.tt.ViewEpoch(); ep != ep0 {
		t.Errorf("events in a channel no row reads now moved the view epoch %d times", ep-ep0)
	}
	// the DM, on screen: its change redraws, and its rows read again
	h.tt.mirror.onEvent(core.Event{Type: "update", Channel: dmAlice, TS: dmTS})
	eventually(t, 2*time.Second, "a redraw for the DM the UI reads", func() bool { return h.ui.invalidations.Load() > n0 })
	if h.tt.ViewEpoch() == ep0 {
		t.Error("a change to the DM on screen left the view epoch: its rows would wait out their 250 ms")
	}
	// the left channel's view is stale all the same: read again, it's
	// fetched again
	f0 := h.tt.ui.views.fetches.Load()
	h.tt.MessageView(chGen, genTS)
	eventually(t, 2*time.Second, "the left channel's view fetched again", func() bool { return h.tt.ui.views.fetches.Load() > f0 })
}

// api.redux.refresh (Message Logger's, on every edit and delete it logs,
// in any channel) asks JS again for the views on screen; only one that
// comes back changed moves the epoch and redraws. It asked for a frame
// every time, which the rows on screen then skipped (inside their 250 ms,
// the epoch where it was): the window drew for nothing, and a view the
// refresh did change stayed as it was until something else redrew.
func TestRefreshRedrawsOnlyChanges(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__mark = ''; globalThis.__un = api.messages.patchMessageText((t) => t + globalThis.__mark); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	const ts = "1700000001.000100"
	eventually(t, 3*time.Second, "the view", func() bool { _, ok := h.tt.MessageView(chGen, ts); return ok })
	time.Sleep(100 * time.Millisecond) // the worker done
	v := &h.tt.ui.views
	n0, ep0, f0 := h.ui.invalidations.Load(), h.tt.ViewEpoch(), v.fetches.Load()
	h.js(`api.redux.refresh(); return 1`)
	eventually(t, 2*time.Second, "the refresh asking JS again", func() bool { return v.fetches.Load() > f0 })
	time.Sleep(80 * time.Millisecond)
	if n := h.ui.invalidations.Load() - n0; n != 0 {
		t.Errorf("a refresh that changed nothing on screen redrew the window %d times", n)
	}
	if h.tt.ViewEpoch() != ep0 {
		t.Error("a refresh that changed nothing on screen moved the view epoch")
	}
	// one that changes what the view says: a redraw, and the epoch moves
	h.js(`globalThis.__mark = ' (refreshed)'; api.redux.refresh(); return 1`)
	eventually(t, 3*time.Second, "the refreshed view, a redraw and the epoch moved", func() bool {
		m, ok := h.tt.MessageView(chGen, ts)
		return ok && strings.HasSuffix(m.Text, " (refreshed)") && h.ui.invalidations.Load() > n0 && h.tt.ViewEpoch() != ep0
	})
}

// jsInvalidations wraps __taut.invalidate to record, in JS, each call's
// changes.
func jsInvalidations(t *testing.T, h *harness) func() [][]change {
	t.Helper()
	h.js(`globalThis.__inv = []; const inv = __taut.invalidate
		__taut.invalidate = (changes) => { __inv.push(changes); return inv(changes) }; return 1`)
	return func() [][]change {
		var out [][]change
		h.jsInto(`return globalThis.__inv`, &out)
		return out
	}
}

// A profile change goes to JS only for a member it read (since its last
// change): JS reads the store as it is when it first reads one. Every
// status change of anyone posted JS a task that re-ran every store
// listener; a busy workspace's flood swamped the runtime, Go's renders
// waiting behind it.
func TestMemberChangesOnlyForWhatJSHolds(t *testing.T) {
	h := newHarness(t)
	calls := jsInvalidations(t, h)
	var id string
	h.jsInto(`return api.redux.getRawState().members['UBOB']?.id ?? ''`, &id)
	if id != "UBOB" {
		t.Fatalf("JS read members.UBOB as %q", id)
	}
	members := func() []string {
		var out []string
		for _, cs := range calls() {
			for _, c := range cs {
				if c.Slice == "members" {
					out = append(out, c.Keys...)
				}
			}
		}
		return out
	}
	for i := 0; i < 20; i++ {
		h.tt.mirror.onEvent(core.Event{Type: "user", UserID: "UALICE"}) // never read by JS
	}
	time.Sleep(150 * time.Millisecond)
	if got := members(); len(got) != 0 {
		t.Errorf("JS got member changes %v for members it never read", got)
	}
	h.tt.mirror.onEvent(core.Event{Type: "user", UserID: "UBOB"})
	eventually(t, 2*time.Second, "Bob's change in JS", func() bool { return slices.Contains(members(), "UBOB") })
	// JS dropped Bob then: until it reads him again, his changes are no news
	n := len(members())
	h.tt.mirror.onEvent(core.Event{Type: "user", UserID: "UBOB"})
	time.Sleep(150 * time.Millisecond)
	if got := members(); len(got) != n {
		t.Errorf("Bob's second change, before JS read him again, went to JS: %v", got)
	}
	// JS reads him again: his next change is news again
	h.jsInto(`return api.redux.getRawState().members['UBOB']?.id ?? ''`, &id)
	h.tt.mirror.onEvent(core.Event{Type: "user", UserID: "UBOB"})
	eventually(t, 2*time.Second, "Bob's change after JS read him again", func() bool { return len(members()) > n })
	// JS lists the members: it may hold any of them, so every change goes
	var keys int
	h.jsInto(`return Object.keys(api.redux.getRawState().members).length`, &keys)
	h.tt.mirror.onEvent(core.Event{Type: "user", UserID: "UALICE"})
	eventually(t, 2*time.Second, "Alice's change once JS listed the members", func() bool { return slices.Contains(members(), "UALICE") })
}

// While an invalidate task waits behind a busy runtime, later batches join
// it: one task for the burst, not one per 8 ms batch.
func TestInvalidationsJoinTheQueuedTask(t *testing.T) {
	h := newHarness(t)
	calls := jsInvalidations(t, h)
	busy := make(chan struct{})
	h.rt().rt.Post(func(*jsrt.Ctx) { <-busy }) // the runtime busy for a while
	for i := 0; i < 10; i++ {
		h.tt.mirror.invalidate("typing")
		time.Sleep(20 * time.Millisecond) // a batch each (8 ms flush)
	}
	close(busy)
	var n int
	eventually(t, 2*time.Second, "the typing changes in JS", func() bool {
		n = 0
		for _, cs := range calls() {
			for _, c := range cs {
				if c.Slice == "typing" {
					n++
				}
			}
		}
		return n > 0
	})
	time.Sleep(100 * time.Millisecond)
	tasks := 0
	for _, cs := range calls() {
		for _, c := range cs {
			if c.Slice == "typing" {
				tasks++
				break
			}
		}
	}
	if tasks > 2 {
		t.Errorf("10 batches while the runtime was busy ran %d invalidate tasks; want them joined into 1 (2 at most)", tasks)
	}
}

// Raw passthrough events (badge counts, a thread marked read, Slack's
// pref_change before the prefs are fetched, a followed thread's
// notification) change nothing a conversion reads: they re-converted every
// message and redrew the window. A names event converts again only what
// read its ids, an emoji change only what used the emoji.
func TestRawEventsConvertNothing(t *testing.T) {
	h := newHarness(t)
	genTS, _ := patchedViews(t, h)
	const reactedTS = "1700000002.000200" // a :tada: reaction
	eventually(t, 3*time.Second, "the reacted message's view", func() bool { _, ok := h.tt.MessageView(chGen, reactedTS); return ok })
	time.Sleep(100 * time.Millisecond)
	v := &h.tt.ui.views
	g0, r0 := v.convGen(chGen, genTS), v.convGen(chGen, reactedTS)
	n0 := h.ui.invalidations.Load()
	for _, ev := range []core.Event{
		{Type: "badge_counts_updated"}, {Type: "thread_marked"}, {Type: "update_thread_state"}, {Type: "pref_change"},
		{Type: "desktop_notification", Channel: chGen}, {Type: "file_trash"}, {Type: "team"}, {Type: "team_prefs"},
		{Type: "prefs"}, // a pref no conversion reads (core marks neither NamePrefs nor DocPrefs)
		{Type: "names", Users: []string{"UCAROL"}}, // a member no conversion read
		{Type: "emoji_changed", Name: "partyparrot"},
		// a mark of the open channel's newest message (core.Mark, then
		// Slack's channel_marked echo): nothing a conversion reads
		{Type: "marked", Channel: chGen, TS: genTS}, {Type: "marked", Channel: chGen, TS: reactedTS},
		{Type: "draft", Channel: chGen, TS: genTS}, {Type: "typing", Channel: chGen, TS: genTS},
	} {
		h.tt.mirror.onEvent(ev)
	}
	time.Sleep(80 * time.Millisecond)
	if g, r := v.convGen(chGen, genTS), v.convGen(chGen, reactedTS); g != g0 || r != r0 {
		t.Errorf("events no conversion reads moved the generations (%d → %d, %d → %d)", g0, g, r0, r)
	}
	if n := h.ui.invalidations.Load() - n0; n != 0 {
		t.Errorf("they redrew %d times", n)
	}
	// an emoji one message used (its :tada: reaction): that message only
	h.tt.mirror.onEvent(core.Event{Type: "emoji_changed", Name: "tada"})
	if g, r := v.convGen(chGen, genTS), v.convGen(chGen, reactedTS); g != g0 || r == r0 {
		t.Errorf(":tada: changed: generations %d → %d (a message without it), %d → %d (the one with it)", g0, g, r0, r)
	}
	// what every conversion reads: every message
	h.tt.mirror.onEvent(core.Event{Type: "experiments"})
	if v.convGen(chGen, genTS) == g0 {
		t.Error("the experiment groups changed and a conversion kept its generation")
	}
}

// A message event for a message no view converted keeps no generation:
// every message, update and delete workspace-wide added one, and at
// maxConvKeys all conversions started over.
func TestUnconvertedMessagesKeepNoGeneration(t *testing.T) {
	h := newHarness(t)
	genTS, _ := patchedViews(t, h)
	v := &h.tt.ui.views
	for i := 0; i < 50; i++ {
		v.bumpConv(chOther, strings.Repeat("1", 10)+".0"+string(rune('0'+i%10))+"0000")
	}
	v.bumpConv(chGen, "1700000099.000100") // in the channel, never converted
	v.gmu.Lock()
	n := 0
	for k := range v.msgGen {
		if k.slice == chOther {
			n++
		}
	}
	_, kept := v.msgGen[viewSK{chGen, "1700000099.000100"}]
	v.gmu.Unlock()
	if n != 0 || kept {
		t.Errorf("generations kept for messages with no conversion: %d of another channel's, the channel's own: %v", n, kept)
	}
	g := v.convGen(chGen, genTS)
	v.bumpConv(chGen, genTS) // converted: its generation moves
	if v.convGen(chGen, genTS) == g {
		t.Error("a converted message's event left its generation")
	}
}

// A refetch of a bucket whose bytes didn't change decodes nothing, and one
// where one message changed decodes that message.
func TestRefetchDecodesOnlyWhatChanged(t *testing.T) {
	h := newHarness(t)
	genTS, _ := patchedViews(t, h)
	const ts2 = "1700000002.000200"
	eventually(t, 3*time.Second, "a second view", func() bool { _, ok := h.tt.MessageView(chGen, ts2); return ok })
	v := &h.tt.ui.views
	refetch := func() {
		t.Helper()
		f0 := v.fetches.Load()
		h.tt.mirror.invalidate("messages", chGen)
		time.Sleep(30 * time.Millisecond) // the mirror's post
		eventually(t, 3*time.Second, "the refetch", func() bool {
			h.tt.MessageView(chGen, genTS)
			h.tt.MessageView(chGen, ts2)
			return v.fetches.Load() > f0
		})
		time.Sleep(50 * time.Millisecond)
	}
	before, _ := h.tt.MessageView(chGen, genTS)
	d0 := decodedMessages.Load()
	refetch()
	if d := decodedMessages.Load() - d0; d != 0 {
		t.Errorf("a refetch of the same bucket decoded %d messages", d)
	}
	if after, _ := h.tt.MessageView(chGen, genTS); docID(after) != docID(before) {
		t.Error("the unchanged view was converted again")
	}
	// one message edited in core: that one decodes again
	h.app.S.SetLatest(chGen, []slack.Message{
		{Type: "message", TS: genTS, User: "UALICE", Text: "the job fair is tomorrow"},
		{Type: "message", TS: ts2, User: "UBOB", Text: "see https://example.com/?utm_source=x", Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"UALICE", selfID}}}},
		{Type: "message", TS: "1700000003.000300", User: selfID, Text: "hello there"},
	}, false)
	d1 := decodedMessages.Load()
	refetch()
	if d := decodedMessages.Load() - d1; d != 1 {
		t.Errorf("a refetch after one message changed decoded %d messages; want 1", d)
	}
	if after, _ := h.tt.MessageView(chGen, genTS); !strings.Contains(after.Text, "tomorrow") {
		t.Errorf("the edited message's view: %q", after.Text)
	}
}

// InjectedMessages checks core once per call however many messages a
// plugin injected (it was once per message: a scan of the timeline and of
// every cached thread each, 4 times a second per open list).
func TestInjectedMessagesOneStorePass(t *testing.T) {
	h := newHarness(t)
	var injected []map[string]any
	for _, ts := range []string{"1700000004.000400", "1700000005.000500", "1700000006.000600"} {
		injected = append(injected, map[string]any{"type": "message", "channel": chGen, "ts": ts, "user": "UBOB", "text": "kept by a logger " + ts})
	}
	b, _ := json.Marshal(injected)
	h.js(`globalThis.__un = api.messages.injectMessages(() => ` + string(b) + `); return 1`)
	eventually(t, 3*time.Second, "the injected messages", func() bool { return len(h.tt.InjectedMessages(chGen)) == 3 })
	p0 := h.tt.ui.views.held.Load()
	for i := 0; i < 4; i++ {
		h.tt.InjectedMessages(chGen)
	}
	if n := h.tt.ui.views.held.Load() - p0; n != 4 {
		t.Errorf("4 calls for 3 injected messages made %d store passes; want 4", n)
	}
	// and what core holds is still left out
	h.app.S.SetLatest(chGen, []slack.Message{{Type: "message", TS: "1700000005.000500", User: "UBOB", Text: "core has it now"}}, false)
	got := h.tt.InjectedMessages(chGen)
	var tss []string
	for _, m := range got {
		tss = append(tss, m.TS)
	}
	if slices.Contains(tss, "1700000005.000500") || len(tss) != 2 {
		t.Errorf("injected after core got one of them: %v", tss)
	}
}

// A change that lands while a message's conversion runs (after it read
// the store, before it was indexed) leaves that conversion stale: it asks
// for a frame, moves the view epoch (so a list re-reads at once, not 250
// ms later: the frame it asked for came and went inside a row's
// viewRefresh, and an idle window drew no other), and the next read makes
// it again with the new name.
func TestChangeDuringFirstConversion(t *testing.T) {
	h := newHarness(t)
	const fileTS = "1700000007.000700"
	h.app.S.SetLatest(chAnn, []slack.Message{{Type: "message", TS: fileTS, User: "UALICE", Text: "the board",
		Files: []slack.File{{ID: "F0BOARD02", Name: "board.pdf", Title: "Board", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Mode: "hosted", User: "UBOB"}}}}, false)
	h.js(`globalThis.__un = api.messages.patchMessageText((t) => t.replace(/board/g, 'BOARD')); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	var once sync.Once
	var epAt uint64
	var invAt int64
	hook := func(k viewSK) {
		if k != (viewSK{chAnn, fileTS}) {
			return
		}
		once.Do(func() { // Bob renames himself while it's being made
			u := *mustUser(h.app.S, "UBOB")
			u.Profile.DisplayName = "robert"
			h.app.S.PutUsers([]slack.User{u})
			h.tt.mirror.noteConv(core.Event{Type: "user", UserID: "UBOB"})
			epAt, invAt = h.tt.ViewEpoch(), h.ui.invalidations.Load()
		})
	}
	convDone.Store(&hook)
	t.Cleanup(func() { convDone.Store(nil) })
	var first core.Message
	eventually(t, 3*time.Second, "the file message's view", func() bool {
		var ok bool
		first, ok = h.tt.MessageView(chAnn, fileTS)
		return ok
	})
	if h.tt.ViewEpoch() == epAt {
		t.Error("the stale conversion left the view epoch: a list would wait out its 250 ms to read again")
	}
	if h.ui.invalidations.Load() == invAt {
		t.Error("the stale conversion asked for no frame")
	}
	again, _ := h.tt.MessageView(chAnn, fileTS)
	if docID(again) == docID(first) || !strings.Contains(fileOwners(again), "robert") {
		t.Errorf("the read after it got owners %q (the stale one: %v); want robert's", fileOwners(again), docID(again) == docID(first))
	}
}

// fileOwners are the uploader names m's Doc shows.
func fileOwners(m core.Message) string {
	var out []string
	for _, b := range m.Doc.Blocks {
		if b.File != nil {
			out = append(out, b.File.Uploader)
		}
	}
	return strings.Join(out, ",")
}
