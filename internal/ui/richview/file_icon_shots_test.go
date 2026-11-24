package richview

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// File pillows of nine types, light and dark, saved for the messages
// audit (MESSAGES_AUDIT_SHOTS_DIR, FILE_ICON_SHOTS_TAG "before" on ui,
// "after" here): Karim's .exe drew a dark "EXE" tile where Slack draws
// its generic svgicon, a blue rounded square (#1D9BD1) with a white page.
// Only APIs ui has, so messages-audit-shots runs it on ui too.
func TestFileIconShots(t *testing.T) {
	files := []*rt.File{
		{ID: "F0ICON0001", Name: "game_scarlett_master.exe", Type: "exe", PrettyType: "Windows Application", Mode: "hosted"},
		{ID: "F0ICON0002", Name: "assets.zip", Type: "zip", PrettyType: "Zip", Mode: "hosted"},
		{ID: "F0ICON0003", Name: "q3-report.pdf", Type: "pdf", PrettyType: "PDF", Mode: "hosted"},
		{ID: "F0ICON0004", Name: "onboarding.docx", Type: "docx", PrettyType: "Word Document", Mode: "hosted"},
		{ID: "F0ICON0005", Name: "information.txt", Type: "binary", PrettyType: "Binary", Mode: "hosted"},
		{ID: "F0ICON0006", Name: "notes.txt", Type: "text", PrettyType: "Plain Text", Mode: "hosted"},
		{ID: "F0ICON0007", Name: "budget.csv", Type: "csv", PrettyType: "CSV", Mode: "hosted"},
		{ID: "F0ICON0008", Name: "deck.pptx", Type: "pptx", PrettyType: "PowerPoint Presentation", Mode: "hosted"},
		{ID: "F0ICON0009", Name: "IMG_0001.png", Type: "png", PrettyType: "PNG", Mode: "hosted"},
	}
	const w, rowH = 460, 74
	for _, dark := range []bool{false, true} {
		st := newTestStyle()
		bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		mode := "light"
		if dark {
			st.SetPalette(DarkPalette())
			bg, mode = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}, "dark"
		}
		h := rowH * len(files)
		ops := new(op.Ops)
		paint.FillShape(ops, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		for i, f := range files {
			v := New(doc(fileBlock(f)), Options{})
			off := op.Offset(image.Pt(8, i*rowH+6)).Push(ops)
			gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w-16, rowH-6))}
			v.Layout(gtx, st, nil)
			off.Pop()
		}
		img := pixeltest.Render(t, w, h, ops)
		fiSave(t, "file-icons-"+mode, img)
		// the .exe's row: the generic svgicon's #1D9BD1 tile (36px, about
		// 1,100 pixels of it less the white page)
		n := 0
		for y := 0; y < rowH; y++ {
			for x := 0; x < 80; x++ {
				if colDist(img.RGBAAt(x, y), color.RGBA{R: 0x1d, G: 0x9b, B: 0xd1, A: 0xff}) <= 24 {
					n++
				}
			}
		}
		if n < 500 {
			t.Errorf("%s: the .exe's row has %d pixels of Slack's generic blue #1d9bd1, want its 36px svgicon tile", mode, n)
		}
	}
}

func colDist(a, b color.RGBA) int {
	ab := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return ab(a.R, b.R) + ab(a.G, b.G) + ab(a.B, b.B)
}

// fiSave saves img as the shot name (tag: FILE_ICON_SHOTS_TAG, "after").
func fiSave(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		return
	}
	tag := os.Getenv("FILE_ICON_SHOTS_TAG")
	if tag == "" {
		tag = "after"
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+tag+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
