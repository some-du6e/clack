package richview

import (
	"image"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// agent lays out a plan / task_card / context_actions block: a slot the
// element host draws (internal/ui/blockkit, agent.go), or without one
// the block's text fallback in Slack's slim plan style (13px, tertiary).
func (l *lay) agent(b *richtext.Block, x, y, w int) int {
	if el := l.opts.Elements; el != nil && len(b.Buttons) > 0 {
		sz, _ := el.ElementsSize(b.Buttons[:1], w, false, l.metric())
		if sz.Y <= 0 {
			return 0
		}
		l.addSlot(b.Buttons[:1], image.Rect(x, y, x+max(sz.X, 1), y+sz.Y), false)
		return sz.Y
	}
	if len(b.Spans) == 0 {
		return 0
	}
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col = l.sp(13), l.dp(20), l.st.Palette.Text2
	fs.emoji = l.dp(16)
	l.addSpans(b.Spans, &fs)
	h, _ := l.flow(x, y, w, &fs)
	return h
}
