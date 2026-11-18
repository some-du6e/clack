package ui

import (
	"log"

	"github.com/some-du6e/clack/internal/ui/messages"
)

// A thread pane never shows a reply as its root (Karim, α.105: a link
// to a reply in a bot's digest opened a thread pane whose only message
// was the reply). Slack's routes get a reply's thread first
// (jumpToMessage, jump.go), and so do Clack's; OpenThread turns a reply's
// ts core holds into its thread at the reply (secondary.go). What's left
// is a reply core didn't hold when the pane opened (a slack:// link
// naming a reply as its thread_ts, a route from before the fix): the
// pane loads conversations.replies for it, which answers the reply with
// its thread_ts, and the list sees that its root is a reply. It draws
// only its loading state and hands the thread on here.

// ReplyAsThreadRoot is messages.ReplyRootRedirector: the pane opened
// with reply's ts, a reply in thread root, goes to that thread at the
// reply, as jumpToMessage would have opened it (the split view's to the
// thread). Posted: the pane is mid-layout.
func (a *App) ReplyAsThreadRoot(ch, reply, root string) {
	pane := a.pane
	a.Post(func() {
		log.Printf("ui: thread pane: its root %s is a reply in %s, whose thread opens at it", logTag(reply), logTag(root))
		if pane == paneSplit {
			s := &a.split
			if n := len(s.stack); n > 0 && s.stack[n-1] == Route("c/"+ch+"/t/"+reply) {
				s.stack[n-1] = Route("c/" + ch + "/t/" + root)
				a.Invalidate()
			}
			return
		}
		if s := a.secondary; s.kind != secThread || s.channel != ch || s.ts != reply {
			return // the pane shows something else by now
		}
		a.withVia("its pane found the root was a reply", func() {
			a.openThreadAtIn(ch, root, reply, a.router.Current(), true)
		})
	})
}

var (
	_ messages.ReplyRootRedirector = (*App)(nil)
	_ messages.PermalinkOpener     = (*App)(nil)
)
