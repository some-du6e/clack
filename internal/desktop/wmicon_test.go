package desktop

import (
	"image"
	"image/color"
	"testing"
)

func TestNetWMIcon(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	m.SetNRGBA(0, 0, color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	m.SetNRGBA(1, 0, color.NRGBA{R: 0xaa, A: 0x80})
	got := netWMIcon([]*image.NRGBA{m})
	want := []uint64{2, 1, 0xff112233, 0x80aa0000}
	if len(got) != len(want) {
		t.Fatalf("got %x", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %x, want %x", got, want)
		}
	}
}
