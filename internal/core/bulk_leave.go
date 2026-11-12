package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Leaving several channels at once, as the sidebar's organize mode does
// it under bulk_leave_channels_from_sidebar (client-channel-list-view
// SYcU OrganizeModeMenu's Leave, build 4d0c78085232):
//
//   - getChannelsThatCannotBeLeft (SYcU oM) keeps the selected
//     conversations the viewer can't leave from the sidebar:
//     !canUserLeaveChannel (C2Vu: unknown, the viewer an ultra-restricted
//     guest outside a DM, #general, org-mandatory, a restricted guest's
//     public channel or last channel), DMs and group DMs (aK5v JM), not a
//     member (JUrK bl), archived (Jgk4 F), record channels (aK5v rI),
//     externally shared (5DYq ju), huddle channels (aK5v cb) and meeting
//     channels (aK5v Dh: properties.meetings_channel.is_meeting_channel,
//     or a name starting "mc:").
//   - leaveChannelsInBulk (boot-render NIXs) is onLeaveChannels' one
//     conversations.bulkLeave call (7/7j conversationsBulkLeaveFetcher)
//     with channels: the web client's getFormData (QwSy) appends a string
//     array as is, so the ids go comma-joined. Slack's sidebar then drops
//     the channels on their channel_left events; Clack drops them when the
//     call answers ok, the way Leave does.

// ChannelsThatCannotBeLeft returns, in order, the ids of ids that
// getChannelsThatCannotBeLeft keeps out of a bulk leave.
func (a *App) ChannelsThatCannotBeLeft(ids []string) []string {
	a.S.RLock()
	self := a.S.Self
	others := 0 // the viewer's other public/private channels, for a restricted guest
	for _, c := range a.S.Convs {
		if !c.IsIM && !c.IsMPIM && c.IsMember && !c.IsArchived {
			others++
		}
	}
	convs := make([]*slack.Conversation, len(ids))
	for i, id := range ids {
		if c, ok := a.S.Convs[id]; ok {
			cp := *c
			convs[i] = &cp
		}
	}
	a.S.RUnlock()
	var out []string
	for i, id := range ids {
		c := convs[i]
		if c == nil || !canUserLeave(c, self, others) || c.IsIM || c.IsMPIM || !c.IsMember || c.IsArchived ||
			convRecordChannel(c) || c.IsExtShared || convHuddleChannel(c) || convMeetingChannel(c) {
			out = append(out, id)
		}
	}
	return out
}

// canUserLeave is canUserLeaveChannel (C2Vu Y9) for a known conversation;
// others is how many unarchived channels the viewer is in.
func canUserLeave(c *slack.Conversation, self slack.User, others int) bool {
	im := c.IsIM || c.IsMPIM
	switch {
	case self.IsUltraRestricted && !im: // USER_IS_ULTRA_RESTRICTED
		return false
	case c.IsGeneral || c.IsOrgMandatory: // CANT_LEAVE_GENERAL, CANT_LEAVE_MANDATORY_SHARED_CHANNEL
		return false
	}
	if self.IsRestricted && !im {
		public := !c.IsPrivate && !c.IsGroup
		if public { // USER_IS_RESTRICTED (a frontline worker's private ones too; Clack has no frontline flag)
			return false
		}
		if others <= 1 { // USER_IS_RESTRICTED_LAST_CHANNEL: no other channel left to be in
			return false
		}
	}
	return true
}

// convProps reads the properties a bulk leave checks.
func convProps(c *slack.Conversation) (p struct {
	Record  json.RawMessage `json:"record_channel"`
	Huddles struct {
		IsHuddle bool `json:"is_huddle_channel"`
	} `json:"huddles"`
	Meetings struct {
		IsMeeting bool `json:"is_meeting_channel"`
	} `json:"meetings_channel"`
}) {
	if len(c.Properties) > 0 {
		_ = json.Unmarshal(c.Properties, &p)
	}
	return p
}

// convRecordChannel is isRecordChannel (aK5v rI): properties.record_channel.
func convRecordChannel(c *slack.Conversation) bool {
	r := convProps(c).Record
	return len(r) > 0 && string(r) != "null"
}

// convHuddleChannel is aK5v cb: properties.huddles.is_huddle_channel.
func convHuddleChannel(c *slack.Conversation) bool { return convProps(c).Huddles.IsHuddle }

// convMeetingChannel is aK5v Dh: a meeting channel, or one named "mc:…".
func convMeetingChannel(c *slack.Conversation) bool {
	return convProps(c).Meetings.IsMeeting || strings.HasPrefix(c.Name, "mc:")
}

// LeaveChannelsBulk is leaveChannelsInBulk's conversations.bulkLeave for
// ids; on success each leaves the sidebar as with Leave.
func (a *App) LeaveChannelsBulk(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := a.C.Call(ctx, "conversations.bulkLeave", url.Values{"channels": {strings.Join(ids, ",")}}, nil); err != nil {
		return err
	}
	for _, ch := range ids {
		a.dropMembership(ch, false)
		a.emit(Event{Type: "conv_removed", Channel: ch})
	}
	return nil
}
