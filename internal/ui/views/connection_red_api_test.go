package views

import (
	"image"
	"time"

	"gioui.org/layout"
)

// Red-tree shims: the connection states' APIs don't exist on the old code.

func setStillLoadingAfter(d time.Duration) (restore func())   { return func() {} }
func setViewsTrouble(f func(Host) time.Time) (restore func()) { return func() {} }
func setViewsOnline(f func(Host) bool) (restore func())       { return func() {} }
func threadsBanner(v *Threads) (string, image.Rectangle)      { return "", image.Rectangle{} }
func unreadsBanner(v *Unreads) (string, image.Rectangle)      { return "", image.Rectangle{} }
func threadsPagingRow(v *Threads, gtx layout.Context)         {}
