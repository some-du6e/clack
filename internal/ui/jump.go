package ui

import "github.com/some-du6e/clack/internal/ui/views"

// jumpToMessage is Slack's jumpToMessage (boot-shared wc0P, "Jumps to a
// message in the client") and the jumpViaRoutes it ends in, for a message
// in this workspace: what a notification's click opens (notificationClicked
// dispatches it with the notification's channel, ts and thread_ts).
//
//   - With a thread: toThread({channelId, threadTs, replyTs: ts,
//     highlightRoot: true}). The conversation goes in the primary and the
//     thread in the secondary, and the navigation's jumpToThreadReply
//     (client-boot 6IaZ) starts the thread pane at the reply and
//     highlights it (openThreadAt's rule), and for highlightRoot moves the
//     primary conversation to the root (startTs) and highlights it there.
//   - With a ts: toMessage(ch, ts, ts) (jpad R9): the conversation started
//     at the message, which is highlighted (startTs, highlightTs). That is
//     Clack's permalink route, c/<id>/m/<ts>.
//   - Neither: toChannel.
//
// jumpToMessage doesn't look a reply up by its ts alone (getThreadTsToOpen):
// every caller here has the thread_ts already. UI goroutine.
func (a *App) jumpToMessage(ch, ts, thread string) {
	if ch == "" {
		return
	}
	// a canvas's file channel is its canvas (canvas_open.go), outside the
	// split view (OpenSplit has its own rule); this whole jump is the
	// fallback for a file that turns out to be no canvas, so a list's
	// thread still opens at the reply
	if (thread != "" || ts != "") && !(a.pane == paneSplit || a.splitClick()) &&
		a.canvasInstead(ch, thread, ts, views.CanvasFull, func() { a.jumpToMessage(ch, ts, thread) }) {
		return
	}
	prev := a.router.Current()
	switch {
	case thread != "":
		a.Navigate("c/" + ch + "/t/" + thread)
		if a.pane == paneSplit || a.router.Current().Channel() != ch {
			return // the split view took it (OpenSplit): it has no reply to start at
		}
		// highlightRoot: the primary conversation at the root, highlighted
		if h, ok := a.convoFor(ch, "").list.(interface{ Highlight(string) }); ok {
			h.Highlight(thread)
		}
		if ts != "" {
			a.openThreadAt(ch, thread, ts, prev)
		}
	case ts != "":
		a.Navigate("c/" + ch + "/m/" + ts)
	default:
		a.Navigate("c/" + ch)
	}
}

// JumpPane is views.PaneJumper: the Activity persistent detail panel's
// jumpToMessage, in the panes the view draws (ChannelPane, ThreadPane):
// the conversation started at ts and ts highlighted, or the thread
// started at reply ts, highlighted (the Primary is the panel's, so
// jumpToThreadReply's highlight rule holds: nothing there is the thread's
// conversation). UI goroutine.
func (a *App) JumpPane(ch, thread, ts string) {
	if ch == "" || ts == "" {
		return
	}
	a.canvasNavigated() // a canvas jump still waiting (canvas_open.go) is dropped
	if thread != "" {
		if j, ok := a.convoFor(ch, thread).list.(interface{ JumpTo(ts string, flash bool) }); ok {
			j.JumpTo(ts, true)
		}
	} else if h, ok := a.convoFor(ch, "").list.(interface{ Highlight(string) }); ok {
		h.Highlight(ts)
	}
	a.Invalidate()
}

var _ views.PaneJumper = (*App)(nil)
