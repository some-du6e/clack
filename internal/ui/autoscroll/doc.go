// Package autoscroll holds the tests of middle-click autoscroll: Clack's
// Gio patch 14 (third_party/gioui.org/CLACK_PATCHES.md), Chromium's
// MiddleClickAutoscroll for every gesture.Scroll (so every layout.List,
// widget.List and widget.Editor) and the views that read pointer.Scroll
// themselves. The tests live here because Clack's module, not the vendored
// Gio's, is what CI tests.
package autoscroll
