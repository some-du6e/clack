package richview

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// FileIconArt follows Slack's getSVGIconType, Icon's alias table and
// client-boot-styles' svgicon files: an .exe (no icon of its own) is the
// generic page, a zip the archive, a csv the spreadsheet (Excel's file),
// a code snippet code, a markdown snippet the snippet; every name it
// gives is registered art.
func TestFileIconArt(t *testing.T) {
	for _, c := range []struct {
		f    rt.File
		want string
	}{
		{rt.File{Type: "exe", PrettyType: "Windows Application", Mode: "hosted"}, "fileicon-generic"},
		{rt.File{Type: "", Mode: "hosted"}, "fileicon-generic"},
		{rt.File{Type: "zip", Mode: "hosted"}, "fileicon-archive"},
		{rt.File{Type: "pdf", Mode: "hosted"}, "fileicon-file-pdf"},
		{rt.File{Type: "docx", Mode: "hosted"}, "fileicon-file-word"},
		{rt.File{Type: "xlsx", Mode: "hosted"}, "fileicon-file-excel"},
		{rt.File{Type: "csv", Mode: "hosted"}, "fileicon-file-excel"},
		{rt.File{Type: "pptx", Mode: "hosted"}, "fileicon-file-powerpoint"},
		{rt.File{Type: "binary", Mode: "hosted"}, "fileicon-binary"},
		{rt.File{Type: "text", Mode: "hosted"}, "fileicon-file-snippet"},
		{rt.File{Type: "python", Mode: "snippet", IsSnippet: true}, "fileicon-code"},
		{rt.File{Type: "markdown", Mode: "snippet", IsSnippet: true}, "fileicon-file-snippet"},
		{rt.File{Type: "html", Mode: "hosted"}, "fileicon-html"},
		{rt.File{Type: "php", Mode: "hosted"}, "fileicon-php"},
		{rt.File{Type: "png", Mode: "hosted"}, "fileicon-file-image"},
		{rt.File{Type: "mp3", Mode: "hosted"}, "fileicon-file-audio"},
		{rt.File{Type: "mov", Mode: "hosted"}, "fileicon-file-video"},
		{rt.File{Type: "gdoc", Mode: "external", IsExternal: true}, "fileicon-file-google-document"},
		{rt.File{Type: "gsheet", Mode: "external", IsExternal: true}, "fileicon-file-google-spreadsheet"},
		{rt.File{Type: "psd", Mode: "hosted"}, "fileicon-file-photoshop"},
		{rt.File{Type: "space", Mode: "space"}, "fileicon-file-text-post"},
		{rt.File{Type: "email", Mode: "email"}, "fileicon-email"},
		{rt.File{Type: "dmg", Mode: "hosted"}, "fileicon-generic"}, // file-disk-image has no file of its own
	} {
		f := c.f
		if got := FileIconArt(&f); got != c.want {
			t.Errorf("FileIconArt(%s, mode %s) = %s, want %s", f.Type, f.Mode, got, c.want)
		}
	}
	for class, file := range svgIconFiles {
		if !icons.HasArt("fileicon-" + file) {
			t.Errorf("%s's file %s isn't registered art", class, file)
		}
	}
	if !icons.HasArt("fileicon-generic") {
		t.Error("the generic svgicon isn't registered art")
	}
	// the pillow draws it as art, not the old label tile
	v := New(doc(fileBlock(&rt.File{ID: "F0ART0001", Name: "game.exe", Type: "exe", Mode: "hosted"})), Options{})
	v.Layout(testGtx(new(op.Ops), 600), newTestStyle(), nil)
	n := 0
	for _, o := range v.d.overlayIcons {
		if o.art && o.name == "fileicon-generic" && o.r.Dx() == 36 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the .exe pillow drew %d generic svgicons (overlays %+v)", n, v.d.overlayIcons)
	}
}

type fileRemoveRec struct {
	recHandler
	removed []string
	deleted []string
}

func (r *fileRemoveRec) RemoveFile(f *rt.File, deletion bool) {
	if deletion {
		r.deleted = append(r.deleted, f.ID)
		return
	}
	r.removed = append(r.removed, f.ID)
}

// FileRemoveButton: on the viewer's own message a file card carries
// PreviewRemoveButton's x (20px at top -9 right -9) while hovered, its
// tooltip getRemoveLabel's; a click asks the handler to remove (or, for a
// file shared into the message, delete) the file. No x elsewhere.
func TestFileRemoveButton(t *testing.T) {
	f := &rt.File{ID: "F0RMV0001", Name: "game_scarlett_master.exe", Title: "game_scarlett_master.exe", Type: "exe", Mode: "hosted"}
	if v := layoutAt(newTestStyle(), doc(fileBlock(f)), Options{}, 600); len(v.d.removes) != 0 {
		t.Fatal("an x on a file nobody may remove")
	}
	for _, del := range []bool{false, true} {
		h := &fileRemoveRec{}
		hs := &harness{st: newTestStyle(), v: New(doc(fileBlock(f)), Options{Own: true, FileRemoves: FileRemovesOf(map[string]bool{f.ID: del})}), h: h, w: 600}
		hs.frame()
		if len(hs.v.d.removes) != 1 {
			t.Fatalf("deletion %v: %d remove buttons", del, len(hs.v.d.removes))
		}
		rm := hs.v.d.removes[0]
		want := "Remove game_scarlett_master.exe from message"
		if del {
			want = "Delete game_scarlett_master.exe"
		}
		if rm.tip != want {
			t.Errorf("deletion %v: tooltip %q, want %q", del, rm.tip, want)
		}
		if card := fileCardRect(hs.v); rm.r.Dx() != 20 || rm.r.Max.X != card.Max.X+9 || rm.r.Min.Y != card.Min.Y-9 {
			t.Errorf("deletion %v: the x is %v for the card %v, want 20px at top -9 right -9", del, rm.r, card)
		}
		c := f32.Pt(float32(rm.r.Min.X+rm.r.Max.X)/2, float32(rm.r.Min.Y+rm.r.Max.Y)/2)
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: c}, press(c, 0), release(c, 20))
		got := h.removed
		if del {
			got = h.deleted
		}
		if len(got) != 1 || got[0] != f.ID || len(h.removed)+len(h.deleted) != 1 {
			t.Errorf("deletion %v: removed %v deleted %v", del, h.removed, h.deleted)
		}
	}
	// a file still uploading has none
	p := *f
	p.Pending = true
	if v := layoutAt(newTestStyle(), doc(fileBlock(&p)), Options{Own: true, FileRemoves: FileRemovesOf(map[string]bool{f.ID: false})}, 600); len(v.d.removes) != 0 {
		t.Error("an x on a pending upload")
	}
}

// TestFileCardHoverShots saves the viewer's .exe card hovered (its x and
// FileActions) and still uploading (the corner spinner), light and dark,
// when EMBEDS_SHOTS_DIR is set (embeds-shots).
func TestFileCardHoverShots(t *testing.T) {
	dir := os.Getenv("EMBEDS_SHOTS_DIR")
	if dir == "" {
		t.Skip("EMBEDS_SHOTS_DIR not set (the embeds-shots workflow sets it)")
	}
	f := &rt.File{ID: "F0SHOT0001", Name: "game_scarlett_master.exe", Title: "game_scarlett_master.exe", Type: "exe", PrettyType: "Windows Application", Mode: "hosted",
		URL: "https://files.slack.com/files-pri/T1-F0SHOT0001/game_scarlett_master.exe", Download: "https://files.slack.com/files-pri/T1-F0SHOT0001/download/game_scarlett_master.exe"}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		for _, kind := range []string{"hover", "uploading"} {
			st := NewStyle(themeShaper(), noImages{}, pal)
			ff := *f
			o := Options{Own: true, FileRemoves: FileRemovesOf(map[string]bool{f.ID: false})}
			if kind == "uploading" {
				ff.Pending, ff.Percent = true, 42
			}
			v := New(doc(fileBlock(&ff)), o)
			var r input.Router
			var ops op.Ops
			const w, h, pad = 520, 120, 24
			frame := func() {
				ops.Reset()
				paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
				tr := op.Offset(image.Pt(pad, pad)).Push(&ops)
				gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(w-2*pad, h-2*pad))}
				v.Layout(gtx, st, &recHandler{})
				tr.Pop()
				r.Frame(&ops)
			}
			frame()
			pt := f32.Pt(pad+60, pad+30)
			if kind == "uploading" && len(v.d.pendSpins) > 0 {
				c := v.d.pendSpins[0].r.Min.Add(v.d.pendSpins[0].r.Max).Div(2)
				pt = f32.Pt(float32(pad+c.X), float32(pad+c.Y))
			}
			r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt})
			frame()
			frame()
			img := pixeltest.Render(t, w, h, &ops)
			fh, err := os.Create(filepath.Join(dir, "file-card-"+kind+"-"+mode+".png"))
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

// fileCardRect is the first file card's box (its click target's hit).
func fileCardRect(v *View) image.Rectangle {
	for _, h := range v.d.hits {
		if h.t >= 0 && int(h.t) < len(v.d.targets) && v.d.targets[h.t].kind == tFile {
			return h.r
		}
	}
	return image.Rectangle{}
}
