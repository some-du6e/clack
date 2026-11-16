//go:build darwin && !ios && cgo

package clipimg

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>

// clack_put_image puts PNG bytes on the general pasteboard as PNG and
// TIFF (older apps read only TIFF). AppKit wants the main thread.
static int clack_put_image(const void *p, long n) {
	__block int ok = 0;
	void (^b)(void) = ^{
		@autoreleasepool {
			NSData *d = [NSData dataWithBytes:p length:(NSUInteger)n];
			NSImage *im = [[NSImage alloc] initWithData:d];
			NSData *tiff = im ? [im TIFFRepresentation] : nil;
			NSPasteboard *pb = [NSPasteboard generalPasteboard];
			NSMutableArray *types = [NSMutableArray arrayWithObject:NSPasteboardTypePNG];
			if (tiff != nil) {
				[types addObject:NSPasteboardTypeTIFF];
			}
			[pb declareTypes:types owner:nil];
			ok = [pb setData:d forType:NSPasteboardTypePNG] ? 1 : 0;
			if (tiff != nil) {
				[pb setData:tiff forType:NSPasteboardTypeTIFF];
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

func write(ctx context.Context, img image.Image, charge func(int64) error) error {
	data, err := encodePNG(ctx, img, charge)
	if err != nil {
		return err
	}
	// one copy, in C's heap, for AppKit (which copies it into its NSData)
	p := C.malloc(C.size_t(data.Len()))
	if p == nil {
		return errors.New("out of memory")
	}
	defer C.free(p)
	data.copyTo(unsafe.Slice((*byte)(p), data.Len()))
	if C.clack_put_image(p, C.long(data.Len())) == 0 {
		return errors.New("the pasteboard refused the image")
	}
	return nil
}
