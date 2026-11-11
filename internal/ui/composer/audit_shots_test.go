package composer

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestComposerAuditShots draws the channel composer in the states the
// composer audit changed (docs/COMPOSER-AUDIT.md), light and dark at 2x:
// typed text (no clip buttons, Run shortcut disabled), an empty composer
// (Run shortcut enabled), a heading line (the ¶ trigger's heading icon,
// bold disabled) and a message 1,234 characters over the limit (the red
// "-1,234" pill, send disabled). It saves composer-audit-<state>-<mode>.png
// to COMPOSER_AUDIT_SHOTS_DIR (the composer-audit workflow) and is
// skipped without it. Synthetic workspace only.
func TestComposerAuditShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	states := []struct {
		name string
		doc  func() *Doc
	}{
		{"typed", func() *Doc { d := NewDoc(); d.Insert(0, "Ship it", Attr{}); return d }},
		{"empty", NewDoc},
		{"heading", func() *Doc { d := NewDoc(); d.Insert(0, "Release notes", Attr{}); d.Lines[0].Kind = Header1; return d }},
		// Karim: "a quote overlaps with the thingy": an empty quote line
		// (no placeholder, the caret past the bar), then a quote of three
		// lines and a paragraph after it
		{"quote-empty", func() *Doc { d := NewDoc(); d.Lines[0].Kind = Quote; return d }},
		{"quote-lines", func() *Doc {
			d := NewDoc()
			d.Insert(0, "first quoted line\nsecond quoted line\n\nafter the quote", Attr{})
			d.SetLineKind(0, 2, Quote)
			return d
		}},
		{"overlimit", func() *Doc {
			d := NewDoc()
			d.Insert(0, strings.Repeat("lorem ipsum ", (composerMaxLength+1234)/12)+strings.Repeat("x", (composerMaxLength+1234)%12), Attr{})
			return d
		}},
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, s := range states {
			t.Run(s.name+"-"+mode, func(t *testing.T) {
				const scale = 2
				w, h := 760*scale, 220*scale
				th := theme.New(dark)
				clearDraft(tChGen, "")
				t.Cleanup(func() { clearDraft(tChGen, "") })
				c := New(&slickHost{app: app, th: th}, tChGen, "")
				t.Cleanup(c.Close)
				c.ed.SetDoc(s.doc())
				d := c.ed.Doc()
				c.ed.SetSelection(d.LineEnd(0), d.LineEnd(0))
				var ops op.Ops
				frame := func() {
					ops.Reset()
					paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
					gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
						Constraints: layout.Constraints{Max: image.Pt(w, h)}, Now: time.Now()}
					slackc.Frame(gtx)
					c.Layout(gtx)
				}
				frame()
				frame()
				if s.name == "overlimit" && c.overLimit() != 1234 {
					t.Fatalf("over by %d, want 1234", c.overLimit())
				}
				img := pixeltest.Render(t, w, h, &ops)
				path := filepath.Join(dir, fmt.Sprintf("composer-audit-%s-%s.png", s.name, mode))
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
			})
		}
	}
}
