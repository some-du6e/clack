package core

import (
	"context"
	"log"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// unreadIgnoredSubtypes is canCountAsUnreadForCurrentUser's
// ignoredMessageTypes (client-boot qxvW): messages of these types never
// make a conversation unread.
var unreadIgnoredSubtypes = map[string]bool{
	"channel_canvas_updated_unread_ignore": true, "tabbed_canvas_updated_unread_ignore": true,
	"channel_join": true, "channel_leave": true, "group_join": true, "group_leave": true,
	"huddle_message": true, "huddle_thread": true, "mpim_notify_disabled": true,
	"app_conversation_join": true, "app_conversation_leave": true, "app_conversation_created": true,
	"channel_tab_added_unread_ignore": true, "folder_item_added_unread_ignore": true,
}

// countsAsUnread is isMessageUnread (qxeT) over
// canCountAsUnreadForCurrentUser (qxvW): m is past lastRead, not a local
// send (isTempMsg) and not hidden (isHidden; Clack's copies keep no
// no_display or _hidden_reply), and is either an invite of the viewer
// (isInviteForCurrentUser) or not the viewer's own (a workflow's from them
// counts) and not an ignored type. Not ported: a file comment's author,
// isMpdmMoveByCurrentUser, dnd_suppressed and the system messages
// redesign's ignored types, which Clack's copies don't carry.
func (a *App) countsAsUnread(m *slack.Message, lastRead, self string) bool {
	if m == nil || !store.TSLess(lastRead, m.TS) || a.isLocalTS(m.TS) || m.Hidden || m.Subtype == "tombstone" {
		return false
	}
	if (m.Subtype == "channel_join" || m.Subtype == "group_join") && m.Inviter != "" && m.User == self {
		return true
	}
	return !(m.User == self && m.WorkflowID == "") && !unreadIgnoredSubtypes[m.Subtype]
}

// maybeMarkChannelFromThread is setThreadLastRead's
// maybeMarkChannelFromThread (boot-shared C7hc, "If a thread with a
// broadcast was just marked, maybe we can mark the channel too"): a thread
// read on from prev to now (a read mark of a thread with a last read)
// whose newly read span, past the channel's last read, holds the thread's
// broadcasts in the channel's newest window marks the channel up to the
// newest of them, when the window covers the channel from its last read
// to that broadcast and nothing else in it counts as unread.
func (a *App) maybeMarkChannelFromThread(ctx context.Context, ch, threadTS, prev, now string) {
	if prev == "" || !store.TSLess(prev, now) {
		return
	}
	if _, member := a.S.Conv(ch); !member {
		return
	}
	ce, ok := a.S.Count(ch)
	chanRead := ce.LastRead
	if !ok || chanRead == "" || !store.TSLess(chanRead, now) {
		return
	}
	tl := a.S.Timeline(ch)
	if tl == nil || len(tl.Msgs) == 0 {
		return
	}
	from := prev
	if store.TSLess(prev, chanRead) {
		from = chanRead
	}
	// the thread's broadcasts among the channel's messages in (from, now]
	var bcasts []string
	isB := map[string]bool{}
	for i := range tl.Msgs {
		m := &tl.Msgs[i]
		if !store.TSLess(from, m.TS) || store.TSLess(now, m.TS) {
			continue
		}
		if (m.Subtype == "thread_broadcast" || m.ReplyBroadcast) && m.ThreadTS == threadTS {
			bcasts = append(bcasts, m.TS)
			isB[m.TS] = true
		}
	}
	if len(bcasts) == 0 {
		return
	}
	newest := bcasts[len(bcasts)-1]
	if !store.TSLess(chanRead, newest) {
		return // the broadcast isn't unread
	}
	// haveEnoughChannelHistory: the window reaches back to the channel's
	// last read and on to the broadcast
	if tl.HasMore && store.TSLess(chanRead, tl.Msgs[0].TS) || store.TSLess(tl.Msgs[len(tl.Msgs)-1].TS, newest) {
		return
	}
	self := a.S.SelfID()
	for i := range tl.Msgs {
		m := &tl.Msgs[i]
		if !store.TSLess(chanRead, m.TS) || store.TSLess(newest, m.TS) || isB[m.TS] {
			continue
		}
		if a.countsAsUnread(m, chanRead, self) {
			return // the channel has other unreads
		}
	}
	if _, err := a.Mark(ctx, ch, newest); err != nil {
		log.Printf("threads: marking %s up to its broadcast %s: %v", ch, newest, err)
	}
}
