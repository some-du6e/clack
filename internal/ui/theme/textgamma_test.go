package theme

import (
	"image/color"
	"math"
	"testing"
)

// TestTextInk: which model each platform draws with, Skia's contrast for
// Slack's text colours (SkMaskGamma with Chromium's Windows settings), the
// two-paint split for translucent text, and that the legacy model is
// untouched by it.
func TestTextInk(t *testing.T) {
	for _, c := range []struct {
		goos, env string
		want      TextModel
	}{
		{"windows", "", TextModelSkiaWin},
		{"linux", "", TextModelLegacy},
		{"darwin", "", TextModelLegacy},
		{"linux", "skia", TextModelSkiaWin},
		{"windows", "legacy", TextModelLegacy},
		{"windows", " Legacy ", TextModelLegacy},
		{"windows", "nonsense", TextModelSkiaWin},
	} {
		if got := defaultTextModel(c.goos, c.env); got != c.want {
			t.Errorf("defaultTextModel(%q, %q) = %v, want %v", c.goos, c.env, got, c.want)
		}
	}

	defer SetTextModel(CurrentTextModel())
	SetTextModel(TextModelSkiaWin)
	for _, c := range []struct {
		ink  uint32
		want float32 // contrast·toLuma(1-src), src the 3-bit canonical luminance
	}{
		{ink: 0x000000, want: 1},       // src 0
		{ink: 0x1d1c1d, want: 1},       // light body: every channel < 32, src 0
		{ink: 0x616061, want: 0.2874},  // light timestamps: src 109/255
		{ink: 0xb9babd, want: 0.0666},  // dark body: src 182/255
		{ink: 0xd1d2d3, want: 0.01764}, // dark primary: src 219/255
		{ink: 0xffffff, want: 0},       // src 255
	} {
		col := color.NRGBA{R: uint8(c.ink >> 16), G: uint8(c.ink >> 8), B: uint8(c.ink), A: 0xff}
		if got := TextContrast(col); math.Abs(float64(got-c.want)) > 0.001 {
			t.Errorf("TextContrast(#%06x) = %.4f, want %.4f", c.ink, got, c.want)
		}
		col.A = 0x80 // alpha plays no part in the table
		if got := TextContrast(col); math.Abs(float64(got-c.want)) > 0.001 {
			t.Errorf("TextContrast(#%06x at half alpha) = %.4f, want %.4f", c.ink, got, c.want)
		}
	}
	// opaque ink: the second paint is k exactly
	if a1, a2 := textAlphas(color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}); a1 != 0xff || a2 != 0xff {
		t.Errorf("#1d1c1d paints (%d, %d), want (255, 255)", a1, a2)
	}
	if a1, a2 := textAlphas(color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}); a1 != 0xff || a2 != 73 {
		t.Errorf("#616061 paints (%d, %d), want (255, 73)", a1, a2)
	}
	if a1, a2 := textAlphas(color.NRGBA{R: 0xd1, G: 0xd2, B: 0xd3, A: 0xff}); a1 != 0xff || a2 != 0 {
		t.Errorf("#d1d2d3 paints (%d, %d), want one paint (k under 2%%)", a1, a2)
	}
	// translucent ink: full coverage stays at α, the slope never passes
	// Skia's α(1+k), and the pair moves continuously with α (fades)
	for _, k := range []float64{0.05, 0.3, 0.7, 1} {
		prev := [2]float64{-1, -1}
		for i := 0; i <= 100; i++ {
			alpha := float64(i) / 100
			a1, a2 := contrastPaints(alpha, k)
			if a1 < 0 || a1 > 1 || a2 < 0 || a2 > 1 {
				t.Fatalf("contrastPaints(%.2f, %.2f) = (%.4f, %.4f): out of range", alpha, k, a1, a2)
			}
			if full := 1 - (1-a1)*(1-a2); math.Abs(full-alpha) > 1e-9 {
				t.Errorf("contrastPaints(%.2f, %.2f): full coverage %.6f, want %.2f", alpha, k, full, alpha)
			}
			if a1+a2 > alpha*(1+k)+1e-9 {
				t.Errorf("contrastPaints(%.2f, %.2f): slope %.4f over %.4f", alpha, k, a1+a2, alpha*(1+k))
			}
			if prev[0] >= 0 && (math.Abs(a1-prev[0]) > 0.15 || math.Abs(a2-prev[1]) > 0.15) {
				t.Errorf("contrastPaints jumps at α %.2f, k %.2f: (%.3f, %.3f) after (%.3f, %.3f)", alpha, k, a1, a2, prev[0], prev[1])
			}
			prev = [2]float64{a1, a2}
		}
		if a1, a2 := contrastPaints(1, k); a1 != 1 || math.Abs(a2-k) > 1e-12 {
			t.Errorf("contrastPaints(1, %.2f) = (%.3f, %.3f), want (1, k)", k, a1, a2)
		}
	}
	// no fade, no embolden under Skia's model
	light := color.NRGBA{R: 0xab, G: 0xab, B: 0xad, A: 0xff}
	if got := lightTextColor(light, 1, Regular, false); got != light {
		t.Errorf("Skia model fades #ababad to %v", got)
	}
	if e := EmboldenFor(rgb(0x1d1c1d)); e != 0 {
		t.Errorf("Skia model emboldens #1d1c1d by %.2f", e)
	}
	if e := GlyphEmbolden(0.33); e != 0 {
		t.Errorf("GlyphEmbolden(0.33) = %.2f under Skia's model", e)
	}

	// the legacy model: one paint at the colour's own alpha, the fade and
	// embolden as before
	SetTextModel(TextModelLegacy)
	for _, c := range []color.NRGBA{rgb(0x1d1c1d), rgb(0x616061), {R: 0x1d, G: 0x1c, B: 0x1d, A: 0x80}} {
		if a1, a2 := textAlphas(c); a1 != c.A || a2 != 0 {
			t.Errorf("legacy paints %v as (%d, %d), want (%d, 0)", c, a1, a2, c.A)
		}
		if k := TextContrast(c); k != 0 {
			t.Errorf("legacy TextContrast(%v) = %.3f", c, k)
		}
	}
	if !noEmbolden {
		if e := EmboldenFor(rgb(0x1d1c1d)); e != Embolden {
			t.Errorf("legacy EmboldenFor(#1d1c1d) = %.2f, want %.2f", e, Embolden)
		}
	}
	if LightTextFade > 0 {
		if got := lightTextColor(light, 1, Regular, false); got.A == 0xff {
			t.Errorf("legacy model doesn't fade #ababad any more")
		}
	}
	if e := GlyphEmbolden(0.33); e != 0.33 {
		t.Errorf("legacy GlyphEmbolden(0.33) = %.2f", e)
	}
}
