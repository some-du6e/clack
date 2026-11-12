package popups

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Buttons joined into one group: Slack Kit's ButtonGroup (jdUv's
// ButtonGroup context, the v2 Button recipe's inGroup variant) and the
// legacy share dialog's split Forward
// (.p-share_dialog__forward_button--with_options):
//
//	inGroup: _first borderEndRadius none, _last borderStartRadius none,
//	  _middle borderRadius none, _notFirst borderInlineStartWidth 0
//	primary _notLast: borderInlineEnd 1px solid palettes.jade.60
//	  (_dark palettes.jade.30); danger: flamingo.40 (_dark flamingo.50)
//	icon only _last: width 6 / 7 / 9 for small / medium / large
//	legacy: the c-button--primary's end corners square, a :before 1px
//	  #fff6 divider 8px shorter than it at its end (otl-ter while
//	  aria-disabled); the schedule c-icon_button's start corners square

// grpSide is a button's place in a joined pair.
type grpSide uint8

// GrpSide is grpSide for ModalButton.Draw functions outside the package.
type GrpSide = grpSide

const (
	grpNone  grpSide = iota
	grpFirst         // the end (right) corners are square
	grpLast          // the start (left) corners are square
)

// rrectGrp fills r with radius rad, squaring the corners grp joins.
func rrectGrp(gtx layout.Context, r image.Rectangle, rad int, grp grpSide, c color.NRGBA) {
	if grp == grpNone {
		rrect(gtx, r, rad, c)
		return
	}
	rr := clip.RRect{Rect: r, NW: rad, NE: rad, SE: rad, SW: rad}
	switch grp {
	case grpFirst:
		rr.NE, rr.SE = 0, 0
	case grpLast:
		rr.NW, rr.SW = 0, 0
	}
	defer rr.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// v2GroupIconW is the last icon-only button's width in a group (dp):
// spacing 6 / 7 / 9.
func v2GroupIconW(s theme.ButtonV2Size) float32 {
	switch s {
	case theme.ButtonV2Small:
		return 24
	case theme.ButtonV2Large:
		return 36
	}
	return 28
}

// Slack's palettes (slack-kit-gantry-v2-styles, :root /
// .sk-client-theme--dark): jade.60 is 23,143,101 light, jade.30 is
// 16,111,77 dark; flamingo.40 is 250,95,144 light, flamingo.50 is
// 217,76,117 dark.
var (
	jade60Light    = rgb(0x178f65)
	jade30Dark     = rgb(0x106f4d)
	flamingo40     = rgb(0xfa5f90)
	flamingo50Dark = rgb(0xd94c75)
)

// v2GroupDivider is the 1px end border of a primary or danger button
// before another in its group.
func v2GroupDivider(th *theme.Theme, variation string) color.NRGBA {
	dark := th != nil && th.Dark
	if variation == "danger" {
		if dark {
			return flamingo50Dark
		}
		return flamingo40
	}
	if dark {
		return jade30Dark
	}
	return jade60Light
}
