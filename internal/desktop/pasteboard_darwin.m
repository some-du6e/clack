//go:build darwin && cgo

// Manual retain/release, as the package's other Objective-C (cgo's CFLAGS
// are the whole package's, and dock_darwin.go and dnd_darwin.go release
// what they make). What's made here is autoreleased inside each block's
// pool, except the CGImage a request hands back (released by its taker or
// by clack_pboard_unref). dispatch_async copies its block, and the copy
// retains the inner block it captures.

#import <AppKit/AppKit.h>
#import <ImageIO/ImageIO.h>
#include <limits.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

// A read's status (pbStatus in pasteboard_darwin.go).
#define CLACK_PB_OK 0
#define CLACK_PB_NONE 1   // nothing of that kind on the pasteboard
#define CLACK_PB_TOOBIG 2 // over the caller's limit
#define CLACK_PB_BUSY 3   // the main thread didn't answer in time

// The general pasteboard is read on the main thread, as Chromium's
// ClipboardMac reads it, but nothing here waits for the main thread
// there: Gio lays out frames on it, so a dispatch_sync could deadlock with
// a frame that's waiting on a paste. Each read is dispatch_async'd with a
// request the block and the caller share (refcounted); the caller waits on
// a semaphore for at most ms and then gives up its share. Whoever drops the
// last reference frees the request and any late result.
typedef struct {
	_Atomic int refs;
	dispatch_semaphore_t done;
	void *out;     // malloc'd
	long n;        // out's length
	CGImageRef img; // retained
	int status;
	int png;       // out is public.png data as it was on the pasteboard
} clack_pboard_req;

static void clack_pboard_unref(clack_pboard_req *r) {
	if (atomic_fetch_sub(&r->refs, 1) == 1) {
		free(r->out);
		if (r->img != NULL) {
			CGImageRelease(r->img);
		}
		dispatch_release(r->done);
		free(r);
	}
}

// clack_pboard_run runs b(r) on the main thread (inline when already on
// it) and waits up to ms for it. It returns r, now the caller's alone, or
// NULL when the main thread didn't answer in time (or out of memory).
static clack_pboard_req *clack_pboard_run(long ms, void (^b)(clack_pboard_req *)) {
	clack_pboard_req *r = calloc(1, sizeof *r);
	if (r == NULL) {
		return NULL;
	}
	atomic_init(&r->refs, 2); // the caller and the main-thread block
	r->done = dispatch_semaphore_create(0);
	r->status = CLACK_PB_NONE;
	if ([NSThread isMainThread]) {
		b(r);
		clack_pboard_unref(r); // the block's share
		return r;
	}
	dispatch_async(dispatch_get_main_queue(), ^{
		b(r);
		dispatch_semaphore_signal(r->done);
		clack_pboard_unref(r);
	});
	if (dispatch_semaphore_wait(r->done, dispatch_time(DISPATCH_TIME_NOW, ms * (int64_t)NSEC_PER_MSEC)) != 0) {
		clack_pboard_unref(r);
		return NULL;
	}
	return r;
}

// clack_pboard_take moves r's bytes to the caller and frees r.
static void *clack_pboard_take(clack_pboard_req *r, long *n, int *status) {
	*n = 0;
	if (r == NULL) {
		*status = CLACK_PB_BUSY;
		return NULL;
	}
	void *out = r->out;
	r->out = NULL;
	*n = r->n;
	*status = r->status;
	clack_pboard_unref(r);
	return out;
}

static void clack_pboard_copy(clack_pboard_req *r, const void *b, long len, long max) {
	if (len > max) {
		r->status = CLACK_PB_TOOBIG;
		return;
	}
	void *p = malloc(len > 0 ? len : 1);
	if (p == NULL) {
		return;
	}
	if (len > 0) {
		memcpy(p, b, len);
	}
	r->out = p;
	r->n = len;
	r->status = CLACK_PB_OK;
}

// clack_pboard_strings copies ss NUL-terminated, one after another.
static void clack_pboard_strings(clack_pboard_req *r, NSArray<NSString *> *ss) {
	NSMutableData *d = [NSMutableData data];
	for (NSString *s in ss) {
		const char *u = [s UTF8String];
		if (u != NULL) {
			[d appendBytes:u length:strlen(u) + 1];
		}
	}
	clack_pboard_copy(r, d.bytes, (long)d.length, LONG_MAX);
}

void *clack_pboard_types(long ms, long *n, int *status) {
	return clack_pboard_take(clack_pboard_run(ms, ^(clack_pboard_req *r) {
		@autoreleasepool {
			NSArray<NSPasteboardType> *ts = [[NSPasteboard generalPasteboard] types];
			clack_pboard_strings(r, ts ? ts : @[]);
		}
	}), n, status);
}

// clack_pboard_files: Chromium's clipboard_util::FilesFromPasteboard, the
// public.file-url of each pasteboard item as a path (Finder's ⌘C puts one
// item per file).
void *clack_pboard_files(long ms, long *n, int *status) {
	return clack_pboard_take(clack_pboard_run(ms, ^(clack_pboard_req *r) {
		@autoreleasepool {
			NSMutableArray<NSString *> *paths = [NSMutableArray array];
			for (NSPasteboardItem *item in [[NSPasteboard generalPasteboard] pasteboardItems]) {
				NSString *s = [item stringForType:NSPasteboardTypeFileURL];
				if (s == nil) {
					continue;
				}
				NSString *p = [[NSURL URLWithString:s] filePathURL].path;
				if (p.length > 0) {
					[paths addObject:p];
				}
			}
			clack_pboard_strings(r, paths);
		}
	}), n, status);
}

// clack_pboard_image: Chromium's ClipboardMac::ReadPngInternal on the main
// thread. public.png's bytes as they are (*png = 1); else an NSImage from
// the pasteboard (TIFF, JPEG, PDF…: whatever NSImage reads, guarded as
// GetNSImage guards it, since NSImage may throw on data it dislikes) and
// its CGImage in *img, for clack_pboard_encode off the main thread.
void *clack_pboard_image(long ms, long max, long *n, int *status, int *png, void **img) {
	*png = 0;
	*img = NULL;
	clack_pboard_req *r = clack_pboard_run(ms, ^(clack_pboard_req *q) {
		@autoreleasepool {
			NSPasteboard *pb = [NSPasteboard generalPasteboard];
			NSData *d = [pb dataForType:NSPasteboardTypePNG];
			if (d.length > 0) {
				clack_pboard_copy(q, d.bytes, (long)d.length, max);
				q->png = 1;
				return;
			}
			NSImage *im = nil;
			@try {
				im = [[[NSImage alloc] initWithPasteboard:pb] autorelease];
			} @catch (id e) {
				im = nil;
			}
			if (im == nil || im.representations.count == 0) {
				return;
			}
			CGImageRef cg = [im CGImageForProposedRect:NULL context:nil hints:nil];
			if (cg == NULL) {
				return;
			}
			q->img = CGImageRetain(cg);
			q->status = CLACK_PB_OK;
		}
	});
	if (r != NULL) {
		*png = r->png;
		*img = (void *)r->img;
		r->img = NULL;
	}
	return clack_pboard_take(r, n, status);
}

// clack_pboard_encode: Chromium's EncodeGfxImageToPng (As1xPNGBytes, an
// NSBitmapImageRep of the image's CGImage as PNG), with ImageIO, which is
// safe off the main thread. It releases img.
void *clack_pboard_encode(void *im, long max, long *n, int *status) {
	CGImageRef img = (CGImageRef)im;
	*n = 0;
	*status = CLACK_PB_NONE;
	void *out = NULL;
	@autoreleasepool {
		CFMutableDataRef d = CFDataCreateMutable(NULL, 0);
		CGImageDestinationRef dst = d ? CGImageDestinationCreateWithData(d, CFSTR("public.png"), 1, NULL) : NULL;
		if (dst != NULL) {
			CGImageDestinationAddImage(dst, img, NULL);
			if (CGImageDestinationFinalize(dst)) {
				long len = (long)CFDataGetLength(d);
				if (len > max) {
					*status = CLACK_PB_TOOBIG;
				} else if (len > 0 && (out = malloc(len)) != NULL) {
					memcpy(out, CFDataGetBytePtr(d), len);
					*n = len;
					*status = CLACK_PB_OK;
				}
			}
			CFRelease(dst);
		}
		if (d != NULL) {
			CFRelease(d);
		}
	}
	CGImageRelease(img);
	return out;
}

void clack_pboard_release(void *img) {
	if (img != NULL) {
		CGImageRelease((CGImageRef)img);
	}
}
