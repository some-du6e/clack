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
		}
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

// SetWindowIcon sets the Dock icon when Clack runs as a bare binary; inside
// Clack.app the bundle's clack.icns is used. See the X11 version for the
// other platforms. Pass every window event; the largest png is used.
func SetWindowIcon(ev event.Event, pngs ...[]byte) {
	if _, ok := ev.(app.AppKitViewEvent); !ok || len(pngs) == 0 {
		return
	}
	dockIcon.Do(func() {
		if exe, err := os.Executable(); err == nil && strings.Contains(exe, ".app/Contents/MacOS/") {
			return
		}
		b := pngs[0]
		for _, p := range pngs {
			if len(p) > len(b) {
				b = p
			}
		}
		C.clack_set_dock_icon(unsafe.Pointer(&b[0]), C.int(len(b)))
	})
}
