//go:build !js

package pluginbench

import _ "embed"

// embedded is bench.json, the benchmark's last results (Load). The browser
// demo has no plugins, so its build has none (bench_embed_js.go).
//
//go:embed bench.json
var embedded []byte
