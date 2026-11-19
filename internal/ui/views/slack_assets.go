//go:build !js

package views

import _ "embed"

// Slack's own pictures, copied from its bundles (THIRD_PARTY_NOTICES.md,
// "Slack"). The browser demo draws stand-ins at the same sizes instead
// (slack_assets_js.go).

//go:embed assets/empty_state_folder-7c5c7a1@1x.png
var emptyFolderPNG1x []byte

//go:embed assets/empty_state_folder-7c5c7a1@2x.png
var emptyFolderPNG2x []byte
