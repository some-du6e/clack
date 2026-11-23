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
// beside a select. Measured from the surface's height alone, so the old
// layout draws through the same calls. Synthetic blocks.

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

func rhythmText(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }

// modalSurface lays the blocks out as a modal's view 480px wide.
func modalSurface(t *testing.T, h Host, blocks ...any) (*Surface, int) {
	t.Helper()
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := newSurface(h, &core.View{Type: "modal", Blocks: raw})
	return s, s.Layout(visGtx(480)).Size.Y
}

func TestModalRhythmLikeSlack(t *testing.T) {
	h, _ := visibleHost()
	opt := func(t, v string) map[string]any { return map[string]any{"text": rhythmText(t), "value": v} }
	input := map[string]any{"type": "input", "block_id": "say", "label": rhythmText("What should it say?"),
		"element": map[string]any{"type": "plain_text_input", "action_id": "v", "multiline": true}}
	sel := map[string]any{"type": "static_select", "action_id": "style", "options": []any{opt("stretchy", "s"), opt("blocky", "b")}}
	boxes := map[string]any{"type": "checkboxes", "action_id": "flags", "options": []any{opt("Give every tile a face", "f"), opt("Tight spacing", "t")}}
	actions := func(id string, els ...any) map[string]any {
		return map[string]any{"type": "actions", "block_id": id, "elements": els}
	}
	copyBtn := actions("copy", map[string]any{"type": "button", "action_id": "copy", "style": "primary", "text": rhythmText("Copy")})
	para := map[string]any{"type": "rich_text", "block_id": "preview", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": "a preview"}}}}}
	pre := map[string]any{"type": "rich_text", "block_id": "code", "elements": []any{
		map[string]any{"type": "rich_text_preformatted", "elements": []any{map[string]any{"type": "text", "text": ":tile::tile:"}}}}}
	height := func(bs ...any) int {
		_, y := modalSurface(t, h, bs...)
		return y
	}
	// what two blocks add between them over each alone: Slack's collapsed
	// wrapper margin, max(a's bottom, 12, b's top), less a's bottom and b's
	// top (each block alone keeps its own margins in the dialog's body)
	between := func(a, b any) int { return height(a, b) - height(a) - height(b) }

	// an actions block: its button 8px down inside it, 8px under it
	s, got := modalSurface(t, h, copyBtn)
	b := &s.blocks[0]
	btnH := s.widget(b, &b.Elements[0]).size(visGtx(480), 480, false).Y
	if want := 8 + btnH + 8; got != want {
		t.Errorf("an actions block alone is %dpx, want %d: its %dpx button 8px down inside it, 8px under it", got, want, btnH)
	}
	for _, c := range []struct {
		name string
		a, b any
		want int
	}{
		{"input, then actions", input, copyBtn, 12},
		{"actions, then rich_text", copyBtn, para, 12 - 8},
		{"rich_text, then rich_text", para, para, 12},
		{"rich_text, then actions", para, copyBtn, 12},
		{"actions, then a code block", copyBtn, pre, 12 - 8 - 4},
	} {
		if got := between(c.a, c.b); got != c.want {
			t.Errorf("%s: %dpx between them over each alone, want %d (wrappers 12px apart, margins collapsing)", c.name, got, c.want)
		}
	}
	// the select and the checkboxes: two rows, the checkboxes' a whole
	// row 8px under the select's, not side by side
	if both, one, other := height(actions("opts", sel, boxes)), height(actions("sel", sel)), height(actions("cb", boxes)); both != one+other-8 {
		t.Errorf("a select and checkboxes in one actions block: %dpx, want %d (the select's row, 8px, then the checkboxes' row: %d + %d - 8)", both, one+other-8, one, other)
	}
}
