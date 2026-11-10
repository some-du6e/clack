# rnnoise-go in Clack

This is [VoiceBlender/rnnoise-go](https://github.com/VoiceBlender/rnnoise-go) v0.3.0, a pure-Go port of Xiph's RNNoise, from the Go module proxy (`proxy.golang.org`, `github.com/!voice!blender/rnnoise-go/@v/v0.3.0.zip`). Clack uses it for the huddle microphone's noise suppression (`internal/huddle/apm.go`).

Licence: BSD-3-Clause (`LICENSE`, upstream's notice in `COPYING.rnnoise`, provenance in `NOTICE`). The trained weights in `model/weights.bin` are upstream's, under the same terms; see `model/LICENSE.weights`.

What changed:

- `go.mod` declares `go 1.24` (upstream declares 1.26, and Clack builds with Go 1.24). Nothing in the package needs a newer release, and `go vet` passes on 1.24.9.
- Left out: the tests, testdata, benchmarks and the `ctest`/`tools` modules (`go.work`). The library sources, the assembly kernels and `model/` are byte-for-byte upstream.

Drop the copy and the `replace` in Clack's `go.mod` once Clack's Go version reaches what upstream declares.
