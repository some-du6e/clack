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
	"image/color"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// timestampTipDelay is Timestamp's Tooltip delay.
const timestampTipDelay = popups.TimeLinkTipDelay

// tsTipper is one Timestamp: MessageTimestamp's link to the message
// (popups.TimeLink: the hand cursor, the underline under the pointer, the
// full date's tooltip, handleTimestampClick on a click; a right-click is
// the link's menu, context.go).
type tsTipper struct {
	link popups.TimeLink
}

// hovered reports whether the pointer is on the time.
func (t *tsTipper) hovered() bool { return t.link.Hovered() }

// begin starts the row's frame (popups.TimeLink.Begin).
func (t *tsTipper) begin() { t.link.Begin() }

// skip is a frame the time isn't drawn in: its hover and tooltip end.
func (t *tsTipper) skip() { t.link.Skip() }

// layout makes the label laid out at the current offset (d, in col) the
// link to r's message.
func (t *tsTipper) layout(gtx layout.Context, r *row, d layout.Dimensions, col color.NRGBA) {
	tip := func() string { return richview.FullTimestampSeconds(r.m.TS, r.rc.now) }
	if c := t.link.Layout(gtx, r.rc.th, d.Size, d.Size.Y-d.Baseline, col, tip); c.Clicked {
		m := &r.src
		popups.OpenMessageTime(r.rc.h, m.Channel, m.TS, m.ThreadTS, c.Cmd)
	}
}
