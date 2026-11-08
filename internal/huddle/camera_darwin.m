//go:build darwin && cgo

#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

extern void clackCamFrame(uintptr_t h, void *y, int ys, void *uv, int uvs, int w, int hh, int full);

// The capture queue's delegate: lock the NV12 buffer, let Go copy it,
// unlock. Go never blocks here (a full channel drops the frame).
@interface ClackCamDelegate : NSObject <AVCaptureVideoDataOutputSampleBufferDelegate>
@property(nonatomic) uintptr_t handle;
@end

@implementation ClackCamDelegate
- (void)captureOutput:(AVCaptureOutput *)output
    didOutputSampleBuffer:(CMSampleBufferRef)sb
           fromConnection:(AVCaptureConnection *)conn {
	CVImageBufferRef pb = CMSampleBufferGetImageBuffer(sb);
	if (pb == NULL || CVPixelBufferGetPlaneCount(pb) != 2) {
		return;
	}
	if (CVPixelBufferLockBaseAddress(pb, kCVPixelBufferLock_ReadOnly) != kCVReturnSuccess) {
		return;
	}
	OSType fmt = CVPixelBufferGetPixelFormatType(pb);
	clackCamFrame(self.handle,
	              CVPixelBufferGetBaseAddressOfPlane(pb, 0), (int)CVPixelBufferGetBytesPerRowOfPlane(pb, 0),
	              CVPixelBufferGetBaseAddressOfPlane(pb, 1), (int)CVPixelBufferGetBytesPerRowOfPlane(pb, 1),
	              (int)CVPixelBufferGetWidth(pb), (int)CVPixelBufferGetHeight(pb),
	              fmt == kCVPixelFormatType_420YpCbCr8BiPlanarFullRange);
	CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
}
@end

typedef struct {
	void *session;  // AVCaptureSession, retained
	void *output;   // AVCaptureVideoDataOutput, retained
	void *delegate; // ClackCamDelegate, retained
	void *queue;    // dispatch_queue_t, retained
} clackCam;

static char *dupErr(NSString *s) { return strdup(s.UTF8String ? s.UTF8String : "camera error"); }

// clack_cam_open starts the default camera at about 960x540 (Chime's
// default camera size), 15 fps. It blocks while the session starts, so Go
// calls it off the main thread and the UI goroutine. NULL + *err on failure.
void *clack_cam_open(uintptr_t h, char **err) {
	@autoreleasepool {
		AVCaptureDevice *dev = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
		if (dev == nil) {
			*err = dupErr(@"no camera found");
			return NULL;
		}
		NSError *e = nil;
		AVCaptureDeviceInput *in = [AVCaptureDeviceInput deviceInputWithDevice:dev error:&e];
		if (in == nil) {
			*err = dupErr(e ? e.localizedDescription : @"can't open the camera");
			return NULL;
		}
		AVCaptureSession *s = [[AVCaptureSession alloc] init];
		[s beginConfiguration];
		NSArray *presets = @[ AVCaptureSessionPreset960x540, AVCaptureSessionPreset1280x720, AVCaptureSessionPreset640x480 ];
		for (NSString *p in presets) {
			if ([s canSetSessionPreset:p]) {
				s.sessionPreset = p;
				break;
			}
		}
		if (![s canAddInput:in]) {
			[s commitConfiguration];
			*err = dupErr(@"the camera is busy or unavailable");
			return NULL;
		}
		[s addInput:in];
		AVCaptureVideoDataOutput *o = [[AVCaptureVideoDataOutput alloc] init];
		o.videoSettings = @{(id)kCVPixelBufferPixelFormatTypeKey : @(kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange)};
		o.alwaysDiscardsLateVideoFrames = YES;
		ClackCamDelegate *d = [[ClackCamDelegate alloc] init];
		d.handle = h;
		dispatch_queue_t q = dispatch_queue_create("clack.huddle.camera", DISPATCH_QUEUE_SERIAL);
		[o setSampleBufferDelegate:d queue:q];
		if (![s canAddOutput:o]) {
			[s commitConfiguration];
			*err = dupErr(@"can't capture from this camera");
			return NULL;
		}
		[s addOutput:o];
		// 15 fps when the format allows it (else the camera's own rate)
		if ([dev lockForConfiguration:nil]) {
			for (AVFrameRateRange *r in dev.activeFormat.videoSupportedFrameRateRanges) {
				if (r.minFrameRate <= 15 && r.maxFrameRate >= 15) {
					dev.activeVideoMinFrameDuration = CMTimeMake(1, 15);
					dev.activeVideoMaxFrameDuration = CMTimeMake(1, 15);
					break;
				}
			}
			[dev unlockForConfiguration];
		}
		[s commitConfiguration];
		[s startRunning];
		if (!s.running) {
			[o setSampleBufferDelegate:nil queue:NULL];
			*err = dupErr(@"the camera didn't start (in use by another app?)");
			return NULL;
		}
		clackCam *c = calloc(1, sizeof *c);
		c->session = (__bridge_retained void *)s;
		c->output = (__bridge_retained void *)o;
		c->delegate = (__bridge_retained void *)d;
		c->queue = (__bridge_retained void *)q;
		return c;
	}
}

// clack_cam_close stops capture and returns once no frame callback can
// still be running (so Go may free the handle).
void clack_cam_close(void *p) {
	clackCam *c = p;
	@autoreleasepool {
		AVCaptureSession *s = (__bridge_transfer AVCaptureSession *)c->session;
		AVCaptureVideoDataOutput *o = (__bridge_transfer AVCaptureVideoDataOutput *)c->output;
		ClackCamDelegate *d = (__bridge_transfer ClackCamDelegate *)c->delegate;
		dispatch_queue_t q = (__bridge_transfer dispatch_queue_t)c->queue;
		[s stopRunning];
		[o setSampleBufferDelegate:nil queue:NULL];
		dispatch_sync(q, ^{}); // drain a callback already in flight
		(void)d;
	}
	free(c);
}
