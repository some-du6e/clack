package messages

// The message time's tooltip: Slack's Timestamp (boot-shared, CDN build
// a09b625d90aa) wraps its label in a Tooltip (BaseTooltip: top, delay
// 300, hideFromScreenReader) unless it is full or noTooltip, whose tip
// is TimestampTooltip: getFullTimestamp({includeSeconds: !full}),
// "{date} at {time}" with the seconds (richview.FullTimestampSeconds).
// The header's time, the compact gutter's and a patched preamble's all
// render Timestamp, so each shows it.

import (
	"image"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// timestampTipDelay is Timestamp's Tooltip delay.
const timestampTipDelay = 300 * time.Millisecond

// tsTipper is one Timestamp's tooltip trigger.
type tsTipper struct {
	hover gesture.Hover
	hov   bool // the pointer is over the label
	tip   popups.TipState
}

// layout tracks the pointer over the label at the current offset (size,
// letting it through to the row) and draws the tooltip above it.
func (t *tsTipper) layout(gtx layout.Context, r *row, size image.Point) {
	t.hov = t.hover.Update(gtx.Source)
	hov := t.hov
	a := clip.Rect{Max: size}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	t.hover.Add(gtx.Ops)
	pass.Pop()
	a.Pop()
	t.tip.Delay = timestampTipDelay
	txt := ""
	if hov {
		txt = richview.FullTimestampSeconds(r.m.TS, r.rc.now)
	}
	popups.Tooltip(gtx, r.rc.th, &t.tip, hov && txt != "", size, popups.Above, txt, "")
}
