package richview

import (
	"image/color"
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestOpaqueFillsStayOverUnder: dark mode's code and pre fills,
// rgba(232,232,232,.04) over the pane, come to (34,37,41), the color of
// the opaque Placeholder a colourless attachment's bar, a v2 card's bar
// and a gallery tile take. See-through fills were found by that color, so
// on a hovered, active-thread, pinned or flashing row the opaque bar took
// the row's tint too. Slack's .c-message_attachment__border is base-ter,
// opaque, on every row, and its code chip shows the row through it.
func TestOpaqueFillsStayOverUnder(t *testing.T) {
	p := DarkPalette()
	st := NewStyle(sharedShaper(), noImages{}, p)
	st.Face, st.Mono = "Go", "Go Mono"
	m := slack.Message{Type: "message", TS: "1762700000.000200", User: "U0", Text: "see `code` here",
		Attachments: []slack.Attachment{{ID: 1, Fallback: "a note", Title: "A note", Text: "an attachment with no colour"}}}
	v := New(rt.FromMessage(codeRes{}, &m), Options{})
	v.Layout(testGtx(new(op.Ops), 600), st, nil) // a real layout: it takes the palette's see-through fills
	code := rgba(232, 232, 232, 0.04)
	for _, under := range []struct {
		name string
		c    color.NRGBA
	}{
		{"a hovered row", rgb(34, 37, 41)},
		{"the open thread's root", rgb(47, 30, 0)},
		{"the jump highlight's hold", rgb(0x45, 0x3f, 0x28)},
	} {
		v.SetUnder(under.c)
		bars, chips := 0, 0
		for i := range v.d.boxes {
			b := &v.d.boxes[i]
			switch {
			case b.kind == bFill && !b.inline && b.fill == p.Placeholder:
				bars++
				if got := v.drawnFill(b, false); got != p.Placeholder {
					t.Errorf("on %s the attachment's bar drew %v; want the opaque Placeholder %v", under.name, got, p.Placeholder)
				}
			case b.kind == bBorder && b.inline:
				chips++
				if got, want := v.drawnFill(b, false), Over(code, under.c); got != want {
					t.Errorf("on %s the code chip drew %v; want its rgba over the row, %v", under.name, got, want)
				}
			}
		}
		if bars == 0 || chips == 0 {
			t.Fatalf("on %s: %d attachment bars and %d code chips laid out", under.name, bars, chips)
		}
	}
}
