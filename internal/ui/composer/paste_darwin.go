//go:build darwin && !ios && cgo

package composer

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// clack_pb_types returns the general pasteboard's types, one per line
// (malloc'd; the caller frees it), or NULL.
static char *clack_pb_types(void) {
	@autoreleasepool {
		NSArray<NSPasteboardType> *ts = [[NSPasteboard generalPasteboard] types];
		if (ts == nil) {
			return NULL;
		}
		const char *u = [[ts componentsJoinedByString:@"\n"] UTF8String];
		return u ? strdup(u) : NULL;
	}
}

static void *clack_copy(NSData *d, long max, long *n) {
	*n = 0;
	if (d == nil || d.length == 0) {
		return NULL;
	}
	if ((long)d.length > max) {
		*n = -1;
		return NULL;
	}
	void *p = malloc(d.length);
	if (p == NULL) {
		return NULL;
	}
	memcpy(p, d.bytes, d.length);
	*n = (long)d.length;
	return p;
}

// clack_pb_data returns the data for one pasteboard type (malloc'd).
static void *clack_pb_data(const char *type, long max, long *n) {
	@autoreleasepool {
		NSString *t = [NSString stringWithUTF8String:type];
		return clack_copy([[NSPasteboard generalPasteboard] dataForType:t], max, n);
	}
}

// clack_rtf_html converts RTF (or flat RTFD) to HTML with AppKit's writer
// (no WebKit, so any thread is fine). Lists come out as <ul>/<ol>.
static void *clack_rtf_html(const void *b, long len, int rtfd, long max, long *n) {
	@autoreleasepool {
		*n = 0;
		NSData *in = [NSData dataWithBytesNoCopy:(void *)b length:len freeWhenDone:NO];
		NSAttributedString *as = rtfd
			? [[NSAttributedString alloc] initWithRTFD:in documentAttributes:NULL]
			: [[NSAttributedString alloc] initWithRTF:in documentAttributes:NULL];
		if (as == nil) {
			return NULL;
		}
		NSDictionary *attrs = @{NSDocumentTypeDocumentAttribute: NSHTMLTextDocumentType};
		NSData *out = [as dataFromRange:NSMakeRange(0, as.length) documentAttributes:attrs error:NULL];
		void *p = clack_copy(out, max, n);
		[as release];
		return p;
	}
}
*/
import "C"

import (
	"context"
	"errors"
	"strings"
	"unsafe"
)

// macOS: Gio reads only the plain string, so the HTML (public.html from
// browsers, Notes, Word…) or RTF (TextEdit, Pages; converted to HTML by
// AppKit) comes from NSPasteboard here.

var errClipNoHTML = errors.New("no html on the clipboard")

func clipHTMLSupported() bool { return true }

func clipReadHTML(ctx context.Context) ([]byte, error) {
	ct := C.clack_pb_types()
	if ct == nil {
		return nil, errClipNoHTML
	}
	types := strings.Split(C.GoString(ct), "\n")
	C.free(unsafe.Pointer(ct))
	typ, kind := clipPickMacType(types)
	if kind == macNone {
		return nil, errClipNoHTML
	}
	limit := C.long(phMaxHTML)
	if kind != macHTML {
		limit = 4 * phMaxHTML // RTF is wordier than the HTML it makes
	}
	b := macRead(func(n *C.long) unsafe.Pointer {
		cs := C.CString(typ)
		defer C.free(unsafe.Pointer(cs))
		return C.clack_pb_data(cs, limit, n)
	})
	if b == nil || ctx.Err() != nil {
		return nil, errClipNoHTML
	}
	if kind == macHTML {
		return b, nil
	}
	rtfd := C.int(0)
	if kind == macRTFD {
		rtfd = 1
	}
	in := C.CBytes(b)
	defer C.free(in)
	out := macRead(func(n *C.long) unsafe.Pointer {
		return C.clack_rtf_html(in, C.long(len(b)), rtfd, C.long(phMaxHTML), n)
	})
	if out == nil {
		return nil, errClipNoHTML
	}
	return out, nil
}

// macRead calls f, which returns malloc'd bytes and their length, and
// copies them into Go memory.
func macRead(f func(n *C.long) unsafe.Pointer) []byte {
	var n C.long
	p := f(&n)
	if p == nil {
		return nil
	}
	defer C.free(p)
	if n <= 0 {
		return nil
	}
	return C.GoBytes(p, C.int(n))
}
