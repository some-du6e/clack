//go:build !js

package taut

import "embed"

// jsFiles is Taut's JavaScript (runtime.go). The browser demo
// leaves plugins out, so its build has none (js_embed_js.go).
//
//go:embed js/*.js
var jsFiles embed.FS
