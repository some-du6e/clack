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
