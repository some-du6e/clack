// SPDX-License-Identifier: Unlicense OR MIT

package widget

import (
	"gioui.org/io/input"
	"gioui.org/layout"
)

// Clack patch 54 (CLACK_PATCHES.md): the caret's width is Blink's
// LocalFrameView::BarCaretWidth, max(1, the device scale), and the device
// scale is the screen's alone: page zoom (Slack's View > Zoom) scales
// layout, never the caret. An app that zooms by scaling its Metric
// records the window's unzoomed scale with SetDeviceScale.

type deviceScaleKey struct{}

// SetDeviceScale records pxPerDp, the window's device pixels per dp before
// any zoom, for every Source of src's router (one per window; Clack patch
// 15's values). Call it each frame with the FrameEvent's Metric.PxPerDp.
func SetDeviceScale(src input.Source, pxPerDp float32) {
	src.SetValue(deviceScaleKey{}, pxPerDp)
}

// DeviceScale is the device pixels per dp SetDeviceScale recorded for
// gtx's window, or gtx's own PxPerDp when none was.
func DeviceScale(gtx layout.Context) float32 {
	if v, ok := gtx.Source.Value(deviceScaleKey{}).(float32); ok && v > 0 {
		return v
	}
	return gtx.Metric.PxPerDp
}
