package richview

// custom_file_sections on a message's file cards (filesection.go): with
// a face, hovering a canvas pillow shows the 28px button in its
// FileActions (top 12px, right 12px, between Share canvas and View canvas
// details); a list entity has the 32px button alone at 8px. A click on
// it opens the section menu and not the file. Without one (the
// experiment off) the canvas has renderFavoriteCanvas' star there, and
// the list no button: the click opens the list.

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

	// the canvas: one of its FileActions
	h := &secHandler{}
	hs := newHarness(doc(canvas), h)
	hs.w = 700
	hs.frame()
	if len(hs.v.d.bars) != 1 || len(hs.v.d.fileSecs) != 0 {
		t.Fatalf("canvas: %d bars, %d section cards", len(hs.v.d.bars), len(hs.v.d.fileSecs))
	}
	card := hs.v.d.bars[0].card
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(card.Min.X+60), float32(card.Min.Y+20))})
	var acts []FileAction
	var btn image.Rectangle
	for _, r := range hs.v.FileActionRects() {
		acts = append(acts, r.Action)
		if r.Action == FileSection {
			btn = r.R
		}
	}
	group, _ := hs.v.barRects(0)
	if len(acts) != 4 || acts[0] != FileShare || acts[1] != FileSection || acts[2] != FileDetails || acts[3] != FileMore {
		t.Fatalf("canvas buttons %v, want Share, the section button, View canvas details, More actions", acts)
	}
	if group.Max.X != card.Max.X-12 || group.Min.Y != card.Min.Y+12 || btn.Dx() != 28 || btn.Dy() != 32 {
		t.Errorf("canvas: group %v button %v in card %v", group, btn, card)
	}
	p := f32.Pt(float32(btn.Min.X+btn.Dx()/2), float32(btn.Min.Y+btn.Dy()/2))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.secs) != 1 || h.secs[0] != canvas.File.ID || len(h.files) != 0 {
		t.Errorf("canvas on: section menus %v, files opened %d", h.secs, len(h.files))
	}
	if len(h.btns) == 1 && h.btns[0] != btn {
		t.Errorf("canvas: menu anchor %v, want %v", h.btns[0], btn)
	}
	on = false
	hs.v.dirty = true
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	if b := hs.v.d.bars[0].btns; len(b) != 4 || b[1].act != FileStar || b[1].icon != "star" {
		t.Errorf("canvas off: buttons %+v, want the star second", b)
	}

	// the list entity: its bar holds the button alone
	h = &secHandler{}
	on = true
	hs = newHarness(doc(list), h)
	hs.w = 700
	hs.frame()
	if len(hs.v.d.fileSecs) != 1 {
		t.Fatalf("list: %d section cards", len(hs.v.d.fileSecs))
	}
	s := &hs.v.d.fileSecs[0]
	bar, lb := hs.v.secBar(s)
	if bar.Max.X != s.card.Max.X-8 || bar.Min.Y != s.card.Min.Y+8 || lb.Dx() != 32 || bar.Dx() != 32+8 {
		t.Errorf("list: bar %v button %v in card %v", bar, lb, s.card)
	}
	p = f32.Pt(float32(lb.Min.X+lb.Dx()/2), float32(lb.Min.Y+lb.Dy()/2))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	if hs.v.fileSecAt(p) != 0 {
		t.Fatal("list: no button under the pointer")
	}
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.secs) != 1 || h.secs[0] != list.File.ID || len(h.files) != 0 {
		t.Errorf("list on: section menus %v, files opened %d", h.secs, len(h.files))
	}
	on = false
	hs.send(press(p, 3*time.Second), release(p, 3*time.Second+20*time.Millisecond))
	if len(h.secs) != 1 || len(h.files) != 1 {
		t.Errorf("list off: section menus %v, files opened %d", h.secs, len(h.files))
	}
	// no bar in a canvas's own doc or a hover card
	FileSectionFace = func(string) (SectionLook, string, bool) { return SectionLookStar, "", true }
	v := layoutDoc(t, doc(list), 700, Options{NoHoverCards: true})
	if len(v.d.fileSecs) != 0 {
		t.Error("a hover card's list has a section button")
	}
	v = layoutDoc(t, doc(canvas), 700, Options{NoHoverCards: true})
	if len(v.d.bars) != 0 {
		t.Error("a hover card's canvas has FileActions")
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
			var card, b image.Rectangle
			switch {
			case len(v.d.fileSecs) == 1:
				s := &v.d.fileSecs[0]
				card = s.card
				_, b = v.secBar(s)
			case len(v.d.bars) == 1: // the canvas's FileActions
				card = v.d.bars[0].card
				_, rs := v.barRects(0)
				for k, bt := range v.d.bars[0].btns {
					if bt.act == FileSection {
						b = rs[k]
					}
				}
			default:
				t.Fatalf("%s: no section button", c.name)
			}
			pt := card.Min.Add(image.Pt(60, 20))
			if c.btn {
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
