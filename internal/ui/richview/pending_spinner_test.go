package richview

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A generic file still uploading (Karim's .exe, "no indicator?") draws
// Slack's PendingFilesSpinner: a 16px spinner on a round background at
// the pillow's top right, 8px out, not at the pillow's .5, with the
// tooltip that says how the upload goes; a gallery image keeps its own
// centred spinner and gets none.
func TestPendingGenericFileSpinner(t *testing.T) {
	st := newTestStyle()
	f := &rt.File{ID: "F0PEND0001", Name: "game_scarlett_master.exe", Type: "exe", PrettyType: "Windows Application", Mode: "hosted", Pending: true, Percent: 42}
	v := New(doc(fileBlock(f)), Options{})
	ops := new(op.Ops)
	v.Layout(testGtx(ops, 600), st, nil)
	if len(v.d.pendSpins) != 1 {
		t.Fatalf("%d pending spinners, want 1", len(v.d.pendSpins))
	}
	s := v.d.pendSpins[0]
	if s.r.Dx() != 16 || s.r.Dy() != 16 {
		t.Errorf("the spinner is %v, want InfiniteSpinner medium (16px)", s.r)
	}
	if s.tip != "Uploading… 42%" {
		t.Errorf("tooltip %q", s.tip)
	}
	if card := fileCardRect(v); s.r.Max.X != card.Max.X+8 || s.r.Min.Y != card.Min.Y-8 {
		t.Errorf("the spinner is %v for the card %v, want top -8px, right -8px", s.r, card)
	}
	found := false
	for _, sp := range v.d.spinners {
		found = found || sp.r == s.r
	}
	if !found {
		t.Error("no InfiniteSpinner drawn in the spinner's box")
	}
	for _, b := range v.d.boxes {
		if b.kind == bCircle && b.r == s.r && b.fill.A != 0xff {
			t.Errorf("the spinner's background is at alpha %d: faded with the pillow", b.fill.A)
		}
	}
	// the tooltip shows while the pointer is on it
	c := s.r.Min.Add(s.r.Max).Div(2)
	v.pointerAt(testGtx(ops, 600), st, f32.Pt(float32(c.X), float32(c.Y)))
	if v.spinHot != 1 {
		t.Errorf("hovering the spinner: spinHot %d", v.spinHot)
	}
	for p, want := range map[int]string{0: "Waiting to upload…", 98: "Uploading… 98%", 99: "Processing…"} {
		if got := pendingSpinnerTip(p); got != want {
			t.Errorf("pendingSpinnerTip(%d) = %q, want %q", p, got, want)
		}
	}

	// an image still uploading: the tile's own spinner, no corner one
	img := &rt.File{ID: "F0PEND0002", Name: "a.png", IsImage: true, Pending: true, Percent: 10, Thumb: &rt.Image{URL: "https://files.test/a.png", W: 100, H: 80}}
	v = New(doc(fileBlock(img)), Options{})
	ops.Reset()
	v.Layout(testGtx(ops, 600), st, nil)
	if len(v.d.pendSpins) != 0 {
		t.Errorf("a pending image has %d corner spinners", len(v.d.pendSpins))
	}
}
