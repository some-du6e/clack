//go:build !js

package jsrt

import _ "embed"

// assetPack is assets.pack (pack.go). The browser demo leaves plugins'
// JavaScript runtime out, so its build has none (pack_embed_js.go).
//
//go:embed assets.pack
var assetPack []byte
