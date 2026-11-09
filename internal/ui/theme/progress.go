package theme

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
)

// ProgressBar draws a thin progress bar in track (the composer's upload
// bar, attProgressFrac/attProgress in internal/ui/composer: a rounded
// track in fg at 15%, filled with bar). frac is 0..1; a negative frac is
// indeterminate: a 40% segment sweeping across every 1.4s, which asks
// for the next frame itself (about 30 a second).
func ProgressBar(gtx layout.Context, track image.Rectangle, fg, bar color.NRGBA, frac float32) {
	if track.Dx() <= 0 || track.Dy() <= 0 {
		return
	}
	rad := track.Dy() / 2
	tc := fg
	tc.A = 0x26
	RRect(gtx, track, rad, tc)
	if frac < 0 {
		const period = 1400 // ms
		t := float32(gtx.Now.UnixMilli()%period) / period
		seg := track.Dx() * 2 / 5
		x := track.Min.X - seg + int(t*float32(track.Dx()+seg))
		r := image.Rect(max(x, track.Min.X), track.Min.Y, min(x+seg, track.Max.X), track.Max.Y)
		if r.Dx() > 0 {
			RRect(gtx, r, rad, bar)
		}
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
		return
	}
	frac = min(max(frac, 0), 1)
	if w := int(frac * float32(track.Dx())); w > 0 {
		RRect(gtx, image.Rect(track.Min.X, track.Min.Y, track.Min.X+w, track.Max.Y), rad, bar)
	}
}
