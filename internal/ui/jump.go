package ui

import (
	"context"
	"log"
	"net/url"
	"regexp"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/views"
)

// jumpToMessage is Slack's jumpToMessage (boot-shared wc0P, "Jumps to a
// message in the client", build 2c0d02ca130f) and the jumpViaRoutes it
// ends in, for a message in this workspace: what a notification's click
// (notificationClicked), a message link's (openPermalink) and a
// slack://channel deep link's (handleParsedDeepLink) open, each with
// the channel, the message's ts and the thread_ts it came with.
//
// A message that came without a thread_ts is looked up first: the
// thread it is a reply in (getThreadTsToOpen) comes from the messages
// core holds, else from messages.list (ensureMessages, reason
// "jumpToMessage:resolveThreadTs"; core.ReplyThread), and when neither
// has it Slack goes on without a thread ("Failed to resolve thread_ts;
// navigating without it"). So a link to a reply opens the reply's
// thread, never a thread with the reply as its root. Then jumpViaRoutes:
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
// UI goroutine; with a lookup to make, the navigation comes later.
func (a *App) jumpToMessage(ch, ts, thread string) { a.jumpMessage(ch, ts, thread, true) }

// jumpMessage is jumpToMessage; resolve: a message without a thread_ts
// is looked up. A notification's click needs no lookup
// (notificationClicked): its payload has the message's thread_ts
// whenever the message is a reply (messageNote reads it off the
// message), and the message Slack's lookup finds is the one in its store
// that the notification was for.
func (a *App) jumpMessage(ch, ts, thread string, resolve bool) {
	if ch == "" {
		return
	}
	via := a.navVia()
	if resolve && thread == "" && ts != "" {
		if c := a.Core(); c != nil {
			if t, found := c.CachedReplyThread(ch, ts); found {
				thread = t
			} else {
				// the click's container goes with the lookup: a click in
				// the split view (or a split modifier click) lands there
				split := a.pane == paneSplit || a.splitClick()
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					t, found := c.ReplyThread(ctx, ch, ts)
					a.Post(func() {
						if !found {
							log.Printf("ui: jumpToMessage: %s's thread_ts wasn't found; navigating without it (%s)", logTag(ts), via)
						}
						a.withVia(via, func() {
							if split {
								log.Printf("ui: jumpToMessage: %s %s thread %s in the split view (%s)", logTag(ch), logTag(ts), logTag(t), via)
								a.OpenSplit(jumpRoute(ch, ts, t))
								return
							}
							a.jumpViaRoutes(ch, ts, t)
						})
					})
				}()
				return
			}
		}
	}
	a.withVia(via, func() { a.jumpViaRoutes(ch, ts, thread) })
}

// jumpRoute is jumpViaRoutes' route: the thread, else the conversation at
// the message, else the conversation.
func jumpRoute(ch, ts, thread string) string {
	switch {
	case thread != "":
		return "c/" + ch + "/t/" + thread
	case ts != "":
		return "c/" + ch + "/m/" + ts
	}
	return "c/" + ch
}

// jumpViaRoutes is jumpToMessage's navigation, its thread known (or none).
func (a *App) jumpViaRoutes(ch, ts, thread string) {
	log.Printf("ui: jumpToMessage: %s %s thread %s (%s)", logTag(ch), logTag(ts), logTag(thread), a.navVia())
	prev := a.router.Current()
	switch {
	case thread != "":
		a.Navigate(jumpRoute(ch, ts, thread))
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
	default:
		a.Navigate(jumpRoute(ch, ts, ""))
	}
}

// OpenPermalink is Slack's openPermalink for a link to a message or a
// conversation (messages.PermalinkOpener, the canvases' and slackc's
// link clicks): openPermalinkInClient (boot-shared 0S61) parses it as
// parsePermalink does (client-boot KyTn: /archives/<C>/p<ts>, the
// thread from ?thread_ts= in formatMessageTimestamp's forms, ETk/) and
// hands {channelId, ts, threadTs} to jumpToMessage, which finds a
// reply's thread when the link has no thread_ts (bots that build links
// by hand leave it out). false: not a link to a message or a
// conversation (the caller opens it as it would any other). UI
// goroutine.
func (a *App) OpenPermalink(u string) bool {
	pl, ok := core.ParsePermalink(u)
	if !ok || pl.Kind != "message" && pl.Kind != "channel" {
		return false
	}
	thread := ""
	if p, err := url.Parse(u); err == nil {
		thread = slackTimestamp(p.Query().Get("thread_ts")) // its own ts too: that message's thread
	}
	if pl.TS == "" {
		thread = ""
	}
	a.withVia("permalink from "+callerOutsideRouter(), func() { a.jumpToMessage(pl.ChannelID, pl.TS, thread) })
	return true
}

// messageTimestampRE is formatMessageTimestamp's (ETk/): "p" optional,
// the dot optional.
var messageTimestampRE = regexp.MustCompile(`^p?(\d{10})\.?(\d{6})$`)

// slackTimestamp is formatMessageTimestamp: a message ts in Slack's
// form, "" for anything else.
func slackTimestamp(s string) string {
	if !messageTimestampRE.MatchString(s) {
		return ""
	}
	return messageTimestampRE.ReplaceAllString(s, "$1.$2")
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
