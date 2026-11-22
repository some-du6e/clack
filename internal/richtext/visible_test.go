package richtext

import "testing"

// visible_to_user_ids: only Slack's Actions block reads it (boot-shared
// /jJF maybeRenderActionsElement): a button or checkboxes whose list
// leaves out the viewer isn't drawn, but it keeps its place (the count
// behind "+ N more" and the first five). A section's accessory and an
// input's element draw for everyone (Section bzDH, InputBlock h/Gw); an
// icon_button hides itself anywhere (X2Vm). The fake viewer is USELF.
func TestVisibleToUserIDs(t *testing.T) {
	btn := func(id, vis string) string {
		v := ""
		if vis != "" {
			v = `,"visible_to_user_ids":` + vis
		}
		return `{"type":"button","action_id":"` + id + `","text":{"type":"plain_text","text":"` + id + `"}` + v + `}`
	}
	cb := func(id string) string {
		return `{"type":"checkboxes","action_id":"` + id + `","options":[{"text":{"type":"plain_text","text":"x"},"value":"x"}],"visible_to_user_ids":["U2"]}`
	}
	icon := func(id string) string {
		return `{"type":"icon_button","action_id":"` + id + `","icon":"trash","text":{"type":"plain_text","text":"Delete"},"visible_to_user_ids":["U2"]}`
	}
	d := blocksMsg(t, `[{"type":"actions","block_id":"a","elements":[`+btn("all", "")+`,`+btn("empty", `[]`)+`,`+btn("mine", `["U1","USELF"]`)+`,`+
		btn("theirs", `["U1"]`)+`,`+cb("cb")+`,`+icon("icon")+`]},
		{"type":"section","block_id":"s","text":{"type":"mrkdwn","text":"an accessory for U2"},"accessory":`+btn("acc", `["U2"]`)+`},
		{"type":"input","block_id":"i","label":{"type":"plain_text","text":"Pick"},"element":`+cb("in")+`},
		{"type":"section","block_id":"s2","text":{"type":"mrkdwn","text":"an icon button for U2"},"accessory":`+icon("icon2")+`},
		{"type":"actions","block_id":"h","elements":[`+btn("only", `["U2"]`)+`]}]`)
	var got []string
	for _, b := range d.Blocks {
		for _, bt := range b.Buttons {
			s := bt.ActionID
			if invisible(bt) {
				s += " (hidden)"
			}
			got = append(got, s)
		}
	}
	eq(t, "elements", got, []string{"all", "empty", "mine", "theirs (hidden)", "cb (hidden)", "icon (hidden)", "acc", "in", "only (hidden)"})
	// the hidden-only actions block stays (Slack keeps its empty wrapper),
	// and the section whose icon_button hid itself keeps its text
	var kinds []string
	for _, b := range d.Blocks {
		kinds = append(kinds, b.kindName()+" "+PlainText(Doc{Blocks: []Block{b}}))
	}
	eq(t, "blocks", kinds, []string{"Actions ", "Section an accessory for U2", "Section Pick", "Section an icon button for U2", "Actions "})
}
