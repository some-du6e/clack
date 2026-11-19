//go:build js

package messages

// The browser demo's stand-ins for Slack's pictures (slack_assets.go),
// drawn at the originals' sizes by internal/ui/standin: none of Slack's
// artwork ships on getclack.club.

import (
	"github.com/some-du6e/clack/internal/ui/standin"
)

var (
	hashGIFLight        = standin.Spinner(false)
	hashGIFDark         = standin.Spinner(true)
	defaultBotAvatarPNG = standin.Avatar(72)
)
