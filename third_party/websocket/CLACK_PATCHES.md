# Clack's copy of coder/websocket v1.8.15

This directory is `github.com/coder/websocket@v1.8.15` from the Go module
cache: its non-test files, byte for byte (LICENSE.txt and go.mod included),
without its browser backend (`ws_js.go`, `netconn_js.go`, `internal/wsjs`),
its tests and test helpers, `wsjson`, and its CI and repository files. The
root `go.mod` points at it with
`replace github.com/coder/websocket => ./third_party/websocket`.

Why: under `GOOS=js` upstream swaps in a backend on the browser's own
`WebSocket`, which ignores `DialOptions.HTTPClient` and has no `Accept`.
The browser demo (`cmd/clack-web`) runs the fake Slack server and Clack in
one page, over in-memory connections (`internal/memnet`), so it needs the
ordinary implementation there too: the handshake goes through the client's
own `http.Client`, and the fake's `Accept` hijacks its in-memory connection.
On every other platform the code is upstream's, unchanged.

Changed files, all only in their build constraint:

- `accept.go`, `close.go`, `compress.go`, `conn.go`, `dial.go`, `doc.go`,
  `frame.go`, `hijack.go`, `netconn_notjs.go`, `read.go`, `write.go`: the
  `//go:build !js` line and the blank line after it are gone.
- `mask_go.go`: `//go:build !amd64 && !arm64 && !js` is now
  `//go:build !amd64 && !arm64`, so wasm uses the Go mask.

To see the whole diff against the release:

    go mod download github.com/coder/websocket@v1.8.15
    diff -ru "$(go env GOMODCACHE)/github.com/coder/websocket@v1.8.15" third_party/websocket

When the demo no longer needs it, drop the replace and delete this
directory.
