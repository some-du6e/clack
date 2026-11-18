package core

import (
	"context"

	"github.com/some-du6e/clack/internal/slack"
)

// Which thread a message is a reply in, the lookup Slack's jumpToMessage
// makes before it routes a message that came without a thread_ts (a
// permalink without ?thread_ts=, a slack://channel?message= deep link):
// boot-shared wc0P (build 2c0d02ca130f) reads the message from the
// store (iR00 getMessage), else ensureMessages (G+Ao) fetches it with
// messages.list (reason "jumpToMessage:resolveThreadTs"), and
// getThreadTsToOpen keeps its thread_ts only for a reply (azCc isReply:
// a thread_ts that isn't the message's own ts, mun8
// getMessageThreadTs). A reply then opens its parent's thread at the
// reply; anything else opens its conversation at the message.

// ReplyThread is the thread message ts of ch is a reply in, from the
// messages core holds, else from messages.list: "" for a message that
// isn't a reply. found is false when neither has the message (it's gone,
// or the request failed), and Slack navigates without a thread then
// ("Failed to resolve thread_ts; navigating without it"). It may call
// Slack: not on the UI goroutine.
func (a *App) ReplyThread(ctx context.Context, ch, ts string) (thread string, found bool) {
	if thread, found = a.CachedReplyThread(ch, ts); found || ch == "" || ts == "" {
		return thread, found
	}
	got, _ := a.hydrateOK(ctx, map[string][]string{ch: {ts}})
	m, ok := got[ch+"/"+ts]
	if !ok {
		return "", false
	}
	return replyThreadOf(&m), true
}

// CachedReplyThread is ReplyThread from the messages core holds only
// (timelines and threads; no request, so any goroutine).
func (a *App) CachedReplyThread(ch, ts string) (thread string, found bool) {
	if ch == "" || ts == "" {
		return "", false
	}
	m, ok := a.S.Find(ch, ts)
	if !ok {
		return "", false
	}
	return replyThreadOf(&m), true
}

// replyThreadOf is getThreadTsToOpen: a reply's thread_ts, else "".
func replyThreadOf(m *slack.Message) string {
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		return m.ThreadTS
	}
	return ""
}
