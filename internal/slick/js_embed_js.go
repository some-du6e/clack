//go:build js

package slick

import "embed"

// jsFiles is empty in the browser demo, which leaves plugins and their
// JavaScript runtime out.
var jsFiles embed.FS
