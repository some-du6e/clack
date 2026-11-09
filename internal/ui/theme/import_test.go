package theme

import (
	"math/rand/v2"
	"testing"
)

// createPayloadFromHexStrings: four hexes are custom roles with the
// primary's brightness; a legacy string maps column_bg / active_item /
// active_presence / badge, as palettes when they're palette shades; junk
// is refused.
func TestImportThemeString(t *testing.T) {
	cur := ThemePresets[0].Theme
	got, ok := ImportThemeString(cur, "#124426, #350d36,#EEB317, #392104")
	if !ok || !got.UseCustomHex || got.Primary.Custom != "#124426" || got.Highlight1.Custom != "#350D36" || got.Important.Custom != "#392104" {
		t.Fatalf("4 hexes: %+v %v", got, ok)
	}
	if got.Brightness < 1 || got.Brightness > 6 || got.SidebarInverted != (got.Brightness > 3) {
		t.Fatalf("brightness %d inverted %v", got.Brightness, got.SidebarInverted)
	}
	legacy := "#3F0E40,#350D36,#1164A3,#FFFFFF,#350D36,#FFFFFF,#2BAC76,#CD2553"
	got, ok = ImportThemeString(cur, legacy)
	if !ok || got.Primary.Custom != "#3F0E40" && got.Primary.Palette == "" || got.Highlight2.Custom == "" && got.Highlight2.Palette == "" {
		t.Fatalf("legacy: %+v %v", got, ok)
	}
	for _, bad := range []string{"", "#12", "#124426,#350D36,#EEB317", "red,green,blue,white", "#GGGGGG,#350D36,#EEB317,#392104"} {
		if _, ok := ImportThemeString(cur, bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	// a palette swatch comes back as that palette
	c := namedRamp("jade", false)[6]
	if p, sh, ok := paletteOfHex(hexOf(c.R, c.G, c.B)); !ok || p != "jade" || sh != "50" {
		t.Fatalf("paletteOfHex %q %q %v", p, sh, ok)
	}
}

func hexOf(r, g, b uint8) string {
	const d = "0123456789ABCDEF"
	return string([]byte{'#', d[r>>4], d[r&15], d[g>>4], d[g&15], d[b>>4], d[b&15]})
}

func TestSurpriseTheme(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 50; i++ {
		th := SurpriseTheme(r)
		if !th.UseCustomHex || th.Brightness < 1 || th.Brightness > 6 || th.SidebarInverted != (th.Brightness > 3) || th.Primary.Palette == "" {
			t.Fatalf("%+v", th)
		}
	}
}
