package theme

import (
	"fmt"
	"image/color"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// The account theme (users.prefs ia_theme) is mapped to colours on the UI
// path at startup (from the cached prefs) and on every prefs event. A
// freeze at launch that survived deleting ui.json pointed at account state
// (2026-11-08), so these tests check that the mapping, HCT included, is
// quick and can't panic for anything Slack (or another client) could have
// stored: every preset, every palette, custom hexes across the whole sRGB
// cube and its extremes, brightness out of range, junk JSON.

// boundsBudget is the worst single theme computation allowed. A custom-hex
// theme takes about a millisecond (≈250 HCT solves at ~3 µs); this only
// catches something pathological (a loop, a runaway solve), not a busy
// CI runner.
const boundsBudget = 500 * time.Millisecond

func checkTheme(t *testing.T, what string, th IATheme) {
	t.Helper()
	for _, dark := range []bool{false, true} {
		for _, grad := range []bool{false, true} {
			t0 := time.Now()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s dark=%v gradients=%v: panic %v (theme %s)", what, dark, grad, r, th.JSON())
					}
				}()
				f := th.Fields(dark, grad)
				var p Palette
				ApplyIATheme(&p, th, dark, grad)
				if len(f) != len(accountFields) {
					t.Fatalf("%s: %d fields, want %d", what, len(f), len(accountFields))
				}
				_ = th.ForSave(dark).PresetID()
			}()
			if d := time.Since(t0); d > boundsBudget {
				t.Errorf("%s dark=%v gradients=%v took %v (theme %s)", what, dark, grad, d, th.JSON())
			}
		}
	}
}

func TestThemeMappingPresetsAndPalettes(t *testing.T) {
	for _, pr := range ThemePresets {
		checkTheme(t, "preset "+pr.ID, pr.Theme)
		for b := -3; b <= 9; b++ {
			th := pr.Theme
			th.Brightness = b
			for _, inv := range []bool{false, true} {
				th.SidebarInverted = inv
				checkTheme(t, fmt.Sprintf("preset %s brightness %d inverted %v", pr.ID, b, inv), th)
			}
		}
	}
	for _, name := range append(append([]string{}, PaletteNames...), "", "nope", "gray") {
		th := DefaultIATheme
		th.Primary.Palette, th.Highlight1.Palette, th.Highlight2.Palette, th.Important.Palette = name, name, name, name
		checkTheme(t, "palette "+name, th)
	}
}

// A 9-bit grid of the sRGB cube (8 levels per channel) in every role, plus
// the extremes, greys and near-greys where HCT's chroma is ~0.
func TestThemeMappingCustomHexCube(t *testing.T) {
	start := time.Now()
	var hexes []string
	for r := 0; r < 256; r += 36 {
		for g := 0; g < 256; g += 36 {
			for b := 0; b < 256; b += 36 {
				hexes = append(hexes, fmt.Sprintf("#%02x%02x%02x", r, g, b))
			}
		}
	}
	for v := 0; v < 256; v++ { // greys and off-by-one greys
		hexes = append(hexes, fmt.Sprintf("#%02x%02x%02x", v, v, v), fmt.Sprintf("#%02x%02x%02x", v, v, (v+1)&0xff))
	}
	hexes = append(hexes, "#000000", "#FFFFFF", "#000001", "#FEFFFF", "#FF0000", "#00FF00", "#0000FF", "#00FFFF", "#FF00FF", "#FFFF00")
	worst, worstHex := time.Duration(0), ""
	for i, h := range hexes {
		th := DefaultIATheme
		th.UseCustomHex = true
		th.Brightness = 1 + i%6
		th.SidebarInverted = i%2 == 0
		// rotate the hex through the roles so every role sees every colour
		roles := []*IARole{&th.Primary, &th.Highlight1, &th.Highlight2, &th.Important}
		for k, r := range roles {
			r.Custom = hexes[(i+k*977)%len(hexes)]
		}
		t0 := time.Now()
		checkTheme(t, "custom "+h, th)
		if d := time.Since(t0); d > worst {
			worst, worstHex = d, h
		}
	}
	el := time.Since(start)
	t.Logf("%d custom themes in %v (worst %v at %s)", len(hexes), el.Round(time.Millisecond), worst, worstHex)
	if avg := el / time.Duration(4*len(hexes)); avg > 20*time.Millisecond {
		t.Errorf("a custom theme takes %v on average", avg)
	}
}

// HCT itself: every tone of every hue at the chroma extremes.
func TestHCTSolveBounded(t *testing.T) {
	t0 := time.Now()
	for h := -720.0; h <= 720; h += 7.5 {
		for _, c := range []float64{0, 1e-5, 1e-4, 0.5, 1, 10, 48, 100, 150, 200, 1e3, 1e9} {
			for l := -10.0; l <= 110; l += 2.5 {
				_ = hctSolveToInt(h, c, l)
			}
		}
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("HCT sweep took %v", d)
	}
}

// Junk ia_theme values: ParseIATheme must answer (ok or not) and whatever
// it returns must map.
func TestParseIAThemeJunk(t *testing.T) {
	junk := []string{
		"", "null", "{}", "[]", "1", `"x"`, "{", `{"primary":null}`, `{"brightness":-99999999}`,
		`{"brightness":9223372036854775807}`, `{"brightness":"6"}`, `{"primary":{"palette":"ocean","custom":"#zzzzzz"},"useCustomHex":true}`,
		`{"primary":{"palette":"","custom":"#12"},"useCustomHex":true}`, `{"primary":{"custom":"   #abc   "},"useCustomHex":true}`,
		`{"primary":{"custom":"#1234567"},"useCustomHex":true}`, `{"important":{"palette":"\u0000"}}`,
		`{"sidebarInverted":"yes"}`, `{"useCustomHex":true}`, strings.Repeat(`{"a":`, 5000) + "1" + strings.Repeat("}", 5000),
	}
	for _, s := range junk {
		th, _ := ParseIATheme(s)
		checkTheme(t, fmt.Sprintf("ParseIATheme(%.40q)", s), th)
	}
}

func FuzzIATheme(f *testing.F) {
	for _, pr := range ThemePresets {
		f.Add(pr.Theme.JSON())
	}
	f.Add(`{"primary":{"palette":"aubergine","custom":"#000000"},"highlight1":{"palette":"grass","custom":"#FFFFFF"},"highlight2":{"palette":"grass","custom":"#808080"},"important":{"palette":"aubergine","custom":"#fff"},"brightness":0,"sidebarInverted":true,"useCustomHex":true}`)
	f.Fuzz(func(t *testing.T, s string) {
		th, _ := ParseIATheme(s)
		checkTheme(t, "fuzz", th)
	})
}

// Random themes (fixed seed): random hexes, brightness and flags.
func TestThemeMappingRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		th := IATheme{Brightness: rng.Intn(12) - 3, SidebarInverted: rng.Intn(2) == 0, UseCustomHex: rng.Intn(4) != 0, HighContrast: rng.Intn(8) == 0}
		for _, r := range []*IARole{&th.Primary, &th.Highlight1, &th.Highlight2, &th.Important} {
			r.Palette = PaletteNames[rng.Intn(len(PaletteNames))]
			c := color.NRGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256))}
			r.Custom = fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
		}
		checkTheme(t, "random", th)
	}
}
