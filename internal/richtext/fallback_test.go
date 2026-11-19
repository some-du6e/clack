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
	}
	for _, c := range cases {
		d := blocksMsg(t, c.blocks)
		eq(t, c.name, d.Blocks, c.want)
	}
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
