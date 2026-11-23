//go:build darwin && cgo

package slack

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UniformTypeIdentifiers
#import <Foundation/Foundation.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include <stdlib.h>
#include <string.h>

// clack_uttype_mime is platform_mime_util_apple.mm's
// GetPlatformMimeTypeFromExtension: the preferred MIME type of the UTType
// for the filename extension, none (NULL) for a dynamic type (one the
// system made up for an extension nothing declares) or a type without one.
// The result is malloc'd. Manual retain/release, like internal/desktop's.
static char *clack_uttype_mime(const char *ext) {
	@autoreleasepool {
		NSString *s = [[NSString alloc] initWithUTF8String:ext];
		if (s == nil) return NULL; // not UTF-8: SysUTF8ToNSString has no string either
		UTType *t = [UTType typeWithFilenameExtension:s];
		[s release];
		if (t.dynamic || t.preferredMIMEType == nil) return NULL;
		const char *m = [t.preferredMIMEType UTF8String];
		return m ? strdup(m) : NULL;
	}
}
*/
import "C"

import "unsafe"

// osMimeType is platform_mime_util_apple.mm's
// GetPlatformMimeTypeFromExtension: [UTType typeWithFilenameExtension:ext]'s
// preferredMIMEType, unless the type is dynamic or has none.
func osMimeType(ext string) (string, bool) {
	cs := C.CString(ext) // ext has no NUL: mimeTypeFromExtension refused it
	defer C.free(unsafe.Pointer(cs))
	m := C.clack_uttype_mime(cs)
	if m == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(m))
	return C.GoString(m), true
}
