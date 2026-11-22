//go:build stockgio

package slackc

import (
	"time"

	"gioui.org/io/pointer"
)

// Stock Gio v0.10.3 has no platform double-click time (see
// gio_patch48.go): Chromium's default, 500 ms.

func doubleClickTime() time.Duration { return 500 * time.Millisecond }

// clicksOf: no window count on stock Gio.
func clicksOf(pointer.Event) int { return 0 }
