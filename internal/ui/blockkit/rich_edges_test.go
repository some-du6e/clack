package blockkit

// A rich_text block's edge elements, whose margins collapse through it
// (blockMargins), are read when the block decodes, not from its JSON on
// every Layout (review of bk-modal-spacing, 2026-11-22: twice a frame).
// Synthetic blocks.

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

func TestRichEdgesParsedOnce(t *testing.T) {
	h, _ := visibleHost()
	r := rhythmBlocks()
	raw, err := json.Marshal([]any{r.para, r.pre})
	if err != nil {
		t.Fatal(err)
	}
	bs := ParseBlocks(raw)
	if len(bs) != 2 {
		t.Fatalf("%d blocks, want 2", len(bs))
	}
	if b := bs[1]; b.richFirst != "rich_text_preformatted" || b.richLast != "rich_text_preformatted" {
		t.Errorf("the code block's edges: %q, %q, want rich_text_preformatted both", b.richFirst, b.richLast)
	}
	b := bs[1]
	b.Raw = nil // the margins come from what decoding kept
	s := newSurface(h, &core.View{Type: "modal"})
	if top, bottom, _, _ := s.blockMargins(&b, false); top != 4 || bottom != 4 {
		t.Errorf("a code block's rich_text margins without its JSON: %d, %d, want 4, 4 (.c-mrkdwn__pre)", top, bottom)
	}
}
