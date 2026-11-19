//go:build !js

package ui

import _ "embed"

// Slack's own pictures, copied from its bundles (THIRD_PARTY_NOTICES.md,
// "Slack"). The browser demo draws stand-ins at the same sizes instead
// (slack_assets_js.go).

//go:embed assets/dragdrop/dragdrop_sprites@2x-ca59764.png
var dropSpritesPNG []byte

//go:embed assets/dragdrop/dragdrop_threads_sprites@2x-f3aaf0a.png
var dropThreadSpritesPNG []byte

//go:embed assets/dragdrop/emptystate-blockedfiles@1x-6b3ea84.png
var dropBlockedPNG []byte
