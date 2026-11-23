//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static void clack_set_dock_icon(const void *b, int n) {
	NSData *d = [NSData dataWithBytes:b length:n];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSImage *img = [[NSImage alloc] initWithData:d];
		if (img != nil) {
			[NSApp setApplicationIconImage:img];
			[img release]; // MRC: the app keeps its own reference
		}
	});
}

// the bundle's icon again (nil: AppKit draws Clack.app's clack.icns)
static void clack_reset_dock_icon(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp setApplicationIconImage:nil];
	});
}
*/
import "C"

import (
	"os"
	"strings"
	"sync"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

var dockIcon sync.Once

// bundled reports whether Clack runs inside Clack.app (the Dock then draws
// the bundle's clack.icns).
func bundled() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

func largestPNG(pngs [][]byte) []byte {
	var b []byte
	for _, p := range pngs {
		if len(p) > len(b) {
			b = p
		}
	}
	return b
}

// SetWindowIcon sets the Dock icon when Clack runs as a bare binary; inside
// Clack.app the bundle's clack.icns is used. See the X11 version for the
// other platforms. Pass every window event; the largest png is used.
func SetWindowIcon(ev event.Event, pngs ...[]byte) {
	if _, ok := ev.(app.AppKitViewEvent); !ok || len(pngs) == 0 {
		return
	}
	dockIcon.Do(func() {
		if bundled() {
			return
		}
		b := largestPNG(pngs)
		C.clack_set_dock_icon(unsafe.Pointer(&b[0]), C.int(len(b)))
	})
}

// SetAppIcon is the Dock icon from now on (CustomAppIcon: Slick's
// app.dock.setIcon): png, a PNG, or Clack's own for nil, the bundle's
// clack.icns inside Clack.app and else the largest of defaults.
func SetAppIcon(png []byte, defaults ...[]byte) {
	if png == nil {
		if bundled() {
			C.clack_reset_dock_icon()
			return
		}
		if png = largestPNG(defaults); len(png) == 0 {
			return
		}
	}
	if len(png) == 0 {
		return
	}
	C.clack_set_dock_icon(unsafe.Pointer(&png[0]), C.int(len(png)))
}
