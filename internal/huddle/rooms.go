package huddle

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Caller is the slice of *slack.Client the huddle needs (Call routes
// rooms.join to the enterprise token, which Enterprise Grid requires).
type Caller interface {
	Call(ctx context.Context, method string, params url.Values, out any) error
}

type chimeMeeting struct {
	MeetingID      string `json:"MeetingId"`
	MediaRegion    string `json:"MediaRegion"`
	MediaPlacement struct {
		AudioHostURL   string `json:"AudioHostUrl"`
		SignalingURL   string `json:"SignalingUrl"`
		TurnControlURL string `json:"TurnControlUrl"`
	} `json:"MediaPlacement"`
}

type chimeAttendee struct {
	AttendeeID     string `json:"AttendeeId"`
	ExternalUserID string `json:"ExternalUserId"`
	JoinToken      string `json:"JoinToken"`
}

// joinInfo is what rooms.join answers.
type joinInfo struct {
	RoomID       string
	Channel      string
	ThreadCh     string
	ThreadTS     string
	CreatedBy    string
	Participants []string
	Meeting      chimeMeeting
	Attendee     chimeAttendee
	Name         string
	DateStart    int64
	Invites      map[string]string // user -> last invite status (DM rings)
}

// participants decodes ["U1",…] or [{"user_id":"U1"},…].
type participants []string

func (p *participants) UnmarshalJSON(b []byte) error {
	var ids []string
	if json.Unmarshal(b, &ids) == nil {
		*p = ids
		return nil
	}
	var objs []struct {
		UserID string `json:"user_id"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(b, &objs); err != nil {
		return nil // unknown shape: ignore
	}
	for _, o := range objs {
		if o.UserID != "" {
			*p = append(*p, o.UserID)
		} else if o.ID != "" {
			*p = append(*p, o.ID)
		}
	}
	return nil
}

type roomJSON struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	CreatedBy       string       `json:"created_by"`
	Participants    participants `json:"participants"`
	ThreadRootTS    string       `json:"thread_root_ts"`
	CanvasThreadTS  string       `json:"canvas_thread_ts"`
	Channels        []string     `json:"channels"`
	DateStart       int64        `json:"date_start"`
	HasEnded        bool         `json:"has_ended"`
	DateEnd         int64        `json:"date_end"`
	ScreenshareOn   []string     `json:"participants_screenshare_on"`
	CameraOn        []string     `json:"participants_camera_on"`
	ChannelID       string       `json:"channel_id"`
	HuddleLink      string       `json:"huddle_link"`
	MediaBackend    string       `json:"media_backend_type"`
	ParticipantHist participants `json:"participant_history"`
	// The people the huddle rang (Slack's server rings a DM's other member
	// on rooms.join): user -> "pending", "joining_soon", "declined",
	// "missed" or "accepted". Slack's huddle window draws a pending tile
	// for each (HuddlePeerTile's invite status overlay).
	LastInviteStatus map[string]string `json:"last_invite_status_by_user"`
	PendingInvitees  json.RawMessage   `json:"pending_invitees"`
}

// invites is the room's invite statuses: last_invite_status_by_user, and
// pending_invitees (an object keyed by user, or a list) as "pending" for
// anyone it doesn't name. nil when the room says nothing about invites.
func (r roomJSON) invites() map[string]string {
	if r.LastInviteStatus == nil && len(r.PendingInvitees) == 0 {
		return nil
	}
	out := map[string]string{}
	for u, st := range r.LastInviteStatus {
		out[u] = st
	}
	var byUser map[string]json.RawMessage
	if json.Unmarshal(r.PendingInvitees, &byUser) == nil {
		for u := range byUser {
			if _, ok := out[u]; !ok {
				out[u] = "pending"
			}
		}
	} else {
		var list participants
		_ = json.Unmarshal(r.PendingInvitees, &list)
		for _, u := range list {
			if _, ok := out[u]; !ok {
				out[u] = "pending"
			}
		}
	}
	return out
}

// roomsJoin starts or joins the huddle in channel (rooms.join).
func roomsJoin(ctx context.Context, c Caller, channel, region string, multi bool) (*joinInfo, error) {
	if region == "" {
		region = "us-east-2"
	}
	var res struct {
		Call struct {
			CallID    string `json:"call_id"`
			FreeWilly struct {
				Meeting  json.RawMessage `json:"meeting"`
				Attendee chimeAttendee   `json:"attendee"`
			} `json:"free_willy"`
		} `json:"call"`
		Huddle roomJSON `json:"huddle"`
		Room   roomJSON `json:"room"`
		Canvas struct {
			ThreadChannelID string `json:"thread_channel_id"`
			RootThreadTS    string `json:"root_thread_ts"`
		} `json:"canvas"`
	}
	// Slack's joinRoom fetcher: {id?, channel_id, thread_ts?, regions,
	// multidevice}. There's no ring call: for a DM the server rings the
	// other member itself (the callee gets huddle_invite, and the room's
	// last_invite_status_by_user says "pending").
	p := url.Values{"channel_id": {channel}, "regions": {region}, "multidevice": {strconv.FormatBool(multi)}}
	if err := c.Call(ctx, "rooms.join", p, &res); err != nil {
		return nil, err
	}
	ji := &joinInfo{Channel: channel, RoomID: res.Call.CallID, Attendee: res.Call.FreeWilly.Attendee}
	if len(res.Call.FreeWilly.Meeting) > 0 {
		if err := json.Unmarshal(res.Call.FreeWilly.Meeting, &ji.Meeting); err != nil {
			return nil, err
		}
	}
	for _, r := range []roomJSON{res.Huddle, res.Room} {
		if r.ID == "" {
			continue
		}
		if ji.RoomID == "" {
			ji.RoomID = r.ID
		}
		if ji.CreatedBy == "" {
			ji.CreatedBy = r.CreatedBy
		}
		if len(ji.Participants) == 0 {
			ji.Participants = r.Participants
		}
		if ji.ThreadTS == "" {
			ji.ThreadTS = firstNonEmpty(r.ThreadRootTS, r.CanvasThreadTS)
		}
		if ji.Name == "" {
			ji.Name = r.Name
		}
		if ji.DateStart == 0 {
			ji.DateStart = r.DateStart
		}
		if ji.Invites == nil {
			ji.Invites = r.invites()
		}
	}
	ji.ThreadCh = res.Canvas.ThreadChannelID
	if res.Canvas.RootThreadTS != "" {
		ji.ThreadTS = res.Canvas.RootThreadTS
	}
	if ji.ThreadCh == "" {
		ji.ThreadCh = channel
	}
	if ji.Meeting.MediaPlacement.SignalingURL == "" || ji.Attendee.JoinToken == "" {
		return nil, errors.New("huddle: rooms.join answered without Chime media placement")
	}
	return ji, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// userOfExternal maps a Chime ExternalUserId to the Slack user id:
// "<enterprise>-<room>-<user>", or with a device suffix when the person is in
// the huddle from more than one device.
func userOfExternal(ext string) string {
	parts := strings.Split(ext, "-")
	for i := len(parts) - 1; i >= 0; i-- {
		if p := parts[i]; len(p) >= 9 && (p[0] == 'U' || p[0] == 'W') && isIDChars(p) {
			return p
		}
	}
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

func isIDChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
