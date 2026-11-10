package bgblur

import (
	"image"
	"image/color"
	"math"
)

// SyntheticPerson draws a head-and-shoulders figure in front of a room (a
// wall, a window, a bookshelf), w×h, for tests and screenshots. person
// is the figure's silhouette, w×h, true inside.
func SyntheticPerson(w, h int) (img *image.RGBA, person []bool) {
	img = image.NewRGBA(image.Rect(0, 0, w, h))
	person = make([]bool, w*h)
	H := float64(h)
	cx := float64(w) * 0.5
	ell := func(x, y, ex, ey, rx, ry float64) float64 { // <1 inside
		dx, dy := (x-ex)/rx, (y-ey)/ry
		return dx*dx + dy*dy
	}
	mix := func(a, b [3]float64, t float64) [3]float64 {
		return [3]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
	}
	scale := func(a [3]float64, s float64) [3]float64 { return [3]float64{a[0] * s, a[1] * s, a[2] * s} }
	books := [][3]float64{{150, 40, 40}, {40, 90, 150}, {200, 170, 60}, {60, 120, 70}, {120, 60, 130}, {210, 110, 50}}
	skin := [3]float64{226, 176, 146}
	hair := [3]float64{58, 38, 28}
	shirt := [3]float64{40, 92, 120}
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			x, y := float64(px)+0.5, float64(py)+0.5
			// the room
			c := mix([3]float64{214, 204, 186}, [3]float64{168, 158, 142}, y/H)
			if x < float64(w)*0.24 && x > float64(w)*0.04 && y > H*0.12 && y < H*0.62 { // window
				c = mix([3]float64{170, 205, 235}, [3]float64{225, 238, 248}, (y-H*0.12)/(H*0.5))
				if math.Abs(x-float64(w)*0.14) < H*0.012 || math.Abs(y-H*0.37) < H*0.012 {
					c = [3]float64{245, 245, 240}
				}
			}
			if x > float64(w)*0.74 && x < float64(w)*0.97 && y > H*0.08 { // shelf
				c = [3]float64{92, 62, 40}
				for s := 0; s < 4; s++ {
					top := H * (0.12 + 0.24*float64(s))
					if y > top && y < top+H*0.19 {
						k := int((x - float64(w)*0.75) / (H * 0.035))
						if (x-float64(w)*0.75)/(H*0.035)-float64(k) > 0.12 && k >= 0 {
							c = scale(books[(k*7+s*3)%len(books)], 0.85+0.15*math.Sin(float64(k)))
							if y < top+H*(0.02+0.03*float64((k*5+s)%3)) {
								c = [3]float64{92, 62, 40}
							}
						}
					}
				}
			}
			in := false
			// torso and shoulders
			if d := ell(x, y, cx, H*1.08, H*0.46, H*0.40); d < 1 {
				in = true
				c = scale(shirt, 1.1-0.35*d)
				if math.Abs(x-cx) < H*(0.09-(y-H*0.70)*0.4) && y > H*0.68 { // collar
					c = scale(skin, 0.8)
				}
			}
			// neck
			if math.Abs(x-cx) < H*0.075 && y > H*0.52 && y < H*0.74 {
				in = true
				c = scale(skin, 0.78+0.1*(1-math.Abs(x-cx)/(H*0.075)))
			}
			// ears
			for _, s := range []float64{-1, 1} {
				if ell(x, y, cx+s*H*0.148, H*0.41, H*0.025, H*0.05) < 1 {
					in = true
					c = scale(skin, 0.85)
				}
			}
			// head
			if d := ell(x, y, cx, H*0.40, H*0.15, H*0.20); d < 1 {
				in = true
				c = scale(skin, 1.04-0.3*d)
				for _, s := range []float64{-1, 1} {
					ex := cx + s*H*0.058
					if ell(x, y, ex, H*0.395, H*0.028, H*0.013) < 1 {
						c = [3]float64{240, 240, 235}
						if ell(x, y, ex, H*0.395, H*0.012, H*0.012) < 1 {
							c = [3]float64{50, 35, 30}
						}
					}
					if ell(x, y, ex, H*0.36, H*0.035, H*0.008) < 1 { // brows
						c = hair
					}
				}
				if ell(x, y, cx, H*0.505, H*0.045, H*0.013) < 1 { // mouth
					c = [3]float64{170, 80, 80}
				}
				if math.Abs(x-cx-H*0.01) < H*0.006 && y > H*0.40 && y < H*0.46 { // nose shadow
					c = scale(skin, 0.75)
				}
			}
			// hair
			if d := ell(x, y, cx, H*0.34, H*0.165, H*0.15); d < 1 && (y < H*0.31 || (math.Abs(x-cx) > H*0.13 && y < H*0.40)) {
				in = true
				c = scale(hair, 1.2-0.4*d)
			}
			person[py*w+px] = in
			img.SetRGBA(px, py, color.RGBA{uint8(min(255, c[0])), uint8(min(255, c[1])), uint8(min(255, c[2])), 255})
		}
	}
	return img, person
}

// FrameFromRGBA converts img (even size) to an NV12 frame, BT.601 video
// range, the camera's layout.
func FrameFromRGBA(img *image.RGBA) *Frame {
	w, h := img.Rect.Dx()&^1, img.Rect.Dy()&^1
	f := &Frame{W: w, H: h, Y: make([]byte, w*h), UV: make([]byte, w*h/2), YStride: w, UVStride: w}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := img.Pix[y*img.Stride+4*x:]
			r, g, b := float64(p[0]), float64(p[1]), float64(p[2])
			f.Y[y*w+x] = byte(16 + (65.481*r+128.553*g+24.966*b)/255 + 0.5)
		}
	}
	for y := 0; y < h/2; y++ {
		for x := 0; x < w/2; x++ {
			var r, g, b float64
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					p := img.Pix[(2*y+dy)*img.Stride+4*(2*x+dx):]
					r, g, b = r+float64(p[0])/4, g+float64(p[1])/4, b+float64(p[2])/4
				}
			}
			f.UV[y*w+2*x] = byte(128 + (-37.797*r-74.203*g+112*b)/255 + 0.5)
			f.UV[y*w+2*x+1] = byte(128 + (112*r-93.786*g-18.214*b)/255 + 0.5)
		}
	}
	return f
}

// RGBA converts f back to RGB (BT.601), for pictures.
func (f *Frame) RGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			yy := float32(f.Y[y*f.YStride+x])
			u := float32(f.UV[(y/2)*f.UVStride+x&^1])
			v := float32(f.UV[(y/2)*f.UVStride+x|1])
			r, g, b := toRGB(yy, u, v, f.FullRange)
			img.SetRGBA(x, y, color.RGBA{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5), 255})
		}
	}
	return img
}
