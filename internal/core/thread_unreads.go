package core

import "context"

// Unread threads per conversation, which the reply bar's "Unread" mark
// reads under unread_thread_indication_in_replybar, as Slack's client
// keeps them (build 4d0c78085232):
//
//   - unreadCounts.threadsUnreadCountByChannel (client-boot 82Kz aX) is
//     client.counts' threads.unread_count_by_channel (h1EQ →
//     threadsUnreadCountChanged). Clack reads client.counts again after
//     thread_marked and update_thread_state (events.go), where Slack also
//     takes update_global_thread_state.
//   - MessagePane maybeFetchUnreadThreadActivity (boot-render bU1n): with
//     that count above 0 and the experiment on, once per mount while the
//     channel's thread activity isn't loaded, fetchActivitiesForChannel
//     (gWaD sv {channelId, fetchNew, types: [ThreadV2]}): activity.feed for
//     the channel, thread_v2 items, unread only, mode chrono_unreads,
//     limit 10.
//   - getUnreadThreadsCountForMessage (client-boot 3x6A AY) counts that
//     channel activity list per thread: isUnreadThreadReply (a thread ts,
//     not read, not a reaction, a thread_v2 item or a reply).

// ThreadsUnreadIn is client.counts' threads.unread_count_by_channel[ch]:
// how many of ch's threads have unread replies.
func (a *App) ThreadsUnreadIn(ch string) int {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.ThreadsUnreadByChannel[ch]
}

// ChannelUnreadThreads is fetchActivitiesForChannel for the reply bar:
// ch's unread thread activity, as unread items per thread ts.
func (a *App) ChannelUnreadThreads(ctx context.Context, ch string) (map[string]int, error) {
	p, err := a.Activity(ctx, ActivityOpts{Types: "thread_v2", Unread: true, Mode: "chrono_unreads", Limit: 10, ChannelIDs: []string{ch}})
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for i := range p.Items {
		if it := &p.Items[i]; unreadThreadReply(it) {
			out[it.ThreadTS]++
		}
	}
	return out, nil
}

// unreadThreadReply is 3x6A isUnreadThreadReply.
func unreadThreadReply(it *ActivityItem) bool {
	return it.ThreadTS != "" && it.Unread && it.Type != "message_reaction" && (it.Type == "thread_v2" || it.TS != it.ThreadTS)
}
