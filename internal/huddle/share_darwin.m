//go:build darwin && cgo

// Screen capture on macOS (share_darwin.go) through ScreenCaptureKit,
// what Electron's desktopCapturer uses on macOS 12.3+. The framework is
// loaded at run time and its classes looked up by name, so Clack still
// starts on macOS 11 (it then can't share). Frames arrive as NV12 on a
// serial queue and are copied out; system audio (macOS 13+) arrives as
// float PCM, converted to 16-bit stereo. The presenter's border is a
// borderless window on the main thread that SCK never sees (excluded
// from the stream's filter, and NSWindowSharingNone).

#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

extern void clackSCFrame(uintptr_t h, void *y, int ys, void *uv, int uvs, int w, int hh);
extern void clackSCAudio(uintptr_t h, int16_t *pcm, int frames);
extern void clackSCStopped(uintptr_t h, char *err);

typedef struct {
	int kind; // 0 display, 1 window
	uint64_t id;
	char name[256];
	char app[128];
	int x, y, w, h; // frame in global points, top-left origin
	int tw, th;
	uint8_t *thumb; // RGBA, tw*th*4 (malloc'd)
} clackSCSource;

static int loadSCK(void) {
	static int ok = -1;
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		void *h = dlopen("/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit", RTLD_LAZY);
		ok = h != NULL && NSClassFromString(@"SCShareableContent") != nil;
	});
	return ok;
}

int clack_sc_supported(void) {
	if (@available(macOS 12.3, *)) {
		return loadSCK();
	}
	return 0;
}

// clack_sc_access: Screen Recording granted (CGPreflightScreenCaptureAccess).
int clack_sc_access(void) {
	if (@available(macOS 10.15, *)) {
		return CGPreflightScreenCaptureAccess() ? 1 : 0;
	}
	return 1;
}

// clack_sc_request shows the OS's Screen Recording prompt the first time
// (or lists Clack in System Settings), without waiting.
void clack_sc_request(void) {
	if (@available(macOS 10.15, *)) {
		dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
			CGRequestScreenCaptureAccess();
		});
	}
}

static NSMutableSet<NSNumber *> *excluded; // CGWindowIDs no capture shows (the border, the mini panel)
static NSLock *excludedLock;

API_AVAILABLE(macos(12.3))
static SCShareableContent *content(NSError **err) {
	__block SCShareableContent *got = nil;
	__block NSError *e = nil;
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	[(id)NSClassFromString(@"SCShareableContent") getShareableContentExcludingDesktopWindows:YES
	                                                                  onScreenWindowsOnly:YES
	                                                                    completionHandler:^(SCShareableContent *c, NSError *er) {
		                                                                    got = c;
		                                                                    e = er;
		                                                                    dispatch_semaphore_signal(sem);
	                                                                    }];
	if (dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) != 0) {
		*err = [NSError errorWithDomain:@"clack" code:-1 userInfo:@{NSLocalizedDescriptionKey : @"timed out listing screens"}];
		return nil;
	}
	*err = e;
	return got;
}

static char *dupErr(NSError *e) {
	NSString *s = e.localizedDescription ?: @"error";
	return strdup(s.UTF8String);
}

// thumbOf draws img into an RGBA buffer fitted in tw×th.
static uint8_t *thumbOf(CGImageRef img, int tw, int th, int *ow, int *oh) {
	size_t w = CGImageGetWidth(img), h = CGImageGetHeight(img);
	if (w == 0 || h == 0) {
		return NULL;
	}
	double s = MIN(1.0, MIN((double)tw / w, (double)th / h));
	int dw = MAX(1, (int)(w * s)), dh = MAX(1, (int)(h * s));
	uint8_t *buf = calloc((size_t)dw * dh, 4);
	CGColorSpaceRef cs = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
	CGContextRef ctx = CGBitmapContextCreate(buf, dw, dh, 8, dw * 4, cs, kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
	CGColorSpaceRelease(cs);
	if (ctx == NULL) {
		free(buf);
		return NULL;
	}
	CGContextSetInterpolationQuality(ctx, kCGInterpolationHigh);
	CGContextDrawImage(ctx, CGRectMake(0, 0, dw, dh), img);
	CGContextRelease(ctx);
	*ow = dw;
	*oh = dh;
	return buf;
}

// clack_sc_list lists displays (kind 0) or windows (1) with thumbnails
// (macOS 14+: SCScreenshotManager). It returns 0, 1 when Screen
// Recording isn't allowed, 2 when ScreenCaptureKit is missing, or -1.
int clack_sc_list(int kind, int tw, int th, clackSCSource **out, int *n, char **err) {
	*out = NULL;
	*n = 0;
	if (!clack_sc_supported()) {
		return 2;
	}
	if (!clack_sc_access()) {
		return 1;
	}
	if (@available(macOS 12.3, *)) {
		@autoreleasepool {
			NSError *e = nil;
			SCShareableContent *c = content(&e);
			if (c == nil) {
				*err = dupErr(e);
				return e.code == -3801 ? 1 : -1; // SCStreamErrorUserDeclined
			}
			NSMutableArray *items = [NSMutableArray array];
			if (kind == 0) {
				[items addObjectsFromArray:c.displays];
			} else {
				pid_t me = getpid();
				[excludedLock lock];
				NSSet *ex = [excluded copy];
				[excludedLock unlock];
				for (SCWindow *w in c.windows) {
					if (w.windowLayer != 0 || !w.onScreen || w.frame.size.width < 50 || w.frame.size.height < 50) {
						continue;
					}
					if ([ex containsObject:@(w.windowID)] || (w.owningApplication.processID == me && w.title.length == 0)) {
						continue;
					}
					if (w.title.length == 0 && w.owningApplication.applicationName.length == 0) {
						continue;
					}
					[items addObject:w];
				}
			}
			clackSCSource *l = calloc(items.count ? items.count : 1, sizeof *l);
			dispatch_group_t g = dispatch_group_create();
			for (NSUInteger i = 0; i < items.count; i++) {
				clackSCSource *s = &l[i];
				SCContentFilter *f = nil;
				CGRect fr;
				if (kind == 0) {
					SCDisplay *d = items[i];
					s->kind = 0;
					s->id = d.displayID;
					fr = d.frame;
					f = [[NSClassFromString(@"SCContentFilter") alloc] initWithDisplay:d excludingWindows:@[]];
				} else {
					SCWindow *w = items[i];
					s->kind = 1;
					s->id = w.windowID;
					fr = w.frame;
					snprintf(s->name, sizeof s->name, "%s", (w.title.length ? w.title : w.owningApplication.applicationName).UTF8String ?: "");
					snprintf(s->app, sizeof s->app, "%s", w.owningApplication.applicationName.UTF8String ?: "");
					f = [[NSClassFromString(@"SCContentFilter") alloc] initWithDesktopIndependentWindow:w];
				}
				s->x = (int)fr.origin.x;
				s->y = (int)fr.origin.y;
				s->w = (int)fr.size.width;
				s->h = (int)fr.size.height;
				if (@available(macOS 14.0, *)) {
					SCStreamConfiguration *cfg = [NSClassFromString(@"SCStreamConfiguration") new];
					double sc = MIN(1.0, MIN((double)tw * 2 / MAX(1, s->w), (double)th * 2 / MAX(1, s->h)));
					cfg.width = MAX(2, (size_t)(s->w * sc));
					cfg.height = MAX(2, (size_t)(s->h * sc));
					cfg.showsCursor = NO;
					dispatch_group_enter(g);
					[(id)NSClassFromString(@"SCScreenshotManager") captureImageWithFilter:f
					                                                       configuration:cfg
					                                                   completionHandler:^(CGImageRef img, NSError *er) {
						                                                   if (img != NULL) {
							                                                   s->thumb = thumbOf(img, tw, th, &s->tw, &s->th);
						                                                   }
						                                                   dispatch_group_leave(g);
					                                                   }];
				}
			}
			if (dispatch_group_wait(g, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) != 0) {
				// a screenshot is late: its handler still writes into l, so
				// hand back a copy and leave l to it (thumbnails done by now
				// move to the copy; the late ones are lost)
				clackSCSource *cp = calloc(items.count ? items.count : 1, sizeof *cp);
				memcpy(cp, l, items.count * sizeof *cp);
				for (NSUInteger i = 0; i < items.count; i++) {
					l[i].thumb = NULL;
				}
				l = cp;
			}
			*out = l;
			*n = (int)items.count;
			return 0;
		}
	}
	return 2;
}

void clack_sc_free_list(clackSCSource *l, int n) {
	for (int i = 0; i < n; i++) {
		free(l[i].thumb);
	}
	free(l);
}

API_AVAILABLE(macos(12.3))
@interface ClackSCOutput : NSObject <SCStreamOutput, SCStreamDelegate>
@property(nonatomic) uintptr_t handle;
@property(nonatomic) int16_t *pcm;
@property(nonatomic) size_t pcmCap;
@end

@implementation ClackSCOutput
- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sb ofType:(SCStreamOutputType)type {
	if (!CMSampleBufferIsValid(sb)) {
		return;
	}
	if (type == SCStreamOutputTypeScreen) {
		CFArrayRef att = CMSampleBufferGetSampleAttachmentsArray(sb, false);
		if (att == NULL || CFArrayGetCount(att) == 0) {
			return;
		}
		static NSString **statusKey;
		static dispatch_once_t once;
		dispatch_once(&once, ^{
			statusKey = (NSString **)dlsym(RTLD_DEFAULT, "SCStreamFrameInfoStatus");
		});
		if (statusKey != NULL && *statusKey != nil) {
			NSDictionary *d = (__bridge NSDictionary *)CFArrayGetValueAtIndex(att, 0);
			NSNumber *st = d[*statusKey];
			if (st == nil || st.integerValue != 0) { // SCFrameStatusComplete
				return;
			}
		}
		CVPixelBufferRef pb = CMSampleBufferGetImageBuffer(sb);
		if (pb == NULL || CVPixelBufferGetPlaneCount(pb) < 2) {
			return;
		}
		CVPixelBufferLockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
		clackSCFrame(self.handle, CVPixelBufferGetBaseAddressOfPlane(pb, 0), (int)CVPixelBufferGetBytesPerRowOfPlane(pb, 0),
		             CVPixelBufferGetBaseAddressOfPlane(pb, 1), (int)CVPixelBufferGetBytesPerRowOfPlane(pb, 1),
		             (int)CVPixelBufferGetWidth(pb), (int)CVPixelBufferGetHeight(pb));
		CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
		return;
	}
	if (@available(macOS 13.0, *)) {
		if (type != SCStreamOutputTypeAudio) {
			return;
		}
		CMFormatDescriptionRef fd = CMSampleBufferGetFormatDescription(sb);
		const AudioStreamBasicDescription *asbd = fd ? CMAudioFormatDescriptionGetStreamBasicDescription(fd) : NULL;
		if (asbd == NULL || asbd->mBitsPerChannel != 32 || !(asbd->mFormatFlags & kAudioFormatFlagIsFloat)) {
			return;
		}
		struct {
			AudioBufferList l;
			AudioBuffer more[7];
		} abl;
		CMBlockBufferRef bb = NULL;
		if (CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(sb, NULL, &abl.l, sizeof abl, NULL, NULL,
		                                                            kCMSampleBufferFlag_AudioBufferList_Assure16ByteAlignment, &bb) != noErr) {
			return;
		}
		int frames = (int)CMSampleBufferGetNumSamples(sb);
		int ch = (int)asbd->mChannelsPerFrame;
		BOOL planar = (asbd->mFormatFlags & kAudioFormatFlagIsNonInterleaved) != 0;
		if (self.pcmCap < (size_t)frames * 2) {
			free(self.pcm);
			self.pcmCap = (size_t)frames * 2;
			self.pcm = malloc(self.pcmCap * sizeof(int16_t));
		}
		for (int i = 0; i < frames; i++) {
			for (int c = 0; c < 2; c++) {
				int src = MIN(c, ch - 1);
				float v = 0;
				if (planar && (UInt32)src < abl.l.mNumberBuffers) {
					v = ((float *)abl.l.mBuffers[src].mData)[i];
				} else if (!planar && abl.l.mNumberBuffers > 0) {
					v = ((float *)abl.l.mBuffers[0].mData)[i * ch + src];
				}
				v = v > 1 ? 1 : v < -1 ? -1 : v;
				self.pcm[2 * i + c] = (int16_t)(v * 32767);
			}
		}
		CFRelease(bb);
		clackSCAudio(self.handle, self.pcm, frames);
	}
}

- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
	clackSCStopped(self.handle, dupErr(error));
}

- (void)dealloc {
	free(_pcm);
}
@end

typedef struct {
	void *stream; // SCStream, retained
	void *out;    // ClackSCOutput, retained
	int kind;
	uint64_t id;
} clackSC;

API_AVAILABLE(macos(12.3))
static NSArray *excludedWindows(SCShareableContent *c) {
	[excludedLock lock];
	NSSet *ex = [excluded copy];
	[excludedLock unlock];
	NSMutableArray *out = [NSMutableArray array];
	for (SCWindow *w in c.windows) {
		if ([ex containsObject:@(w.windowID)]) {
			[out addObject:w];
		}
	}
	return out;
}

// clack_sc_start captures a display (kind 0) or a window at w×h pixels,
// 15 frames a second, with the cursor, and system audio (macOS 13+) when
// audio is set. NULL with *err on failure (*denied when Screen Recording
// isn't allowed, *gone when the source isn't there).
void *clack_sc_start(int kind, uint64_t id, int w, int h, int audio, uintptr_t handle, char **err, int *denied, int *gone) {
	*denied = *gone = 0;
	if (!clack_sc_supported()) {
		*err = strdup("ScreenCaptureKit needs macOS 12.3 or later");
		return NULL;
	}
	if (!clack_sc_access()) {
		*denied = 1;
		*err = strdup("Screen Recording isn't allowed");
		return NULL;
	}
	if (@available(macOS 12.3, *)) {
		@autoreleasepool {
			NSError *e = nil;
			SCShareableContent *c = content(&e);
			if (c == nil) {
				*denied = e.code == -3801;
				*err = dupErr(e);
				return NULL;
			}
			SCContentFilter *f = nil;
			if (kind == 0) {
				for (SCDisplay *d in c.displays) {
					if (d.displayID == id) {
						f = [[NSClassFromString(@"SCContentFilter") alloc] initWithDisplay:d excludingWindows:excludedWindows(c)];
					}
				}
			} else {
				for (SCWindow *win in c.windows) {
					if (win.windowID == id) {
						f = [[NSClassFromString(@"SCContentFilter") alloc] initWithDesktopIndependentWindow:win];
					}
				}
			}
			if (f == nil) {
				*gone = 1;
				*err = strdup("the screen or window is gone");
				return NULL;
			}
			SCStreamConfiguration *cfg = [NSClassFromString(@"SCStreamConfiguration") new];
			cfg.width = w;
			cfg.height = h;
			cfg.minimumFrameInterval = CMTimeMake(1, 15);
			cfg.pixelFormat = kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
			cfg.showsCursor = YES;
			cfg.queueDepth = 5;
			if (@available(macOS 13.0, *)) {
				if (audio) {
					cfg.capturesAudio = YES;
					cfg.excludesCurrentProcessAudio = YES; // not the huddle's own sound
					cfg.sampleRate = 48000;
					cfg.channelCount = 2;
				}
			}
			ClackSCOutput *o = [ClackSCOutput new];
			o.handle = handle;
			SCStream *s = [[NSClassFromString(@"SCStream") alloc] initWithFilter:f configuration:cfg delegate:o];
			dispatch_queue_t q = dispatch_queue_create("clack.screenshare", DISPATCH_QUEUE_SERIAL);
			if (![s addStreamOutput:o type:SCStreamOutputTypeScreen sampleHandlerQueue:q error:&e]) {
				*err = dupErr(e);
				return NULL;
			}
			if (@available(macOS 13.0, *)) {
				if (audio && ![s addStreamOutput:o type:SCStreamOutputTypeAudio sampleHandlerQueue:q error:&e]) {
					*err = dupErr(e);
					return NULL;
				}
			}
			__block NSError *se = nil;
			dispatch_semaphore_t sem = dispatch_semaphore_create(0);
			[s startCaptureWithCompletionHandler:^(NSError *er) {
				se = er;
				dispatch_semaphore_signal(sem);
			}];
			if (dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) != 0) {
				*err = strdup("timed out starting the capture");
				return NULL;
			}
			if (se != nil) {
				*denied = se.code == -3801;
				*err = dupErr(se);
				return NULL;
			}
			clackSC *p = calloc(1, sizeof *p);
			p->stream = (__bridge_retained void *)s;
			p->out = (__bridge_retained void *)o;
			p->kind = kind;
			p->id = id;
			return p;
		}
	}
	return NULL;
}

// clack_sc_refilter re-applies the exclusions to a display capture (the
// mini panel opened after the stream started).
void clack_sc_refilter(void *p) {
	clackSC *c = p;
	if (c == NULL || c->kind != 0) {
		return;
	}
	if (@available(macOS 12.3, *)) {
		@autoreleasepool {
			NSError *e = nil;
			SCShareableContent *sc = content(&e);
			if (sc == nil) {
				return;
			}
			for (SCDisplay *d in sc.displays) {
				if (d.displayID == c->id) {
					SCContentFilter *f = [[NSClassFromString(@"SCContentFilter") alloc] initWithDisplay:d excludingWindows:excludedWindows(sc)];
					[(__bridge SCStream *)c->stream updateContentFilter:f completionHandler:^(NSError *er){
					}];
				}
			}
		}
	}
}

void clack_sc_stop(void *p) {
	clackSC *c = p;
	if (c == NULL) {
		return;
	}
	if (@available(macOS 12.3, *)) {
		SCStream *s = (__bridge_transfer SCStream *)c->stream;
		ClackSCOutput *o = (__bridge_transfer ClackSCOutput *)c->out;
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		[s stopCaptureWithCompletionHandler:^(NSError *er) {
			dispatch_semaphore_signal(sem);
		}];
		dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
		o.handle = 0;
		(void)o;
	}
	free(c);
}

// clack_sc_exclude adds (or removes) a window every capture leaves out.
void clack_sc_exclude(uint32_t wid, int on) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		excluded = [NSMutableSet set];
		excludedLock = [NSLock new];
	});
	[excludedLock lock];
	if (on) {
		[excluded addObject:@(wid)];
	} else {
		[excluded removeObject:@(wid)];
	}
	[excludedLock unlock];
}

// clack_sc_display_pixels is a display's size in pixels (its current mode).
void clack_sc_display_pixels(uint32_t id, int *w, int *h) {
	CGDisplayModeRef m = CGDisplayCopyDisplayMode(id);
	if (m != NULL) {
		*w = (int)CGDisplayModeGetPixelWidth(m);
		*h = (int)CGDisplayModeGetPixelHeight(m);
		CGDisplayModeRelease(m);
	}
}

// clack_sc_backing_scale is the scale of the screen containing the global
// point x, y (top-left origin), 2 when unknown.
double clack_sc_backing_scale(int x, int y) {
	__block double s = 2;
	void (^find)(void) = ^{
		CGFloat mainH = NSScreen.screens.count ? NSScreen.screens[0].frame.size.height : 0;
		NSPoint p = NSMakePoint(x, mainH - y);
		for (NSScreen *sc in NSScreen.screens) {
			if (NSPointInRect(p, sc.frame)) {
				s = sc.backingScaleFactor;
			}
		}
	};
	if ([NSThread isMainThread]) {
		find();
	} else {
		dispatch_sync(dispatch_get_main_queue(), find);
	}
	return s;
}

// ---- the presenter's border (BIqb: createBorderWindow, width 6, colour
// 0, 255/265, 0.2, 1, showBorder; hideBorder) ----

static NSWindow *border;
static NSTimer *borderFollow;
static int borderKind;
static uint64_t borderID;

static NSRect cocoaRect(CGRect r) {
	CGFloat mainH = NSScreen.screens.count ? NSScreen.screens[0].frame.size.height : 0;
	return NSMakeRect(r.origin.x, mainH - r.origin.y - r.size.height, r.size.width, r.size.height);
}

static BOOL borderTarget(NSRect *out) {
	if (borderKind == 0) {
		for (NSScreen *sc in NSScreen.screens) {
			NSNumber *n = sc.deviceDescription[@"NSScreenNumber"];
			if (n.unsignedIntValue == (uint32_t)borderID) {
				*out = sc.frame;
				return YES;
			}
		}
		return NO;
	}
	CFArrayRef info = CGWindowListCopyWindowInfo(kCGWindowListOptionIncludingWindow, (CGWindowID)borderID);
	BOOL ok = NO;
	if (info != NULL && CFArrayGetCount(info) > 0) {
		NSDictionary *d = (__bridge NSDictionary *)CFArrayGetValueAtIndex(info, 0);
		CGRect r;
		if (CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)d[(id)kCGWindowBounds], &r)) {
			*out = cocoaRect(r);
			ok = [d[(id)kCGWindowIsOnscreen] boolValue];
		}
	}
	if (info != NULL) {
		CFRelease(info);
	}
	return ok;
}

static void placeBorder(void) {
	NSRect r;
	if (!borderTarget(&r)) {
		[border orderOut:nil];
		return;
	}
	if (!NSEqualRects(border.frame, r)) {
		[border setFrame:r display:YES];
	}
	if (!border.visible) {
		[border orderFrontRegardless];
	}
}

void clack_border_show(int kind, uint64_t id, double width, double r, double g, double b, double a) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (border == nil) {
			border = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 100, 100)
			                                     styleMask:NSWindowStyleMaskBorderless
			                                       backing:NSBackingStoreBuffered
			                                         defer:NO];
			border.releasedWhenClosed = NO;
			border.opaque = NO;
			border.backgroundColor = NSColor.clearColor;
			border.hasShadow = NO;
			border.ignoresMouseEvents = YES;
			border.level = NSScreenSaverWindowLevel;
			border.sharingType = NSWindowSharingNone;
			border.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary |
			                            NSWindowCollectionBehaviorFullScreenAuxiliary | NSWindowCollectionBehaviorIgnoresCycle;
			NSView *v = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 100, 100)];
			v.wantsLayer = YES;
			v.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
			border.contentView = v;
			clack_sc_exclude((uint32_t)border.windowNumber, 1);
		}
		border.contentView.layer.borderWidth = width;
		border.contentView.layer.borderColor = [NSColor colorWithSRGBRed:r green:g blue:b alpha:a].CGColor;
		borderKind = kind;
		borderID = id;
		placeBorder();
		[borderFollow invalidate];
		borderFollow = nil;
		if (kind == 1) { // a window moves: follow it
			borderFollow = [NSTimer scheduledTimerWithTimeInterval:0.1
			                                               repeats:YES
			                                                 block:^(NSTimer *t) {
				                                                 placeBorder();
			                                                 }];
		}
	});
}

void clack_border_hide(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[borderFollow invalidate];
		borderFollow = nil;
		[border orderOut:nil];
	});
}

// ---- the presenter's mini panel (HuddleMiniPanelMounter's window) ----

// clack_panel_setup makes the Gio window behind view the mini panel:
// above everything ("screen-saver" level, setAlwaysOnTop), on every
// Space, out of captures (setContentProtection), at x, y (global points,
// top-left) w×h.
void clack_panel_setup(uintptr_t view, int x, int y, int w, int h) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSView *v = (__bridge NSView *)(void *)view;
		NSWindow *win = v.window;
		if (win == nil) {
			return;
		}
		win.level = NSScreenSaverWindowLevel;
		win.sharingType = NSWindowSharingNone;
		win.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
		win.movableByWindowBackground = YES;
		[win setFrame:cocoaRect(CGRectMake(x, y, w, h)) display:YES];
		clack_sc_exclude((uint32_t)win.windowNumber, 1);
	});
}
