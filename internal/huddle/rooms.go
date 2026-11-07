package huddle

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
}

// roomsJoin starts or joins the huddle in channel (rooms.join).
func roomsJoin(ctx context.Context, c Caller, channel, region string) (*joinInfo, error) {
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
	p := url.Values{"channel_id": {channel}, "regions": {region}}
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

// userOfExternal maps a Chime ExternalUserId ("<enterprise>-<room>-<user>")
// to the Slack user id.
func userOfExternal(ext string) string {
	if i := strings.LastIndexByte(ext, '-'); i >= 0 {
		return ext[i+1:]
	}
	return ext
}
