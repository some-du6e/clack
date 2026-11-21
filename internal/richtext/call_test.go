package richtext

import (
	"strconv"
	"testing"
	"time"
)

// A call block draws Slack's PlatformCallSubtype card (boot-shared 9CtN,
// build 846030e1ce97), worded by getCallObjectText from the call the
// block carries (Ero4 convertCallSubtypeBlockForRedux). Synthetic calls.
func TestCallBlock(t *testing.T) {
	now := time.Now().Unix()
	at := func(sec int64) string { return strconv.FormatInt(sec, 10) }
	call := func(v1 string) string {
		return `[{"type":"call","block_id":"c","call_id":"R1","api_decoration_available":false,"call":{"media_backend_type":"platform_call","v1":{` + v1 + `}}}]`
	}
	icons := `"app_icon_urls":{"image_32":"https://call.test/32.png","image_72":"https://call.test/72.png"}`
	ended := func(start, end int64) string {
		return "Ended at " + FormatDate(time.Unix(end, 0), "{time}") + " - Lasted "
	}
	cases := []struct {
		name, blocks string
		want         callView
	}{
		{"ended platform call with a meeting id: no button, no facepile",
			call(`"id":"R1","name":"Standup","app_id":"A1",` + icons + `,"date_start":1700000000,"date_end":1700001500,"display_id":"123-456",` +
				`"join_url":"https://call.test/j","active_participants":[],"all_participants":[],"has_ended":true`),
			callView{Title: "Standup", Status: ended(1700000000, 1700001500) + "25 minutes", Description: "0 people joined",
				MeetingID: "123-456", JoinURL: "https://call.test/j", Icon: "https://call.test/72.png", Ended: true}},
		{"running call with people in it: Join (primary), the facepile, no description",
			call(`"id":"R2","name":"Design review","app_id":"A1",` + icons + `,"date_start":` + at(now-2*3600-5*60-30) +
				`,"join_url":"https://call.test/j2","active_participants":[{"slack_id":"U1"},{"slack_id":"USELF"},` +
				`{"external_id":"x1","display_name":"Guest","avatar_url":"https://call.test/guest.png"}],"all_participants":[]`),
			callView{Title: "Design review", Status: "Started 2 hours and 5 minutes ago", Button: "Join", Primary: true,
				JoinURL: "https://call.test/j2", Icon: "https://call.test/72.png",
				Faces: []string{"U1|alice|", "USELF|me|", "|Guest|https://call.test/guest.png"}}},
		{"running call nobody joined: waiting, Join, no facepile",
			call(`"id":"R3","name":"Zoom meeting","app_id":"A1",` + icons + `,"date_start":` + at(now-30) + `,"join_url":"https://call.test/j3"`),
			callView{Title: "Zoom meeting", Status: "Started a few seconds ago", Description: "Waiting for people to join",
				Button: "Join", Primary: true, JoinURL: "https://call.test/j3", Icon: "https://call.test/72.png"}},
		{"one person in: waiting for others",
			call(`"id":"R4","name":"1:1","app_id":"A1","date_start":` + at(now-3*60-10) + `,"join_url":"https://call.test/j4","active_participants":[{"slack_id":"U1"}]`),
			callView{Title: "1:1", Status: "Started 3 minutes ago", Description: "Waiting for others to join",
				Button: "Join", Primary: true, JoinURL: "https://call.test/j4", Faces: []string{"U1|alice|"}}},
		{"no name, ended after seconds, everyone who joined",
			call(`"id":"R5","app_id":"A00","date_start":1700000000,"date_end":1700000020,"all_participants":[{"slack_id":"U2"}],"active_participants":[]`),
			callView{Title: "Call Ended", Status: ended(1700000000, 1700000020) + "a few seconds", Description: "1 person joined",
				Ended: true, Faces: []string{"U2|@bob|"}}},
		{"long calls name their biggest unit",
			call(`"id":"R6","name":"Hackathon","date_start":1700000000,"date_end":` + at(1700000000+2*86400+3*3600)),
			callView{Title: "Hackathon", Status: ended(1700000000, 1700000000+2*86400+3*3600) + "2 days", Description: "0 people joined", Ended: true}},
		{"scheduled",
			call(`"id":"R7","app_id":"A1","date_start":` + at(now+3*86400)),
			callView{Title: "Scheduled Call", Description: "Waiting for people to join", Button: "Join", Primary: true,
				Status: func() string {
					s := time.Unix(now+3*86400, 0)
					return "Starting on " + s.Format("Mon") + ", " + s.Format("Jan ") + ordinal(s.Day()) + " at " + FormatDate(s, "{time}")
				}()}},
		{"a missed DM call: Call Back (outline), no description",
			call(`"id":"R8","is_dm_call":true,"created_by":"U1","was_missed":true,"date_start":1700000000,"date_end":1700000060,"all_participants":[{"slack_id":"U1"}]`),
			callView{Title: "Missed Call", Status: "You missed this call", Button: "Call Back", Ended: true}},
		{"accepted wins over missed",
			call(`"id":"R9","is_dm_call":true,"created_by":"U1","was_missed":true,"was_accepted":true,"date_start":1700000000,"date_end":1700000060,` +
				`"all_participants":[{"slack_id":"U1"},{"slack_id":"USELF"}]`),
			callView{Title: "Call Ended", Status: ended(1700000000, 1700000060) + "1 minute", Description: "You both joined", Ended: true,
				Faces: []string{"U1|alice|", "USELF|me|"}}},
	}
	for _, c := range cases {
		d := blocksMsg(t, c.blocks)
		got, ok := callOf(*d)
		if !ok {
			t.Errorf("%s: no call card in %s", c.name, dump(d.Blocks))
			continue
		}
		eq(t, c.name, got, c.want)
	}
}

// TestCallGlyph: without an app icon the card draws renderCallIcon's
// font icon (9CtN): phone, phone-flat once the call ended, and for a DM
// call the viewer missed call-missed, declined call-declined (the invite
// response: accepted wins, then reject, then missed).
func TestCallGlyph(t *testing.T) {
	call := func(v1 string) string {
		return `[{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","date_start":1700000000,` + v1 + `}}}]`
	}
	for _, c := range []struct{ name, v1, want string }{
		{"running", `"date_end":0`, "phone"},
		{"ended", `"date_end":1700000600`, "phone-flat"},
		{"a missed DM call", `"date_end":1700000060,"is_dm_call":true,"was_missed":true`, "call-missed"},
		{"a declined DM call", `"date_end":1700000060,"is_dm_call":true,"was_rejected":true`, "call-declined"},
		{"a DM call accepted after all", `"date_end":1700000600,"is_dm_call":true,"was_accepted":true,"was_missed":true`, "phone-flat"},
		{"missed but not a DM call", `"date_end":1700000060,"was_missed":true`, "phone-flat"},
	} {
		if got := callGlyphOf(*blocksMsg(t, call(c.v1))); got != c.want {
			t.Errorf("%s: glyph %q, want %q", c.name, got, c.want)
		}
	}
}
