package composer

// Consecutive emoji in the composer touch, as Slack's texty draws them:
// .c-texty_input .ql-editor img.emoji is 22px with no margin, and Quill's
// guards around an embed are zero-width. The composer gave each emoji a
// pixel on either side, so stretchy custom emoji tiles drew with seams
// (Karim, #clack 2026-11-22). Synthetic.

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestComposerEmojiTouch: four emoji in a row are drawn edge to edge, each
// box starting where the one before it ends, at 100% and 125%.
func TestComposerEmojiTouch(t *testing.T) {
	st := DefaultEditorStyle(theme.NewShaper())
	for _, scale := range []float32{1, 1.25} {
		ed := NewEditor()
		d := NewDoc()
		for i := 0; i < 4; i++ {
			d.InsertAtom(i, &Atom{Kind: AtomEmoji, ID: "large_green_circle", Text: ":large_green_circle:", Uni: "🟢"})
		}
		ed.SetDoc(d)
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(600, 60))}
		ed.Layout(gtx, st)
		es := ed.lay.emojis
		if len(es) != 4 {
			t.Fatalf("at %v: %d emoji drawn, want 4", scale, len(es))
		}
		for i := 1; i < len(es); i++ {
			if prev, cur := es[i-1].rect, es[i].rect; cur.Min.X != prev.Max.X {
				t.Errorf("at %v: emoji %d at %v, emoji %d at %v: want them touching", scale, i-1, prev, i, cur)
			}
		}
	}
}
