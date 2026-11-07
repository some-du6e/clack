package slackc

// STUB: replaced by svg.go (branch taut-ui-layout-svg). Delete when it lands.

import (
	"image/color"

	"gioui.org/f32"
	"gioui.org/op"
)

type svgDoc struct{ n *Node }

func compileSVG(n *Node) *svgDoc { return &svgDoc{n: n} }

func (d *svgDoc) intrinsic() (w, h float32, wok, hok bool, vb [4]float32, hasVB bool) {
	if f, ok := attrNum(d.n, "width"); ok {
		w, wok = f, true
	}
	if f, ok := attrNum(d.n, "height"); ok {
		h, hok = f, true
	}
	return
}

func (d *svgDoc) paint(ops *op.Ops, size f32.Point, root svgPaint, sty func(*Node) *svgCSS, mapColor func(color.NRGBA) color.NRGBA) {
}

type svgPaint struct {
	Color       color.NRGBA
	Fill        color.NRGBA
	FillNone    bool
	Stroke      color.NRGBA
	StrokeNone  bool
	StrokeWidth float32
	Opacity     float32
}

type svgCSS struct {
	Color                                color.NRGBA
	ColorSet                             bool
	Fill                                 color.NRGBA
	FillSet, FillNone, FillCurrent       bool
	Stroke                               color.NRGBA
	StrokeSet, StrokeNone, StrokeCurrent bool
	StrokeWidth                          float32
	StrokeWidthSet                       bool
	Opacity                              float32
	OpacitySet                           bool
	Hidden                               bool
}
