//go:build darwin && !ios && cgo

package richclip

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Chromium's pasteboard type for web custom data
// (clipboard_format_type_mac.mm).
static NSString *clackCustomType(void) { return @"org.chromium.web-custom-data"; }

static void clack_on_main(void (^b)(void)) {
	if ([NSThread isMainThread]) {
		b();
	} else {
		dispatch_sync(dispatch_get_main_queue(), b);
	}
}

// clack_rc_write puts text, html (may be NULL) and custom (may be NULL)
// on the general pasteboard in one change.
static int clack_rc_write(const char *text, const char *html, const void *custom, long ncustom) {
	__block int ok = 0;
	clack_on_main(^{
		@autoreleasepool {
			NSPasteboard *pb = [NSPasteboard generalPasteboard];
			NSMutableArray *types = [NSMutableArray arrayWithObject:NSPasteboardTypeString];
			if (html != NULL) {
				[types addObject:NSPasteboardTypeHTML];
			}
			if (custom != NULL) {
				[types addObject:clackCustomType()];
			}
			[pb declareTypes:types owner:nil];
			ok = [pb setString:[NSString stringWithUTF8String:text] forType:NSPasteboardTypeString] ? 1 : 0;
			if (html != NULL) {
				[pb setString:[NSString stringWithUTF8String:html] forType:NSPasteboardTypeHTML];
			}
			if (custom != NULL) {
				[pb setData:[NSData dataWithBytes:custom length:(NSUInteger)ncustom] forType:clackCustomType()];
			}
		}
	});
	return ok;
}

// clack_rc_custom returns a malloc'd copy of the pasteboard's custom data
// (NULL when it has none, or more than limit bytes).
static void *clack_rc_custom(long limit, long *n) {
	__block void *out = NULL;
	*n = 0;
	clack_on_main(^{
		@autoreleasepool {
			NSData *d = [[NSPasteboard generalPasteboard] dataForType:clackCustomType()];
			if (d == nil || (long)[d length] > limit || [d length] == 0) {
				return;
			}
			out = malloc([d length]);
			if (out != NULL) {
				memcpy(out, [d bytes], [d length]);
				*n = (long)[d length];
			}
		}
	});
	return out;
}
*/
import "C"

import (
	"context"
	"errors"
	"strings"
	"unsafe"
)

func supported() bool { return true }

func write(_ context.Context, c Copy) error {
	text := C.CString(strings.ReplaceAll(c.Text, "\x00", ""))
	defer C.free(unsafe.Pointer(text))
	var html *C.char
	if c.HTML != "" {
		html = C.CString(strings.ReplaceAll(c.HTML, "\x00", ""))
		defer C.free(unsafe.Pointer(html))
	}
	var custom unsafe.Pointer
	n := 0
	if len(c.Custom) > 0 {
		b := EncodeCustom(c.Custom)
		custom = C.CBytes(b)
		n = len(b)
		defer C.free(custom)
	}
	if C.clack_rc_write(text, html, custom, C.long(n)) == 0 {
		return errors.New("the pasteboard refused the text")
	}
	return nil
}

func readCustom(context.Context) (map[string]string, error) {
	var n C.long
	p := C.clack_rc_custom(C.long(maxCustom), &n)
	if p == nil {
		return nil, nil
	}
	defer C.free(p)
	return DecodeCustom(C.GoBytes(p, C.int(n)))
}
