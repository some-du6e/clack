package richview

import "testing"

// Two Styles (the light and dark shapers' each) never share a generation:
// a View laid out with one and then drawn with the other re-lays out, so
// dark mode doesn't keep light-mode text colours.
func TestStyleGenerationsAreDistinct(t *testing.T) {
	light := NewStyle(sharedShaper(), noImages{}, LightPalette())
	dark := NewStyle(sharedShaper(), noImages{}, LightPalette())
	dark.SetPalette(DarkPalette())
	light.SetPalette(LightPalette())
	if light.gen == dark.gen {
		t.Fatalf("light and dark styles share generation %d", light.gen)
	}
	v := layoutAt(light, doc(para(txt("synthetic body"))), Options{}, 400)
	if v.laidGen != light.gen {
		t.Fatalf("laidGen %d, want %d", v.laidGen, light.gen)
	}
	if dark.gen == v.laidGen {
		t.Error("a View laid out in light mode would not re-lay out in dark mode")
	}
}
