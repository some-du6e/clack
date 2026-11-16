//go:build stockgio

package inputidle

import "time"

// Stock Gio v0.10.3 keeps no input clock (see router.go): everything
// built against it (.github/scripts/gio-layer-fix.sh) reads the process
// as idle since it started.

var start = time.Now()

func routerSince() time.Duration { return time.Since(start) }
