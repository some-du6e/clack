//go:build darwin && cgo

package openurl

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#include <stdlib.h>

// clackOpenURL is Electron's platform_util_mac.mm OpenExternal: the URL
// as an NSURL, refused when Launch Services has no application for it
// (so macOS shows no dialog), then NSWorkspace openURL:configuration:
// completionHandler: with activates set. It waits up to wait seconds for
// the completion handler, which runs on a queue of NSWorkspace's own:
// 0 opened, 1 a bad URL, 2 no application, 3 the open failed, 4 no
// answer in time. Manual reference counting (the desktop package's rule):
// the pool drains what's autoreleased, the semaphore is released here.
static int clackOpenURL(const char *s, int activate, double wait) {
	int rc;
	@autoreleasepool {
		NSString *str = [NSString stringWithUTF8String:s];
		if (str == nil) { // not UTF-8 (Check refuses it first)
			return 1;
		}
		NSURL *url = [NSURL URLWithString:str];
		if (url == nil) {
			return 1;
		}
		NSWorkspace *ws = [NSWorkspace sharedWorkspace];
		if ([ws URLForApplicationToOpenURL:url] == nil) {
			return 2;
		}
		NSWorkspaceOpenConfiguration *cfg = [NSWorkspaceOpenConfiguration configuration];
		cfg.activates = activate ? YES : NO;
		dispatch_semaphore_t done = dispatch_semaphore_create(0);
		__block int result = 4;
		[ws openURL:url configuration:cfg completionHandler:^(NSRunningApplication *app, NSError *err) {
			result = err ? 3 : 0;
			dispatch_semaphore_signal(done);
		}];
		if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, (int64_t)(wait * NSEC_PER_SEC))) != 0) {
			rc = 4; // the block may still run: it only writes result and signals
		} else {
			rc = result;
		}
		// the block holds its own reference to done while it is pending
		dispatch_release(done);
	}
	return rc;
}
*/
import "C"

import (
	"errors"
	"log"
	"unsafe"
)

// On macOS a link goes where Slack desktop sends it: its openExternal
// (main.bundle.cjs, darwin 4.53.276) calls Electron's shell.openExternal,
// and its retry after a failure is the same call (`o6 != o6`: there is no
// other way there). Electron's is NSWorkspace openURL (clackOpenURL).
// Clack ran `open <url>`: a process to start and exit before Launch
// Services got the URL (openurl_latency_darwin_test.go measures both).

var errNoApp = errors.New("openurl: no application to open the link")

// shellOpen hands an already-checked URL to NSWorkspace and waits for its
// answer (up to 10 s) off the UI goroutine. activate: Slack opens a
// link's browser in front (activate unless the link asked for a
// background tab, which Clack's links never do).
func shellOpen(u string) error {
	cs := C.CString(nsURLPrecursor(u)) // NSURLWithGURL's escaping
	defer C.free(unsafe.Pointer(cs))
	switch C.clackOpenURL(cs, 1, 10) {
	case 0:
		return nil
	case 1:
		return errors.New("openurl: not a URL")
	case 2:
		return errNoApp
	case 3:
		return errors.New("openurl: NSWorkspace couldn't open the link")
	}
	// no answer in 10 s (a slow first start of the browser, a Gatekeeper
	// prompt): handed over, not failed, so no retry opens it twice;
	// Electron's callback waits on the same answer and Slack retries only
	// on an error
	log.Printf("openurl: NSWorkspace gave no answer in 10 s; taken as opened")
	return nil
}

// retryWithTool: Slack's macOS retry is the same shell.openExternal.
const retryWithTool = false
