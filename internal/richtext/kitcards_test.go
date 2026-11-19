package richtext

import "testing"

// Slack's alert (MyNk/MlDl), callout (zMv1/Xhau) and loading (KSQB)
// blocks, build 846030e1ce97. Synthetic fixtures.
func TestBoxedBlocks(t *testing.T) {
	cases := []struct {
		name, blocks string
		want         []string
	}{
		{"alert levels: unset and unknown are default",
			`[{"type":"alert","text":{"type":"mrkdwn","text":"*Heads up*: maintenance"},"level":"warning"},
			  {"type":"alert","text":{"type":"plain_text","text":"Saved"},"level":"success"},
			  {"type":"alert","text":{"type":"plain_text","text":"Plain"}},
			  {"type":"alert","text":{"type":"plain_text","text":"Odd"},"level":"loud"},
			  {"type":"alert","text":{"type":"mrkdwn","text":"FYI"},"level":"info"},
			  {"type":"alert","text":{"type":"mrkdwn","text":"Failed"},"level":"error"}]`,
			[]string{"alert warning: Heads up: maintenance", "alert success: Saved", "alert default: Plain", "alert default: Odd",
				"alert info: FYI", "alert error: Failed"}},
		{"callout colours and child blocks; unknown children without a fallback draw nothing",
			`[{"type":"callout","background_color":"indigo","child_blocks":[{"type":"header","text":{"type":"plain_text","text":"Title"}},
				{"type":"section","text":{"type":"mrkdwn","text":"body"}},{"type":"mystery"},{"type":"divider"},
				{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"rich"}]}]}]},
			  {"type":"callout","background_color":"teal","child_blocks":[]},
			  {"type":"callout"}]`,
			[]string{"callout indigo [Header* Section* Divider* Paragraph]", "callout green []", "callout green []"}},
		{"loading", `[{"type":"loading","block_id":"l1"}]`, []string{"loading"}},
		{"a header's numeric level still decodes beside a string one",
			`[{"type":"header","text":{"type":"plain_text","text":"H"},"level":2},{"type":"alert","text":{"type":"plain_text","text":"A"},"level":"info"}]`,
			[]string{"alert info: A"}},
	}
	for _, c := range cases {
		d := blocksMsg(t, c.blocks)
		eq(t, c.name, boxedOf(*d), c.want)
	}
	d := blocksMsg(t, `[{"type":"header","text":{"type":"plain_text","text":"H"},"level":2}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Level != 2 {
		t.Errorf("header level: %s", dump(d.Blocks))
	}
}
