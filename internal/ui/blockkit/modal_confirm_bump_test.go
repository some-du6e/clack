package blockkit

import (
	"encoding/json"
	"image"
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestModalConfirmIsSpeedBump: an element's confirm object inside a Block
// Kit modal comes up as BlockKitModal's in-card speed bump
// (showConfirmDialog → maybeRenderConfirm: WizardSpeedBump), not a dialog
// of its own: the header its title, the body its text (mrkdwn as
// Mrkdwn), the buttons its deny and confirm, the continue type its
// style. Without them: "Are you sure?", Cancel, OK, primary. Cancel and
// Escape only close it; continue closes it, then acts. Clack opened
// AttachmentActionConfirmDialog over the modal.
func TestModalConfirmIsSpeedBump(t *testing.T) {
	h := &confirmHost{th: theme.New(false)}
	blocks, _ := json.Marshal([]any{map[string]any{"type": "actions", "block_id": "b1", "elements": []any{map[string]any{
		"type": "button", "action_id": "del", "text": map[string]any{"type": "plain_text", "text": "Delete"}, "style": "danger",
		"confirm": map[string]any{
			"title":   map[string]any{"type": "plain_text", "text": "Delete the build?"},
			"text":    map[string]any{"type": "mrkdwn", "text": "This *can't* be undone."},
			"confirm": map[string]any{"type": "plain_text", "text": "Delete"},
			"deny":    map[string]any{"type": "plain_text", "text": "Keep it"},
			"style":   "danger",
		}}}}})
	v := &core.View{ID: "V1", RootViewID: "V1", Type: "modal", Blocks: blocks,
		Title:  mustJSON(Text{Type: "plain_text", Text: "Builds"}),
		Submit: mustJSON(Text{Type: "plain_text", Text: "Done"})}
	s := newStack(h, v)
	var r input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
		s.Layout(gtx)
		r.Frame(&ops)
	}
	frames := func() {
		for i := 0; i < 3; i++ {
			frame()
		}
	}
	press := func(n key.Name) {
		r.Queue(key.Event{Name: n, State: key.Press}, key.Event{Name: n, State: key.Release})
		frames()
	}
	frame()
	sf := s.top()
	var b *Block
	for i := range sf.blocks {
		if sf.blocks[i].BlockID == "b1" {
			b = &sf.blocks[i]
		}
	}
	if b == nil || len(b.Elements) != 1 {
		t.Fatalf("no actions block b1 in the modal (%d blocks)", len(sf.blocks))
	}
	sf.widget(b, &b.Elements[0]).press()
	frames()
	if len(h.shown) != 0 {
		t.Fatalf("the confirm opened %d overlays over the modal, want none (the in-card speed bump)", len(h.shown))
	}
	bp, up := mcBump(s)
	if !up {
		t.Fatal("no speed bump in the modal")
	}
	if bp.header != "Delete the build?" || bp.keepTxt != "Keep it" || bp.cont != "Delete" || bp.style != "danger" || !bp.mrkdwn {
		t.Errorf("bump: %+v; want Delete the build?, Keep it, Delete (danger), the mrkdwn body", bp)
	}
	if !r.Source().Focused(mcKeep(s)) {
		t.Error("the cancel button doesn't have the focus")
	}
	press(key.NameEscape)
	if _, up := mcBump(s); up || s.done {
		t.Fatalf("Escape: bump up %v, modal closed %v; want the bump gone and the modal kept", up, s.done)
	}

	// the defaults; continue acts once, cancel runs the deny
	oks, denies := 0, 0
	mcConfirmIn(sf, &Confirm{Title: &Text{Type: "plain_text", Text: "Sure?"}}, func() { oks++ }, func() { denies++ })
	frames()
	if bp, up := mcBump(s); !up || bp.body != "Are you sure?" || bp.keepTxt != "Cancel" || bp.cont != "OK" || bp.style != "primary" {
		t.Fatalf("defaults: up %v %+v", up, bp)
	}
	press(key.NameTab)
	press(key.NameReturn)
	if _, up := mcBump(s); oks != 1 || denies != 0 || up {
		t.Errorf("Enter on OK: acted %d, denied %d, bump up %v; want acted once", oks, denies, up)
	}
	mcConfirmIn(sf, &Confirm{Title: &Text{Type: "plain_text", Text: "Sure?"}}, func() { oks++ }, func() { denies++ })
	frames()
	press(key.NameReturn) // on Cancel, which has the focus
	if _, up := mcBump(s); oks != 1 || denies != 1 || up {
		t.Errorf("Enter on Cancel: acted %d, denied %d, bump up %v; want denied once", oks, denies, up)
	}
}
