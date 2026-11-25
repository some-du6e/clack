//go:build darwin && !ios && cgo

package clipimg

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>

// clack_put_image writes the pasteboard as Chromium's ClipboardMac does
// for an image (WritePortableAndPlatformRepresentationsInternal,
// WriteBitmapInternal, WriteHTML): new contents, then one
// NSPasteboardItem with the PNG as public.png given to writeObjects:
// (the pasteboard offers it as TIFF too), or when that fails the image as
// an NSImage (a TIFF only); then html (NULL for none) as public.html,
// which setString:forType: puts on that first item. AppKit wants the main
// thread. Manual retain/release (cgo's Objective-C isn't ARC).
static int clack_put_image(const void *p, long n, const char *html) {
	__block int ok = 0;
	void (^b)(void) = ^{
		@autoreleasepool {
			NSPasteboard *pb = [NSPasteboard generalPasteboard];
			[pb prepareForNewContentsWithOptions:0];
			NSData *d = [NSData dataWithBytes:p length:(NSUInteger)n];
			NSPasteboardItem *item = [[[NSPasteboardItem alloc] init] autorelease];
			if ([item setData:d forType:NSPasteboardTypePNG] && [pb writeObjects:@[ item ]]) {
				ok = 1;
			} else {
				NSImage *im = [[[NSImage alloc] initWithData:d] autorelease];
				ok = im != nil && [pb writeObjects:@[ im ]] ? 1 : 0;
			}
			if (ok && html != NULL) {
				NSString *s = [NSString stringWithUTF8String:html];
				if (s != nil) {
					[pb setString:s forType:NSPasteboardTypeHTML];
				}
			}
		}
	};
	if ([NSThread isMainThread]) {
		b();
	} else {
		dispatch_sync(dispatch_get_main_queue(), b);
	}
	return ok;
}
*/
import "C"

import (
	"context"
	"errors"
	"image"
	"unsafe"
)

func write(ctx context.Context, img image.Image, tag *Tag, charge func(int64) error) error {
	data, err := encodePNG(ctx, img, charge)
	if err != nil {
		return err
	}
	var html *C.char
	if m := imageMarkup(tag); m != "" {
		s := macHTMLPrefix + m
		if err := charge(int64(len(s) + 1)); err != nil {
			return err
		}
		html = C.CString(s)
		defer C.free(unsafe.Pointer(html))
	}
	// one copy, in C's heap, for AppKit (which copies it into its NSData)
	p := C.malloc(C.size_t(data.Len()))
	if p == nil {
		return errors.New("out of memory")
	}
	defer C.free(p)
	data.copyTo(unsafe.Slice((*byte)(p), data.Len()))
	if C.clack_put_image(p, C.long(data.Len()), html) == 0 {
		return errors.New("the pasteboard refused the image")
	}
	return nil
}
