package composer

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestSpellcheckShots draws the composer's text with two misspellings,
// then right-clicks one: the underline (Chromium's wave on Windows and
// Linux, its macOS dots) and the desktop shell's menu, light and dark at
// 2x. It checks the underline inked the marker colour under each word
// and nowhere under the correct ones, and saves
// spellcheck-<mode>-<style>.png to SPELL_SHOTS_DIR when set.
func TestSpellcheckShots(t *testing.T) {
	dir := os.Getenv("SPELL_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		for _, mac := range []bool{false, true} {
			mode, style := "light", "wave"
			if dark {
				mode = "dark"
			}
			if mac {
				style = "mac"
			}
			t.Run(mode+"-"+style, func(t *testing.T) {
				prev := SpellMacStyle
				SpellMacStyle = mac
				t.Cleanup(func() { SpellMacStyle = prev })
				th := theme.New(dark)
				st := DefaultEditorStyle(theme.NewShaper())
				p := &th.C
				st.Color, st.Caret = p.Fg, p.Fg
				const sc = 2
				size := image.Pt(420, 260)
				e := newSpellEnv(t, 0, func(e *spellEnv) {
					e.scale, e.size, e.st = sc, size, st
					e.host = &keysHost{slickHost: &slickHost{th: th}}
					e.paint = func(gtx layout.Context) {
						paint.FillShape(gtx.Ops, p.Bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
					}
				})
				d := NewDoc()
				d.Insert(0, "Teh quick fox jumps over the wrold", Attr{})
				e.ed.SetDoc(d)
				e.frame()
				e.settle(1200 * time.Millisecond)
				if got := e.bad(); len(got) != 2 {
					t.Fatalf("marked %q", got)
				}
				// marker pixels: clearly red (the marker colour over the
				// background, blended in linear light by Gio)
				under := func(img *image.RGBA, lo, hi int) int {
					x0, base, _, _, _ := e.ed.lay.caretInfo(lo)
					x1, _, _, _, _ := e.ed.lay.caretInfo(hi)
					n := 0
					for y := base; y < base+8*sc; y++ {
						for x := x0; x < x1; x++ {
							c := img.RGBAAt(x, y)
							if int(c.R)-int(max(c.G, c.B)) > 60 {
								n++
							}
						}
					}
					return n
				}
				// the underline, before any menu
				e.ops.Reset()
				g := e.gtx()
				e.paint(g)
				e.ed.Layout(g, e.st)
				img := pixeltest.Render(t, size.X*sc, size.Y*sc, &e.ops)
				if n := under(img, 0, 3); n < 6 {
					t.Errorf("%d marker pixels under Teh", n)
				}
				if n := under(img, 29, 34); n < 6 {
					t.Errorf("%d marker pixels under wrold", n)
				}
				if n := under(img, 4, 9); n != 0 {
					t.Errorf("%d marker pixels under quick", n)
				}
				save(t, dir, fmt.Sprintf("spellcheck-%s-%s.png", mode, style), img)

				// the menu at a right-click on "wrold"
				x, base, _, _, _ := e.ed.lay.caretInfo(31)
				pt := f32.Pt(float32(x), float32(base-4*sc))
				NotePointer(pt.Round())
				e.r.Queue(
					pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt},
					pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
				)
				e.settle(100 * time.Millisecond)
				if len(e.host.shown) == 0 {
					t.Fatal("no menu")
				}
				m := e.host.shown[len(e.host.shown)-1].(*popups.Menu)
				e.ops.Reset()
				g = e.gtx()
				e.paint(g)
				e.ed.Layout(g, e.st)
				for i := 0; i < 2; i++ { // menus measure, then place
					m.Layout(g)
				}
				img = pixeltest.Render(t, size.X*sc, size.Y*sc, &e.ops)
				save(t, dir, fmt.Sprintf("spellcheck-menu-%s-%s.png", mode, style), img)
				t.Logf("menu %q", m.Labels())
			})
		}
	}
}

func save(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
