// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"testing"
	"unsafe"

	"gioui.org/io/pointer"
)

// Clack patch 14: every panning cursor loads from Chromium's .cur file as
// a cursor (not an icon) with the file's centre hotspot, scaled to the
// system cursor size the way LoadCursor scales Chromium's own, and
// SetCursor uses it instead of the IDC fallback.
func TestPanCursors(t *testing.T) {
	getIconInfo := testUser32.NewProc("GetIconInfo")
	getSystemMetrics := testUser32.NewProc("GetSystemMetrics")
	cx, _, _ := getSystemMetrics.Call(13) // SM_CXCURSOR
	for c := range panCursorNames {
		h, ok := loadPanCursor(c)
		if !ok || h == 0 {
			t.Errorf("%v: didn't load", c)
			continue
		}
		if h2, _ := loadPanCursor(c); h2 != h {
			t.Errorf("%v: loaded twice (%#x, %#x), want it kept", c, h, h2)
		}
		if got, err := loadCursor(c); err != nil || got != h {
			t.Errorf("%v: loadCursor = %#x, %v; want the pan cursor %#x", c, got, err, h)
		}
		var info struct {
			fIcon            uint32
			xHotspot, yHotsp uint32
			mask, color      uintptr
		}
		if r, _, err := getIconInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info))); r == 0 {
			t.Errorf("%v: GetIconInfo: %v", c, err)
			continue
		}
		if info.fIcon != 0 {
			t.Errorf("%v: made an icon, want a cursor", c)
		}
		// the files' hotspot is (16, 16) of 32x32: the centre at any size
		if want := uint32(cx) / 2; info.xHotspot != want || info.yHotsp != want {
			t.Errorf("%v: hotspot (%d, %d), want (%d, %d) at SM_CXCURSOR %d", c, info.xHotspot, info.yHotsp, want, want, cx)
		}
	}
	if len(panCursorNames) != 11 {
		t.Errorf("%d panning cursors, want Chromium's 11", len(panCursorNames))
	}
	if _, ok := loadPanCursor(pointer.CursorText); ok {
		t.Errorf("CursorText loaded as a panning cursor")
	}
}
