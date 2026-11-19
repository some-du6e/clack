package richview

import (
	"image/color"
	"testing"
)

// TestSeeThroughFillsOverUnder: Slack's code, pre and mention fills are
// rgba, so over a row's own background (the jump highlight's yellow, a
// hover) they show it through them. A View over such a fill (SetUnder)
// composites its palette's see-through fills over it instead of the pane,
// leaves opaque colors alone, and records again when the fill changes.
func TestSeeThroughFillsOverUnder(t *testing.T) {
	p := LightPalette()
	yellow := color.NRGBA{R: 0xfc, G: 0xf4, B: 0xda, A: 255} // #f2c74433 over white
	v := &View{see: p.see}
	if got := v.overUnder(p.CodeBg); got != p.CodeBg {
		t.Errorf("over the pane: code %v, want the palette's %v", got, p.CodeBg)
	}
	v.SetUnder(yellow)
	for _, c := range []struct {
		name     string
		flat, in color.NRGBA
	}{
		{"code", p.CodeBg, rgba(29, 28, 29, 0.04)},
		{"pre", p.PreBg, rgba(29, 28, 29, 0.04)},
		{"a mention", p.MentionBg, rgba(54, 197, 240, 0.13)},
		{"your own mention", p.SelfMentionBg, rgba(255, 198, 0, 0.18)},
	} {
		if got, want := v.overUnder(c.flat), Over(c.in, yellow); got != want {
			t.Errorf("%s over the highlight: %v, want %v", c.name, got, want)
		}
	}
	if got := v.overUnder(p.Text); got != p.Text {
		t.Errorf("an opaque color changed over the highlight: %v", got)
	}
	v.recorded = true
	v.SetUnder(yellow)
	if !v.recorded {
		t.Errorf("the same fill again dropped the recording")
	}
	v.SetUnder(color.NRGBA{})
	if v.recorded {
		t.Errorf("a new fill kept the recording drawn over the old one")
	}
}
