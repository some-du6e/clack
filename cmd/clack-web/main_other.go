//go:build !(js && wasm)

// Command clack-web is the browser demo (main_js.go). It only builds for
// the browser, GOOS=js GOARCH=wasm, which the site workflow does
// (site/README.md, "The web demo"); anywhere else it says so and exits.
// The desktop demo is `clack demo`.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "clack-web is the browser demo: build it with GOOS=js GOARCH=wasm (site/README.md). On the desktop, run: clack demo")
	os.Exit(2)
}
