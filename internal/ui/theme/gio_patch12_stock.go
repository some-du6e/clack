//go:build stockgio

package theme

import "gioui.org/text"

// Stock Gio v0.10.3 has no letter spacing (see gio_patch12.go).

func paramsPxPerSp(*text.Parameters, float32) {}

// ParamsUntracked is a no-op on stock Gio.
func ParamsUntracked(*text.Parameters) {}

func shaperSetTracking(*text.Shaper, func(f fontT, size float32) float32) {}

func shaperSetTrackingScale(*text.Shaper, float32) {}
