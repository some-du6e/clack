package messages

// The message time's tooltip: Slack's Timestamp (boot-shared, CDN build
// a09b625d90aa) wraps its label in a Tooltip (BaseTooltip: top, delay
// 300, hideFromScreenReader) unless it is full or noTooltip, whose tip
// is TimestampTooltip: getFullTimestamp({includeSeconds: !full}),
// "{date} at {time}" with the seconds (richview.FullTimestampSeconds).
// The header's time, the compact gutter's and a patched preamble's all
// render Timestamp, so each shows it.
//
// The hover is the trigger's own pointer handler, which any Leave or
// Cancel ends whatever pointer it names, as statusArea's: when a time
// isn't laid out (the gutter's once its row isn't hovered, a row off
// screen) Gio drops the Leave it never read and makes the handler again,
// whose first event is a Cancel of pointer 0, and on Windows the mouse is
// pointer 1. A time that isn't drawn lets its hover go (skip).

import (
	"image"
	"time"

	"gioui.org/io/event"
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
	tag int
	hov bool // the pointer is over the label
	tip popups.TipState
}

// update reads the trigger's pointer events.
func (t *tsTipper) update(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			t.hov = e.Kind == pointer.Enter
		}
	}
}

// skip is a frame the time isn't drawn in: its hover and tooltip end.
func (t *tsTipper) skip() {
	t.hov = false
	t.tip.Reset()
}

// layout tracks the pointer over the label at the current offset (size,
// letting it through to the row) and draws the tooltip above it.
func (t *tsTipper) layout(gtx layout.Context, r *row, size image.Point) {
	t.update(gtx)
	a := clip.Rect{Max: size}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.tag)
	pass.Pop()
	a.Pop()
	t.tip.Delay = timestampTipDelay
	txt := ""
	if t.hov {
		txt = richview.FullTimestampSeconds(r.m.TS, r.rc.now)
	}
	popups.Tooltip(gtx, r.rc.th, &t.tip, t.hov && txt != "", size, popups.Above, txt, "")
}
