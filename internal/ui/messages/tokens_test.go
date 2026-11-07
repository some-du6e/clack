package messages

import (
	"image/color"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type fakeTokens map[string]color.NRGBA

func (f fakeTokens) Color(expr string) (color.NRGBA, bool, bool) {
	c, ok := f[expr]
	return c, ok, ok
}
func (f fakeTokens) Overridden() bool { return len(f) > 0 }
func (f fakeTokens) Key() uint64      { return uint64(len(f)) }

// A Slick theme's tokens recolour the message list itself (live, every
// theme left the list on Slack's #1a1d21 while the chrome followed).
func TestMessageColorsFollowTokens(t *testing.T) {
	th := theme.Bare(true)
	if c := colorsFor(th); c.Bg != darkColors.Bg {
		t.Fatalf("no tokens: Bg %v, want Slack's %v", c.Bg, darkColors.Bg)
	}
	red := color.NRGBA{0xff, 0, 0, 0xff}
	yellow := color.NRGBA{0xff, 0xff, 0, 0xff}
	th.Tokens = fakeTokens{
		"rgb(var(--sk_primary_background))": red,
		"var(--dt_color-content-pry)":       yellow,
		"var(--dt_color-content-hgl-1)":     yellow,
	}
	th.Gen++
	c := colorsFor(th)
	for name, w := range map[string][2]color.NRGBA{
		"Bg": {c.Bg, red}, "ThreadBg": {c.ThreadBg, red}, "Text": {c.Text, yellow}, "Link": {c.Link, yellow},
		"Text2 (not overridden)": {c.Text2, darkColors.Text2},
	} {
		if w[0] != w[1] {
			t.Errorf("%s = %v, want %v", name, w[0], w[1])
		}
	}
	th.Tokens = nil
	th.Gen++
	if c := colorsFor(th); c.Bg != darkColors.Bg {
		t.Errorf("tokens removed: Bg %v, want %v", c.Bg, darkColors.Bg)
	}
}
