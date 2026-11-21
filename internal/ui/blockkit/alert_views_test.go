package blockkit

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// TestSurfaceAlertFills: modals and App Home lay their blocks out as a
// view's (richview FillBlocks): WSwB's block wrappers aren't flex, so an
// alert fills the view, a container's children too.
func TestSurfaceAlertFills(t *testing.T) {
	h, _ := visibleHost()
	raw, _ := json.Marshal([]any{
		map[string]any{"type": "alert", "level": "warning", "text": map[string]any{"type": "plain_text", "text": "Pick a date"}},
		map[string]any{"type": "container", "block_id": "c1", "title": map[string]any{"type": "plain_text", "text": "Deploy"},
			"child_blocks": []any{map[string]any{"type": "actions", "block_id": "a1", "elements": []any{visBtn("go")}}}},
	})
	for _, typ := range []string{"modal", "home"} {
		s := newSurface(h, &core.View{Type: typ, Blocks: raw})
		s.Layout(visGtx(480))
		if len(s.texts) < 2 {
			t.Fatalf("%s: %d block views, want the alert's and the container's", typ, len(s.texts))
		}
		for k, v := range s.texts {
			if !viewFills(v) {
				t.Errorf("%s: block view %q doesn't fill (a view's blocks aren't flex items)", typ, k)
			}
		}
	}
}
