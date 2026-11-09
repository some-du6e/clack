package popups

import (
	"image"

	"gioui.org/layout"
	"gioui.org/widget"
)

// NewOptionList opens a long select's listbox (Slack's Select dropdown:
// Preferences > Language & region's time zones) under anchor, drawn like
// the time list: labels in rows, sel highlighted and scrolled to,
// arrows/Enter, Escape or a click outside closes it. onPick gets the
// index. Show it with h.ShowOverlay.
func NewOptionList(h Host, anchor image.Rectangle, labels []string, sel int, onPick func(i int)) Overlay {
	t := &dtTimeList{h: h, anchor: anchor, labels: labels, sel: sel, cur: -1, onPick: onPick}
	t.slots = make([]int, len(labels))
	for i := range t.slots {
		t.slots[i] = i
	}
	t.list.Axis = layout.Vertical
	t.clicks = make([]widget.Clickable, len(labels))
	if sel > 3 {
		t.list.Position.First = sel - 3
	}
	return t
}
