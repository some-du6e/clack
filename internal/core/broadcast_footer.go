package core

import "github.com/some-du6e/clack/internal/slack"

// Slack's "View newer replies" under a thread broadcast in its channel
// (build c07ab895506f, 2026-11-15): BroadcastFooter (boot-shared NnKv)
// draws ThreadBroadcastFooter for a message that isThreadBroadcast (R+b2
// SM: subtype thread_broadcast) with a thread_ts and a channel, and that
// draws its ButtonLink only when KYIV k holds:
//
//	root = messages[channel][msg.root.ts]
//	(getLatestReply(root) || 0) > msg.ts && isThreadBroadcast(msg)
//
// getLatestReply (client-boot p6HR M$) is latest_reply, else the newest
// ts of an old-style replies list; the ts strings compare as strings.
// The root Slack reads is its stored parent, which a broadcast's own
// root object keeps current (udbq makeBroadcastRootObject merges it into
// the store) and message_replied / the root's message_changed move on.
// Clack keeps the parent when it has it and otherwise the broadcast's
// root copy, which syncBroadcastRoots moves the same way.

// newerReplies is KYIV k for m.
func (a *App) newerReplies(ch string, m *slack.Message) bool {
	if m.Subtype != "thread_broadcast" || m.ThreadTS == "" || firstNonEmpty(ch, m.Channel) == "" || m.Root == nil {
		return false
	}
	rootTS := m.Root.TS // msg.root.ts; "" finds nothing
	if rootTS == "" {
		return false
	}
	latest := m.Root.LatestReply
	if p, ok := a.S.Find(firstNonEmpty(ch, m.Channel), rootTS); ok {
		latest = p.LatestReply
	}
	return latest != "" && latest > m.TS
}

// syncBroadcastRoots applies f (the parent's new reply metadata) to the
// root copies of thread root's broadcasts in ch and announces those
// broadcasts again: whether they show "View newer replies" follows the
// thread's latest reply.
func (a *App) syncBroadcastRoots(ch, root string, f func(*slack.Message)) {
	for _, b := range a.S.MutateBroadcastRoots(ch, root, f) {
		a.emitUpdate(ch, &b)
	}
}
