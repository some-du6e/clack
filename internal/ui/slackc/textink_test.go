package slackc

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// A host tree's words ink like the labels around them. UserPronouns'
// 12px rgba(var(--sk_foreground_max_solid)) is #ababad in dark mode, the
// colour of the native timestamp it follows; theme.Text fades that light
// glyph for Gio's linear-light coverage, and slackc painted it at full
// alpha, so the pronouns read heavier than the time beside them.
func TestTextInkMatchesLabels(t *testing.T) {
	prev := theme.CurrentTextModel()
	t.Cleanup(func() { theme.SetTextModel(prev) })
	theme.SetTextModel(theme.TextModelLegacy)

	darkTS := color.NRGBA{R: 171, G: 171, B: 173, A: 0xff} // --sk_foreground_max_solid, dark
	lightTS := color.NRGBA{R: 97, G: 96, B: 97, A: 0xff}   // light
	for _, dp := range []float32{1, 2} {
		got := TextInk(darkTS, 400, dp, false)
		want := theme.LabelInk(darkTS, dp, theme.Regular, false)
		if got != want {
			t.Errorf("dark #ababad at %vx: ink %v, the timestamp's label ink %v", dp, got, want)
		}
		if got.A == 0xff {
			t.Errorf("dark #ababad at %vx: drawn at full alpha, want the light-text fade", dp)
		}
		if got := TextInk(lightTS, 400, dp, false); got != lightTS {
			t.Errorf("light #616061 at %vx: ink %v, want it as is (dark ink isn't faded)", dp, got)
		}
		// bold faded 3/4 as much, Black not at all, as labels are
		if got, want := TextInk(darkTS, 700, dp, false), theme.LabelInk(darkTS, dp, theme.Bold, false); got != want {
			t.Errorf("bold at %vx: ink %v, want %v", dp, got, want)
		}
		if got := TextInk(darkTS, 900, dp, false); got != darkTS {
			t.Errorf("black at %vx: ink %v, want it as is", dp, got)
		}
		// the aubergine chrome's lighter fade
		if got, want := TextInk(darkTS, 400, dp, true), theme.LabelInk(darkTS, dp, theme.Regular, true); got != want {
			t.Errorf("chrome at %vx: ink %v, want %v", dp, got, want)
		}
	}

	// Windows inks like Skia: no fade for labels, none here
	theme.SetTextModel(theme.TextModelSkiaWin)
	if got := TextInk(darkTS, 400, 1, false); got != darkTS {
		t.Errorf("Skia model: ink %v, want %v unfaded", got, darkTS)
	}
}
