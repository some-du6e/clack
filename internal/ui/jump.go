package ui

import (
	"context"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// jumpToMessage is Slack's jumpToMessage (boot-shared wc0P, "Jumps to a
// message in the client", build 2c0d02ca130f) and the jumpViaRoutes it
// ends in, for a message in this workspace: what a notification's click
// (notificationClicked), a link's (openPermalink), a message_mention
// pill's (MessageMention, p9lB) and a slack://channel deep link's
// (handleParsedDeepLink) open, each with the channel, the message's ts
// and the thread_ts it came with.
//
// A message that came without a thread_ts is looked up first: the
// message comes from what core holds, else from messages.list
// (ensureMessages, reason "jumpToMessage:resolveThreadTs";
// core.ReplyThread), and when neither has it Slack goes on without a
// thread ("Failed to resolve thread_ts; navigating without it").
// getThreadTsToOpen then opens a reply's thread, and with openRoot (a
// mrkdwn or rich_text link: BaseMrkdwnLink's openThreadForRootMessage) a
// thread's parent's too, unless the conversation is a DM with Slackbot
// or a bot. So a link to a reply opens the reply's thread, never a thread
// with the reply as its root. Then jumpViaRoutes:
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
// In the split view (a click inside it, or the split modifier click) the
// route goes on its back stack and the thread starts at the reply there.
// UI goroutine; with a lookup to make, the navigation comes later.
func (a *App) jumpToMessage(ch, ts, thread string, openRoot bool) {
	a.jumpMessage(ch, ts, thread, lookupFetch, openRoot)
}

// threadLookup is how far jumpMessage looks for a message's thread_ts.
type threadLookup uint8

const (
	lookupFetch threadLookup = iota // the store, then messages.list
	// lookupStore: the store only. A notification's click (notificationClicked)
	// has the message's thread_ts in its payload whenever the message is a
	// reply (messageNote reads it off the message), and the message Slack's
	// lookup finds is the one in its store the notification was for.
	lookupStore
)

// jumpMessage is jumpToMessage with its lookup.
func (a *App) jumpMessage(ch, ts, thread string, lookup threadLookup, openRoot bool) {
	if ch == "" {
		return
	}
	// a file channel's message is its canvas or its list, in the
	// container the jump starts in (canvas_open.go jumpWhere). This whole
	// jump is the fallback for a file that turns out to be neither, so a
	// thread still opens at the reply
	if (thread != "" || ts != "") && a.jumpInstead(ch, thread, ts, a.jumpWhere(), func() { a.jumpMessage(ch, ts, thread, lookup, openRoot) }) {
		return
	}
	via := a.navVia()
	if thread == "" && ts != "" {
		if c := a.Core(); c != nil {
			if t, found := c.CachedReplyThread(ch, ts, openRoot); found {
				thread = t
			} else if lookup == lookupFetch {
				a.jumpAfterLookup(c, ch, ts, openRoot, via)
				return
			}
		}
	}
	a.withVia(via, func() { a.jumpViaRoutes(ch, ts, thread) })
}

// jumpAfterLookup is jumpToMessage once messages.list answered. The
// click's container goes with the lookup: from inside the split view the
// route goes on its back stack (unless the view closed meanwhile), a
// split modifier click opens it in the split view.
func (a *App) jumpAfterLookup(c *core.App, ch, ts string, openRoot bool, via string) {
	fromSplit := a.pane == paneSplit
	splitClick := !fromSplit && a.splitClick()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		t, found := c.ReplyThread(ctx, ch, ts, openRoot)
		a.Post(func() {
			if !found {
				log.Printf("ui: jumpToMessage: %s's thread_ts wasn't found; navigating without it (%s)", logTag(ts), via)
			}
			a.withVia(via, func() {
				switch {
				case fromSplit && a.splitOpen():
					a.inPane(paneSplit, func() { a.jumpViaRoutes(ch, ts, t) })
				case splitClick:
					a.jumpSplit(ch, ts, t)
				default:
					a.jumpViaRoutes(ch, ts, t)
				}
			})
		})
	}()
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
	ts, thread = a.replyAsThread(ch, ts, thread)
	if (a.pane == paneSplit || a.splitClick()) && a.SplitViewOK() {
		a.jumpSplit(ch, ts, thread)
		return
	}
	log.Printf("ui: jumpToMessage: %s %s thread %s (%s)", logTag(ch), logTag(ts), logTag(thread), a.navVia())
	prev := a.router.Current()
	switch {
	case thread != "":
		if ts != "" && a.pane != paneSplit && !a.splitClick() {
			a.threadStartAt(ch, thread, ts) // jumpToThreadReply: before the pane mounts (thread_close.go)
		}
		// highlightRoot gives the primary a startTs (the root), so the
		// navigation isn't an entry without one (determineTimestamps)
		a.navHasStart = true
		a.Navigate(jumpRoute(ch, ts, thread))
		a.navHasStart = false
		if a.router.Current().Channel() != ch {
			return
		}
		// highlightRoot: the primary conversation at the root, highlighted.
		// Its highlightLazy holds only where the primary was showing this
		// conversation (MessageList's lazyScroll: the same channel and the
		// root inside the range it held, else the anchor puts the root on
		// the 64px line): from anywhere else the root is anchored.
		if h, ok := a.convoFor(ch, "").list.(interface {
			Highlight(string)
			HighlightLazy(string)
		}); ok {
			if prev.Channel() == ch {
				h.HighlightLazy(thread)
			} else {
				h.Highlight(thread)
			}
		}
		if ts != "" {
			a.openThreadAt(ch, thread, ts, prev)
		}
	default:
		a.Navigate(jumpRoute(ch, ts, ""))
	}
}

// replyAsThread is the guard for a thread_ts naming a reply core holds
// (slack://channel?thread_ts=<a reply>, which Slack's ThreadView draws
// with the reply as its root and its whole thread under it): the reply's
// thread instead, at the reply, its root highlighted in the conversation
// rather than the reply the timeline never holds. A reply core doesn't
// hold yet is its pane's to find (ReplyAsThreadRoot). docs/PARITY.md's
// deliberate differences.
func (a *App) replyAsThread(ch, ts, thread string) (string, string) {
	c := a.Core()
	if c == nil || thread == "" {
		return ts, thread
	}
	root, ok := c.ReplyParent(ch, thread)
	if !ok {
		return ts, thread
	}
	log.Printf("ui: jumpToMessage: its thread_ts %s is a reply in %s, whose thread opens at it (%s)", logTag(thread), logTag(root), a.navVia())
	if ts == "" {
		ts = thread
	}
	return ts, root
}

// jumpSplit is jumpViaRoutes in the split view: the route on its back
// stack (OpenSplit; from the main window a new split view), and a
// thread started at the reply, highlighted (toThread's replyTs).
func (a *App) jumpSplit(ch, ts, thread string) {
	log.Printf("ui: jumpToMessage: %s %s thread %s in the split view (%s)", logTag(ch), logTag(ts), logTag(thread), a.navVia())
	a.OpenSplit(jumpRoute(ch, ts, thread))
	if thread != "" && ts != "" {
		a.splitAtReply(ch, thread, ts, true)
	}
}

// splitAtReply starts the split view's thread at reply, highlighted with
// flash (jumpToThreadReply's startTs and highlightTs, in the Split
// container).
func (a *App) splitAtReply(ch, thread, reply string, flash bool) {
	if !a.splitOpen() || a.splitTop() != Route("c/"+ch+"/t/"+thread) {
		return
	}
	a.inPane(paneSplit, func() {
		if j, ok := a.convoFor(ch, thread).list.(interface{ JumpTo(ts string, flash bool) }); ok {
			j.JumpTo(reply, flash)
		}
	})
	a.Invalidate()
}

// jumpLatest is a thread link to its latest reply (slack://channel
// ?message=latest&thread_ts=, /archives/<C>/latest?thread_ts=): cdTA's
// Mz. Under thread_latest_reply_permalink the thread opens at its newest
// reply (resolveLatestThreadReplyTs: conversations.replies limit 1, the
// root's latest_reply, the store's when that fails); otherwise at its
// root ({ts: threadTs, threadTs}).
func (a *App) jumpLatest(ch, thread string) {
	c := a.Core()
	if c == nil || c.Experiment("thread_latest_reply_permalink") != "on" {
		a.jumpToMessage(ch, thread, thread, false)
		return
	}
	via := a.navVia()
	fromSplit := a.pane == paneSplit
	splitClick := !fromSplit && a.splitClick() // the modifier is gone once the reply's known
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		latest := c.LatestReply(ctx, ch, thread)
		a.Post(func() {
			a.withVia(via, func() {
				switch {
				case fromSplit && a.splitOpen():
					a.inPane(paneSplit, func() { a.jumpViaRoutes(ch, latest, thread) })
				case splitClick:
					a.jumpSplit(ch, latest, thread)
				default:
					a.jumpViaRoutes(ch, latest, thread)
				}
			})
		})
	}()
}

// OpenPermalink is Slack's openPermalink for a link to a message or a
// conversation (messages.PermalinkOpener, the canvases' and slackc's
// link clicks): openPermalinkInClient (boot-shared 0S61) parses it as
// parsePermalink does (client-boot KyTn: /archives/<C>/p<ts>, the
// thread from ?thread_ts= in formatMessageTimestamp's forms, ETk/, and
// /archives/<C>/latest?thread_ts= for the thread's latest reply) and
// hands {channelId, ts, threadTs} to jumpToMessage, which finds the
// thread to open when the link has none (bots that build links by hand
// leave it out). openRoot is the link's openThreadForRootMessage (a
// mrkdwn or rich_text link's, BaseMrkdwnLink kQRT). A link into another
// workspace (its subdomain isn't this one's and the conversation isn't
// one Clack knows) is jumpToMessage's "Could not find suitable target
// workspace", which openPermalink opens in a browser tab: false, as for
// anything that isn't a link to a message or a conversation (the caller
// opens it as it would any other). UI goroutine.
func (a *App) OpenPermalink(u string, openRoot bool) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	thread := slackTimestamp(p.Query().Get("thread_ts")) // its own ts too: that message's thread
	if m := permalinkLatest.FindStringSubmatch(p.Path); m != nil && core.IsSlackURL(u) {
		return a.openLinkIn(u, p, m[1], func() {
			if thread != "" {
				a.jumpLatest(m[1], thread)
			} else {
				a.jumpToMessage(m[1], "", "", false) // "latest" names no message: the conversation
			}
		})
	}
	pl, ok := core.ParsePermalink(u)
	if !ok || pl.Kind != "message" && pl.Kind != "channel" {
		return false
	}
	// a link with only a thread (/archives/<C>?thread_ts=) is toThread
	// without a reply: jumpToMessage's `if (ed || !i)` branch
	return a.openLinkIn(u, p, pl.ChannelID, func() { a.jumpToMessage(pl.ChannelID, pl.TS, thread, openRoot) })
}

// openLinkIn opens a link to conversation ch with jump, once its
// workspace is this one (linkWorkspace): false for another workspace's
// (the caller's browser tab); on Enterprise Grid a link from another of
// the org's workspaces opens once conversations.info finds the
// conversation, else in the browser.
func (a *App) openLinkIn(u string, p *url.URL, ch string, jump func()) bool {
	via := "permalink from " + callerOutsideRouter()
	switch a.linkWorkspace(p, ch) {
	case linkForeign:
		return false
	case linkLookup:
		c, pane, mods := a.Core(), a.pane, a.clickMods
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, found := c.ResolveConv(ctx, ch)
			a.Post(func() {
				if !found {
					openURL(u) // jumpToMessage's "Could not find suitable target workspace": a new tab
					return
				}
				a.WithClickMods(mods, func() {
					a.withVia(via, func() {
						if pane == paneSplit && a.splitOpen() {
							a.inPane(paneSplit, jump)
							return
						}
						jump()
					})
				})
			})
		}()
		return true
	}
	a.withVia(via, jump)
	return true
}

// linkPlace is where a link's conversation is (linkWorkspace).
type linkPlace uint8

const (
	linkLocal   linkPlace = iota // this workspace's: opens in the client
	linkForeign                  // another workspace's: the browser
	linkLookup                   // another of the Grid org's workspaces: looked up first
)

// linkWorkspace is jumpToMessage's target workspace for a link to
// conversation ch (wc0P: the channel's team, else the org's team with the
// link's subdomain): this workspace when the link's subdomain is its own,
// the org's enterprise domain, app or slack.com's, or core knows the
// conversation. On Enterprise Grid, where any of the org's workspaces can
// hold it, a link with another subdomain is looked up (linkLookup);
// elsewhere it's another workspace's.
func (a *App) linkWorkspace(p *url.URL, ch string) linkPlace {
	host := strings.ToLower(p.Hostname())
	sub, _, _ := strings.Cut(host, ".")
	if host == "slack.com" || sub == "app" {
		return linkLocal
	}
	c := a.Core()
	if c == nil {
		return linkLocal
	}
	c.S.RLock()
	dom := c.S.TeamDomain
	c.S.RUnlock()
	if dom == "" || strings.EqualFold(sub, dom) {
		return linkLocal
	}
	if _, ok := c.S.Conv(ch); ok {
		return linkLocal
	}
	if _, ok := c.S.ExtConv(ch); ok {
		return linkLocal
	}
	if c.C == nil || c.C.Creds.EnterpriseID == "" {
		return linkForeign
	}
	if e, err := url.Parse(c.C.Creds.EnterpriseURL); err == nil && e.Hostname() != "" && strings.EqualFold(e.Hostname(), host) {
		return linkLocal // the org's own domain (acme.enterprise.slack.com)
	}
	return linkLookup
}

// permalinkLatest is parsePermalink's /archives/<C>/latest (a thread's
// latest reply, with ?thread_ts=).
var permalinkLatest = regexp.MustCompile(`^/archives/([CDG][A-Z0-9]+)/latest/?$`)

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

// FollowLink is Slack's Link click (boot-shared jRB6) for a link Clack
// draws (messages.LinkFollower, views' rows, the topic card, search
// cards, Block Kit surfaces, canvases, slackc, toasts): a slack: link goes
// to handleDeepLink (eUmj U; slack://open and slack://room go to
// window.open, which the OS hands back to the app: the same handler),
// and openPermalink opens the kinds of Slack link it parses in the client
// (0S61 openPermalinkInClient): a canvas, a channel's tab, a list, and a
// message or a conversation through jumpToMessage (openRoot:
// BaseMrkdwnLink's flag). false: the caller opens it in the browser, as
// openPermalink opens every other link in a new tab. UI goroutine.
func (a *App) FollowLink(u string, openRoot bool) bool {
	if isSlackScheme(u) {
		a.OpenURL(u)
		return true
	}
	if a.OpenCanvasLink(u) || a.OpenChannelTabLink(u) || a.OpenListLink(u) {
		return true
	}
	return a.OpenPermalink(u, openRoot)
}

// isSlackScheme is a slack: link, which only the app opens.
func isSlackScheme(u string) bool {
	return len(u) > 6 && strings.EqualFold(u[:6], "slack:")
}

// JumpToMessage is a message_mention pill's click (messages.MessageJumper,
// p9lB MessageMention): jumpToMessage({channelId, ts: message_ts,
// threadTs: thread_ts}), without openThreadForRootMessage; the pill's
// thread_ts is the parent's own ts for a thread's parent (KwXc
// hydrateMessageSlugData), so that opens the thread too. UI goroutine.
func (a *App) JumpToMessage(ch, ts, thread string) {
	// the pill's ids as a deep link's are checked: a conversation id and
	// formatMessageTimestamp's ts, else nothing to jump to
	if ts = slackTimestamp(ts); ts == "" || !slackIDRE.MatchString(ch) || !strings.ContainsAny(ch[:1], "CDG") {
		return
	}
	a.withVia("message mention from "+callerOutsideRouter(), func() { a.jumpToMessage(ch, ts, slackTimestamp(thread), false) })
}

// OpenAtMessage is a navigation to a message whose thread is known
// (popups.MessageOpener): a search result's Jump (messageActionsJumpAction
// in the SEARCH container: toThread({channelId, threadTs, replyTs}) for a
// reply), Activity's Open in home (toThread with highlightRoot); a
// message that isn't a reply opens its conversation at it (toMessage).
// UI goroutine.
func (a *App) OpenAtMessage(ch, ts, thread string, highlightRoot bool) {
	if ch == "" {
		return
	}
	a.withVia("a message from "+callerOutsideRouter(), func() {
		if highlightRoot || thread == "" || (a.pane == paneSplit || a.splitClick()) && a.SplitViewOK() {
			a.jumpViaRoutes(ch, ts, thread)
			return
		}
		log.Printf("ui: jumpToMessage: %s %s thread %s (%s)", logTag(ch), logTag(ts), logTag(thread), a.navVia())
		prev := a.router.Current()
		a.Navigate(jumpRoute(ch, ts, thread))
		if ts != "" && a.router.Current().Channel() == ch {
			a.openThreadAt(ch, thread, ts, prev)
		}
	})
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
		a.threadStartAt(ch, thread, ts) // before convoFor mounts the pane (thread_close.go)
		if j, ok := a.convoFor(ch, thread).list.(interface{ JumpTo(ts string, flash bool) }); ok {
			j.JumpTo(ts, true)
		}
	} else if h, ok := a.convoFor(ch, "").list.(interface{ Highlight(string) }); ok {
		h.Highlight(ts)
	}
	a.Invalidate()
}

var (
	_ views.PaneJumper     = (*App)(nil)
	_ popups.MessageOpener = (*App)(nil)
)
