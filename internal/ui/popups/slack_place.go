package popups

import (
	"image"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's Popover placement (client-boot L1PI, CDN build 4d0c78085232):
// getInitialPosition puts the card at one of twelve positions around the
// target with offsetX / offsetY; keepInBounds then keeps it allowanceX
// (16) / allowanceY (10) inside the window, shifting it rather than
// flipping, and with tryOppositePosition, when the top had to move, takes
// the vertically opposite position (offsetY negated) if that fits. The
// code is theme.KeepInBounds, which tooltips share (theme/tooltip_place.go).

// slackPlace is keepInBounds: the card's top-left for target t in a win
// window, allowances ax / ay.
func slackPlace(t image.Rectangle, sz, win image.Point, pos string, offX, offY, ax, ay int, tryOpposite bool) image.Point {
	p, _ := theme.KeepInBounds(t, sz, image.Rectangle{Max: win}, pos, offX, offY, ax, ay, tryOpposite)
	return p
}
