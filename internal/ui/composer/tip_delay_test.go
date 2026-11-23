package composer

import (
	"testing"
	"time"

	"gioui.org/io/pointer"
)

// The composer's buttons are Slack's ComposerButton, whose Tooltip waits
// 500 ms (CDN build 36d315d11944): Aa's shows only after it, without
// plugins and with the composer drawn from OwlMode's tree.
func TestComposerButtonTipDelay(t *testing.T) {
	for _, tc := range []tbCase{tbCases[4], tbCases[0]} {
		t.Run(tc.name, func(t *testing.T) {
			g := newTBRig(t, tc, 633)
			g.settle()
			g.mouse(pointer.Move, 0, g.at(crAa))
			t0 := g.now
			tip := &g.c.ui.tip[crAa]
			for g.now.Sub(t0) < 470*time.Millisecond {
				g.frame()
				if tip.Showing(g.now) {
					t.Fatalf("Aa's tooltip showed %v into the hover; ComposerButton waits 500 ms", g.now.Sub(t0))
				}
			}
			for g.now.Sub(t0) < 560*time.Millisecond {
				g.frame()
			}
			if !tip.Showing(g.now) {
				t.Errorf("Aa's tooltip didn't show %v into the hover (%s)", g.now.Sub(t0), g.state())
			}
		})
	}
}
