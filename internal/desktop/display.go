package desktop

import "math"

// winRect is a RECT: a display's work area, a window's bounds.
type winRect struct{ Left, Top, Right, Bottom int32 }

// fitCentered is rH then rG (main.bundle.cjs 4.53.276 win32 @806941,
// @807329): w×h scaled down, aspect kept, to fit work (height first, then
// width), rounded, and centred in work with its top-left kept inside.
func fitCentered(w, h float64, work winRect) (x, y, fw, fh int32) {
	ww, wh := float64(work.Right-work.Left), float64(work.Bottom-work.Top)
	ratio := w / h
	if h > wh {
		w, h = wh*ratio, wh
	}
	if w > ww {
		w, h = ww, ww/ratio
	}
	fw, fh = jsRound(w), jsRound(h)
	cx, cy := float64(work.Left)+ww/2, float64(work.Top)+wh/2
	x = max(jsRound(cx-float64(fw)/2), work.Left)
	y = max(jsRound(cy-float64(fh)/2), work.Top)
	return x, y, fw, fh
}

// jsRound is JavaScript's Math.round: halves go up, -2.5 to -2.
func jsRound(v float64) int32 { return int32(math.Floor(v + 0.5)) }
