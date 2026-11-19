//go:build !js

package desktop

import _ "embed"

// badgeJSON is unread-badge-resources.json (taskbar_badge.go): Slack's
// own file from its Windows app. The browser demo has no taskbar, so its
// build has none (badge_embed_js.go).
//
//go:embed unread-badge-resources.json
var badgeJSON []byte
