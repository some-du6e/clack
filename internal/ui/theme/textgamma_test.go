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
	prev := primaryTheme.Load()
	defer SetPrimary(prev)
	// Skia's own table (its guessed background, 1-src)
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
		if got := skiaContrast(col, skiaWinContrast); math.Abs(float64(got-c.want)) > 0.001 {
			t.Errorf("skiaContrast(#%06x) = %.4f, want %.4f", c.ink, got, c.want)
		}
		col.A = 0x80 // alpha plays no part in the table
		if got := skiaContrast(col, skiaWinContrast); math.Abs(float64(got-c.want)) > 0.001 {
			t.Errorf("skiaContrast(#%06x at half alpha) = %.4f, want %.4f", c.ink, got, c.want)
		}
	}
	// what gets painted: Skia's k in dark mode; in light mode dark ink's k
	// is fitted to Chrome's blit of that table over white
	paints := func(ink uint32) [2]uint8 {
		a1, a2 := textAlphas(color.NRGBA{R: uint8(ink >> 16), G: uint8(ink >> 8), B: uint8(ink), A: 0xff})
		return [2]uint8{a1, a2}
	}
	near := func(a, b [2]uint8) bool { // ±1 for the fitted alphas' rounding
		return a[0] == b[0] && int(a[1])-int(b[1]) <= 1 && int(b[1])-int(a[1]) <= 1
	}
	SetPrimary(Bare(true))
	for ink, want := range map[uint32][2]uint8{
		0x1d1c1d: {255, 255},
		0x616061: {255, 73}, // Skia's 0.2874
		0xb9babd: {255, 17},
		0xd1d2d3: {255, 0}, // k under 2%: one paint
	} {
		if got := paints(ink); !near(got, want) {
			t.Errorf("dark mode: #%06x paints %v, want %v", ink, got, want)
		}
	}
	SetPrimary(Bare(false))
	for ink, want := range map[uint32][2]uint8{
		0x000000: {255, 255},
		0x1d1c1d: {255, 255},
		0x616061: {255, 172}, // fitted over white: 0.676 (Skia's own 0.287 drew it at 87% of Chrome's ink)
		0x868686: {255, 186}, // 0.728 (Skia's own 0.153)
		0x454447: {255, 168},
		0xd1d2d3: {255, 0}, // light ink keeps Skia's k
	} {
		if got := paints(ink); !near(got, want) {
			t.Errorf("light mode: #%06x paints %v, want %v", ink, got, want)
		}
	}
	if k := TextContrast(rgb(0x616061)); math.Abs(float64(k)-0.6756) > 0.002 {
		t.Errorf("light mode TextContrast(#616061) = %.4f, want 0.6756", k)
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

// The light-text fade is continuous on a dark background, the chrome scope
// or a dark primary theme: it stopped at once below y=.6, from alpha .55
// to 1, so a muted DM row drew its #958a9d timestamp (y=.56, unfaded)
// brighter than its bold #9a9b9e name (y=.61, faded), the reverse of
// Slack's #9a9b9e over #958a9d. Greys on white stay unfaded.
func TestLightTextFadeContinuous(t *testing.T) {
	if LightTextFade <= 0 {
		t.Skip("CLACK_LABEL_TEXT_FADE turns the fade off")
	}
	defer SetTextModel(CurrentTextModel())
	SetTextModel(TextModelLegacy)
	prev := primaryTheme.Load()
	defer SetPrimary(prev)
	SetPrimary(New(true))
	alpha := func(c color.NRGBA, bold, chrome bool) float64 {
		w := Regular
		if bold {
			w = Bold
		}
		return float64(lightTextColor(c, 1, w, chrome).A) / 255
	}
	for _, chrome := range []bool{false, true} {
		for _, bold := range []bool{false, true} {
			for v := uint32(0x60); v < 0xf0; v++ {
				a0, a1 := alpha(rgb(v*0x010101), bold, chrome), alpha(rgb((v+1)*0x010101), bold, chrome)
				if math.Abs(a1-a0) > 0.02 {
					t.Errorf("chrome=%v bold=%v: the fade jumps from alpha %.3f at #%06x to %.3f at #%06x", chrome, bold, a0, v*0x010101, a1, (v+1)*0x010101)
				}
			}
		}
	}
	// the muted DM row on the dark card, as Gio blends it (linear light)
	card := rgb(0x212428)
	drawn := func(c color.NRGBA, bold bool) float64 {
		a := alpha(c, bold, false)
		lum := func(c color.NRGBA) float64 {
			return 0.2126*srgbLin(c.R) + 0.7152*srgbLin(c.G) + 0.0722*srgbLin(c.B)
		}
		return a*lum(c) + (1-a)*lum(card)
	}
	if name, ts := drawn(rgb(0x9a9b9e), true), drawn(rgb(0x958a9d), false); name <= ts {
		t.Errorf("a muted DM's name (#9a9b9e bold) draws at luminance %.3f, its timestamp (#958a9d) at %.3f: Slack's name is the brighter", name, ts)
	}
	SetPrimary(New(false))
	for _, v := range []uint32{0x868686, 0x9a9b9e - 0x060606, 0x737373} {
		if a := alpha(rgb(v), false, false); a != 1 {
			t.Errorf("light mode fades #%06x on white to alpha %.3f", v, a)
		}
	}
	// an account theme's light sidebar (Karim's #FED4BE as the picker
	// stores it: #1c0901 ink): its muted rows (#1c0901 at .5 over the
	// near-white sidebar, y≈.52) are dark ink on light, so unfaded
	ia := mustTheme(t, slackInkCases[0].ia)
	lsb := New(false)
	lsb.Restyle = func(th *Theme, p *Palette) { ApplyIATheme(p, ia, th.Dark, true) }
	lsb.Refresh()
	SetPrimary(lsb)
	muted := Over(color.NRGBA{R: 0x1c, G: 0x09, B: 0x01, A: 0x80}, rgb(0xfdf7fa))
	if a := alpha(muted, false, true); a != 1 {
		t.Errorf("a light sidebar's muted row (%v) fades to alpha %.3f", muted, a)
	}
	SetPrimary(New(false)) // the default theme: its inverted sidebar is dark
	if a := alpha(muted, false, true); a == 1 {
		t.Errorf("the dark sidebar's grey %v (y≈.52) is unfaded", muted)
	}
}
