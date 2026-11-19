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
// reply, highlighted, as jumpToMessage would have opened it: in the
// split view its route is swapped on the view's stack, in the secondary
// pane the route the reply's pushed is swapped too. Posted: the pane is
// mid-layout. A pane that shows something else by then is left alone
// (its list asks again when it next opens with the reply).
func (a *App) ReplyAsThreadRoot(ch, reply, root string) {
	pane := a.pane
	a.Post(func() {
		if pane == paneSplit {
			s := &a.split
			if n := len(s.stack); n > 0 && s.stack[n-1] == Route("c/"+ch+"/t/"+reply) {
				log.Printf("ui: thread pane: its root %s is a reply in %s, whose thread opens at it in the split view", logTag(reply), logTag(root))
				s.stack[n-1] = Route("c/" + ch + "/t/" + root)
				a.splitAtReply(ch, root, reply, true)
				a.Invalidate()
			}
			return
		}
		if s := a.secondary; s.kind != secThread || s.channel != ch || s.ts != reply {
			return // the pane shows something else by now
		}
		log.Printf("ui: thread pane: its root %s is a reply in %s, whose thread opens at it", logTag(reply), logTag(root))
		a.withVia("its pane found the root was a reply", func() {
			a.openThreadAtIn(ch, root, reply, "", true) // the reply highlighted (OpenThread's guard)
			a.leaveReplyRoute(ch, reply, root)
			a.rootForReplyHighlight(ch, reply, root)
		})
	})
}

// rootForReplyHighlight: the conversation beside a handed-on pane was
// asked to highlight the reply as a thread's root (toThread's
// highlightRoot with the reply's ts, a message the conversation never
// holds, so the jump waits for it or gives up); it highlights the
// thread's root, as toThread for the reply's thread does, whether that
// jump is still waiting or not.
func (a *App) rootForReplyHighlight(ch, reply, root string) {
	if a.router.Current().Channel() != ch {
		return
	}
	c := a.convos[ch+"/"]
	if c == nil {
		return
	}
	if h, ok := c.list.(interface {
		HighlightRequested() string
		HighlightLazy(ts string)
	}); ok && h.HighlightRequested() == reply {
		h.HighlightLazy(root) // highlightRoot's highlightLazy
	}
}

var (
	_ messages.ReplyRootRedirector = (*App)(nil)
	_ messages.LinkFollower        = (*App)(nil)
	_ messages.MessageJumper       = (*App)(nil)
)
