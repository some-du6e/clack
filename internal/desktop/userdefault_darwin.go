//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

// key's value in the user's defaults (the app's domain, then the global
// one) as JSON, or NULL when it's unset or isn't of type typ. The caller
// frees the string.
static char *clack_user_default_json(const char *key, const char *typ) {
	@autoreleasepool {
		NSUserDefaults *d = [NSUserDefaults standardUserDefaults];
		NSString *k = [NSString stringWithUTF8String:key];
		NSString *t = [NSString stringWithUTF8String:typ];
		id v = [d objectForKey:k];
		if (v == nil) {
			return NULL;
		}
		id out = nil;
		if ([t isEqualToString:@"string"]) {
			out = [v isKindOfClass:[NSString class]] ? v : nil;
		} else if ([t isEqualToString:@"boolean"]) {
			out = [NSNumber numberWithBool:[d boolForKey:k]];
		} else if ([t isEqualToString:@"integer"]) {
			out = [NSNumber numberWithLongLong:(long long)[d integerForKey:k]];
		} else if ([t isEqualToString:@"float"] || [t isEqualToString:@"double"]) {
			out = [NSNumber numberWithDouble:[d doubleForKey:k]];
		} else if ([t isEqualToString:@"url"]) {
			NSURL *u = [d URLForKey:k];
			out = u ? [u absoluteString] : nil;
		} else if ([t isEqualToString:@"array"]) {
			out = [v isKindOfClass:[NSArray class]] ? v : nil;
		} else if ([t isEqualToString:@"dictionary"]) {
			out = [v isKindOfClass:[NSDictionary class]] ? v : nil;
		}
		if (out == nil) {
			return NULL;
		}
		NSArray *wrap = @[ out ];
		if (![NSJSONSerialization isValidJSONObject:wrap]) {
			return NULL;
		}
		NSData *j = [NSJSONSerialization dataWithJSONObject:wrap options:0 error:nil];
		if (j == nil) {
			return NULL;
		}
		char *s = malloc([j length] + 1);
		memcpy(s, [j bytes], [j length]);
		s[[j length]] = 0;
		return s;
	}
}
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

// UserDefault is Electron's systemPreferences.getUserDefault: key's value
// in the user's defaults as typ ("string", "boolean", "integer", "float",
// "double", "url", "array", "dictionary"), nil when it's unset or of
// another type.
func UserDefault(key, typ string) any {
	k, t := C.CString(key), C.CString(typ)
	defer C.free(unsafe.Pointer(k))
	defer C.free(unsafe.Pointer(t))
	s := C.clack_user_default_json(k, t)
	if s == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(s))
	var v []any
	if json.Unmarshal([]byte(C.GoString(s)), &v) != nil || len(v) != 1 {
		return nil
	}
	return v[0]
}
