//go:build !stockgio

package theme

import "gioui.org/text"

// Gio patch 12 (letter spacing) entry points. The stockgio build tag swaps
// in no-ops (gio_patch12_stock.go) so the drawLayers regression check
// (.github/scripts/gio-layer-fix.sh) can still build theme's dependents
// against stock Gio v0.10.3.

func paramsPxPerSp(p *text.Parameters, pxPerSp float32) { p.PxPerSp = pxPerSp }

// ParamsUntracked opts p out of the shaper's tracking (slackc spaces its
// own words).
func ParamsUntracked(p *text.Parameters) { p.Untracked = true }

func shaperSetTracking(sh *text.Shaper, fn func(f fontT, size float32) float32) {
	sh.SetTracking(fn)
}

func shaperSetTrackingScale(sh *text.Shaper, pxPerSp float32) { sh.SetTrackingScale(pxPerSp) }
