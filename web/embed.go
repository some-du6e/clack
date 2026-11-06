// Package web embeds the browser UI.
package web

import "embed"

//go:embed index.html css js img fonts
var FS embed.FS
