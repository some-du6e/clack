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
	return NewSelectList(h, anchor, labels, sel, SelectListOpts{}, onPick)
}

// SelectListOpts tune a basic select's listbox.
type SelectListOpts struct {
	// Placeholder is the select's placeholderText when it opened without
	// a selected option (BasicSelect's hasPreselectedOption false, the
	// placeholder option not disabled): under
	// basic_select_placeholder_option the list starts with
	// "--{Placeholder}--" (getOptionsList's sk-select-placeholder row),
	// and picking it clears the selection: onPick(-1). "" never lists it.
	Placeholder string
}

// NewSelectList is a basic select's listbox, Slack Kit's
// SelectOptionsList (client-boot Fd8o) for a BasicSelect (boot-shared
// zDm3): on open the selected row is the active one and is scrolled to,
// or, under a11y_no_auto_highlight_search_multi_select, no row is active,
// the list stays at its top and the first row is pseudo-active (Enter
// picks it); the pointer's row is the active one; Enter picks the active
// row, Escape or a click outside closes. onPick gets an index into
// labels, or -1 for the placeholder row.
func NewSelectList(h Host, anchor image.Rectangle, labels []string, sel int, o SelectListOpts, onPick func(i int)) Overlay {
	noAuto := noAutoHighlight(h)
	rows, slots := labels, make([]int, 0, len(labels)+1)
	if o.Placeholder != "" && placeholderOption(h) {
		rows = append([]string{formatPlaceholderText(o.Placeholder)}, labels...)
		slots = append(slots, -1)
	}
	for i := range labels {
		slots = append(slots, i)
	}
	row := -1 // the selected option's row
	for r, s := range slots {
		if s == sel && sel >= 0 {
			row = r
		}
	}
	t := &dtTimeList{h: h, anchor: anchor, labels: rows, slots: slots, sel: row, cur: -1, onPick: onPick,
		sm: &selectMode{noAuto: noAuto, hovered: -1}}
	t.list.Axis = layout.Vertical
	t.clicks = make([]widget.Clickable, len(rows))
	if !noAuto && row >= 0 {
		// getInitalActiveRowIndex: the selected option, scrolled to
		t.cur = row
		if row > 3 {
			t.list.Position.First = row - 3
		}
	}
	return t
}
