//go:build !js

package messages

import _ "embed"

// Slack's own pictures, copied from its bundles (THIRD_PARTY_NOTICES.md,
// "Slack"). The browser demo draws stand-ins at the same sizes instead
// (slack_assets_js.go).

// Slack's loading_hash_animation@2x (light and dark), the image
// FlexpaneLoading shows: 80px frames for a 40px box, 30ms each.
var (
	//go:embed assets/loading_hash_animation_2x.gif
	hashGIFLight []byte
	//go:embed assets/loading_hash_animation_2x_dark.gif
	hashGIFDark []byte
)

// Slack's default bot avatar (getMemberProfileImage's isBot fallback,
// ava_0002: a white person silhouette on blue) for a user-less bot with no
// icons. The web client bundles it as ava_0002-72-b939583.png; this is
// that file (byte-identical to a.slack-edge.com/df10d/img/avatars/
// ava_0002-72.png), embedded so it never waits on the network.
//
//go:embed assets/ava_0002-72.png
var defaultBotAvatarPNG []byte
