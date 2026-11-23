package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// A composer taken off the screen lets go of the focus at once
// (composer_release.go). KeepFocus used to hold its focus for 2 s after a
// switch to another page, a closed thread pane or another thread in it;
// what was typed meanwhile went into the hidden box and came back out
// wherever that box was drawn next (Karim's log, 2026-11-15). Closing the
// thread pane hands the focus to the channel's message input, as Slack's
// onViewClosed does.
func TestLeftComposerLetsGoOfFocus(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch, root, other := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadDMID
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })

	box := func(ch, ts string) composerBox {
		t.Helper()
		c := e.a.convos[ch+"/"+ts]
		if c == nil {
			t.Fatalf("no conversation %s/%s", ch, ts)
		}
		cb, ok := c.comp.(composerBox)
		if !ok {
			t.Fatalf("%s/%s's composer is a %T", ch, ts, c.comp)
		}
		return cb
	}
	focused := func(ch, ts string) bool { return e.r.Source().Focused(box(ch, ts).FocusTag()) }
	typed := func(s string) {
		e.typeText(s)
		e.frame()
		e.frame()
	}

	// another page: nothing keeps the channel's composer focused, and what
	// is typed there doesn't come back in it
	e.focusComposer(ch, "")
	if !focused(ch, "") {
		t.Fatal("the channel's composer didn't take the focus")
	}
	e.a.Navigate("activity")
	e.frame()
	typed("typed-on-activity")
	if focused(ch, "") {
		t.Error("on Activity the channel's composer still has the focus")
	}
	e.openChannel(ch)
	e.frame()
	e.frame()
	if got := box(ch, "").DocText(); strings.Contains(got, "typed-on-activity") {
		t.Errorf("text typed on Activity landed in the channel's composer: %q", got)
	}

	// the thread pane closes: the channel's composer takes the focus
	// the pane draws its reply box once the thread has loaded and its
	// newer replies have been read (ThreadView.HidesComposer: the
	// loading-end row stands in its place until then); a load the frames
	// didn't cover left nothing to focus (the boot channel's older page read
	// meanwhile; mf-first-frame's full-test, the reopen below)
	replyBox := func(what string) {
		t.Helper()
		e.until(what, 10*time.Second, func() bool {
			c := e.a.convos[ch+"/"+root]
			if c == nil {
				return false
			}
			hc, ok := c.list.(interface{ HidesComposer() bool })
			return !ok || !hc.HidesComposer()
		})
	}
	e.a.OpenThread(ch, root)
	e.frame()
	replyBox("the thread's reply box")
	e.focusComposer(ch, root)
	if !focused(ch, root) {
		t.Fatal("the thread's composer didn't take the focus")
	}
	e.a.CloseSecondary()
	e.frame()
	e.frame()
	e.frame()
	if focused(ch, root) {
		t.Error("the closed thread's composer kept the focus")
	}
	if !focused(ch, "") {
		t.Error("closing the thread pane didn't focus the channel's composer")
	}
	typed("typed-after-close")
	if got := box(ch, "").DocText(); !strings.Contains(got, "typed-after-close") {
		t.Errorf("after closing the thread, typing didn't reach the channel's composer (%q)", got)
	}

	// another thread in the pane: the first thread's composer lets go,
	// and what is typed meanwhile isn't replayed into it when it's back
	// the reopened pane draws its reply box once its thread is in
	// (mf-first-frame's full-test: focused a frame after the reopen, the
	// box wasn't drawn yet)
	e.a.OpenThread(ch, root)
	e.frame()
	replyBox("the reopened thread's reply box")
	e.focusComposer(ch, root)
	if !focused(ch, root) {
		t.Fatal("the thread's composer didn't take the focus again")
	}
	e.a.OpenThread(other, root)
	e.frame()
	typed("typed-in-other-thread")
	if focused(ch, root) {
		t.Error("the replaced thread's composer kept the focus")
	}
	e.a.OpenThread(ch, root)
	e.frame()
	e.frame()
	if got := box(ch, root).DocText(); strings.Contains(got, "typed-in-other-thread") {
		t.Errorf("text typed while another thread was open landed in this one's composer: %q", got)
	}

	// a conversation in the secondary column on the Activity tab (where
	// Activity opens a channel item): closing the column (Mod+. there)
	// lets go of its composer, though no channel takes the focus
	e.a.Navigate("activity")
	e.frame()
	e.a.OpenChannelSecondary(ch)
	e.frame()
	e.frame()
	e.focusComposer(ch, "")
	if !focused(ch, "") {
		t.Fatal("the column's composer didn't take the focus")
	}
	e.a.CloseSecondary()
	e.frame()
	typed("typed-after-the-column")
	if focused(ch, "") {
		t.Error("the closed column's composer kept the focus")
	}
	e.openChannel(ch)
	e.frame()
	e.frame()
	if got := box(ch, "").DocText(); strings.Contains(got, "typed-after-the-column") {
		t.Errorf("text typed after the column closed landed in its composer: %q", got)
	}

	// CloseSecondary asks for the channel's composer; a navigation away
	// before it's drawn drops the request, so a permalink into the channel
	// later leaves the composer alone (Slack focuses the list there)
	e.a.OpenThread(ch, root)
	e.frame()
	e.a.CloseSecondary()
	e.a.Navigate("activity")
	e.frame()
	e.frame()
	e.a.Navigate("c/" + ch + "/m/" + root)
	e.frame()
	e.frame()
	e.frame()
	if focused(ch, "") {
		t.Error("a permalink into the channel focused its composer: CloseSecondary's request outlived the navigation")
	}

	// Mod+. hides the thread pane while typing in it: the channel's
	// composer takes the focus, as on a close (Slack's onViewClosed), so
	// the next keys land there, not nowhere
	e.openChannel(ch)
	e.a.OpenThread(ch, root)
	e.frame()
	e.focusComposer(ch, root)
	if !focused(ch, root) {
		t.Fatal("the thread's composer didn't take the focus before Mod+.")
	}
	e.key(".", key.ModShortcut)
	e.frame()
	e.frame()
	if !e.a.secondary.hidden {
		t.Fatal("Mod+. didn't hide the thread pane")
	}
	typed("typed-after-hiding")
	if got := box(ch, "").DocText(); !strings.Contains(got, "typed-after-hiding") {
		t.Errorf("after Mod+. hid the thread pane, typing didn't reach the channel's composer (%q)", got)
	}
	if got := box(ch, root).DocText(); strings.Contains(got, "typed-after-hiding") {
		t.Errorf("text typed after the pane hid landed in its composer: %q", got)
	}
}

// fakeComposer is a composerView whose focus tag, live draw and Release
// the test sets and reads.
type fakeComposer struct {
	tag      int
	drawn    bool
	released int
}

func (f *fakeComposer) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (f *fakeComposer) SetOrigin(image.Point)                       {}
func (f *fakeComposer) SetOnEditLast(func())                        {}
func (f *fakeComposer) Done() bool                                  { return false }
func (f *fakeComposer) Focus()                                      {}
func (f *fakeComposer) Close()                                      {}
func (f *fakeComposer) FocusTag() any                               { return &f.tag }
func (f *fakeComposer) Release()                                    { f.released++ }
func (f *fakeComposer) DrawnLive(layout.Context) bool               { return f.drawn }

// Wherever a composer lives (the DMs tab's or Activity's conversation, any
// pane the state model doesn't know), a focused one this frame didn't draw
// is released at the end of the frame; inside a conversation the state
// still shows, its missing draw is a glitch, left to KeepFocus.
func TestUndrawnFocusedComposerReleased(t *testing.T) {
	var r input.Router
	var ops op.Ops
	f := &fakeComposer{}
	gtx := func() layout.Context {
		return layout.Context{Ops: &ops, Source: r.Source(), Now: time.Now(), Constraints: layout.Exact(image.Pt(100, 100))}
	}
	focusIt := func() {
		t.Helper()
		ops.Reset()
		g := gtx()
		event.Op(g.Ops, f.FocusTag())
		for {
			if _, ok := g.Event(key.FocusFilter{Target: f.FocusTag()}); !ok {
				break
			}
		}
		r.Frame(&ops)
		gtx().Execute(key.FocusCmd{Tag: f.FocusTag()})
		if !r.Source().Focused(f.FocusTag()) {
			t.Fatal("the fake didn't take the focus")
		}
	}
	a := &App{}
	convos := map[string]*convo{"D0DMSTAB/": {ch: "D0DMSTAB", comp: f}}
	focusIt()

	f.drawn = true
	a.releaseUndrawn(gtx(), convos, nil)
	if f.released != 0 {
		t.Fatal("a composer drawn this frame was released")
	}
	f.drawn = false
	a.releaseUndrawn(gtx(), convos, map[string]bool{"D0DMSTAB/": true})
	if f.released != 0 {
		t.Fatal("a composer the state still shows was released over one missed draw (a glitch)")
	}
	a.releaseUndrawn(gtx(), convos, nil)
	if f.released != 1 {
		t.Fatalf("a focused composer nothing drew, outside what the state shows, was released %d times, want 1", f.released)
	}
	gtx().Execute(key.FocusCmd{}) // unfocused: left alone
	a.releaseUndrawn(gtx(), convos, nil)
	if f.released != 1 {
		t.Error("an unfocused composer was released")
	}
}

// The History menu names a visited DM whose partner the members cache let
// go: nothing else draws a closed DM's partner, so the menu looks the
// partner up as it draws the entry (getMemberById), and it's there once
// users/info answers.
func TestHistoryMenuNamesEvictedDMPartner(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const dm, partner = "D0HISTCLOSE1", fakeslack.ProfileID // a closed DM; nobody else draws its partner
	fs.AddClosedDM(dm, partner)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.Core()
	e.a.Navigate("c/" + dm) // visited: the history menu lists it
	e.until("the DM's partner", 20*time.Second, func() bool { _, ok := c.S.RawUser(partner); return ok })
	e.openChannel(fs.ChannelIDs()[0])
	e.settle(500 * time.Millisecond)
	r := Route("c/" + dm)
	if en, ok := e.a.historyEntryFor(r); !ok || en.label == "" {
		t.Fatalf("the visited DM has no history entry while its partner is known (%+v)", en)
	}
	c.S.SetMembersSizeForDebug(1, 1)
	c.EvictMembers()
	c.S.SetMembersSizeForDebug(-1, -1)
	e.settle(500 * time.Millisecond)
	if _, ok := c.S.RawUser(partner); ok {
		t.Fatal("the closed DM's partner came back on its own: the test needs one nothing draws")
	}
	e.until("the history menu's DM named again", 20*time.Second, func() bool {
		en, ok := e.a.historyEntryFor(r)
		return ok && en.label != ""
	})
}
