package desktop

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// decodeIcons decodes PNG icons to RGBA, skipping any that fail.
func decodeIcons(pngs [][]byte) []*image.NRGBA {
	var out []*image.NRGBA
	for _, b := range pngs {
		m, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			continue
		}
		n, ok := m.(*image.NRGBA)
		if !ok {
			n = image.NewNRGBA(m.Bounds())
			draw.Draw(n, n.Bounds(), m, m.Bounds().Min, draw.Src)
		}
		out = append(out, n)
	}
	return out
}

// netWMIcon packs icons into _NET_WM_ICON's CARDINAL list: for each icon
// width, height, then width*height ARGB pixels (non-premultiplied).
func netWMIcon(icons []*image.NRGBA) []uint64 {
	var out []uint64
	for _, m := range icons {
		b := m.Bounds()
		out = append(out, uint64(b.Dx()), uint64(b.Dy()))
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				i := m.PixOffset(x, y)
				p := m.Pix[i : i+4]
				out = append(out, uint64(p[3])<<24|uint64(p[0])<<16|uint64(p[1])<<8|uint64(p[2]))
			}
		}
	}
	return out
}

// iconSizes is m at the sizes a window manager picks from: fit to largest,
// then each icon size under that down to 16 px, its aspect kept and never
// larger than m.
func iconSizes(m *image.NRGBA, largest int) []*image.NRGBA {
	top := fitIcon(m, largest)
	out := []*image.NRGBA{top}
	side := max(top.Bounds().Dx(), top.Bounds().Dy())
	for _, size := range []int{128, 64, 48, 32, 16} {
		if size < side {
			out = append(out, fitIcon(m, size))
		}
	}
	return out
}

// fitIcon is m scaled down to fit size × size, its aspect kept (m itself
// when it fits).
func fitIcon(m *image.NRGBA, size int) *image.NRGBA {
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return m
	}
	if w >= h {
		w, h = size, max(1, h*size/w)
	} else {
		w, h = max(1, w*size/h), size
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), m, b, xdraw.Src, nil)
	return dst
}
