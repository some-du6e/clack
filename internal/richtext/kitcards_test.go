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
		{"containers: rich_text_title wins, widths, collapsing, the divider only without collapsing",
			`[{"type":"container","title":{"type":"plain_text","text":"Plain :wave:"},"rich_text_title":{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Rich ","style":{"bold":true}},{"type":"emoji","name":"smile"}]}]},
				"subtitle":{"type":"mrkdwn","text":"*sub* title"},"icon":{"type":"image","image_url":"https://img.test/i.png","alt_text":"i"},"width":"wide","has_header_divider":true,
				"child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"a"}},{"type":"mystery"},{"type":"divider"}]},
			  {"type":"container","title":{"type":"plain_text","text":"Fold"},"is_collapsible":true,"default_collapsed":true,"has_header_divider":true,"width":"enormous","child_blocks":[]},
			  {"type":"container","title":{"type":"plain_text","text":"Open"},"default_collapsed":true},
			  {"type":"container","title":{"type":"plain_text","text":"Null"},"width":null}]`,
			// a width Slack lacks, or null, adds no max-width class: full
			// ("standard" is only width's default parameter)
			// the unknown child gets the container's fallback card (yWQF's
			// slot keeps the message's UnsupportedBlockFallback)
			[]string{"container wide title=Rich 😄 subtitle=sub title icon=https://img.test/i.png divider [Section UnknownBlock Divider]",
				"container full title=Fold subtitle= icon= collapsible collapsed []",
				"container standard title=Open subtitle= icon= []",
				"container full title=Null subtitle= icon= []"}},
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
	// beside a string level, the header's numeric one still decodes: it
	// draws, with its level
	d = blocksMsg(t, `[{"type":"header","text":{"type":"plain_text","text":"H"},"level":2},{"type":"alert","text":{"type":"plain_text","text":"A"},"level":"info"}]`)
	if len(d.Blocks) != 2 || d.Blocks[0].Kind != Header || d.Blocks[0].Level != 2 {
		t.Errorf("header beside an alert: %s", dump(d.Blocks))
	}
}
