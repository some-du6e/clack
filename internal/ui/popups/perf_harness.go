//go:build perfharness

package popups

import "gioui.org/layout"

// PerfScrollPicker moves an open emoji picker's grid down by rows rows,
// as a reader scrolling through every category does (the memory
// workflow's driver, internal/ui/perfdrive.go; perfharness builds only).
// It reports whether the grid has reached its last row, and ok=false
// when o isn't an emoji picker. UI goroutine.
func PerfScrollPicker(o any, rows int) (atEnd, ok bool) {
	p, isPicker := o.(*EmojiPicker)
	if !isPicker {
		return false, false
	}
	if len(p.rows) == 0 {
		return false, true
	}
	f := min(p.list.Position.First+rows, len(p.rows)-1)
	p.list.Position = layout.Position{First: f, BeforeEnd: true}
	return f >= len(p.rows)-1, true
}
