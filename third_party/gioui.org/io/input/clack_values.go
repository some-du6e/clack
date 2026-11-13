// SPDX-License-Identifier: Unlicense OR MIT

package input

// Clack patch 15 (CLACK_PATCHES.md): a router keeps values for the
// gestures that share it, one router per window. gesture's middle-click
// autoscroll keeps the window's autoscroll there, so every scroller in a
// window finds the same one and another window never does.

// Value returns what SetValue stored under key on any Source of this
// router, or nil. A disabled Source has none.
func (s Source) Value(key any) any {
	if !s.Enabled() {
		return nil
	}
	return s.r.values[key]
}

// SetValue stores v under key for every Source of this router; nil
// deletes it. A disabled Source stores nothing.
func (s Source) SetValue(key, v any) {
	if !s.Enabled() {
		return
	}
	if v == nil {
		delete(s.r.values, key)
		return
	}
	if s.r.values == nil {
		s.r.values = make(map[any]any)
	}
	s.r.values[key] = v
}
