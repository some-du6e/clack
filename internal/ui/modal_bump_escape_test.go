package ui

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// A Block Kit modal's speed bump through the real shell, whose Close
// binding reads Escape before the overlays lay out. WizardSpeedBump binds
// Escape while it's up and cancels (onCancel), and Slack's BlockKitModal
// raises both the leave prompt and an element's confirm object in it:
// Escape puts the bump away and the modal stays. Clack's shell dismissed
// the modal under the bump instead: Escape on a confirm backed out of the
// view (here, closing the modal), and on the leave prompt ("Are you sure
// you want to leave?", α.164) put the prompt straight back. Cmd+Enter
// (Super+Enter off macOS) doesn't submit while a confirm is up: the value
// it asks about isn't chosen yet. Synthetic view only.
func TestModalBumpEscapeThroughShell(t *testing.T) {
	const confirmTitle, leaveTitle = "Archive the draft?", "Are you sure you want to leave?"
	open := func(t *testing.T, e *perfEnv, id string) {
		t.Helper()
		raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
		txt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
		blocks := raw([]any{
			map[string]any{"type": "input", "block_id": "note", "optional": true, "label": txt("Note"),
				"element": map[string]any{"type": "plain_text_input", "action_id": "note"}},
			map[string]any{"type": "actions", "block_id": "acts", "elements": []any{map[string]any{
				"type": "button", "action_id": "archive", "text": txt("Archive"),
				"confirm": map[string]any{"title": txt(confirmTitle), "text": txt("It goes to the archive."),
					"confirm": txt("Archive"), "deny": txt("Keep it")}}}},
		})
		blockkit.ShowModalView(e.a, &core.View{ID: id, RootViewID: id, Type: "modal", Blocks: blocks,
			Title: raw(txt("Drafts")), Submit: raw(txt("Save")), Close: raw(txt("Cancel"))})
		e.until("the modal", 10*time.Second, func() bool { m, ok := blockkit.OpenModal(e.a); return ok && m.ViewID == id })
		e.settle(200 * time.Millisecond)
	}
	state := func(e *perfEnv) (header string, up, modal bool) {
		header, up = blockkit.ModalBump(e.a)
		_, modal = blockkit.OpenModal(e.a)
		return header, up, modal
	}

	t.Run("confirm", func(t *testing.T) {
		e, fs := sectionEnv(t)
		open(t, e, "VBUMPESC1")
		if !blockkit.PressModalElement(e.a, "acts", "archive") {
			t.Fatal("no Archive button in the modal")
		}
		e.until("the confirm's bump", 5*time.Second, func() bool { h, up, _ := state(e); return up && h == confirmTitle })
		cmd := key.ModSuper
		if shortcut.GOOS == "darwin" {
			cmd = key.ModCommand
		}
		before := fs.CallCount("views.submit")
		e.press(key.NameReturn, cmd)
		e.settle(300 * time.Millisecond)
		if n := fs.CallCount("views.submit") - before; n != 0 {
			t.Errorf("Cmd+Enter with the confirm up sent views.submit %d times, want none", n)
		}
		e.press(key.NameEscape, 0)
		if h, up, modal := state(e); up || !modal {
			t.Fatalf("Escape on the confirm: bump up %v (%q), modal open %v; want the bump gone and the modal kept", up, h, modal)
		}
		e.press(key.NameEscape, 0) // nothing typed: the modal closes
		if _, _, modal := state(e); modal {
			t.Error("Escape with no bump left the modal open")
		}
	})

	t.Run("leave", func(t *testing.T) {
		e, _ := sectionEnv(t)
		open(t, e, "VBUMPESC2")
		fields := blockkit.ModalFields(e.a)
		if len(fields) != 1 {
			t.Fatalf("modal fields: %+v, want the note", fields)
		}
		r := fields[0].R
		e.click(image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2))
		for range 3 {
			e.frame()
		}
		e.typeText("half a thought")
		e.until("the note typed", 5*time.Second, func() bool {
			fs := blockkit.ModalFields(e.a)
			return len(fs) == 1 && fs[0].Text == "half a thought"
		})
		e.press(key.NameEscape, 0)
		if h, up, modal := state(e); !up || h != leaveTitle || !modal {
			t.Fatalf("Escape with a note typed: bump %q (up %v), modal open %v; want %q over the modal", h, up, modal, leaveTitle)
		}
		e.press(key.NameEscape, 0)
		if h, up, modal := state(e); up || !modal {
			t.Errorf("Escape on the leave prompt: bump up %v (%q), modal open %v; want the prompt gone and the modal kept", up, h, modal)
		}
	})
}
