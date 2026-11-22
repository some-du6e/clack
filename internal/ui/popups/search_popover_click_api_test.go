package popups

import "gioui.org/widget"

// spClick is popover row i's button and spLanded what the popover does
// when its rows change (search_popover_click_test.go).
func spClick(p *SearchPopover, i int) *widget.Clickable { return p.click(i) }
func spLanded(p *SearchPopover)                         { p.pruneClicks() }
