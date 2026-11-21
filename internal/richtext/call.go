package richtext

// The call block (a Calls API call: Zoom, Meet…), as Slack's web client
// draws it (build 846030e1ce97): the call renderable (boot-shared FkcF)
// is ClientStoreConnectedPlatformCallSubtype (9CtN), which reads the call
// the block's call.v1 put in the calls store (client-boot Ero4
// convertCallSubtypeBlockForRedux) and words it with getCallObjectText.
// The texts depend on the time, so they're worked out when the Doc is
// built (like HuddleMsgText).

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// bkCall is a call block's "call" field.
type bkCall struct {
	MediaBackendType string   `json:"media_backend_type"`
	V1               bkCallV1 `json:"v1"`
}

type bkCallV1 struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	CreatedBy          string         `json:"created_by"`
	IsDMCall           bool           `json:"is_dm_call"`
	WasAccepted        bool           `json:"was_accepted"`
	WasRejected        bool           `json:"was_rejected"`
	WasMissed          bool           `json:"was_missed"`
	Channels           []bkStr        `json:"channels"`
	DateStart          int64          `json:"date_start"`
	DateEnd            int64          `json:"date_end"`
	ActiveParticipants []bkCallMember `json:"active_participants"`
	AllParticipants    []bkCallMember `json:"all_participants"`
	DisplayID          string         `json:"display_id"`
	JoinURL            string         `json:"join_url"`
	DesktopJoinURL     string         `json:"desktop_app_join_url"`
	AppID              string         `json:"app_id"`
	// app_icon_urls is read only for its string values: an app's icon map
	// can carry others (is_default), and Slack takes the icon from the app
	AppIconURLs map[string]json.RawMessage `json:"app_icon_urls"`
}

// bkCallMember is a call participant: a Slack member or an external one.
// Its fields take any scalar, so one odd value can't fail the whole card.
type bkCallMember struct {
	SlackID     bkStr `json:"slack_id"`
	ExternalID  bkStr `json:"external_id"`
	DisplayName bkStr `json:"display_name"`
	AvatarURL   bkStr `json:"avatar_url"`
}

// DMResolver is an optional Resolver extension: an IM's other member
// (getOtherMemberNameInDmCall reads the DM call's D channel).
type DMResolver interface {
	DMUser(channelID string) (userID string, ok bool)
}

// CallCard is a call block as PlatformCallSubtype draws it: "Call" and
// its collapse toggle, then the p-call_subtype card.
type CallCard struct {
	ID string
	// Title, Status and Description are getCallObjectText's: the header's
	// two lines and the footer's text ("" for none).
	Title, Status, Description string
	// MeetingID is display_id: the "Meeting ID: …" body between header
	// and footer.
	MeetingID string
	// Button is the footer's button ("Join", "Show Call", "Call Back",
	// "Call Again"; "" for none) and Primary its type (else outline).
	// JoinURL is where Join goes (window.open of the call's join_url).
	Button  string
	Primary bool
	JoinURL string
	// Icon is the app's icon (36px), "" for the phone glyph in Slack's
	// blue square. Ended: the icon box is greyed (p-call_subtype__icon--ended).
	Icon  string
	Ended bool
	// Glyph is the y8g2 font icon renderCallIcon draws without an app
	// icon: phone, phone-flat once the call ended, call-missed or
	// call-declined for a DM call the viewer missed or declined.
	Glyph string
	// Faces is the facepile (getMembersOfCall: everyone who joined once the
	// call ended, else who is in it); nil when Slack hides it
	// (hideFacepile).
	Faces []CallFace
	// NoFooter: renderCallFooter draws nothing (a DM call missed or
	// declined, with a join_url, its facepile hidden)
	NoFooter bool
}

// CallFace is one face of the call's facepile.
type CallFace struct {
	UserID string // a Slack member (ConnectedBaseAvatar); "" for an external participant
	Name   string // its tooltip
	Avatar string
}

// callBlock converts a call block.
func (c *builder) callBlock(b *bk) Block {
	cc := &CallCard{ID: b.CallID}
	if b.Call != nil {
		cc = callCard(c.r, b.CallID, &b.Call.V1, time.Now())
	}
	return Block{Kind: CallBlock, Call: cc}
}

// callCard is the store's call (Ero4) worded by getCallObjectText at now.
func callCard(r Resolver, id string, v *bkCallV1, now time.Time) *CallCard {
	self := r.SelfID()
	members := v.ActiveParticipants // getMembersOfCall
	if v.DateEnd != 0 {
		members = v.AllParticipants
	}
	in := false
	for _, m := range members {
		if m.SlackID != "" && string(m.SlackID) == self {
			in = true
		}
	}
	// convertInvitationStatusToEnum: accepted wins, then rejected, then missed
	accepted, rejected, missed := v.WasAccepted, false, false
	if !accepted {
		rejected = v.WasRejected
		missed = rejected || v.WasMissed
	}
	s := callState{
		name: v.Name, start: v.DateStart, end: v.DateEnd, dm: v.IsDMCall,
		creator: self != "" && self == v.CreatedBy, in: in, count: len(members),
		missed: missed, rejected: rejected, joinURL: v.JoinURL,
		elsewhere: in && v.JoinURL == "", // !activeCallWindowExists: Clack never has one
		now:       now.Unix(),
	}
	if s.dm { // getOtherMemberNameInDmCall: the call's D channel's other member
		if dr, ok := r.(DMResolver); ok {
			for _, ch := range v.Channels {
				if strings.HasPrefix(string(ch), "D") {
					if u, ok := dr.DMUser(string(ch)); ok {
						s.dmName = memberName(r, u)
					}
					break
				}
			}
		}
	}
	cc := &CallCard{ID: firstNonEmpty(v.ID, id), Title: s.title(), Status: s.status(now.Location()),
		Description: s.description(), MeetingID: v.DisplayID, JoinURL: SafeURL(v.JoinURL), Ended: v.DateEnd != 0}
	cc.Glyph = "phone" // renderCallIcon (9CtN)
	if v.DateEnd != 0 {
		cc.Glyph = "phone-flat"
	}
	if v.IsDMCall && missed { // wasMissed: invite response missed or reject
		cc.Glyph = "call-missed"
	}
	if v.IsDMCall && rejected {
		cc.Glyph = "call-declined"
	}
	cc.Button, cc.Primary = s.button()
	cc.Icon = callIcon(v.AppID, v.AppIconURLs)
	hide := s.count == 0 || s.dm && s.count == 1 && !s.creator
	cc.NoFooter = s.dm && (s.missed || s.rejected) && v.JoinURL != "" && hide
	if !hide {
		mn, _ := r.(MemberNamer)
		for _, m := range members {
			id := string(m.SlackID)
			f := CallFace{UserID: id, Name: string(m.DisplayName), Avatar: SafeImage(string(m.AvatarURL))}
			if id != "" {
				f.Name, f.Avatar = memberName(r, id), ""
				if mn != nil {
					f.Avatar = SafeImage(mn.MemberAvatar(id))
				}
			}
			cc.Faces = append(cc.Faces, f)
		}
	}
	return cc
}

// callIcon is the app's 36px icon (Slack reads the app's icons; the
// block carries the same set), none for Slack's own calls (A00).
func callIcon(appID string, icons map[string]json.RawMessage) string {
	if appID == "" || appID == "A00" {
		return ""
	}
	for _, k := range []string{"image_72", "image_64", "image_96", "image_48", "image_128", "image_36", "image_192", "image_512", "image_1024", "image_32"} {
		var s string
		if json.Unmarshal(icons[k], &s) == nil {
			if u := SafeImage(s); u != "" {
				return u
			}
		}
	}
	return ""
}

// memberName is a member's name for messages (MemberNamer when it knows
// them, else the resolver's UserName).
func memberName(r Resolver, id string) string {
	if mn, ok := r.(MemberNamer); ok {
		if n, ok := mn.MemberName(id); ok {
			return n
		}
	}
	return r.UserName(id)
}

// callState is getCallObjectText's input.
type callState struct {
	name             string
	start, end, now  int64
	dm, creator, in  bool
	count            int
	missed, rejected bool
	joinURL          string
	elsewhere        bool // onAnotherDeviceOnly
	dmName           string
}

// title is getCallTitle.
func (s callState) title() string {
	if s.name != "" {
		return s.name
	}
	if s.end == 0 && s.start > s.now {
		return "Scheduled Call"
	}
	if s.dm {
		switch {
		case s.end == 0 && s.creator && s.count < 2:
			return "Calling…"
		case s.end == 0 && !s.in:
			return "Calling You…"
		case s.end == 0 && s.count == 2:
			return "Call Started"
		case s.end != 0 && s.missed && s.rejected:
			return "Call Declined"
		case s.end != 0 && s.missed:
			return "Missed Call"
		}
	}
	if s.end != 0 {
		return "Call Ended"
	}
	return "Call Started"
}

// span is z4qU: whole weeks, then the days, hours and minutes left over.
type span struct{ w, d, h, m int64 }

func spanOf(sec int64) span {
	if sec < 0 {
		sec = 0
	}
	return span{w: sec / 604800, d: sec / 86400 % 7, h: sec / 3600 % 24, m: sec / 60 % 60}
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.FormatInt(n, 10) + " " + many
}

// status is getCallStatus.
func (s callState) status(loc *time.Location) string {
	if s.end == 0 && s.elsewhere {
		return "You are on this call on another device"
	}
	d := spanOf(s.now - s.start)
	if s.end != 0 {
		d = spanOf(s.end - s.start)
	}
	if s.dm {
		switch {
		case s.rejected && s.creator:
			return strings.TrimSpace(s.dmName + " declined this call")
		case s.rejected:
			return "You declined this call"
		case s.missed && s.creator:
			return strings.TrimSpace(s.dmName + " missed this call")
		case s.missed:
			return "You missed this call"
		}
	}
	if s.end == 0 {
		if s.start > s.now {
			t := time.Unix(s.start, 0).In(loc)
			return "Starting on " + t.Format("Mon") + ", " + t.Format("Jan ") + ordinal(t.Day()) + " at " + FormatDate(t, "{time}")
		}
		switch {
		case d.w > 0:
			return "Started " + strconv.FormatInt(d.w, 10) + " weeks ago"
		case d.d > 0:
			return "Started " + strconv.FormatInt(d.d, 10) + " days ago"
		case d.h > 0 && d.m > 0:
			return "Started " + plural(d.h, "hour", "hours") + " and " + plural(d.m, "minute", "minutes") + " ago"
		case d.h > 0:
			return "Started " + plural(d.h, "hour", "hours") + " ago"
		case d.m > 0:
			return "Started " + plural(d.m, "minute", "minutes") + " ago"
		}
		return "Started a few seconds ago"
	}
	end := "Ended at " + FormatDate(time.Unix(s.end, 0).In(loc), "{time}") + " - Lasted "
	switch {
	case d.w > 0:
		return end + plural(d.w, "week", "weeks")
	case d.d > 0:
		return end + plural(d.d, "day", "days")
	case d.h > 0 && d.m > 0:
		return end + plural(d.h, "hour", "hours") + " and " + plural(d.m, "minute", "minutes")
	case d.h > 0:
		return end + plural(d.h, "hour", "hours")
	case d.m > 0:
		return end + plural(d.m, "minute", "minutes")
	}
	return end + "a few seconds"
}

// description is getCallDescription.
func (s callState) description() string {
	joined := plural(int64(s.count), "person", "people") + " joined"
	switch {
	case s.end == 0 && !s.dm && s.count == 0:
		return "Waiting for people to join"
	case s.end == 0 && !s.dm && s.count == 1:
		return "Waiting for others to join"
	case s.end != 0 && !s.dm:
		return joined
	}
	if s.end == 0 && s.creator && s.dm && s.count < 2 {
		if s.dmName != "" {
			return "Waiting for " + s.dmName + " to join"
		}
		if !s.in {
			return "Waiting for the other person to join"
		}
	}
	switch {
	case s.end == 0 && !s.creator && s.count < 2 && s.dm:
		return "Waiting for you to join"
	case s.end != 0 && s.dm && !s.missed && !s.rejected:
		if s.count == 2 {
			return "You both joined"
		}
		return joined
	}
	return ""
}

// button is getButtonText and renderPrimaryButton's type (no incoming
// invite, no call window of Clack's own, the calls-deprecation
// experiment off).
func (s callState) button() (text string, primary bool) {
	switch {
	case s.dm && s.end != 0 && s.missed && s.creator:
		text = "Call Again"
	case s.dm && s.end != 0 && s.missed:
		text = "Call Back"
	case s.end == 0 && !s.elsewhere && s.joinURL == "" && s.in:
		text = "Show Call"
	case s.end == 0:
		text = "Join"
	}
	if s.dm && s.missed && s.joinURL != "" {
		return "", false // renderPrimaryButton: none
	}
	return text, text != "" && s.end == 0
}
