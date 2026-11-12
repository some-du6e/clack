package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Slack's own file, unchanged (win32 app.asar 4.53.276).
func TestBadgeResourcesAreSlacks(t *testing.T) {
	sum := sha256.Sum256(badgeJSON)
	if got := hex.EncodeToString(sum[:]); got != "22d76a5db36140a109ea39e5de46a0bb90652b6c383b190cadd06ede45cd18c1" {
		t.Fatalf("unread-badge-resources.json sha256 %s", got)
	}
}

// vc's index: the count up to 99, the "+" image (100) past it; each a
// 64×64 PNG.
func TestBadgePNG(t *testing.T) {
	plus, err := BadgePNG(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 9, 42, 99, 100, 101, 5000} {
		b, err := BadgePNG(n)
		if err != nil {
			t.Fatalf("%d: %v", n, err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%d: %v", n, err)
		}
		if s := img.Bounds().Size(); s != image.Pt(64, 64) {
			t.Fatalf("%d: %v", n, s)
		}
		if (n > 99) != bytes.Equal(b, plus) {
			t.Errorf("%d: the + image is for counts past 99 only", n)
		}
	}
	if o, err := BadgeOverlay(0); o != nil || err != nil {
		t.Fatalf("BadgeOverlay(0) = %v, %v; want nil (the overlay cleared)", o, err)
	}
}

// Each output pixel's Lanczos3 weights sum to 1.0 in 14-bit fixed point,
// so a flat image stays flat (ResizeFilter's leftovers).
func TestLanczosFiltersNormalized(t *testing.T) {
	for _, f := range computeFilters(64, 16) {
		s := 0
		for _, v := range f.values {
			s += int(v)
		}
		if s != 1<<filterShiftBits {
			t.Fatalf("filter at %d sums to %d", f.offset, s)
		}
	}
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 0xb0, 0x1a, 0x4f, 0xff
	}
	dst := resizeLanczos3(src, 16, 16)
	for i := 0; i < len(dst.Pix); i += 4 {
		if got := dst.Pix[i : i+4]; got[0] != 0xb0 || got[1] != 0x1a || got[2] != 0x4f || got[3] != 0xff {
			t.Fatalf("flat image resized to %v at %d", got, i/4)
		}
	}
}

// TaskbarHost::SetOverlayIcon's 16×16: the badge's red middle, transparent
// corners outside the oval clip, and premultiplied pixels throughout
// (alpha never under a colour channel). BADGE_SHOTS_DIR gets a sheet of
// overlays at 8× next to Slack's 64px images.
func TestBadgeOverlay(t *testing.T) {
	counts := []int{1, 2, 9, 10, 42, 99, 100}
	sheet := image.NewRGBA(image.Rect(0, 0, len(counts)*(128+64+16), 136))
	for i, n := range counts {
		o, err := BadgeOverlay(n)
		if err != nil {
			t.Fatal(err)
		}
		if o.Rect.Size() != image.Pt(16, 16) {
			t.Fatalf("%d: %v", n, o.Rect)
		}
		for _, c := range []image.Point{{0, 0}, {15, 0}, {0, 15}, {15, 15}} {
			if a := o.RGBAAt(c.X, c.Y).A; a != 0 {
				t.Errorf("%d: corner %v alpha %d", n, c, a)
			}
		}
		for p := 0; p < len(o.Pix); p += 4 {
			if a := o.Pix[p+3]; o.Pix[p] > a || o.Pix[p+1] > a || o.Pix[p+2] > a {
				t.Fatalf("%d: pixel %d %v isn't premultiplied", n, p/4, o.Pix[p:p+4])
			}
		}
		// (8, 1) is the badge's dark outline (#4F031B at 64px), (8, 3)
		// its #E01E5A fill
		if c := o.RGBAAt(8, 1); c.A < 200 || c.R > 170 || c.G > 25 {
			t.Errorf("%d: outline pixel %v isn't the badge's dark rim", n, c)
		}
		if c := o.RGBAAt(8, 3); c.A < 200 || c.R < 150 || c.G > 80 {
			t.Errorf("%d: pixel %v under the rim isn't the badge red", n, c)
		}
		x := i * (128 + 64 + 16)
		for y := 0; y < 128; y++ {
			for xx := 0; xx < 128; xx++ {
				sheet.SetRGBA(x+xx, y+4, o.RGBAAt(xx/8, y/8))
			}
		}
		b, _ := BadgePNG(n)
		src, _ := png.Decode(bytes.NewReader(b))
		draw.Draw(sheet, image.Rect(x+132, 36, x+196, 100), src, image.Point{}, draw.Over)
	}
	if dir := os.Getenv("BADGE_SHOTS_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "taskbar-badge-overlays.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, sheet); err != nil {
			t.Fatal(err)
		}
	}
}
