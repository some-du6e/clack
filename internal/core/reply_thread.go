package core

import (
	"context"
	"net/url"

	"github.com/some-du6e/clack/internal/slack"
)

// Which thread to open for a message, the lookup Slack's jumpToMessage
// makes before it routes a message that came without a thread_ts (a
// permalink without ?thread_ts=, a slack://channel?message= deep link):
// boot-shared wc0P (build 2c0d02ca130f) reads the message from the
// store (iR00 getMessage), else ensureMessages (G+Ao) fetches it with
// messages.list (reason "jumpToMessage:resolveThreadTs"), one request
// per message however often it's asked (fetchMessagesByIds' debouncer),
// and the answer stays in the store. getThreadTsToOpen keeps its
// thread_ts for a reply (azCc isReply: a thread_ts that isn't the
// message's own ts, mun8 getMessageThreadTs), and with
// openThreadForRootMessage (every mrkdwn and rich_text link,
// BaseMrkdwnLink kQRT) for a thread's parent too, unless the
// conversation is a DM with Slackbot or a bot (DUy0 G). A reply then
// opens its parent's thread at the reply, a parent its own thread;
// anything else opens its conversation at the message.

// ReplyThread is the thread to open for message ts of ch, from the
// messages core holds, else from messages.list: a reply's thread, with
// openRoot a thread's parent's own, "" for anything else. found is false
// when neither has the message (it's gone, or the request failed), and
// Slack navigates without a thread then ("Failed to resolve thread_ts;
// navigating without it"). It may call Slack: not on the UI goroutine.
func (a *App) ReplyThread(ctx context.Context, ch, ts string, openRoot bool) (thread string, found bool) {
	if thread, found = a.CachedReplyThread(ch, ts, openRoot); found || ch == "" || ts == "" {
		return thread, found
	}
	_ = a.once("ml:"+ch+"/"+ts, func() error {
		got, _ := a.hydrateOK(ctx, map[string][]string{ch: {ts}})
		if m, ok := got[ch+"/"+ts]; ok {
			a.noteMsgThread(ch, ts, m.ThreadTS)
		}
		return nil
	})
	if t, ok := a.msgThreadOf(ch, ts); ok {
		return a.threadToOpen(ch, ts, t, openRoot), true
	}
	return "", false
}

// CachedReplyThread is ReplyThread from what core holds only: the cached
// timelines and threads, and the messages looked up before (no request,
// so any goroutine).
func (a *App) CachedReplyThread(ch, ts string, openRoot bool) (thread string, found bool) {
	if ch == "" || ts == "" {
		return "", false
	}
	if m, ok := a.S.Find(ch, ts); ok {
		return a.threadToOpen(ch, ts, m.ThreadTS, openRoot), true
	}
	if t, ok := a.msgThreadOf(ch, ts); ok {
		return a.threadToOpen(ch, ts, t, openRoot), true
	}
	return "", false
}

// ReplyParent is the parent of reply ts of ch, when core knows the
// message is a reply ("", false otherwise).
func (a *App) ReplyParent(ch, ts string) (string, bool) {
	if t, found := a.CachedReplyThread(ch, ts, false); found && t != "" {
		return t, true
	}
	return "", false
}

// threadToOpen is getThreadTsToOpen for message ts of ch with thread_ts
// thread.
func (a *App) threadToOpen(ch, ts, thread string, openRoot bool) string {
	switch {
	case thread == "":
		return ""
	case thread != ts: // isReply
		return thread
	case openRoot && !a.botDM(ch):
		return thread
	}
	return ""
}

// botDM is DUy0 G: an IM with Slackbot, USLACK or a bot user (isBotUser,
// nMbN: is_bot, Slackbot, USLACK; an app user isn't one).
func (a *App) botDM(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM {
		return false
	}
	switch c.User {
	case slackbotID, "USLACK":
		return true
	}
	u, ok := a.S.User(c.User)
	return ok && u != nil && (u.IsBot || u.IsSlackbot)
}

// noteMsgThread remembers message ts of ch has thread_ts thread ("" none).
func (a *App) noteMsgThread(ch, ts, thread string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.msgThread == nil || len(a.msgThread) >= 4096 {
		a.msgThread = map[string]string{}
	}
	a.msgThread[ch+"/"+ts] = thread
}

// forgetMsgThread drops what was remembered of message ts of ch: it got
// replies, or a new copy (message_replied, message_changed), so a parent
// looked up before it had any isn't left without its thread (Slack's
// store keeps the message, and messageReplied updates it there).
func (a *App) forgetMsgThread(ch, ts string) {
	a.mu.Lock()
	delete(a.msgThread, ch+"/"+ts)
	a.mu.Unlock()
}

// msgThreadOf is what noteMsgThread remembered for message ts of ch.
func (a *App) msgThreadOf(ch, ts string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.msgThread[ch+"/"+ts]
	return t, ok
}

// replyParentIn is the parent of reply ts in a conversations.replies
// answer for ts (conversations.replies' ts is "a thread's parent message
// or a message in the thread": a reply's ts answers with its whole
// thread, parent first): "" unless the answer holds the reply, as one,
// and its parent.
func replyParentIn(all []slack.Message, ts string) string {
	parent := ""
	for i := range all {
		if all[i].TS == ts && all[i].ThreadTS != "" && all[i].ThreadTS != ts {
			parent = all[i].ThreadTS
		}
	}
	if parent == "" {
		return ""
	}
	for i := range all {
		if all[i].TS == parent {
			return parent
		}
	}
	return ""
}

// replyRooted is a replies fetch for a reply's ts: the thread is its
// parent's (ReplyParent), so the parent's key holds it, nothing is kept
// or watched under the reply's, and the reply's thread_ts is
// remembered.
func (a *App) replyRooted(ch, ts, parent string) {
	a.noteMsgThread(ch, ts, parent)
	a.unwatch(ch, ts)
}

// LatestReply is resolveLatestThreadReplyTs (cdTA n9): the newest reply
// in thread of ch, from conversations.replies limit 1 (reason
// "permalink:resolveLatestThreadReply") and its root's latest_reply,
// else the root core holds; the thread itself when there is none newer.
func (a *App) LatestReply(ctx context.Context, ch, thread string) string {
	after := func(latest string) string { // replyAfterRoot
		if latest != "" && tsLess(thread, latest) {
			return latest
		}
		return thread
	}
	var res struct {
		Messages []slack.Message `json:"messages"`
	}
	p := url.Values{"channel": {ch}, "ts": {thread}, "limit": {"1"}}
	if err := a.C.Call(ctx, "conversations.replies", p, &res); err == nil {
		for i := range res.Messages {
			if res.Messages[i].TS == thread {
				return after(res.Messages[i].LatestReply)
			}
		}
	}
	if m, ok := a.S.Find(ch, thread); ok {
		return after(m.LatestReply)
	}
	return thread
}
