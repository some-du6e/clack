//go:build js

package jsrt

// assetPack: none in the browser demo, which leaves plugins' JavaScript
// runtime out (engine_js.go): no QuickJS, no React. Nothing reads it there,
// since New fails before the runtime would unpack it.
var assetPack []byte
