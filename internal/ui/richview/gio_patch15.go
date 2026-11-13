//go:build !stockgio

package richview

import (
	"gioui.org/gesture"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
)

// Gio patch 15 (third_party/gioui.org/CLACK_PATCHES.md): middle-click
// autoscroll. gio_patch14_stock.go stands in on stock Gio
// (.github/scripts/gio-layer-fix.sh builds this package against it).

func autoscrollPress(e pointer.Event) bool { return gesture.AutoscrollPress(e) }

func autoscrollVeto(q input.Source, e pointer.Event) { gesture.AutoscrollVeto(q, e) }
