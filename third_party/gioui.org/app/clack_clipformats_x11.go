// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

/*
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>
*/
import "C"

import (
	"time"
	"unsafe"

	"gioui.org/io/clipboard"
)

// Clack patch (CLACK_PATCHES.md §68): an X11 window's CLIPBOARD with
// several formats (a clipboard.FormatsType write), as Chromium's
// selection owner offers them (ui/base/x/selection_owner.cc):
//
//   - the write owns CLIPBOARD only (PRIMARY keeps what it had: Chromium
//     copies only text there);
//   - TARGETS lists TARGETS and each format's MIME type, and a request
//     for one gets its data as a property of that type, 8 bits a unit;
//     any other target is refused (property None) at once;
//   - a format larger than the server takes in one request goes by INCR
//     (ICCCM 2.7.2): an INCR property with its size, then a chunk each
//     time the requestor deletes the property, then an empty one;
//   - SelectionClear (another client took CLIPBOARD) drops the formats.
//
// A text write afterwards (Gio's own WriteClipboard) replaces them.

type x11ClipFormat struct {
	atom C.Atom
	data []byte
}

// x11Incr is one INCR transfer of a format to a requestor's property.
type x11Incr struct {
	requestor        C.Window
	property, target C.Atom
	data             []byte
	sent             int // bytes sent; -1 once the closing empty chunk is
	at               time.Time
}

// x11ClipIncrMax, when above 0, is the most a format sends in one property
// (tests); otherwise it's the server's largest request.
var x11ClipIncrMax int

// x11IncrStale is how long a transfer may wait for its requestor before
// it's dropped (a requestor that died).
const x11IncrStale = 30 * time.Second

// clackWriteFormats takes a FormatsType write (true), or drops the formats
// for any other write (false: Gio writes its text).
func (w *x11Window) clackWriteFormats(mime string, b []byte) bool {
	if mime != clipboard.FormatsType {
		w.clipboard.formats = nil
		return false
	}
	fs, ok := clipboard.DecodeFormats(b)
	if !ok || len(fs) == 0 {
		return true
	}
	w.clipboard.formats = make([]x11ClipFormat, 0, len(fs))
	for _, f := range fs {
		w.clipboard.formats = append(w.clipboard.formats, x11ClipFormat{w.internAtom(f.Type), f.Data})
	}
	C.XSetSelectionOwner(w.x, w.atoms.clipboard, w.xw, C.CurrentTime)
	return true
}

func (w *x11Window) internAtom(name string) C.Atom {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.XInternAtom(w.x, cs, C.False)
}

// clipMaxProp is the most bytes one property change sends.
func (w *x11Window) clipMaxProp() int {
	if x11ClipIncrMax > 0 {
		return x11ClipIncrMax
	}
	n := int(C.XExtendedMaxRequestSize(w.x))
	if n == 0 {
		n = int(C.XMaxRequestSize(w.x))
	}
	return max(n*4-100, 4096)
}

// clackServeFormats answers a SelectionRequest for CLIPBOARD while it holds
// formats (true); false leaves the request to Gio's text answer.
func (w *x11Window) clackServeFormats(cevt *C.XSelectionRequestEvent) bool {
	if w.clipboard.formats == nil || cevt.selection != w.atoms.clipboard {
		return false
	}
	w.pruneIncrs()
	prop := cevt.property
	switch {
	case cevt.target == w.atoms.targets:
		targets := make([]C.long, 0, 1+len(w.clipboard.formats))
		targets = append(targets, C.long(w.atoms.targets))
		for _, f := range w.clipboard.formats {
			targets = append(targets, C.long(f.atom))
		}
		C.XChangeProperty(w.x, cevt.requestor, prop, w.atoms.atom, 32, C.PropModeReplace,
			(*C.uchar)(unsafe.Pointer(&targets[0])), C.int(len(targets)))
	default:
		var f *x11ClipFormat
		for i := range w.clipboard.formats {
			if w.clipboard.formats[i].atom == cevt.target {
				f = &w.clipboard.formats[i]
				break
			}
		}
		switch {
		case f == nil:
			prop = C.None
		case len(f.data) > w.clipMaxProp():
			if cevt.requestor == w.xw { // not this window's own read (Gio's reads text only)
				prop = C.None
				break
			}
			if w.clipboard.incrAtom == 0 {
				w.clipboard.incrAtom = w.internAtom("INCR")
			}
			// watch the requestor's property before it hears of the transfer
			C.XSelectInput(w.x, cevt.requestor, C.PropertyChangeMask)
			size := C.long(len(f.data))
			C.XChangeProperty(w.x, cevt.requestor, prop, w.clipboard.incrAtom, 32, C.PropModeReplace,
				(*C.uchar)(unsafe.Pointer(&size)), 1)
			w.clipboard.incrs = append(w.clipboard.incrs, &x11Incr{requestor: cevt.requestor, property: prop,
				target: f.atom, data: f.data, at: time.Now()})
		default:
			w.changeProp(cevt.requestor, prop, f.atom, f.data)
		}
	}
	var xev C.XEvent
	ev := (*C.XSelectionEvent)(unsafe.Pointer(&xev))
	*ev = C.XSelectionEvent{
		_type:     C.SelectionNotify,
		display:   cevt.display,
		requestor: cevt.requestor,
		selection: cevt.selection,
		target:    cevt.target,
		property:  prop,
		time:      cevt.time,
	}
	C.XSendEvent(w.x, cevt.requestor, 0, 0, &xev)
	return true
}

// changeProp sets the requestor's property to data as an 8-bit property
// of type typ ("" data: an empty one).
func (w *x11Window) changeProp(requestor C.Window, prop, typ C.Atom, data []byte) {
	var p *C.uchar
	if len(data) > 0 {
		p = (*C.uchar)(unsafe.Pointer(unsafe.SliceData(data)))
	}
	C.XChangeProperty(w.x, requestor, prop, typ, 8, C.PropModeReplace, p, C.int(len(data)))
}

// clackIncrProperty sends a transfer's next chunk when its requestor has
// deleted the property (read the last one), and ends it after the empty
// chunk.
func (w *x11Window) clackIncrProperty(ev *C.XPropertyEvent) {
	if ev.state != C.PropertyDelete || len(w.clipboard.incrs) == 0 {
		return
	}
	for i, t := range w.clipboard.incrs {
		if t.requestor != ev.window || t.property != ev.atom {
			continue
		}
		if t.sent < 0 { // the empty chunk was read: done
			w.clipboard.incrs = append(w.clipboard.incrs[:i], w.clipboard.incrs[i+1:]...)
			w.unwatch(t.requestor)
			return
		}
		n := min(len(t.data)-t.sent, w.clipMaxProp())
		w.changeProp(t.requestor, t.property, t.target, t.data[t.sent:t.sent+n])
		if n == 0 {
			t.sent = -1
		} else {
			t.sent += n
		}
		t.at = time.Now()
		return
	}
}

// unwatch stops PropertyNotify for requestor once no transfer goes to it.
func (w *x11Window) unwatch(requestor C.Window) {
	for _, t := range w.clipboard.incrs {
		if t.requestor == requestor {
			return
		}
	}
	C.XSelectInput(w.x, requestor, C.NoEventMask)
}

// pruneIncrs drops transfers whose requestor stopped reading.
func (w *x11Window) pruneIncrs() {
	keep := w.clipboard.incrs[:0]
	var gone []C.Window
	for _, t := range w.clipboard.incrs {
		if time.Since(t.at) < x11IncrStale {
			keep = append(keep, t)
		} else {
			gone = append(gone, t.requestor)
		}
	}
	w.clipboard.incrs = keep
	for _, r := range gone {
		w.unwatch(r)
	}
}

// clackSelectionClear drops the formats once another client owns CLIPBOARD.
func (w *x11Window) clackSelectionClear(ev *C.XSelectionClearEvent) {
	if ev.selection == w.atoms.clipboard {
		w.clipboard.formats = nil
	}
}
