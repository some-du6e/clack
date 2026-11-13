// SPDX-License-Identifier: Unlicense OR MIT

package app

// Clack patch 14 (CLACK_PATCHES.md): middle-click autoscroll's cursors on
// Windows are Chromium's own. Windows has no panning cursors of its own,
// so Chromium bundles them: ui/resources/cursors/pan_*.cur (BSD-3-Clause,
// clack_cursors/LICENSE), IDC_PAN_* in ui_unscaled_resources.grd, which
// ui/base/win/win_cursor_factory.cc loads with LoadCursor for
// CursorType::kMiddlePanning and the rest. LoadCursor sizes a cursor to
// SM_CXCURSOR; CreateIconFromResourceEx with LR_DEFAULTSIZE does the same
// from the file's bytes.

import (
	"embed"
	"encoding/binary"
	"sync"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/io/pointer"
)

//go:embed clack_cursors/*.cur
var panCursorFiles embed.FS

var panCursorNames = map[pointer.Cursor]string{
	pointer.CursorMiddlePanning:           "pan_middle",
	pointer.CursorMiddlePanningVertical:   "pan_middle_vertical",
	pointer.CursorMiddlePanningHorizontal: "pan_middle_horizontal",
	pointer.CursorNorthPanning:            "pan_north",
	pointer.CursorNorthEastPanning:        "pan_north_east",
	pointer.CursorEastPanning:             "pan_east",
	pointer.CursorSouthEastPanning:        "pan_south_east",
	pointer.CursorSouthPanning:            "pan_south",
	pointer.CursorSouthWestPanning:        "pan_south_west",
	pointer.CursorWestPanning:             "pan_west",
	pointer.CursorNorthWestPanning:        "pan_north_west",
}

var panCursors struct {
	sync.Mutex
	h map[pointer.Cursor]syscall.Handle
}

// loadPanCursor is Chromium's cursor for c, made once and kept (as
// LoadCursor's shared cursors are). ok is false for the other cursors and
// when the image doesn't load.
func loadPanCursor(c pointer.Cursor) (syscall.Handle, bool) {
	name, ok := panCursorNames[c]
	if !ok {
		return 0, false
	}
	panCursors.Lock()
	defer panCursors.Unlock()
	if h, ok := panCursors.h[c]; ok {
		return h, h != 0
	}
	h := makePanCursor(name)
	if panCursors.h == nil {
		panCursors.h = map[pointer.Cursor]syscall.Handle{}
	}
	panCursors.h[c] = h
	return h, h != 0
}

// makePanCursor turns a .cur file (an ICONDIR with one entry) into the
// RT_CURSOR resource layout CreateIconFromResourceEx takes: the entry's
// hotspot, then its image.
func makePanCursor(name string) syscall.Handle {
	data, err := panCursorFiles.ReadFile("clack_cursors/" + name + ".cur")
	if err != nil || len(data) < 22 {
		return 0
	}
	le := binary.LittleEndian
	if le.Uint16(data[2:]) != 2 || le.Uint16(data[4:]) < 1 { // type 2: cursor
		return 0
	}
	hotX, hotY := le.Uint16(data[10:]), le.Uint16(data[12:])
	size, off := le.Uint32(data[14:]), le.Uint32(data[18:])
	if uint64(off)+uint64(size) > uint64(len(data)) || size == 0 {
		return 0
	}
	res := make([]byte, 4+size)
	le.PutUint16(res[0:], hotX)
	le.PutUint16(res[2:], hotY)
	copy(res[4:], data[off:off+size])
	h, err := windows.CreateIconFromResourceEx(res, false, 0, 0, windows.LR_DEFAULTSIZE)
	if err != nil {
		return 0
	}
	return h
}
