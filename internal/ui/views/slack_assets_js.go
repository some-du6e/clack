//go:build js

package views

// The browser demo's stand-ins for Slack's pictures (slack_assets.go),
// drawn at the originals' sizes by internal/ui/standin: none of Slack's
// artwork ships on getclack.club.

import (
	"github.com/some-du6e/clack/internal/ui/standin"
)

var (
	emptyFolderPNG1x = standin.Picture(182, 146, standin.Folder)
	emptyFolderPNG2x = standin.Picture(364, 292, standin.Folder)
)
