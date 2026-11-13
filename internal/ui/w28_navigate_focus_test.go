package ui

import (
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestW28NavigateFocusesComposer: Slack's TRANSITION_FOCUS_NAVIGATE_LISTENER
// (client-boot) moves the focus to a channel's message input when a
// navigation opens it (getFocusKeyForView: MESSAGE_PANE_MESSAGE_INPUT,
// forced where you can post), except under an open overlay (n7.ZV) and for
// a permalink (the message list's). gap-w28 (GAPS-2026-11-12 gap 17).
func TestW28NavigateFocusesComposer(t *testing.T) {
	e := newActShell(t)
	focused := func(ch string) bool {
		cv := e.a.convos[ch+"/"]
		if cv == nil || cv.comp == nil {
			return false
		}
		ft, ok := cv.comp.(interface{ FocusTag() any })
		return ok && e.rt.Source().Focused(ft.FocusTag())
	}
	e.a.Navigate("c/" + fakeslack.ViewsChannel)
	e.until("the channel's composer focused", func() bool { return focused(fakeslack.ViewsChannel) })

	e.a.Navigate("c/D0FAKE00000")
	e.until("the DM's composer focused", func() bool { return focused("D0FAKE00000") })
	if focused(fakeslack.ViewsChannel) {
		t.Error("the old channel's composer kept the focus")
	}

	// a view that isn't a conversation takes it away (nothing to focus
	// here), and coming back focuses the composer again
	e.a.Navigate("activity")
	e.frames(5)
	e.a.Navigate("c/" + fakeslack.ViewsChannel)
	e.until("the composer focused again", func() bool { return focused(fakeslack.ViewsChannel) })

	// under the jumper the navigation doesn't move the focus
	e.a.openSwitcher()
	e.frames(3)
	n := len(e.a.overlays)
	if n == 0 {
		t.Fatal("no jumper")
	}
	e.a.Navigate("c/D0FAKE00000")
	e.frames(5)
	if focused("D0FAKE00000") {
		t.Error("a navigation under the open jumper focused the composer")
	}
	e.a.dismissTop()
	e.frames(3)

	// a permalink is the list's
	e.a.Navigate("c/" + fakeslack.ViewsChannel + "/m/1700000000.000100")
	e.frames(5)
	if focused(fakeslack.ViewsChannel) {
		t.Error("a permalink focused the composer")
	}
}
