package taut

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestMirrorHoldsJumpWindows: a jump's window (core's store windows) is in
// the mirror as Slack's messages store and channelHistory hold every
// slice: messages[ch] has its messages, channelHistory[ch] lists it as a
// slice of its own before the timeline's, its arrival changes messages'
// fingerprint (the sweep sends it to JS), and Held counts its messages
// (InjectedMessages doesn't take them for a plugin's). The mirror held the
// timeline and the threads only: plugins never saw a window's messages,
// and its rows drew unpatched.
func TestMirrorHoldsJumpWindows(t *testing.T) {
	h := newHarness(t)
	m := h.tt.mirror
	// the timeline with older pages beyond it: a whole history (no older
	// page) takes any older stretch in (timelineMeets), leaving no window
	tl := h.app.S.Timeline(chGen)
	if tl == nil {
		t.Fatalf("no timeline for %s", chGen)
	}
	h.app.S.SetLatest(chGen, tl.Msgs, true)
	fp := m.messagesFP(chGen)
	old := []slack.Message{
		{Type: "message", TS: "1600000100.000100", User: "UALICE", Text: "an old message"},
		{Type: "message", TS: "1600000200.000100", User: "UBOB", Text: "another old one"},
	}
	if _, merged := h.app.S.AddWindow(chGen, old, old[0].TS, old[1].TS, true, true, h.app.S.LiveSeq()); merged {
		t.Fatal("the old stretch joined the timeline; the test needs a window")
	}
	if m.messagesFP(chGen) == fp {
		t.Error("the window left messages' fingerprint as it was: the sweep never sends it to JS")
	}
	b := m.bucket(chGen)
	for _, x := range old {
		if b[x.TS] == nil {
			t.Errorf("messages[%s] lacks the window's %s", chGen, x.TS)
		}
	}
	hist, _ := m.history(chGen).(map[string]any)
	slices, _ := hist["slices"].([]any)
	var first []string
	if len(slices) > 0 {
		if s0, ok := slices[0].(map[string]any); ok {
			first, _ = s0["timestamps"].([]string)
		}
	}
	if len(slices) != 2 || len(first) != 2 || first[0] != old[0].TS || first[1] != old[1].TS {
		t.Errorf("channelHistory[%s]: %d slices, the first %v; want the window's (%s, %s) and then the timeline's", chGen, len(slices), first, old[0].TS, old[1].TS)
	}
	if held := h.app.S.Held(chGen, []store.MsgKey{{TS: old[0].TS}, {TS: old[1].TS}}); !held[0] || !held[1] {
		t.Errorf("Held %v for the window's messages: InjectedMessages takes them for a plugin's own", held)
	}
}

// TestFetchedMessageMarkedOnce: a load that brings a message the plugins'
// slice holds as a copy fetched by id (a jump's window around the message
// its thread lookup fetched: core.ReplyThread hydrates it first) doesn't
// mark it again: it was in JS's copy already (the bucket holds fetched
// copies; one new to the store was marked as it landed, the fetched hook),
// and a second mark held its view a JS round trip more. A message the
// slice lacks is marked, as edit-latency's MessagesAdded has it.
func TestFetchedMessageMarkedOnce(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = T.redux.patchSlice('messages', (ch, b) => b); return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	m := h.tt.mirror
	m.watch("messages", chGen, m.messagesFP(chGen)) // JS read #general's messages before the load
	const fetched, fresh = "1600000100.000100", "1600000200.000100"
	h.app.S.PutFetched(chGen, []slack.Message{{Type: "message", TS: fetched, User: "UALICE", Text: "fetched by id"}}, h.app.S.LiveSeq())
	if b := m.bucket(chGen); b[fetched] == nil {
		t.Fatalf("messages[%s] lacks the message fetched by id", chGen)
	}
	v := &h.tt.ui.views
	ver := func(ts string) uint64 {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.msgVer[msgKey{chGen, ts}]
	}
	f0, n0 := ver(fetched), ver(fresh)
	h.tt.MessagesAdded(chGen, []string{fetched, fresh})
	if ver(fetched) != f0 {
		t.Errorf("a message fetched by id, in the plugins' slice already, was marked again")
	}
	if ver(fresh) == n0 {
		t.Errorf("a message the plugins' slice lacked wasn't marked")
	}
}
