package core

// The Unreads view's and the channel join banner's server calls, as
// Slack's web client makes them (CDN build bv1-13, chunks
// page-unreads-view, the catch-up hooks shared chunk 2cdE/gvf3,
// client-boot LbeF/QJKZ, megaphone XUhT/NznF; docs/PREFS-AUDIT.md).
//
//   - UnreadHistory is fetchHistory (QJKZ) with the frame's oldest:
//     conversations.history channel, limit 28 (determineDefaultHistoryLimit),
//     oldest (a zero cursor becomes 0000000000.000001), inclusive,
//     include_pin_count, no_user_profile, include_free_team_extra_messages.
//   - MarkMarkers is markManyLastRead (LbeF R5 -> 6ndL markMany):
//     unread.markRead markers=<JSON {channel: ts}>. The single-channel
//     "Mark as read" is markLastRead, conversations.mark (Mark).
//   - SetUnreadCollapsed is expandChannel / collapseChannel (gvf3): the
//     unread_collapsed_channels pref updated locally (setUserPref, no
//     users.prefs.set) and unread.expand / unread.collapse {channel}.
//   - MegaphoneSpaces is loadMegaphoneNotificationData (XUhT eF):
//     megaphone.notifications.list event={type:request_spaces, spaces},
//     channel_id; the answer maps each space to {name, data} or null.
//   - RecordMegaphone is recordNotificationEvent (NznF):
//     megaphone.setNotificationAsSeen notification, action, channel_id.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// UnreadHistoryLimit is determineDefaultHistoryLimit's 28.
const UnreadHistoryLimit = 28

// UnreadPage is one fetchHistory answer for an Unreads frame.
type UnreadPage struct {
	Messages []Message // ascending, top-level (and broadcasts) only
	HasMore  bool      // newer messages than the last one exist
}

// UnreadHistory loads the messages after oldest (inclusive), the way the
// Unreads view fills a frame and pages it (fetchPost with the frame's
// slice end).
func (a *App) UnreadHistory(ctx context.Context, ch, oldest string) (UnreadPage, error) {
	if oldest == "0000000000.000000" {
		oldest = "0000000000.000001"
	}
	p := url.Values{
		"channel":                          {ch},
		"limit":                            {strconv.Itoa(UnreadHistoryLimit)},
		"include_pin_count":                {"true"},
		"inclusive":                        {"true"},
		"no_user_profile":                  {"true"},
		"include_free_team_extra_messages": {"true"},
		"include_date_joined":              {"false"},
	}
	if oldest != "" {
		p.Set("oldest", oldest)
	}
	var r slack.HistoryPage
	if err := a.C.Call(ctx, "conversations.history", p, &r); err != nil {
		return UnreadPage{}, err
	}
	a.EnsureUsers(ctx, referencedUsers(r.Messages))
	msgs := append([]slack.Message(nil), r.Messages...)
	store.SortAsc(msgs)
	return UnreadPage{Messages: a.timeline(ch, msgs), HasMore: r.HasMore}, nil
}

// MarkMarkers sets many read cursors in one unread.markRead call
// (markManyLastRead: "Mark All Messages Read" and its Undo), then updates
// each conversation's unread state like Mark.
func (a *App) MarkMarkers(ctx context.Context, markers map[string]string) error {
	if len(markers) == 0 {
		return nil
	}
	b, err := json.Marshal(markers)
	if err != nil {
		return err
	}
	if err := a.C.Call(ctx, "unread.markRead", url.Values{"markers": {string(b)}}, nil); err != nil {
		return err
	}
	again := false
	for ch, ts := range markers {
		e := a.S.MarkRead(ch, ts)
		cv := a.Count(ch)
		a.emit(Event{Type: "marked", Channel: ch, TS: ts, Count: &cv})
		again = again || e.HasUnreads
	}
	if again {
		a.refreshCountsSoon()
	}
	return nil
}

// UnreadCollapsed is the unread_collapsed_channels pref: the channels
// collapsed in the Unreads view.
func (a *App) UnreadCollapsed() []string {
	var out []string
	for _, id := range strings.Split(a.PrefString("unread_collapsed_channels"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// SetUnreadCollapsed collapses or expands ch in the Unreads view.
func (a *App) SetUnreadCollapsed(ctx context.Context, ch string, collapsed bool) error {
	cur := a.UnreadCollapsed()
	has := false
	var next []string
	for _, id := range cur {
		if id == ch {
			has = true
			if collapsed {
				next = append(next, id)
			}
			continue
		}
		next = append(next, id)
	}
	if has == collapsed {
		return nil // Slack's thunks return early too
	}
	if collapsed {
		next = append(next, ch)
	}
	a.setLocalPref("unread_collapsed_channels", strings.Join(next, ","))
	method := "unread.expand"
	if collapsed {
		method = "unread.collapse"
	}
	return a.C.Call(ctx, method, url.Values{"channel": {ch}}, nil)
}

// setLocalPref is Slack's setUserPref action: the pref changes in the
// client's copy only (the server keeps its own through the call that
// follows).
func (a *App) setLocalPref(name, value string) {
	local, _ := json.Marshal(value)
	a.mergePrefs(map[string]json.RawMessage{name: local})
	p := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &p})
}

// Megaphone space and notification names (hro4).
const (
	SpaceMessageInput                = "message_input"
	NotificationChannelJoinBanner    = "education_channel_join_banner"
	MegaphoneImpression              = "impression"
	MegaphoneDismiss                 = "dismiss"
	megaphoneRequestSpaces           = "request_spaces"
	megaphoneReasonLoad              = "load-megaphone-notification-data"
	megaphoneReasonRecordNotifyEvent = "record-notification-event"
)

// MegaphoneSpaces is ChannelJoinBanner's REQUEST_SPACES on mount (and
// maybeHideChannelJoinBanner's): loadMegaphoneNotificationData with
// event {type: request_spaces, spaces} and channelId, the answers filed
// under ch (megaphone.go). It returns space -> name, "" for a space the
// server answered null.
func (a *App) MegaphoneSpaces(ctx context.Context, ch string, spaces []string) (map[string]string, error) {
	return a.loadMegaphone(ctx, map[string]any{"type": megaphoneRequestSpaces, "spaces": spaces}, ch, megaphoneReasonLoad, nil)
}

// RecordMegaphone records an impression or a dismissal of a megaphone
// notification shown for ch.
func (a *App) RecordMegaphone(ctx context.Context, notification, action, ch string) error {
	p := url.Values{"notification": {notification}, "action": {action}, "reason": {megaphoneReasonRecordNotifyEvent}}
	if ch != "" {
		p.Set("channel_id", ch)
	}
	return a.C.Call(ctx, "megaphone.setNotificationAsSeen", p, nil)
}

// PaidTeam is isPaidTeam for the viewer's team (getAllUnreadsSortOrder's
// default: "priority" on a paid team).
func (a *App) PaidTeam() bool { return paidTeam }
