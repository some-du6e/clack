package richtext

import "testing"

// A card block is Slack's Card (boot-shared ARtW → 7Xkb): its hero
// image, icon (the slack_icon wins), title and subtitle, body, subtext and
// its actions split by partitionActions (fWi9): with three or more the
// danger ones go left, the rest sit right, danger first and primary last;
// a button's type is getButtonType (szY5: style_v2, then style).
func TestCardBlock(t *testing.T) {
	btn := func(id, style, v2 string) string {
		s := `{"type":"button","action_id":"` + id + `","text":{"type":"plain_text","text":"` + id + `"}`
		if style != "" {
			s += `,"style":"` + style + `"`
		}
		if v2 != "" {
			s += `,"style_v2":"` + v2 + `"`
		}
		return s + `}`
	}
	cases := []struct {
		name, blocks string
		want         []string
	}{
		{"every part; two actions sit right, the primary last",
			`[{"type":"card","block_id":"c1","hero_image":{"type":"image","image_url":"https://img.test/h.png","alt_text":"harbour"},
			  "icon":{"type":"image","image_url":"https://img.test/i.png","alt_text":"app"},
			  "title":{"type":"mrkdwn","text":"*Plan* it"},"subtitle":{"type":"mrkdwn","text":"Due Friday"},
			  "body":{"type":"mrkdwn","text":"Review the roadmap"},"subtext":{"type":"mrkdwn","text":"Updated"},
			  "actions":[` + btn("open", "primary", "") + `,` + btn("later", "", "") + `]}]`,
			[]string{"hero=https://img.test/h.png|alt=harbour|icon=https://img.test/i.png|slack=|title=Plan it|subtitle=Due Friday|" +
				"body=Review the roadmap|subtext=Updated|left=[]|right=[later/ open/primary]"}},
		{"three actions: the danger one goes left; style_v2 wins over style",
			`[{"type":"card","block_id":"c2","slack_icon":{"name":"calendar"},"icon":{"type":"image","image_url":"https://img.test/i.png"},
			  "title":{"type":"plain_text","text":"Review"},
			  "actions":[` + btn("decline", "danger", "") + `,` + btn("accept", "primary", "") + `,` + btn("maybe", "danger", "default") + `]}]`,
			[]string{"hero=|alt=|icon=https://img.test/i.png|slack=calendar|title=Review|subtitle=|body=|subtext=|left=[decline/danger]|" +
				"right=[maybe/ accept/primary]"}},
		{"under three actions nothing goes left: danger first",
			`[{"type":"card","block_id":"c3","actions":[` + btn("ok", "primary", "") + `,` + btn("delete", "danger", "") + `]}]`,
			[]string{"hero=|alt=|icon=|slack=|title=|subtitle=|body=|subtext=|left=[]|right=[delete/danger ok/primary]"}},
	}
	for _, c := range cases {
		eq(t, c.name, cardOf(*blocksMsg(t, c.blocks)), c.want)
	}
}
