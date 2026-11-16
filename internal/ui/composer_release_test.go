package ui

import (
	"strings"
	"testing"
	"time"

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
	e.a.OpenThread(ch, root)
	e.frame()
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
	e.a.OpenThread(ch, root)
	e.frame()
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
}
