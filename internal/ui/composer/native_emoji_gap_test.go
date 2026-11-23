package composer

// A canvas edits in the composer's editor, which draws a standard emoji
// with the space richview's canvas text has after it (EditorStyle's
// NativeEmojiGap, the native glyph span's margin-right on a 1x Mac), so
// the text stays put when a canvas block opens for editing. At 2x there's
// none. Synthetic.

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestComposerNativeEmojiGap(t *testing.T) {
	st := DefaultEditorStyle(theme.NewShaper())
	st.NativeEmojiGap = 4
	for _, c := range []struct {
		scale float32
		gap   int
	}{{1, 4}, {2, 0}} {
		ed := NewEditor()
		d := NewDoc()
		for i := 0; i < 3; i++ {
			d.InsertAtom(i, &Atom{Kind: AtomEmoji, ID: "large_green_circle", Text: ":large_green_circle:", Uni: "🟢"})
		}
		ed.SetDoc(d)
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: c.scale, PxPerSp: c.scale},
			Constraints: layout.Exact(image.Pt(600, 80))}
		ed.Layout(gtx, st)
		es := ed.lay.emojis
		if len(es) != 3 {
			t.Fatalf("at %v: %d emoji drawn, want 3", c.scale, len(es))
		}
		for i := 1; i < len(es); i++ {
			if got := es[i].rect.Min.X - es[i-1].rect.Max.X; got != c.gap {
				t.Errorf("at %v: %dpx after emoji %d, want %d", c.scale, got, i-1, c.gap)
			}
		}
	}
}
