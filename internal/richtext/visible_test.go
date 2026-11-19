package richtext

import "testing"

// visible_to_user_ids: Slack's Actions block draws a button or
// checkboxes only for the members listed (client-boot isVisibleToMember:
// no list, or the viewer in it); the section accessory too. The fake
// viewer is USELF.
func TestVisibleToUserIDs(t *testing.T) {
	btn := func(id, vis string) string {
		v := ""
		if vis != "" {
			v = `,"visible_to_user_ids":` + vis
		}
		return `{"type":"button","action_id":"` + id + `","text":{"type":"plain_text","text":"` + id + `"}` + v + `}`
	}
	d := blocksMsg(t, `[{"type":"actions","block_id":"a","elements":[`+btn("all", "")+`,`+btn("empty", `[]`)+`,`+btn("mine", `["U1","USELF"]`)+`,`+
		btn("theirs", `["U1"]`)+`,{"type":"checkboxes","action_id":"cb","options":[{"text":{"type":"plain_text","text":"x"},"value":"x"}],"visible_to_user_ids":["U2"]}]},
		{"type":"section","block_id":"s","text":{"type":"mrkdwn","text":"hidden accessory"},"accessory":`+btn("acc", `["U2"]`)+`}]`)
	var got []string
	for _, b := range d.Blocks {
		for _, bt := range b.Buttons {
			got = append(got, bt.ActionID)
		}
	}
	eq(t, "visible elements", got, []string{"all", "empty", "mine"})
}
