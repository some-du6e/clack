package richtext

import "testing"

// Slack's message renderers (InteractiveBlocks / ReadonlyBlocks, build
// 846030e1ce97) draw a block whose type has no renderable through
// resolveFallback (boot-shared Q5CE): its fallback list's payload items,
// each through the same lookup, its one payload item, else
// UnsupportedBlockFallback (1G65). Types in Slack's own enum fall back
// too when nothing renders them (fallback_canary, expandable, group,
// error). Fixtures are synthetic.

var cantDisplay = Block{Kind: UnknownBlock, Spans: []Span{tx("This message can’t be fully displayed.")}}

func TestBlockFallbacks(t *testing.T) {
	sec := func(s string) string { return `{"type":"section","text":{"type":"mrkdwn","text":"` + s + `"}}` }
	secB := func(s string) Block { return Block{Kind: Section, Kit: true, Spans: []Span{tx(s)}} }
	div := Block{Kind: Divider, Kit: true}
	cases := []struct {
		name, blocks string
		want         []Block
	}{
		{"fallback_canary draws its fallback list",
			`[{"type":"fallback_canary","fallback":[` + sec("degraded") + `,{"type":"divider"}]}]`,
			[]Block{secB("degraded"), div}},
		{"enum types nothing renders are the card",
			`[{"type":"expandable"},{"type":"group"},{"type":"error"}]`,
			[]Block{cantDisplay, cantDisplay, cantDisplay}},
		{"an empty fallback list draws nothing",
			`[` + sec("before") + `,{"type":"mystery","fallback":[]},{"type":"divider"}]`,
			[]Block{secB("before"), div}},
		{"nested fallbacks recurse; non-payload items are skipped",
			`[{"type":"mystery","fallback":[{"type":"also_unknown","fallback":{"type":"header","text":{"type":"plain_text","text":"deep"}}},` +
				`{"type":"other_unknown"},"not a payload",{"no":"type"},` + sec("kept") + `]}]`,
			[]Block{{Kind: Header, Kit: true, Spans: []Span{tx("deep")}}, cantDisplay, secB("kept")}},
		{"a fallback that isn't a block or a list is the card",
			`[{"type":"mystery","fallback":"text"},{"type":"mystery","fallback":42}]`,
			[]Block{cantDisplay, cantDisplay}},
		{"a fallback_canary without a fallback is the card",
			`[{"type":"fallback_canary"}]`,
			[]Block{cantDisplay}},
		// Slack draws a message's blocks instead of its text whenever it
		// has any (shouldRenderBlocksInsteadOfText), even if they draw
		// nothing; only a block Clack can't draw yet lets the text stand in
		{"a lone empty fallback draws an empty body, not the text",
			`[{"type":"mystery","fallback":[]}]`,
			nil},
		// Blocks (lby4) draws nothing when they're all null or rich_text
		// without elements, and the text still doesn't draw: the array
		// isn't empty (iub5 hasMessageBlocks)
		{"blocks that are all null or empty rich_text draw an empty body, not the text",
			`[null,{"type":"rich_text","elements":[]}]`,
			nil},
		{"a block Clack can't draw yet lets the text stand in",
			`[{"type":"timeline","block_id":"t1"}]`,
			[]Block{para(tx("fallback text"))}},
		{"a rich_text piece or an inline element at the top level draws as inside a rich_text",
			`[{"type":"rich_text_section","elements":[{"type":"text","text":"hi"}]},{"type":"text","text":"there"}]`,
			[]Block{para(tx("hi")), para(tx("there"))}},
		// their fields' shapes (an object name, object rows) no longer
		// fail the decode into the per-block error line
		{"known blocks Clack can't draw keep that rule whatever their fields' shapes",
			`[{"type":"auth_provider","name":{"type":"plain_text","text":"Google"}},{"type":"canvas_table","rows":[{"cells":[]}]},` +
				`{"type":"layout","rows":[{"items":[]}]},{"type":"data_visualization","title":"Q3","chart":{"type":"pie"}},` +
				`{"type":"share_prompt","label":"Share"}]`,
			[]Block{para(tx("fallback text"))}},
	}
	for _, c := range cases {
		d := blocksMsg(t, c.blocks)
		eq(t, c.name, d.Blocks, c.want)
	}
	// child blocks: a container's unknown child is the card (yWQF's slot
	// keeps the message's fallback component), a callout's draws nothing
	// (createRenderBlocks has none), and so does one in a container that
	// sits in a callout
	d := blocksMsg(t, `[{"type":"container","title":{"type":"plain_text","text":"Box"},"child_blocks":[{"type":"future_widget"},`+sec("kept")+`]},
		{"type":"callout","child_blocks":[{"type":"future_widget"},{"type":"container","title":{"type":"plain_text","text":"Inner"},
			"child_blocks":[{"type":"future_widget"},`+sec("inner")+`]}]}]`)
	kinds := func(bs []Block) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.kindName())
		}
		return out
	}
	if len(d.Blocks) != 2 || len(d.Blocks[1].Children) != 1 {
		t.Fatalf("blocks %s", dump(d.Blocks))
	}
	eq(t, "container's children", kinds(d.Blocks[0].Children), []string{"UnknownBlock", "Section"})
	eq(t, "callout's container's children", kinds(d.Blocks[1].Children[0].Children), []string{"Section"})
}

// teamRes is the fake resolver with a workspace and an enterprise.
type teamRes struct {
	fakeRes
	team, ent string
}

func (r teamRes) TeamIDs() (string, string) { return r.team, r.ent }

// Condition blocks (CRur expandConditionBlocks): the first case whose
// conditions all hold, else default, else nothing. "client" compares
// with "desktop", "team" with the viewer's workspace or enterprise,
// "content_type" matches condition_demo whatever the op.
func TestConditionBlocks(t *testing.T) {
	r := teamRes{team: "T1", ent: "E1"}
	sec := func(s string) string { return `{"type":"section","text":{"type":"mrkdwn","text":"` + s + `"}}` }
	secB := func(s string) Block { return Block{Kind: Section, Kit: true, Spans: []Span{tx(s)}} }
	cond := func(conds, blocks string) string { return `{"conditions":[` + conds + `],"blocks":[` + blocks + `]}` }
	cases := []struct {
		name, blocks string
		want         []Block
	}{
		{"client desktop matches",
			`[{"type":"condition","cases":[` + cond(`{"type":"client","op":"=","value":"ios"}`, sec("ios")) + `,` +
				cond(`{"type":"client","op":"=","value":"desktop"}`, sec("desktop")+`,{"type":"divider"}`) + `],"default":[` + sec("default") + `]}]`,
			[]Block{secB("desktop"), {Kind: Divider, Kit: true}}},
		{"client != and team by enterprise",
			`[{"type":"condition","cases":[` + cond(`{"type":"client","op":"!=","value":"android"},{"type":"team","op":"=","value":"E1"}`, sec("both")) + `]}]`,
			[]Block{secB("both")}},
		{"team != the viewer's falls to default",
			`[{"type":"condition","cases":[` + cond(`{"type":"team","op":"!=","value":"T1"}`, sec("other team")) + `],"default":[` + sec("default") + `]}]`,
			[]Block{secB("default")}},
		{"content_type matches condition_demo whatever the op",
			`[{"type":"condition","cases":[` + cond(`{"type":"content_type","op":"!=","value":"condition_demo"}`, sec("demo")) + `]}]`,
			[]Block{secB("demo")}},
		{"unknown condition types and ops never hold; no default is nothing",
			`[` + sec("before") + `,{"type":"condition","cases":[` + cond(`{"type":"weather","op":"=","value":"sunny"}`, sec("x")) + `,` +
				cond(`{"type":"client","op":"~","value":"desktop"}`, sec("y")) + `]},` + sec("after") + `]`,
			[]Block{secB("before"), secB("after")}},
		{"an empty case list is its default",
			`[{"type":"condition","cases":[],"default":[` + sec("default") + `]}]`,
			[]Block{secB("default")}},
		{"a case without conditions holds",
			`[{"type":"condition","cases":[{"conditions":[],"blocks":[` + sec("always") + `]}]}]`,
			[]Block{secB("always")}},
		{"cases that aren't a list: no renderable, so the card",
			`[{"type":"condition","cases":"nope"}]`,
			[]Block{cantDisplay}},
		{"a lone condition with no case for the desktop is an empty body, not the text",
			`[{"type":"condition","cases":[` + cond(`{"type":"client","op":"=","value":"ios"}`, sec("ios")) + `]}]`,
			nil},
	}
	for _, c := range cases {
		d := FromMessage(r, msg(t, `{"ts":"1.0","text":"fallback text","blocks":`+c.blocks+`}`))
		checkInvariants(t, c.name, d.Blocks)
		eq(t, c.name, d.Blocks, c.want)
	}
	// attachments' blocks expand too (MessageAttachmentDefaultV2 draws them through the same renderers)
	d := FromMessage(r, msg(t, `{"ts":"1.0","text":"x","attachments":[{"id":1,"color":"36a64f","blocks":[{"type":"condition","cases":[`+
		cond(`{"type":"team","op":"=","value":"T1"}`, sec("in attachment"))+`]}]}]}`))
	var got []Block
	for _, b := range d.Blocks {
		if b.Att != nil {
			got = b.Att.Text[b.Att.KitFrom:]
		}
	}
	eq(t, "attachment", got, []Block{secB("in attachment")})
}

// Table cells: Slack's TableBlock (bx/I) renders raw_text, cell_text and
// raw_number cells by their text.
func TestTableTextCells(t *testing.T) {
	d := blocksMsg(t, `[{"type":"table","rows":[
		[{"type":"raw_text","text":"Item"},{"type":"raw_text","text":"Count"}],
		[{"type":"cell_text","text":"apples"},{"type":"raw_number","value":42,"text":"42"}],
		[{"type":"raw_text","text":"pears"},{"type":"raw_number","value":1234.5,"text":"1,234.5"}]]}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != Table {
		t.Fatalf("blocks: %s", dump(d.Blocks))
	}
	eq(t, "cells", d.Blocks[0].Rows, [][][]Span{
		{{tx("Item")}, {tx("Count")}},
		{{tx("apples")}, {tx("42")}},
		{{tx("pears")}, {tx("1,234.5")}},
	})
}

// An inline element Slack's renderer has no component for is
// UnknownElement (Dvg3): a link to its url when it has one, else its
// text, else nothing.
func TestUnknownInlineElement(t *testing.T) {
	d := blocksMsg(t, `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		{"type":"text","text":"a "},{"type":"future_thing","text":"linked","url":"https://f.test/x"},
		{"type":"text","text":" b "},{"type":"future_thing","url":"https://f.test/bare"},
		{"type":"text","text":" c "},{"type":"future_thing","text":"plain"},
		{"type":"text","text":" d"},{"type":"future_thing"}]}]}]`)
	eq(t, "unknown inline", d.Blocks, []Block{para(tx("a "), lk("linked", "https://f.test/x"), tx(" b "),
		lk("https://f.test/bare", "https://f.test/bare"), tx(" c "), tx("plain"), tx(" d"))})
	// J825's last branches: a null fallback draws nothing, an object
	// fallback draws in the element's place, and UnknownElement takes the
	// text only when it's a string (BeP8); team is no type of Slack's
	// either, so its team_id draws nothing; a citation without
	// details.citation_type isn't Citation's and Lv's gate leaves it
	// empty; inside preformatted text the url still links
	d = blocksMsg(t, `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		{"type":"text","text":"e "},{"type":"future_mention","text":"@x","fallback":{"type":"text","text":"@Ana"}},
		{"type":"text","text":" f "},{"type":"future_mention","text":"@x","fallback":null},
		{"type":"text","text":" g "},{"type":"future_mention","text":{"type":"plain_text","text":"obj"}},
		{"type":"text","text":" h "},{"type":"team","team_id":"T1"},
		{"type":"text","text":" i "},{"type":"citation","text":"[1]","url":"https://s.test/a"},
		{"type":"text","text":" j"}]},
		{"type":"rich_text_preformatted","elements":[{"type":"future_thing","url":"https://f.test/pre"}]}]}]`)
	if len(d.Blocks) != 2 {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	eq(t, "fallbacks", d.Blocks[0], para(tx("e "), tx("@Ana"), tx(" f "), tx(" g "), tx(" h "), tx(" i "), tx(" j")))
	if sp := d.Blocks[1].Spans; len(sp) != 1 || sp[0].Kind != Link || sp[0].URL != "https://f.test/pre" {
		t.Errorf("preformatted unknown element %+v, want a link", sp)
	}
}
