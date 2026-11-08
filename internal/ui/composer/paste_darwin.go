//go:build darwin && !ios && cgo

package composer

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>

// AppKit's pasteboard is read on the main thread, but nothing here waits
// for it there: Gio lays out frames synchronously on the main thread, so a
// dispatch_sync could deadlock with a frame that's waiting on us. Each read
// is dispatch_async'd with a request the block and the caller share
// (refcounted); the reader goroutine waits on a semaphore with a timeout
// and gives up on its share if the main thread is busy. Whoever drops the
// last reference frees the request and any late result.
typedef struct {
	_Atomic int refs;
	dispatch_semaphore_t done;
	void *out;
	long n;
} clack_req;

static clack_req *clack_req_new(void) {
	clack_req *r = calloc(1, sizeof *r);
	if (r == NULL) {
		return NULL;
	}
	atomic_init(&r->refs, 2); // the caller and the main-thread block
	r->done = dispatch_semaphore_create(0);
	return r;
}

static void clack_req_unref(clack_req *r) {
	if (atomic_fetch_sub(&r->refs, 1) == 1) {
		free(r->out);
		dispatch_release(r->done);
		free(r);
	}
}

// clack_run runs b(r) on the main thread and waits up to ms for it
// (inline when already on the main thread). It returns 1 when b finished;
// then r->out is the caller's to take. r is released either way.
static int clack_run(clack_req *r, void (^b)(clack_req *), long ms, void **out, long *n) {
	*out = NULL;
	*n = 0;
	if (r == NULL) {
		return 0;
	}
	int ok;
	if ([NSThread isMainThread]) {
		b(r);
		clack_req_unref(r); // the block's share
		ok = 1;
	} else {
		dispatch_async(dispatch_get_main_queue(), ^{
			b(r);
			dispatch_semaphore_signal(r->done);
			clack_req_unref(r);
		});
		ok = dispatch_semaphore_wait(r->done, dispatch_time(DISPATCH_TIME_NOW, ms * (long)NSEC_PER_MSEC)) == 0;
	}
	if (ok) {
		*out = r->out;
		*n = r->n;
		r->out = NULL;
	}
	clack_req_unref(r);
	return ok;
}

// clack_wait is how long one read waits for the main thread.
#define clack_wait 1000

// clack_pb_types returns the general pasteboard's types, one per line
// (malloc'd; the caller frees it), or NULL.
static char *clack_pb_types(void) {
	void *out;
	long n;
	clack_run(clack_req_new(), ^(clack_req *r) {
		@autoreleasepool {
			NSArray<NSPasteboardType> *ts = [[NSPasteboard generalPasteboard] types];
			const char *u = ts ? [[ts componentsJoinedByString:@"\n"] UTF8String] : NULL;
			r->out = u ? strdup(u) : NULL;
		}
	}, clack_wait, &out, &n);
	return out;
}

static void clack_copy(clack_req *r, NSData *d, long max) {
	r->n = 0;
	if (d == nil || d.length == 0) {
		return;
	}
	if ((long)d.length > max) {
		r->n = -1;
		return;
	}
	void *p = malloc(d.length);
	if (p == NULL) {
		return;
	}
	memcpy(p, d.bytes, d.length);
	r->out = p;
	r->n = (long)d.length;
}

// clack_pb_data returns the data for one pasteboard type (malloc'd).
static void *clack_pb_data(const char *type, long max, long *n) {
	NSString *t = [[NSString alloc] initWithUTF8String:type]; // the block's own copy
	void *out;
	clack_run(clack_req_new(), ^(clack_req *r) {
		@autoreleasepool {
			clack_copy(r, [[NSPasteboard generalPasteboard] dataForType:t], max);
		}
	}, clack_wait, &out, n);
	[t release];
	return out;
}

// clack_rtf_html converts RTF (or flat RTFD) to HTML with AppKit's
// writer. Lists come out as <ul>/<ol>.
static void *clack_rtf_html(const void *b, long len, int rtfd, long max, long *n) {
	NSData *in = [[NSData alloc] initWithBytes:b length:len]; // b is the caller's
	void *out;
	clack_run(clack_req_new(), ^(clack_req *r) {
		@autoreleasepool {
			NSAttributedString *as = rtfd
				? [[NSAttributedString alloc] initWithRTFD:in documentAttributes:NULL]
				: [[NSAttributedString alloc] initWithRTF:in documentAttributes:NULL];
			if (as == nil) {
				return;
			}
			NSDictionary *attrs = @{NSDocumentTypeDocumentAttribute: NSHTMLTextDocumentType};
			NSData *html = [as dataFromRange:NSMakeRange(0, as.length) documentAttributes:attrs error:NULL];
			clack_copy(r, html, max);
			[as release];
		}
	}, clack_wait, &out, n);
	[in release];
	return out;
}
*/
import "C"

import (
	"context"
	"errors"
	"strings"
	"unsafe"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

// macOS: Gio reads only the plain string, so the HTML (public.html from
// browsers, Notes, Word…) or RTF (TextEdit, Pages; converted to HTML by
// AppKit) comes from NSPasteboard here.

var errClipNoHTML = errors.New("no html on the clipboard")

func clipHTMLSupported() bool { return true }

func clipReadHTML(ctx context.Context) ([]byte, error) {
	uiguard.OffUI("clipboard HTML read") // waits for the main thread
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
