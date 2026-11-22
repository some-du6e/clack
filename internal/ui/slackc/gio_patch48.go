//go:build !stockgio

package slackc

import (
	"time"

	"gioui.org/gesture"
)

// Gio patch 48 (third_party/gioui.org/CLACK_PATCHES.md): the platform's
// double-click time, Chromium's on it (the OS setting on Windows and
// macOS), which a click's count follows here as Slack's event detail
// does. gio_patch48_stock.go stands in on stock Gio
// (.github/scripts/gio-layer-fix.sh builds this package against it).

func doubleClickTime() time.Duration { return gesture.DoubleClickTime() }
