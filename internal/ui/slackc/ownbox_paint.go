package slackc

import (
	"image"
	"image/color"

	"gioui.org/layout"
)

// BoxLayers selects what Host.PaintBox paints.
type BoxLayers uint8

const (
	// BoxShadows is the box's outer box-shadows, painted outside its
	// border box: before the component's own background.
	BoxShadows BoxLayers = 1 << iota
	// BoxImages is its background-image layers (gradients, url()
	// images), over the background colour the component painted.
	BoxImages
	// BoxInsetShadows is its inset box-shadows, inside its padding box.
	BoxInsetShadows
	// BoxBorder is the border CSS gives it, at the widths the component
	// laid out (bd), in its rule's styles and colours.
	BoxBorder
)

// BoxLayersSet reports which of BoxShadows, BoxImages and BoxInsetShadows
// the component's box has (a rule set them), so a native skips the rest.
func (h *Host) BoxLayersSet() BoxLayers {
	if h.view == nil || h.Node == nil {
		return 0
	}
	b := h.view.boxOf(h.Node)
	if b == nil {
		return 0
	}
	var l BoxLayers
	for i := range b.st.BoxShadow {
		if sh := &b.st.BoxShadow[i]; sh.Color.A > 0 {
			if sh.Inset {
				l |= BoxInsetShadows
			} else {
				l |= BoxShadows
			}
		}
	}
	if bg := &b.st.Background; bg.Image != "" || bg.Gradient != nil && len(bg.Gradient.Stops) > 0 {
		l |= BoxImages
	}
	return l
}

// PaintBox paints, for a component that draws its own box
// (HostElement.OwnBox: kit's Button), the CSS layers of that box it
// doesn't draw itself, the way the engine paints any other box's: a rule's
// box-shadow, background-image or a border of its own widths. The engine
// skips them for such a box, whose native paints its background, padding
// and border; the native calls this in CSS paint order (BoxShadows, its
// background colour, BoxImages, BoxInsetShadows, BoxBorder, then its
// content), with the box drawn at the origin, size px, radius px corners
// unless a rule set border-radius, border widths bd px (top, right,
// bottom, left) and under, the colour it painted beneath (its background;
// translucent layers are composited over it). border, when not
// transparent, paints BoxBorder in that colour on every side instead of
// the rule's (currentColor, or the component's own kind of border).
func (h *Host) PaintBox(gtx layout.Context, size image.Point, radius int, bd [4]int, under, border color.NRGBA, layers BoxLayers) {
	if h.view == nil || h.Node == nil || layers == 0 {
		return
	}
	b := h.view.boxOf(h.Node)
	if b == nil {
		return
	}
	st := &b.st
	if layers&BoxBorder != 0 && bd == [4]int{} {
		layers &^= BoxBorder
	}
	if layers&(BoxShadows|BoxInsetShadows) != 0 && len(st.BoxShadow) == 0 {
		layers &^= BoxShadows | BoxInsetShadows
	}
	if layers&BoxImages != 0 && st.Background.Image == "" && (st.Background.Gradient == nil || len(st.Background.Gradient.Stops) == 0) {
		layers &^= BoxImages
	}
	if layers == 0 {
		return
	}
	p := painter{v: h.view, ops: gtx.Ops, clip: noClip}
	w, ht := float32(size.X), float32(size.Y)
	r := rectF{0, 0, w, ht}
	var rad [4]float32
	if h.Has("border-radius") {
		rad = h.view.radii(b, w, ht)
	} else {
		f := float32(max(radius, 0))
		rad = [4]float32{f, f, f, f}
	}
	// the widths the component laid out, for the padding box and the
	// border (the engine left the box none: edges)
	saveBd := b.bd
	b.bd = [4]float32{float32(bd[0]), float32(bd[1]), float32(bd[2]), float32(bd[3])}
	defer func() { b.bd = saveBd }()
	if layers&BoxShadows != 0 {
		p.shadows(b, r, rad, false)
	}
	p.coverWith(under)
	if layers&BoxImages != 0 {
		bg := &st.Background
		if bg.Gradient != nil && len(bg.Gradient.Stops) > 0 {
			p.gradient(bg.Gradient, r, rad)
		}
		if bg.Image != "" {
			p.bgImage(b, r, rad)
		}
	}
	if layers&BoxInsetShadows != 0 {
		p.shadows(b, r, rad, true)
	}
	if layers&BoxBorder != 0 {
		if border.A > 0 {
			save := st.BorderColor
			st.BorderColor = [4]color.NRGBA{border, border, border, border}
			defer func() { st.BorderColor = save }()
		}
		p.border(b, r, rad)
	}
}
