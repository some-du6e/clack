package richview

// custom_file_sections on a message's file cards (filesection.go): with
// a face, hovering a canvas pillow shows the 28px button in the bar at
// top 12px, right 12px (a list entity: 32px at 8px); a click on it opens
// the section menu and not the file. Without one (the experiment off)
// there is no button and the click opens the file.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

type secHandler struct {
	recHandler
	secs []string
	btns []image.Rectangle
}

func (h *secHandler) OpenFileSection(f *rt.File, btn image.Rectangle) {
	h.secs = append(h.secs, f.ID)
	h.btns = append(h.btns, btn)
}

func TestFileCardSectionButton(t *testing.T) {
	defer func(f func(string) (SectionLook, string, bool)) { FileSectionFace = f }(FileSectionFace)
	on := true
	FileSectionFace = func(string) (SectionLook, string, bool) { return SectionLookStar, "", on }
	canvas := canvasFileDoc(2).Blocks[0]
	list := rt.Block{Kind: rt.FileBlock, File: &rt.File{ID: "F0LIST00001", Title: "Launch tracker", Type: "list", PrettyType: "List"}}
	for _, c := range []struct {
		name       string
		blk        rt.Block
		inset, btn int
	}{{"canvas", canvas, 12, 28}, {"list", list, 8, 32}} {
		h := &secHandler{}
		hs := newHarness(doc(c.blk), h)
		hs.w = 700
		hs.frame()
		if len(hs.v.d.fileSecs) != 1 {
			t.Fatalf("%s: %d section cards", c.name, len(hs.v.d.fileSecs))
		}
		s := &hs.v.d.fileSecs[0]
		bar, btn := hs.v.secBar(s)
		if bar.Max.X != s.card.Max.X-c.inset || bar.Min.Y != s.card.Min.Y+c.inset || btn.Dx() != c.btn || bar.Dx() != c.btn+8 {
			t.Errorf("%s: bar %v button %v in card %v", c.name, bar, btn, s.card)
		}
		p := f32.Pt(float32(btn.Min.X+btn.Dx()/2), float32(btn.Min.Y+btn.Dy()/2))
		on = true
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
		if hs.v.fileSecAt(p) != 0 {
			t.Fatalf("%s: no button under the pointer", c.name)
		}
		hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
		if len(h.secs) != 1 || h.secs[0] != c.blk.File.ID || len(h.files) != 0 {
			t.Errorf("%s on: section menus %v, files opened %d", c.name, h.secs, len(h.files))
		}
		if len(h.btns) == 1 && h.btns[0] != btn {
			t.Errorf("%s: menu anchor %v, want %v", c.name, h.btns[0], btn)
		}
		on = false
		hs.send(press(p, 3*time.Second), release(p, 3*time.Second+20*time.Millisecond))
		if len(h.secs) != 1 || len(h.files) != 1 {
			t.Errorf("%s off: section menus %v, files opened %d", c.name, h.secs, len(h.files))
		}
	}
	// no bar in a canvas's own doc or a hover card
	FileSectionFace = func(string) (SectionLook, string, bool) { return SectionLookStar, "", true }
	v := layoutDoc(t, doc(list), 700, Options{NoHoverCards: true})
	if len(v.d.fileSecs) != 0 {
		t.Error("a hover card's list has a section button")
	}
}

// TestFileCardSectionShots saves a hovered canvas pillow (the star face)
// and list entity (Starred's face), light and dark (the
// messages-audit-shots workflow sets MESSAGES_AUDIT_SHOTS_DIR):
// file-card-section-*.png.
func TestFileCardSectionShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	defer func(f func(string) (SectionLook, string, bool)) { FileSectionFace = f }(FileSectionFace)
	FileSectionFace = func(id string) (SectionLook, string, bool) {
		if id == "F0LIST00001" {
			return SectionLookStarred, "", true
		}
		return SectionLookStar, "", true
	}
	list := rt.Block{Kind: rt.FileBlock, File: &rt.File{ID: "F0LIST00001", Title: "Launch tracker", Type: "list", PrettyType: "List"}}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		for _, c := range []struct {
			name string
			doc  rt.Doc
			btn  bool // hover the button (its tooltip)
		}{{"canvas", canvasFileDoc(3), true}, {"list", doc(list), false}} {
			st := NewStyle(themeShaper(), &gradImages{cache: map[string]paint.ImageOp{}}, pal)
			v := New(c.doc, Options{Channel: "C1", TS: "1762700000.000300"})
			var r input.Router
			var ops op.Ops
			const w, h, pad = 720, 420, 20
			frame := func() {
				ops.Reset()
				paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
				tr := op.Offset(image.Pt(pad, pad+40)).Push(&ops)
				gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(w-2*pad, h-2*pad-40)), Now: time.Now()}
				v.Layout(gtx, st, &recHandler{})
				tr.Pop()
				r.Frame(&ops)
			}
			frame()
			if len(v.d.fileSecs) != 1 {
				t.Fatalf("%s: no section card", c.name)
			}
			s := &v.d.fileSecs[0]
			pt := s.card.Min.Add(image.Pt(60, 20))
			if c.btn {
				_, b := v.secBar(s)
				pt = b.Min.Add(b.Size().Div(2))
			}
			r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(pt.X+pad), float32(pt.Y+pad+40))})
			frame()
			time.Sleep(EmojiTipDelay + 50*time.Millisecond)
			frame()
			frame()
			img := pixeltest.Render(t, w, h, &ops)
			fh, err := os.Create(filepath.Join(dir, "file-card-section-"+c.name+"-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(fh, img); err != nil {
				t.Fatal(err)
			}
			fh.Close()
		}
	}
}
