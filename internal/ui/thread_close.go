package ui

import (
	"strings"

	"github.com/some-du6e/clack/internal/ui/messages"
)

// A thread pane's mount and unmount, for its history (messages
// thread_load.go, core thread_pages.go):
//
//   - before a pane opens at a reply, the navigation's jumpToThreadReply
//     (client-boot eCxs) sets the thread's start, so the pane's first load
//     reads around the reply and nothing else (threadStartAt);
//   - a pane that shows another thread, or none, left the thread it showed:
//     truncateThreadHistory, which lets the history of a thread in an
//     archived or unjoined public channel go and keeps any other's newest
//     1,000 timestamps, and when the pane unmounted clearStartTs too
//     (threadPaneLeft, from threadFocusUnmount once a frame).

// threadStartAt sets the start of the thread ch/thread to reply ts before
// its pane mounts.
func (a *App) threadStartAt(ch, thread, ts string) {
	if a.data == nil || a.data.core == nil || ts == "" {
		return
	}
	messages.SetThreadStart(a.data.core, ch, thread, ts)
}

// threadPaneLeft is what a ThreadsFlexpane does as it stops showing the
// thread prev (now: the thread it shows instead, "" when it unmounted;
// other: the thread the other pane, side or split, shows): Slack's
// componentDidUpdate on a thread switch and componentWillUnmount call
// truncateThreadHistory for the thread left (boot-shared l6n6), and the
// unmount clears its startTs too (clearStartTs). One thread the other
// pane still shows is left alone. threadFocusUnmount (thread_focus_input.go)
// calls it once a frame, for both panes, with the unmount's
// cancelFocusInput. UI goroutine.
func (a *App) threadPaneLeft(prev, now, other string) {
	if prev == "" || prev == now || prev == other || a.data == nil || a.data.core == nil {
		return
	}
	ch, ts, _ := strings.Cut(prev, "/")
	messages.CloseThread(a.data.core, ch, ts, now == "")
}
