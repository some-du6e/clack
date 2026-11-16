//go:build !stockgio

package inputidle

import (
	"time"

	"gioui.org/io/input"
)

// routerSince is Gio patch 32's clock (third_party/gioui.org/io/input/clack_inputidle.go).
func routerSince() time.Duration { return input.SinceInput() }
