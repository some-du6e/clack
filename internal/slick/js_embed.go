//go:build !js

package slick

import "embed"

// jsFiles is Slick's JavaScript (slick.go). The browser demo
// leaves plugins out, so its build has none (js_embed_js.go).
//
//go:embed js/*.js
var jsFiles embed.FS
