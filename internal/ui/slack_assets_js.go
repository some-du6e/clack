//go:build js

package ui

// The browser demo's stand-ins for Slack's pictures (slack_assets.go),
// drawn at the originals' sizes by internal/ui/standin: none of Slack's
// artwork ships on getclack.club.

import (
	"image"

	"github.com/some-du6e/clack/internal/ui/standin"
)

var (
	// the drag-and-drop cards, cut where drop_overlay.go's dropSprites cuts
	dropSpritesPNG = standin.Cards(200, 716, []standin.Cell{
		{R: image.Rect(0, 0, 200, 178)}, {R: image.Rect(0, 192, 200, 370), Night: true},
		{R: image.Rect(0, 378, 184, 532)}, {R: image.Rect(0, 546, 184, 700)},
	})
	dropThreadSpritesPNG = standin.Cards(220, 406, []standin.Cell{
		{R: image.Rect(0, 0, 220, 46), Text: true},
		{R: image.Rect(0, 100, 220, 254)}, {R: image.Rect(0, 250, 220, 404), Night: true},
	})
	dropBlockedPNG = standin.Picture(130, 84, standin.Blocked)
)
