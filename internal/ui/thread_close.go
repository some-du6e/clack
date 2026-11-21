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
//   - a pane that shows a thread no more is ThreadsFlexpane's unmount
//     (boot-shared l6n6): clearStartTs, and truncateThreadHistory, which
//     lets the history of a thread in an archived or unjoined public
//     channel go, and keeps any other's newest 1,000 timestamps
//     (closeLeftThreads, once a frame).

// threadStartAt sets the start of the thread ch/thread to reply ts before
// its pane mounts.
func (a *App) threadStartAt(ch, thread, ts string) {
	if a.data == nil || a.data.core == nil || ts == "" {
		return
	}
	messages.SetThreadStart(a.data.core, ch, thread, ts)
}

// shownThreads is the threads a pane shows now, by "channel/ts": the
// secondary view's (a pane Ctrl+. hides unmounts, as thread-reply-focus's
// threadFocusUnmount has it) and the split view's.
func (a *App) shownThreads() map[string]bool {
	out := map[string]bool{}
	if s := &a.secondary; s.kind == secThread && s.open() && s.channel != "" && s.ts != "" {
		out[s.channel+"/"+s.ts] = true
	}
	if st := a.split.stack; len(st) > 0 {
		if r := st[len(st)-1]; r.Thread() != "" && r.Channel() != "" {
			out[r.Channel()+"/"+r.Thread()] = true
		}
	}
	return out
}

// closeLeftThreads closes the thread panes the last frame showed and this
// one doesn't (messages.CloseThread). UI goroutine, once a frame.
func (a *App) closeLeftThreads() {
	if a.data == nil || a.data.core == nil {
		return
	}
	now := a.shownThreads()
	for k := range a.threadsShown {
		if !now[k] {
			ch, ts, _ := strings.Cut(k, "/")
			messages.CloseThread(a.data.core, ch, ts)
		}
	}
	a.threadsShown = now
}
