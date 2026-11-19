//go:build js

package pluginbench

// embedded: none in the browser demo, which has no plugins; Load gives
// empty data, as it does when bench.json is absent.
var embedded []byte
