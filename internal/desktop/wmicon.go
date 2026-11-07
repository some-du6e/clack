package desktop

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
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
