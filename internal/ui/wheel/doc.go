// Package wheel holds the tests of animated mouse-wheel scrolling: Clack's
// Gio patch 9 (third_party/gioui.org/CLACK_PATCHES.md), which every
// gesture.Scroll (so every layout.List and widget.List) and the views
// reading pointer.Scroll themselves (gesture.Wheel) share. The tests live
// here because Clack's module, not the vendored Gio's, is what CI tests.
package wheel
