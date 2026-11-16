package slackc

import "slices"

// Plugin layers that take the window's Escape. Kit's Popover is Slack's
// react-modal (shouldCloseOnEsc): its content's onKeyDown closes it on an
// Escape from anything inside it and stops the event there, so neither
// the shell's "Close / mark as read" nor a layer under it sees one. Gio
// gives a key to the first reader whose filter matches, and the shell
// reads first in the frame, so a layer says it's open on every frame it
// lays out (OpenLayer): the topmost of the window's last frame reads
// Escape before its content does, whatever has the focus, and the shell's
// Escape bindings stand down while one is open (LayerOpen).

// A Layer is a floating plugin layer that closes on Escape.
type Layer interface {
	// LayerClosed runs as its window's next frame begins when the layer
	// was open in the frame before and not in the last one: it closed or
	// left the tree. The focus has left its tags by then.
	LayerClosed()
}

// layerList is one window's open layers: in its last frame (prev, in
// layout order, so the last is drawn over the rest) and so far in this
// one (cur).
type layerList struct{ prev, cur []Layer }

// frame starts the window's next frame: the layers the last frame left
// out close.
func (ls *layerList) frame() {
	for _, l := range ls.prev {
		if !slices.Contains(ls.cur, l) {
			l.LayerClosed()
		}
	}
	clear(ls.prev) // its array is the new frame's: no closed layer kept reachable
	ls.prev, ls.cur = ls.cur, ls.prev[:0]
}

// OpenLayer says l is open in the frame the current window lays out; call
// it on every pass that lays l out. top reports whether l was the window's
// topmost layer in its last frame: Escape is its to read. others reports
// whether another layer is open too.
func OpenLayer(l Layer) (top, others bool) {
	ls := &curClock.layers
	if !slices.Contains(ls.cur, l) {
		ls.cur = append(ls.cur, l)
	}
	if n := len(ls.prev); n > 0 && ls.prev[n-1] == l {
		top = true
	}
	for _, s := range [2][]Layer{ls.prev, ls.cur} {
		for _, x := range s {
			if x != l {
				others = true
			}
		}
	}
	return top, others
}

// LayerOpen reports whether a plugin layer was open in the current
// window's last frame: Escape is the topmost layer's then.
func LayerOpen() bool { return len(curClock.layers.prev) > 0 }
