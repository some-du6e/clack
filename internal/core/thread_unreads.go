package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Unread threads per conversation, which the reply bar's "Unread" mark
// reads under unread_thread_indication_in_replybar, as Slack's client
// keeps them (build 4d0c78085232):
//
//   - unreadCounts.threadsUnreadCountByChannel (client-boot 82Kz aX) is
//     client.counts' threads.unread_count_by_channel (h1EQ →
//     threadsUnreadCountChanged), and update_thread_state's at once
//     (onUpdateThreadState). Clack reads client.counts again after
//     thread_marked and update_thread_state too (events.go).
//   - MessagePane maybeFetchUnreadThreadActivity (boot-render bU1n): with
//     that count above 0 and the experiment on, once per mount while the
//     channel's thread activity isn't loaded, fetchActivitiesForChannel
//     (gWaD sv {channelId, fetchNew, types: [ThreadV2]}): activity.feed for
//     the channel, thread_v2 items, unread only, mode chrono_unreads,
//     limit 10.
//   - getUnreadThreadsCountForMessage (client-boot 3x6A AY) counts that
//     channel activity list per thread: isUnreadThreadReply (a thread ts,
//     not read, not a reaction, a thread_v2 item or a reply).

// onUpdateThreadState is updateThreadState (boot-render /+fI, build
// 35b42de8b870), which takes the event's state at once: has_unreads,
// mention_count and unread_count_by_channel for the threads' unread
// state (the reply bar's Unread mark reads the count), and channel_badges
// for the badges (updateChannelBadgesCounts). Clack only read
// client.counts again 1.5 s later (refreshCountsSoon), so the Unread mark
// came seconds after the reply it was for (Karim, 2026-11-22: "i swear i
// saw your new reply before the Unread thing popped up"). An event whose
// org_wide isn't the client's (isEnterprise: Grid clients take the
// org-wide one) is rejected, as Slack rejects it; Clack takes no
// update_global_thread_state, which only an org-unified client reads. It
// reports whether the threads' unread state changed.
func (a *App) onUpdateThreadState(raw json.RawMessage) bool {
	var e struct {
		HasUnreads           *bool          `json:"has_unreads"`
		MentionCount         *int           `json:"mention_count"`
		UnreadCountByChannel map[string]int `json:"unread_count_by_channel"`
		ChannelBadges        map[string]int `json:"channel_badges"`
		OrgWide              bool           `json:"org_wide"`
	}
	if json.Unmarshal(raw, &e) != nil || e.OrgWide != a.IsEnterprise() {
		return false
	}
	if e.ChannelBadges != nil && a.mergeChannelBadges(e.ChannelBadges) {
		a.emit(Event{Type: "activity_badges"}) // the app badge reads them (classic_badges.go)
	}
	return a.S.SetThreadState(e.HasUnreads, e.MentionCount, e.UnreadCountByChannel)
}

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

// LooksLikeBotID is Slack's looksLikeBotId (2jcO): a bot id, not a member.
func LooksLikeBotID(id string) bool {
	id = strings.TrimPrefix(id, "@")
	if len(id) < 9 || id[0] != 'B' {
		return false
	}
	for _, r := range id[1:] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// ReplyBarBotAvatar is the reply bar's picture of a bot replier
// (ReplyBarAvatar with botId, client-boot iR00 oV and ewDY): the root's
// reply_user_icons entry for it; under agent_identity_overrides, the
// latest reply's icons when that reply is the bot's (icons.emoji, else
// image_48, image_72, image_36), read from the loaded thread; else the
// bot's own icon (bots.info), else Slack's default bot avatar. An emoji
// only counts when the workspace has it.
func (a *App) ReplyBarBotAvatar(m *Message, botID string, px int) string {
	emoji := func(name string) string {
		if name == "" {
			return ""
		}
		return a.emojiURL(name)
	}
	for _, ic := range m.ReplyUserIcons {
		if ic.ID != botID {
			continue
		}
		if u := emoji(ic.IconEmoji); u != "" {
			return u
		}
		if ic.IconURL != "" {
			return ic.IconURL
		}
	}
	if a.experiment("agent_identity_overrides") == "on" && m.LatestReply != "" {
		root := m.ThreadTS
		if root == "" {
			root = m.TS
		}
		var icons *slack.Icons
		a.S.ThreadHas(m.Channel, root, func(r *slack.Message) bool {
			if r.TS != m.LatestReply {
				return false
			}
			if r.BotID == botID {
				icons = r.Icons
			}
			return true
		})
		if icons != nil {
			if u := emoji(icons.Emoji); u != "" {
				return u
			}
			if u := firstNonEmpty(icons.Image48, icons.Image72, icons.Image36); u != "" {
				return u
			}
		}
	}
	if b, ok := a.S.Bot(botID); ok && b.Icon != "" {
		return b.Icon
	}
	a.S.NoteMissingBot(botID)
	return defaultBotAvatarAt(px)
}
