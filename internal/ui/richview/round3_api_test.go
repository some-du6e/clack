package richview

import "image"

// Round 3's image fields and helpers, reached only through this file so a
// red-evidence commit can stub them.

// imgNoUp is im's no-upscale factor (0: may grow).
func imgNoUp(im imgPrim) float32 { return im.noUp }

// imgOnFail is what im draws once its own image failed ("" none).
func imgOnFail(im imgPrim) string { return im.onFail }

// noUpRectOf is noUpRect.
func noUpRectOf(r image.Rectangle, sz image.Point, f float32) (image.Rectangle, bool) {
	return noUpRect(r, sz, f)
}
