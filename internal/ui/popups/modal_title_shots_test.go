package popups

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestModalTitleShots saves long-titled modals, light and dark, under
// DIALOG_KEYS_SHOTS_DIR (dialog-keys.yml): Make Channel Manager with its
// channel entity wrapped (the icon kept), Remove from Channel Managers
// wrapped to whole lines, and RemoveMemberDialog's DeprecatedDialog title
// cut with an ellipsis, as Slack leaves it.
func TestModalTitleShots(t *testing.T) {
	dir := os.Getenv("DIALOG_KEYS_SHOTS_DIR")
	if dir == "" {
		t.Skip("DIALOG_KEYS_SHOTS_DIR unset (dialog-keys.yml sets it)")
	}
	const w, hgt = 900, 640
	member, ch := "Avery Admin-Hargreaves", "agents-lab-release-coordination"
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		h := &tzHost{th: theme.New(dark)}
		shots := []struct {
			name string
			m    *Modal
		}{
			{"modal-title-make-manager", OpenMakeChannelManagerModal(h, ch, member).Modal},
			{"modal-title-remove-manager", OpenRemoveManagerModal(h, ch, member).Modal},
			{"modal-title-remove-member", OpenRemoveMemberDialog(h, ch, member, nil).Modal},
			{"modal-title-membership", mtShown(h, func() { MaybeLeaveChannel(h, ch+"-announcements-for-everyone", "channel", true, nil) })},
			{"modal-title-leave", mtShown(h, func() { MaybeLeaveChannel(h, ch, "private", false, func() {}) })},
		}
		for _, s := range shots {
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, hgt))}
			fill(gtx, image.Rect(0, 0, w, hgt), colorsOf(h.th).Bg)
			s.m.Layout(gtx)
			t.Logf("%s-%s: title lines %q, header %v", s.name, mode, s.m.TitleLines(), s.m.TitleBar())
			img := pixeltest.Render(t, w, hgt, &ops)
			f, err := os.Create(filepath.Join(dir, s.name+"-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}

// TestModalTitleLinePixels: each wrapped line is drawn in its own 30px
// line box, as Chrome lays out the h1 (line-height 1.36365 × 22px),
// not at Gio's 1.2× paragraph pitch (36px): the ink of line i is
// centred in [top+i·30, top+(i+1)·30), and nothing is drawn in the
// bar's bottom padding under the last line. Needs a GPU
// (CLACK_PIXEL_TESTS: dialog-keys' screenshot step).
func TestModalTitleLinePixels(t *testing.T) {
	pixeltest.Need(t)
	m := mtModal(ModalOpts{Title: mtLong})
	const w, hgt = 1200, 800
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, hgt))}
	m.Layout(gtx)
	lines := m.TitleLines()
	if len(lines) < 2 {
		t.Fatalf("the title didn't wrap: %q", lines)
	}
	img := pixeltest.Render(t, w, hgt, &ops)
	x0, x1 := m.rect.Min.X+28, m.rect.Min.X+520-76
	top := m.rect.Min.Y + 20 // padding-top 20px
	dark := func(y int) (n int) {
		for x := x0; x < x1; x++ {
			if c := img.RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) < 3*110 {
				n++
			}
		}
		return n
	}
	for i := range lines {
		var sum, n int
		for y := top + i*30; y < top+(i+1)*30; y++ {
			k := dark(y)
			sum, n = sum+k*y, n+k
		}
		if n < 40 {
			t.Errorf("line %d %q: %d ink pixels in its 30px box, want the line there", i, lines[i], n)
			continue
		}
		if c, mid := sum/n, top+i*30+15; c < mid-5 || c > mid+5 {
			t.Errorf("line %d %q: its ink is centred at y %d, want %d±5 (its own 30px line box)", i, lines[i], c, mid)
		}
	}
	for y := top + len(lines)*30; y < top+len(lines)*30+18; y++ {
		if k := dark(y); k > 0 {
			t.Errorf("y %d, under the last line in the bar's padding: %d ink pixels", y, k)
			break
		}
	}
}

// mtShown runs open and returns the modal it showed last.
func mtShown(h *tzHost, open func()) *Modal {
	open()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shown[len(h.shown)-1].(*Modal)
}
