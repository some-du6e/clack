//go:build js

package popups

// The browser demo's stand-ins for Slack's pictures (slack_assets.go),
// drawn at the originals' sizes by internal/ui/standin: none of Slack's
// artwork ships on getclack.club.

import (
	"github.com/some-du6e/clack/internal/ui/standin"
)

var (
	ugSectionPNG      = standin.Picture(88, 125, standin.Group)
	srcEmptyPNG1x     = standin.Picture(159, 140, standin.Search)
	srcEmptyPNG2x     = standin.Picture(318, 280, standin.Search)
	defaultAppIconPNG = standin.AppIcon(256) // app_icon.go says where Slack's comes from
	adGlassPNG        = standin.Picture(512, 348, standin.Glass)
)
