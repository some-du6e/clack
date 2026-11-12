// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

/*
#cgo freebsd openbsd CFLAGS: -I/usr/X11R6/include -I/usr/local/include

#include <stdlib.h>
#include <X11/Xlib.h>
*/
import "C"

import (
	"unsafe"

	"gioui.org/f32"
	"gioui.org/io/key"
)

// Clack patch (CLACK_PATCHES.md §11): file drops on X11, the XDND
// protocol (version 5, https://freedesktop.org/wiki/Specifications/XDND/)
// as Chromium's XDragDropClient speaks it for a drop target.
//
// The window says XdndAware. A drag whose types include text/uri-list
// (files from Nautilus, Dolphin, Thunar, a browser's downloads) sends
// FileDragEnter on its first XdndPosition, FileDragOver on the rest,
// FileDragLeave on XdndLeave. XdndDrop converts XdndSelection to
// text/uri-list; the SelectionNotify brings the file: URIs, and FileDrop
// carries their paths. The window answers XdndStatus with
// XdndActionCopy, and XdndFinished once it has read the list.

const xdndVersion = 5

type xdndState struct {
	atoms struct {
		aware, enter, position, status, leave, drop, finished C.Atom
		selection, typeList, actionCopy, uriList, prop        C.Atom
	}
	source  C.Window // the drag's source window, 0 when no drag
	version int
	files   bool // the drag offers text/uri-list
	entered bool // FileDragEnter was sent
	pos     f32.Point
	mods    key.Modifiers
	// dropping is set between XdndDrop and the SelectionNotify.
	dropping bool
}

func (w *x11Window) initXdnd() {
	a := &w.xdnd.atoms
	a.aware = w.atom("XdndAware", false)
	a.enter = w.atom("XdndEnter", false)
	a.position = w.atom("XdndPosition", false)
	a.status = w.atom("XdndStatus", false)
	a.leave = w.atom("XdndLeave", false)
	a.drop = w.atom("XdndDrop", false)
	a.finished = w.atom("XdndFinished", false)
	a.selection = w.atom("XdndSelection", false)
	a.typeList = w.atom("XdndTypeList", false)
	a.actionCopy = w.atom("XdndActionCopy", false)
	a.uriList = w.atom("text/uri-list", false)
	a.prop = w.atom("GIO_XDND_DATA", false)
	v := C.long(xdndVersion)
	C.XChangeProperty(w.x, w.xw, a.aware, w.atoms.atom, 32, C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&v)), 1)
}

// xdndData is a ClientMessage's five longs.
func xdndData(cevt *C.XClientMessageEvent) *[5]C.long {
	return (*[5]C.long)(unsafe.Pointer(&cevt.data))
}

// handleXdnd handles the XDND client messages; it reports whether cevt
// was one.
func (w *x11Window) handleXdnd(cevt *C.XClientMessageEvent) bool {
	d := &w.xdnd
	a := &d.atoms
	l := xdndData(cevt)
	switch cevt.message_type {
	case a.enter:
		d.source = C.Window(l[0])
		d.version = int(uint64(l[1]) >> 24)
		d.entered, d.dropping = false, false
		d.files = false
		if l[1]&1 != 0 {
			d.files = w.xdndTypeListHas(d.source, a.uriList)
		} else {
			for _, t := range l[2:5] {
				if C.Atom(t) == a.uriList {
					d.files = true
				}
			}
		}
		return true
	case a.position:
		src := C.Window(l[0])
		if src != d.source {
			return true
		}
		root := uint64(l[2])
		d.pos = w.xdndLocal(int(uint16(root>>16)), int(uint16(root)))
		d.mods = w.xdndMods()
		action := C.long(C.None)
		accept := C.long(0)
		if d.files {
			accept, action = 1, C.long(a.actionCopy)
		}
		// bit 1: send XdndPosition even without moves inside a rectangle
		// (none given), so the target sees every move
		w.xdndSend(src, a.status, [5]C.long{C.long(w.xw), accept | 2, 0, 0, action})
		if !d.files {
			return true
		}
		kind := FileDragOver
		if !d.entered {
			d.entered = true
			kind = FileDragEnter
		}
		w.ProcessEvent(FileDropEvent{Kind: kind, Position: d.pos, Modifiers: d.mods})
		return true
	case a.leave:
		if C.Window(l[0]) != d.source {
			return true
		}
		w.xdndEnd(true)
		return true
	case a.drop:
		src := C.Window(l[0])
		if src != d.source {
			return true
		}
		if !d.files {
			w.xdndSend(src, a.finished, [5]C.long{C.long(w.xw), 0, C.None})
			w.xdndEnd(true)
			return true
		}
		d.dropping = true
		ts := C.Time(C.CurrentTime)
		if d.version >= 1 {
			ts = C.Time(l[2])
		}
		C.XDeleteProperty(w.x, w.xw, a.prop)
		C.XConvertSelection(w.x, a.selection, a.uriList, a.prop, w.xw, ts)
		return true
	}
	return false
}

// handleXdndSelection takes XdndSelection's text/uri-list after a drop;
// it reports whether cevt was that.
func (w *x11Window) handleXdndSelection(cevt *C.XSelectionEvent) bool {
	d := &w.xdnd
	if cevt.selection != d.atoms.selection || !d.dropping {
		return false
	}
	var paths []string
	if cevt.property != C.None {
		paths = parseURIList(w.xdndReadProp(w.xw, cevt.property))
	}
	accepted := C.long(0)
	action := C.long(C.None)
	if len(paths) > 0 {
		accepted, action = 1, C.long(d.atoms.actionCopy)
	}
	w.xdndSend(d.source, d.atoms.finished, [5]C.long{C.long(w.xw), accepted, action})
	if len(paths) > 0 {
		pos, mods := d.pos, d.mods
		w.xdndEnd(false)
		w.ProcessEvent(FileDropEvent{Kind: FileDrop, Position: pos, Paths: paths, Modifiers: mods})
	} else {
		w.xdndEnd(true)
	}
	return true
}

// xdndEnd forgets the drag, with a FileDragLeave when one was entered.
func (w *x11Window) xdndEnd(leave bool) {
	d := &w.xdnd
	was := d.entered
	d.source, d.files, d.entered, d.dropping = 0, false, false, false
	if leave && was {
		w.ProcessEvent(FileDropEvent{Kind: FileDragLeave})
	}
}

func (w *x11Window) xdndSend(to C.Window, typ C.Atom, l [5]C.long) {
	var xev C.XEvent
	ev := (*C.XClientMessageEvent)(unsafe.Pointer(&xev))
	ev._type = C.ClientMessage
	ev.display = w.x
	ev.window = to
	ev.message_type = typ
	ev.format = 32
	*xdndData(ev) = l
	C.XSendEvent(w.x, to, C.False, C.NoEventMask, &xev)
	C.XFlush(w.x)
}

// xdndLocal converts root coordinates to the window's.
func (w *x11Window) xdndLocal(x, y int) f32.Point {
	var lx, ly C.int
	var child C.Window
	root := C.XDefaultRootWindow(w.x)
	C.XTranslateCoordinates(w.x, root, w.xw, C.int(x), C.int(y), &lx, &ly, &child)
	return f32.Pt(float32(lx), float32(ly))
}

// xdndMods reads the keyboard modifiers held now (XdndPosition doesn't
// carry them).
func (w *x11Window) xdndMods() key.Modifiers {
	var root, child C.Window
	var rx, ry, wx, wy C.int
	var mask C.uint
	if C.XQueryPointer(w.x, w.xw, &root, &child, &rx, &ry, &wx, &wy, &mask) == 0 {
		return 0
	}
	var m key.Modifiers
	if mask&C.ShiftMask != 0 {
		m |= key.ModShift
	}
	if mask&C.ControlMask != 0 {
		m |= key.ModCtrl
	}
	if mask&C.Mod1Mask != 0 {
		m |= key.ModAlt
	}
	if mask&C.Mod4Mask != 0 {
		m |= key.ModSuper
	}
	return m
}

// xdndTypeListHas reads the source's XdndTypeList (more than three types).
func (w *x11Window) xdndTypeListHas(src C.Window, want C.Atom) bool {
	var typ C.Atom
	var format C.int
	var n, after C.ulong
	var data *C.uchar
	if C.XGetWindowProperty(w.x, src, w.xdnd.atoms.typeList, 0, 0x8000000, C.False, w.atoms.atom,
		&typ, &format, &n, &after, &data) != C.Success || data == nil {
		return false
	}
	defer C.XFree(unsafe.Pointer(data))
	if format != 32 {
		return false
	}
	// format 32 properties come back as an array of longs
	for _, a := range unsafe.Slice((*C.long)(unsafe.Pointer(data)), int(n)) {
		if C.Atom(a) == want {
			return true
		}
	}
	return false
}

// xdndReadProp reads (and deletes) a format 8 property.
func (w *x11Window) xdndReadProp(win C.Window, prop C.Atom) string {
	var typ C.Atom
	var format C.int
	var n, after C.ulong
	var data *C.uchar
	if C.XGetWindowProperty(w.x, win, prop, 0, 0x8000000, C.True, C.AnyPropertyType,
		&typ, &format, &n, &after, &data) != C.Success || data == nil {
		return ""
	}
	defer C.XFree(unsafe.Pointer(data))
	if format != 8 {
		return ""
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(data)), C.int(n))
}
