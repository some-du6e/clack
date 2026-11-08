//go:build darwin && cgo

#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>
#include <stdint.h>

extern void clackAVPermDone(uintptr_t h);

// usageMissing: Clack runs from an .app bundle (it has a bundle id) whose
// Info.plist lacks the usage string. Asking TCC then kills the process, so
// report it instead. A bare binary run from a terminal has no bundle id:
// TCC attributes it to the terminal app, which has its own strings.
static BOOL usageMissing(int video) {
	NSBundle *b = [NSBundle mainBundle];
	if (b.bundleIdentifier == nil) {
		return NO;
	}
	NSString *key = video ? @"NSCameraUsageDescription" : @"NSMicrophoneUsageDescription";
	return [b objectForInfoDictionaryKey:key] == nil;
}

int clack_avperm_status(int video) {
	if (usageMissing(video)) {
		return -1;
	}
	return (int)[AVCaptureDevice authorizationStatusForMediaType:(video ? AVMediaTypeVideo : AVMediaTypeAudio)];
}

// clack_avperm_request returns at once; the completion handler runs on an
// arbitrary queue and calls back into Go, which never touches the UI.
void clack_avperm_request(int video, uintptr_t h) {
	if (usageMissing(video)) {
		clackAVPermDone(h);
		return;
	}
	[AVCaptureDevice requestAccessForMediaType:(video ? AVMediaTypeVideo : AVMediaTypeAudio)
	                         completionHandler:^(BOOL granted) {
		(void)granted; // Go re-reads the status: denied vs restricted
		clackAVPermDone(h);
	}];
}
