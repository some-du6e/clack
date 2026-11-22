package richtext

import "testing"

// dmRes is the fake resolver that knows the IM D1 is with U2.
type dmRes struct{ fakeRes }

func (dmRes) DMUser(ch string) (string, bool) {
	if ch == "D1" {
		return "U2", true
	}
	return "", false
}

// TestCallCardEdges: a DM call's wording names the other member
// (getOtherMemberNameInDmCall: the call's D channel's other member); a
// missed or declined DM call with a join_url and its facepile hidden has
// no footer (renderCallFooter returns null); a call whose app icons carry
// a non-string value or whose channels hold an odd scalar still draws
// (Slack never decodes app_icon_urls: the icon is the app's).
func TestCallCardEdges(t *testing.T) {
	call := func(v1 string) string {
		return `{"ts":"1.0","text":"call","blocks":[{"type":"call","block_id":"c","call_id":"R1","call":{"v1":{` + v1 + `}}}]}`
	}
	dm := `"is_dm_call":true,"channels":["D1"],"date_start":1700000000,`
	cases := []struct {
		name, msg string
		check     func(v callView) bool
	}{
		{"the creator's missed DM call names the other member",
			call(`"id":"R5",` + dm + `"created_by":"USELF","was_missed":true,"date_end":1700000060,"all_participants":[{"slack_id":"USELF"}]`),
			func(v callView) bool { return v.Status == "@bob missed this call" }},
		{"the creator's declined DM call names them too",
			call(`"id":"R6",` + dm + `"created_by":"USELF","was_rejected":true,"date_end":1700000060,"all_participants":[{"slack_id":"USELF"}]`),
			func(v callView) bool { return v.Status == "@bob declined this call" }},
		{"a running DM call the viewer started waits for them by name",
			call(`"id":"R7",` + dm + `"created_by":"USELF","active_participants":[{"slack_id":"USELF"}],"join_url":"https://call.test/j7"`),
			func(v callView) bool { return v.Description == "Waiting for @bob to join" }},
		{"the callee's missed DM call with a join_url: no footer",
			call(`"id":"R8",` + dm + `"created_by":"U2","was_missed":true,"date_end":1700000060,"join_url":"https://call.test/j8",` +
				`"all_participants":[{"slack_id":"U2"}]`),
			func(v callView) bool { return v.NoFooter && v.Status == "You missed this call" && v.Faces == nil }},
		{"odd icon and channel values don't fail the card",
			call(`"id":"R9","name":"Standup","app_id":"A1","app_icon_urls":{"image_72":"https://call.test/72.png","is_default":false},` +
				`"channels":["C1",7],"date_start":1700000000,"join_url":"https://call.test/j9"`),
			func(v callView) bool { return v.Title == "Standup" && v.Icon == "https://call.test/72.png" }},
	}
	for _, c := range cases {
		d := FromMessage(dmRes{}, msg(t, c.msg))
		v, ok := callOf(d)
		if !ok || !c.check(v) {
			t.Errorf("%s: %+v (card %v) in %s", c.name, v, ok, dump(d.Blocks))
		}
	}
}
