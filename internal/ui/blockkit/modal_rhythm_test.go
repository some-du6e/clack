package blockkit

// A modal's blocks are spaced as Slack's modal CSS spaces them (Karim,
// #clack 2026-11-22: "is the spacing intended?", on a stretchy emoji
// app's modal): block wrappers 12px apart (.p-block_kit_modal
// .p-block_wrapper--modal_view:not(:first-of-type){margin:12px 0 0})
// whose margins collapse with the blocks' own; an actions block's elements
// 8px down inside it (each action's margin-top in its flex row) with its
// 8px margin-bottom under them; checkboxes and radio buttons a row of
// their own (p-actions_block__action--full_width); a code block's 4px
// (.c-mrkdwn__pre) collapsing into the 12. Clack put every block 8px from
// the next, an actions block's elements at its very top, and checkboxes
// beside a select. The newer blocks' own margins collapse into the 12
// too (.alertBlock and .card 8px 0, .containerBlock 16px 0 8px,
// UnsupportedBlockFallback's 8px 0), where Clack added them on top; a
// header keeps its 1em above it when it comes first (a view has no
// p-block_kit_renderer for the 4px rule). App Home's blocks keep their
// own margins, collapsing (no wrapper margins, its actions wrapper a flex
// box), and a code channel view's draw as a message's do: margins adding
// up, the first block's top taken off. Measured from the surface's height
// alone, so the old layout draws through the same calls. Synthetic blocks.

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

func rhythmText(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }

// modalSurface lays the blocks out as a modal's view 480px wide.
func modalSurface(t *testing.T, h Host, blocks ...any) (*Surface, int) {
	t.Helper()
	return kindSurface(t, h, "modal", blocks...)
}

// kindSurface lays the blocks out 480px wide as a view of type typ:
// "modal", "home", or "code_channel_view" (NewCodeChannelSurface's).
func kindSurface(t *testing.T, h Host, typ string, blocks ...any) (*Surface, int) {
	t.Helper()
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	var s *Surface
	if typ == "code_channel_view" {
		s = NewCodeChannelSurface(h, raw, "C1", "V1", "S1", "A1")
	} else {
		s = newSurface(h, &core.View{Type: typ, Blocks: raw})
	}
	return s, s.Layout(visGtx(480)).Size.Y
}

// rhythm is the blocks the rhythm tests lay out.
type rhythm struct {
	input, sel, boxes, copyBtn, para, pre, header, section, divider, context, alert, card, container, callout, table, unknown any
	actions                                                                                                                   func(id string, els ...any) map[string]any
	button                                                                                                                    func(id string) map[string]any
}

func rhythmBlocks() rhythm {
	opt := func(t, v string) map[string]any { return map[string]any{"text": rhythmText(t), "value": v} }
	mrkdwn := func(s string) map[string]any { return map[string]any{"type": "mrkdwn", "text": s} }
	section := func(s string) map[string]any { return map[string]any{"type": "section", "text": mrkdwn(s)} }
	r := rhythm{
		input: map[string]any{"type": "input", "block_id": "say", "label": rhythmText("What should it say?"),
			"element": map[string]any{"type": "plain_text_input", "action_id": "v", "multiline": true}},
		sel:   map[string]any{"type": "static_select", "action_id": "style", "options": []any{opt("stretchy", "s"), opt("blocky", "b")}},
		boxes: map[string]any{"type": "checkboxes", "action_id": "flags", "options": []any{opt("Give every tile a face", "f"), opt("Tight spacing", "t")}},
		para: map[string]any{"type": "rich_text", "block_id": "preview", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": "a preview"}}}}},
		pre: map[string]any{"type": "rich_text", "block_id": "code", "elements": []any{
			map[string]any{"type": "rich_text_preformatted", "elements": []any{map[string]any{"type": "text", "text": ":tile::tile:"}}}}},
		header:    map[string]any{"type": "header", "text": rhythmText("A header")},
		section:   section("A section"),
		divider:   map[string]any{"type": "divider"},
		context:   map[string]any{"type": "context", "elements": []any{mrkdwn("A context")}},
		alert:     map[string]any{"type": "alert", "block_id": "al", "level": "info", "text": mrkdwn("Heads up")},
		card:      map[string]any{"type": "card", "block_id": "cd", "title": rhythmText("A card"), "body": mrkdwn("Its body")},
		container: map[string]any{"type": "container", "block_id": "ct", "title": rhythmText("A container"), "child_blocks": []any{section("inside")}},
		callout:   map[string]any{"type": "callout", "block_id": "co", "child_blocks": []any{section("tinted")}},
		table: map[string]any{"type": "table", "block_id": "tb", "rows": []any{
			[]any{map[string]any{"type": "raw_text", "text": "a"}, map[string]any{"type": "raw_text", "text": "b"}}}},
		unknown: map[string]any{"type": "zz_no_such_block", "block_id": "uk"},
	}
	r.actions = func(id string, els ...any) map[string]any {
		return map[string]any{"type": "actions", "block_id": id, "elements": els}
	}
	r.button = func(id string) map[string]any {
		return map[string]any{"type": "button", "action_id": id, "text": rhythmText(id)}
	}
	r.copyBtn = r.actions("copy", map[string]any{"type": "button", "action_id": "copy", "style": "primary", "text": rhythmText("Copy")})
	return r
}

// rhythmCase is two blocks and what laying them out together adds over
// each alone: the collapsed margin between them less a's bottom and b's
// top (each alone keeps its own margins on the surface).
type rhythmCase struct {
	name string
	a, b any
	want int
}

func checkRhythm(t *testing.T, h Host, typ, rule string, cases []rhythmCase) {
	t.Helper()
	height := func(bs ...any) int {
		_, y := kindSurface(t, h, typ, bs...)
		return y
	}
	for _, c := range cases {
		if got := height(c.a, c.b) - height(c.a) - height(c.b); got != c.want {
			t.Errorf("%s, %s: %dpx between them over each alone, want %d (%s)", typ, c.name, got, c.want, rule)
		}
	}
}

func TestModalRhythmLikeSlack(t *testing.T) {
	h, _ := visibleHost()
	r := rhythmBlocks()
	height := func(bs ...any) int {
		_, y := modalSurface(t, h, bs...)
		return y
	}

	// an actions block: its button 8px down inside it, 8px under it
	s, got := modalSurface(t, h, r.copyBtn)
	b := &s.blocks[0]
	btnH := s.widget(b, &b.Elements[0]).size(visGtx(480), 480, false).Y
	if want := 8 + btnH + 8; got != want {
		t.Errorf("an actions block alone is %dpx, want %d: its %dpx button 8px down inside it, 8px under it", got, want, btnH)
	}
	// what two blocks add between them over each alone: Slack's collapsed
	// wrapper margin, max(a's bottom, 12, b's top), less a's bottom and b's top
	checkRhythm(t, h, "modal", "wrappers 12px apart, margins collapsing", []rhythmCase{
		{"input, then actions", r.input, r.copyBtn, 12},
		{"actions, then rich_text", r.copyBtn, r.para, 12 - 8},
		{"rich_text, then rich_text", r.para, r.para, 12},
		{"rich_text, then actions", r.para, r.copyBtn, 12},
		{"actions, then a code block", r.copyBtn, r.pre, 12 - 8 - 4},
		// the newer blocks' own margins collapse into the 12
		{"input, then an alert", r.input, r.alert, 12 - 8},
		{"an alert, then an alert", r.alert, r.alert, 12 - 8 - 8},
		{"an alert, then rich_text", r.alert, r.para, 12 - 8},
		{"rich_text, then a card", r.para, r.card, 12 - 8},
		{"a card, then a container", r.card, r.container, 16 - 8 - 16},
		{"a container, then rich_text", r.container, r.para, 12 - 8},
		{"rich_text, then a container", r.para, r.container, 16 - 16},
		{"a callout, then rich_text", r.callout, r.para, 12},
		{"rich_text, then a table", r.para, r.table, 12}, // .tableMargin's 4px is padding: inside
		{"rich_text, then an unknown block", r.para, r.unknown, 12 - 8},
		{"rich_text, then a header", r.para, r.header, 15 - 15},
	})
	// a header's 1em above it is the same when it comes first
	if first, after := height(r.header), height(r.para, r.header)-height(r.para); first != after {
		t.Errorf("a header first is %dpx, after rich_text it adds %dpx: want its 15px top margin both times (a view's blocks have no p-block_kit_renderer, the 4px first-child rule's)", first, after)
	}
	// the select and the checkboxes: two rows, the checkboxes' a whole
	// row 8px under the select's, not side by side
	if both, one, other := height(r.actions("opts", r.sel, r.boxes)), height(r.actions("sel", r.sel)), height(r.actions("cb", r.boxes)); both != one+other-8 {
		t.Errorf("a select and checkboxes in one actions block: %dpx, want %d (the select's row, 8px, then the checkboxes' row: %d + %d - 8)", both, one+other-8, one, other)
	}
}

// TestAppHomeRhythmLikeSlack: App Home's block wrappers have no margins,
// so the blocks' own collapse with each other's (.p-section_block 8px 0
// 4px, .p-header_block 1em 0 4px, .p-divider_block 8px 0, .p-context_block
// 4px 0, an input wrapper's 12px 0), except across its actions wrapper, a
// flex box that keeps the block's 8px margin-bottom and its actions' 8px
// tops inside. Clack put every block 8px from the next, an actions block's
// elements at its top, and checkboxes beside a button.
func TestAppHomeRhythmLikeSlack(t *testing.T) {
	h, _ := visibleHost()
	r := rhythmBlocks()
	height := func(bs ...any) int {
		_, y := kindSurface(t, h, "home", bs...)
		return y
	}
	checkRhythm(t, h, "home", "margins collapsing, no wrapper margins", []rhythmCase{
		{"a section, then a section", r.section, r.section, 8 - 4 - 8},
		{"a header, then a section", r.header, r.section, 8 - 4 - 8},
		{"a section, then a divider", r.section, r.divider, 8 - 4 - 8},
		{"a divider, then a context", r.divider, r.context, 8 - 8 - 4},
		{"a context, then a context", r.context, r.context, 4 - 4 - 4},
		{"an input, then a section", r.input, r.section, 12 - 12 - 8},
		{"a section, then actions", r.section, r.copyBtn, 4 - 4},      // the flex wrapper's own margins: none
		{"actions, then a section", r.copyBtn, r.section, 8 - 8},      // its 8px bottom stays inside it
		{"a section, then an alert", r.section, r.alert, 8 - 4 - 8},   // the newer blocks' too
		{"rich_text, then a container", r.para, r.container, 16 - 16}, // .containerBlock's 16px
		{"rich_text, then a code block", r.para, r.pre, 4 - 4},        // .c-mrkdwn__pre's 4px
		{"rich_text, then rich_text", r.para, r.para, 0},              // no margins of their own here
		{"rich_text, then a header", r.para, r.header, 15 - 15},       // its 1em, first or not
	})
	// an actions block: its button 8px down inside it, its 8px margin
	// under it
	s, got := kindSurface(t, h, "home", r.copyBtn)
	b := &s.blocks[0]
	btnH := s.widget(b, &b.Elements[0]).size(visGtx(480), 480, false).Y
	if want := 8 + btnH + 8; got != want {
		t.Errorf("an App Home actions block alone is %dpx, want %d: its %dpx button 8px down inside it, 8px under it", got, want, btnH)
	}
	// a header first keeps its 1em above it
	if first, after := height(r.header), height(r.para, r.header)-height(r.para); first != after {
		t.Errorf("an App Home header first is %dpx, after rich_text it adds %dpx: want its 15px top margin both times", first, after)
	}
	// checkboxes take a whole row (p-actions_block__action--full_width),
	// a button beside a button doesn't
	if both, one, other := height(r.actions("o", r.button("go"), r.boxes)), height(r.actions("a", r.button("go"))), height(r.actions("b", r.boxes)); both != one+other-8 {
		t.Errorf("a button and checkboxes in one App Home actions block: %dpx, want %d (the button's row, 8px, then the checkboxes': %d + %d - 8)", both, one+other-8, one, other)
	}
	if three, one := height(r.actions("t", r.button("one"), r.button("two"), r.button("three"))), height(r.actions("a", r.button("one"))); three != one {
		t.Errorf("three buttons in one App Home actions block: %dpx, want one row (%dpx): actions are their elements' width there", three, one)
	}
}

// TestCodeChannelViewRhythmLikeMessage: a code channel view draws its
// blocks as a message's (InteractiveBlocks with getMessageOverrides):
// each in a flex wrapper, so margins add up, the first block's top
// margin taken off a section (p-block_kit_renderer__block_wrapper--first)
// and a header's down to 4px, and actions their elements' width. Clack
// laid it out as a modal: collapsing 12px gaps, two actions to a row.
func TestCodeChannelViewRhythmLikeMessage(t *testing.T) {
	h, _ := visibleHost()
	r := rhythmBlocks()
	height := func(bs ...any) int {
		_, y := kindSurface(t, h, "code_channel_view", bs...)
		return y
	}
	checkRhythm(t, h, "code_channel_view", "flex wrappers: margins add up, the first block's top off", []rhythmCase{
		{"a section, then a section", r.section, r.section, 8},    // 4 + 8 between, the first one's 0 on top
		{"a section, then a header", r.section, r.header, 15 - 4}, // 4 + 15, the first header's 4
		{"a section, then a divider", r.section, r.divider, 0},    // 4 + 8: the divider keeps its 8 first too
		{"a divider, then a context", r.divider, r.context, 0},    // 8 + 4, nothing collapsing
		{"a section, then an alert", r.section, r.alert, 0},       // 4 + 8
		{"an alert, then a container", r.alert, r.container, 0},   // 8 + 16
	})
	if three, one := height(r.actions("t", r.button("one"), r.button("two"), r.button("three"))), height(r.actions("a", r.button("one"))); three != one {
		t.Errorf("three buttons in one code channel view actions block: %dpx, want one row (%dpx): a message's actions are their elements' width", three, one)
	}
}
