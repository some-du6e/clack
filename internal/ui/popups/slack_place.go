package popups

import "image"

// Slack's Popover placement (client-boot L1PI, CDN build 4d0c78085232):
// getInitialPosition puts the card at one of twelve positions around the
// target with offsetX / offsetY; keepInBounds then keeps it allowanceX
// (16) / allowanceY (10) inside the window, shifting it rather than
// flipping, and with tryOppositePosition, when the top had to move, takes
// the vertically opposite position (offsetY negated) if that fits.

// slackBox is getInitialPosition's result.
type slackBox struct{ top, bottom, left, right int }

// slackOpposite is L1PI's y table.
var slackOpposite = map[string]string{
	"top": "bottom", "top-left": "bottom-left", "top-right": "bottom-right",
	"left": "left", "left-bottom": "left-top", "left-top": "left-bottom",
	"right": "right", "right-bottom": "right-top", "right-top": "right-bottom",
	"bottom": "top", "bottom-left": "top-left", "bottom-right": "top-right",
}

// slackInitial is getInitialPosition for a target t and a card sz (its
// "right-bottom" bottom, i.bottom + height, is Slack's as written).
func slackInitial(pos string, t image.Rectangle, sz image.Point, n, a int) slackBox {
	w, h := sz.X, sz.Y
	switch pos {
	case "top":
		return slackBox{t.Min.Y - h + a, t.Min.Y + a, t.Min.X + (t.Dx()-w)/2 + n, t.Max.X - (t.Dx()-w)/2 - n}
	case "top-left":
		return slackBox{t.Min.Y - h + a, t.Min.Y + a, t.Min.X + n, t.Min.X + w + n}
	case "top-right":
		return slackBox{t.Min.Y - h + a, t.Min.Y + a, t.Max.X - w + n, t.Max.X + n}
	case "left":
		return slackBox{t.Min.Y + (t.Dy()-h)/2 + a, t.Max.Y - (t.Dy()-h)/2 - a, t.Min.X - w + n, t.Min.X + n}
	case "left-bottom":
		return slackBox{t.Min.Y + a, t.Min.Y + h + a, t.Min.X - w + n, t.Min.X + n}
	case "left-top":
		return slackBox{t.Max.Y - h + a, t.Max.Y + a, t.Min.X - w + n, t.Min.X + n}
	case "right":
		return slackBox{t.Min.Y + (t.Dy()-h)/2 + a, t.Max.Y - (t.Dy()-h)/2 - a, t.Max.X + n, t.Max.X + w + n}
	case "right-bottom":
		return slackBox{t.Min.Y + a, t.Max.Y + h + a, t.Max.X + n, t.Max.X + w + n}
	case "right-top":
		return slackBox{t.Max.Y - h + a, t.Max.Y + a, t.Max.X + n, t.Max.X + w + n}
	case "bottom":
		return slackBox{t.Max.Y + a, t.Max.Y + h + a, t.Min.X + (t.Dx()-w)/2 + n, t.Max.X - (t.Dx()-w)/2 - n}
	case "bottom-left":
		return slackBox{t.Max.Y + a, t.Max.Y + h + a, t.Min.X + n, t.Min.X + w + n}
	case "bottom-right":
		return slackBox{t.Max.Y + a, t.Max.Y + h + a, t.Max.X - w + n, t.Max.X + n}
	}
	return slackBox{t.Min.Y, t.Min.Y + h, t.Min.X, t.Min.X + w}
}

// slackPlace is keepInBounds: the card's top-left for target t in a win
// window, allowances ax / ay.
func slackPlace(t image.Rectangle, sz, win image.Point, pos string, offX, offY, ax, ay int, tryOpposite bool) image.Point {
	s := slackInitial(pos, t, sz, offX, offY)
	left, top := s.left, s.top
	if s.left < ax {
		left = ax
	} else if s.right > win.X-ax {
		left = max(ax, win.X-ax-sz.X)
	}
	if s.top < ay {
		top = ay
	} else if s.bottom > win.Y-ay {
		top = max(ay, win.Y-ay-sz.Y)
	}
	if top != s.top && tryOpposite {
		o := slackInitial(slackOpposite[pos], t, sz, offX, -offY)
		if !(o.top < ay || o.bottom > win.Y-ay) {
			return image.Pt(left, o.top)
		}
	}
	return image.Pt(left, top)
}
