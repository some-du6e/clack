//go:build !stockgio

package slackc

import (
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
)

// Gio patch 48 (third_party/gioui.org/CLACK_PATCHES.md): the platform's
// double-click time, and the window's click count, Chromium's on each
// platform (the router stamps every press and release with it,
// pointer.Event.Clicks), which a click's detail follows here as Slack's
// event's does. gio_patch48_stock.go stands in on stock Gio
// (.github/scripts/gio-layer-fix.sh builds this package against it).

func doubleClickTime() time.Duration { return gesture.DoubleClickTime() }

// clicksOf is the window's count of the click that press or release e is
// part of (0 without one).
func clicksOf(e pointer.Event) int { return e.Clicks }
