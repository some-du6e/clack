package popups

import (
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// The thread header's ⋮ is SecondaryHeaderMore (boot-shared G8VF, build
// 2cd9270e786c): MenuTrigger (pe4G) with position "bottom-left", so
// Popover (L1PI) puts the menu's top-left at the button's bottom-left
// (offsets 0) and keepInBounds pulls it back to 16px inside the window's
// right edge (allowanceX), not right-aligned to the button.
func TestThreadHeaderMenuPosition(t *testing.T) {
	h := hcEnv(t)
	root := core.Message{Channel: "C0THMENU01", TS: "1762700000.000100"}
	for _, tc := range []struct {
		name string
		btn  image.Rectangle
		left func(w int) int
	}{
		{"mid-window", image.Rect(400, 10, 428, 38), func(int) int { return 400 }},
		{"at the right edge", image.Rect(950, 10, 978, 38), func(w int) int { return 1000 - 16 - w }},
	} {
		m := NewThreadHeaderMenu(h, tc.btn, root, false, false, func() {})
		newMkEnv(t, m) // a 1000x800 window
		r := m.Rect()
		if r.Empty() {
			t.Fatalf("%s: the menu wasn't laid out", tc.name)
		}
		if want := image.Pt(tc.left(r.Dx()), tc.btn.Max.Y); r.Min != want {
			t.Errorf("%s: menu at %v, Slack's bottom-left Popover %v", tc.name, r.Min, want)
		}
	}
}
