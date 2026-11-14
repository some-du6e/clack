//go:build darwin && cgo

package menubar

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
#include "menubar_darwin.h"
*/
import "C"

import "unsafe"

// install hands the model to AppKit (menubar_darwin.m), which builds the
// menus on the main thread.
func install(menus []Menu) {
	spec := newSpec(menus)
	C.clack_menubar_install(spec)
}

// InstallNow is Install done synchronously, returning a description of
// NSApp's menu bar as AppKit has it (menubar_darwin.m's dump: title|key
// equivalent|action|target, "ok"/"MISSING" for whether the action is
// implemented, ">" for submenus). For tests: call it on the main thread.
func InstallNow(menus []Menu, onRecent func(route string)) string {
	menus = Clean(menus)
	mu.Lock()
	flat = Flatten(menus)
	recent = onRecent
	mu.Unlock()
	spec := newSpec(menus)
	if spec == 0 {
		return ""
	}
	d := C.clack_menubar_install_now(spec)
	defer C.free(unsafe.Pointer(d))
	return C.GoString(d)
}

// newSpec copies the model into an AppKit-side description (a retained
// NSArray; thread-safe to make). Top-level menus come first in their
// group, then their items depth-first with tags in Flatten's order.
func newSpec(menus []Menu) C.CFTypeRef {
	for role, k := range EditRoles {
		cs, ck := C.CString(role), C.CString(k.Key)
		C.clack_menubar_edit_role(cs, ck, C.int(k.Mods))
		C.free(unsafe.Pointer(cs))
		C.free(unsafe.Pointer(ck))
	}
	var items []C.clack_mi
	var strs []*C.char
	cstr := func(s string) *C.char {
		if s == "" {
			return nil
		}
		c := C.CString(s)
		strs = append(strs, c)
		return c
	}
	tag := 0
	var walk func(parent int, list []Item)
	walk = func(parent int, list []Item) {
		for _, it := range list {
			var flags C.int
			if it.Sep {
				flags |= C.CLACK_MI_SEP
			}
			if it.Forward {
				flags |= C.CLACK_MI_FORWARD
			}
			if it.Recent {
				flags |= C.CLACK_MI_RECENT
			}
			if it.Sub != nil {
				flags |= C.CLACK_MI_SUB
			}
			idx := len(items)
			items = append(items, C.clack_mi{
				title: cstr(it.Title), key: cstr(it.Key), role: cstr(it.Role),
				mods: C.int(it.Mods), parent: C.int(parent), flags: flags, tag: C.int(tag),
			})
			tag++
			walk(idx, it.Sub)
		}
	}
	for _, m := range menus {
		idx := len(items)
		items = append(items, C.clack_mi{title: cstr(m.Title), role: cstr(m.Kind), parent: -1, flags: C.CLACK_MI_MENU, tag: -1})
		walk(idx, m.Items)
	}
	var spec C.CFTypeRef
	if len(items) > 0 {
		spec = C.clack_menubar_spec(&items[0], C.int(len(items)))
	}
	for _, s := range strs {
		C.free(unsafe.Pointer(s))
	}
	return spec
}

// The callbacks below run on AppKit's main thread: they read state or
// start a goroutine, nothing else (TestCallbacksOnlyHandOff).

//export clackMenuChoose
func clackMenuChoose(tag C.int) { choose(int(tag)) }

//export clackMenuEnabled
func clackMenuEnabled(tag C.int) C.int {
	if enabled(int(tag)) {
		return 1
	}
	return 0
}

//export clackMenuTitle
func clackMenuTitle(tag C.int) *C.char {
	if t := title(int(tag)); t != "" {
		return C.CString(t) // freed by the caller
	}
	return nil
}

//export clackMenuHidden
func clackMenuHidden(tag C.int) C.int {
	if hidden(int(tag)) {
		return 1
	}
	return 0
}

//export clackMenuRecentBegin
func clackMenuRecentBegin() C.int { return C.int(len(recentShown())) }

//export clackMenuRecentTitle
func clackMenuRecentTitle(i C.int) *C.char {
	s := shown.Load()
	if s == nil || int(i) < 0 || int(i) >= len(s.Recent) {
		return nil
	}
	return C.CString(s.Recent[i].Title) // freed by the caller
}

//export clackMenuRecentChoose
func clackMenuRecentChoose(i C.int) { chooseRecent(int(i)) }
