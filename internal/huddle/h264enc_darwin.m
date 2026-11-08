//go:build darwin && cgo

#import <CoreFoundation/CoreFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#import <VideoToolbox/VideoToolbox.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

extern void clackVTOutput(uintptr_t h, void *data, int len, int key);

typedef struct {
	VTCompressionSessionRef s;
	uintptr_t h;
	int w, hh;
} clackVT;

static void appendBytes(uint8_t **buf, size_t *len, size_t *cap, const void *p, size_t n) {
	if (*len + n > *cap) {
		size_t c = (*cap ? *cap * 2 : 4096);
		while (c < *len + n) {
			c *= 2;
		}
		*buf = realloc(*buf, c);
		*cap = c;
	}
	memcpy(*buf + *len, p, n);
	*len += n;
}

// output runs on a VideoToolbox thread: convert the AVCC sample to Annex B
// (parameter sets first on a keyframe) and hand a copy to Go, which only
// offers it to a buffered channel.
static void output(void *refcon, void *frameRefcon, OSStatus status, VTEncodeInfoFlags flags, CMSampleBufferRef sb) {
	clackVT *e = refcon;
	if (status != noErr || sb == NULL || !CMSampleBufferDataIsReady(sb) || (flags & kVTEncodeInfo_FrameDropped)) {
		return;
	}
	int key = 1;
	CFArrayRef att = CMSampleBufferGetSampleAttachmentsArray(sb, false);
	if (att != NULL && CFArrayGetCount(att) > 0) {
		CFDictionaryRef d = CFArrayGetValueAtIndex(att, 0);
		CFBooleanRef notSync = NULL;
		if (CFDictionaryGetValueIfPresent(d, kCMSampleAttachmentKey_NotSync, (const void **)&notSync) && notSync != NULL) {
			key = !CFBooleanGetValue(notSync);
		}
	}
	static const uint8_t sc[4] = {0, 0, 0, 1};
	uint8_t *buf = NULL;
	size_t len = 0, cap = 0;
	int lenSize = 4;
	CMFormatDescriptionRef fd = CMSampleBufferGetFormatDescription(sb);
	size_t nps = 0;
	if (fd != NULL && CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fd, 0, NULL, NULL, &nps, &lenSize) != noErr) {
		lenSize = 4;
	}
	if (key && fd != NULL) {
		for (size_t i = 0; i < nps; i++) {
			const uint8_t *ps = NULL;
			size_t psn = 0;
			if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fd, i, &ps, &psn, NULL, NULL) == noErr) {
				appendBytes(&buf, &len, &cap, sc, 4);
				appendBytes(&buf, &len, &cap, ps, psn);
			}
		}
	}
	CMBlockBufferRef bb = CMSampleBufferGetDataBuffer(sb);
	size_t total = bb ? CMBlockBufferGetDataLength(bb) : 0;
	uint8_t *avcc = total ? malloc(total) : NULL;
	if (avcc == NULL || CMBlockBufferCopyDataBytes(bb, 0, total, avcc) != kCMBlockBufferNoErr) {
		free(avcc);
		free(buf);
		return;
	}
	for (size_t off = 0; off + (size_t)lenSize <= total;) {
		size_t n = 0;
		for (int i = 0; i < lenSize; i++) {
			n = n << 8 | avcc[off + i];
		}
		off += lenSize;
		if (n > total - off) {
			break;
		}
		appendBytes(&buf, &len, &cap, sc, 4);
		appendBytes(&buf, &len, &cap, avcc + off, n);
		off += n;
	}
	free(avcc);
	if (len > 0) {
		clackVTOutput(e->h, buf, (int)len, key);
	}
	free(buf);
}

static void setInt(VTCompressionSessionRef s, CFStringRef k, int v) {
	CFNumberRef n = CFNumberCreate(NULL, kCFNumberIntType, &v);
	VTSessionSetProperty(s, k, n);
	CFRelease(n);
}

// clack_vt_new makes a realtime constrained-baseline H.264 session (the
// hardware encoder when there is one, else Apple's software one).
void *clack_vt_new(int w, int h, int fps, int bps, uintptr_t handle, int *status) {
	@autoreleasepool {
		clackVT *e = calloc(1, sizeof *e);
		e->h = handle;
		e->w = w;
		e->hh = h;
		NSDictionary *src = @{
			(id)kCVPixelBufferPixelFormatTypeKey : @(kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange),
			(id)kCVPixelBufferWidthKey : @(w),
			(id)kCVPixelBufferHeightKey : @(h),
			(id)kCVPixelBufferIOSurfacePropertiesKey : @{},
		};
		OSStatus st = VTCompressionSessionCreate(NULL, w, h, kCMVideoCodecType_H264, NULL, (__bridge CFDictionaryRef)src, NULL,
		                                         output, e, &e->s);
		if (st != noErr) {
			*status = (int)st;
			free(e);
			return NULL;
		}
		VTSessionSetProperty(e->s, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
		CFStringRef prof = kVTProfileLevel_H264_Baseline_AutoLevel;
		if (@available(macOS 12.0, *)) {
			prof = kVTProfileLevel_H264_ConstrainedBaseline_AutoLevel;
		}
		if (VTSessionSetProperty(e->s, kVTCompressionPropertyKey_ProfileLevel, prof) != noErr) {
			VTSessionSetProperty(e->s, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_Baseline_AutoLevel);
		}
		VTSessionSetProperty(e->s, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse);
		setInt(e->s, kVTCompressionPropertyKey_ExpectedFrameRate, fps);
		setInt(e->s, kVTCompressionPropertyKey_MaxKeyFrameInterval, fps * 20); // Chime asks (PLI) when it needs one
		setInt(e->s, kVTCompressionPropertyKey_AverageBitRate, bps);
		// at most 1.5x the average over any second
		int limit = bps / 8 * 3 / 2, one = 1;
		CFNumberRef ln = CFNumberCreate(NULL, kCFNumberIntType, &limit), on = CFNumberCreate(NULL, kCFNumberIntType, &one);
		CFTypeRef lim[2] = {ln, on};
		CFArrayRef la = CFArrayCreate(NULL, lim, 2, &kCFTypeArrayCallBacks);
		VTSessionSetProperty(e->s, kVTCompressionPropertyKey_DataRateLimits, la);
		CFRelease(la);
		CFRelease(ln);
		CFRelease(on);
		st = VTCompressionSessionPrepareToEncodeFrames(e->s);
		if (st != noErr) {
			VTCompressionSessionInvalidate(e->s);
			CFRelease(e->s);
			free(e);
			*status = (int)st;
			return NULL;
		}
		*status = 0;
		return e;
	}
}

// clack_vt_encode copies one NV12 picture into a pooled pixel buffer,
// encodes it and waits for its output (no reordering, so that's the frame
// itself): the output callback has run when this returns. Go calls it on
// the camera goroutine, never the UI goroutine.
int clack_vt_encode(void *p, const uint8_t *y, int ys, const uint8_t *uv, int uvs, int64_t ptsUs, int key) {
	clackVT *e = p;
	@autoreleasepool {
		CVPixelBufferPoolRef pool = VTCompressionSessionGetPixelBufferPool(e->s);
		CVPixelBufferRef pb = NULL;
		CVReturn cr = pool ? CVPixelBufferPoolCreatePixelBuffer(NULL, pool, &pb)
		                   : CVPixelBufferCreate(NULL, e->w, e->hh, kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange, NULL, &pb);
		if (cr != kCVReturnSuccess || pb == NULL) {
			return (int)cr ? (int)cr : -1;
		}
		CVPixelBufferLockBaseAddress(pb, 0);
		uint8_t *dy = CVPixelBufferGetBaseAddressOfPlane(pb, 0);
		size_t dys = CVPixelBufferGetBytesPerRowOfPlane(pb, 0);
		for (int r = 0; r < e->hh; r++) {
			memcpy(dy + r * dys, y + (size_t)r * ys, e->w);
		}
		uint8_t *duv = CVPixelBufferGetBaseAddressOfPlane(pb, 1);
		size_t duvs = CVPixelBufferGetBytesPerRowOfPlane(pb, 1);
		for (int r = 0; r < e->hh / 2; r++) {
			memcpy(duv + r * duvs, uv + (size_t)r * uvs, e->w);
		}
		CVPixelBufferUnlockBaseAddress(pb, 0);
		CMTime pts = CMTimeMake(ptsUs, 1000000);
		NSDictionary *props = key ? @{(id)kVTEncodeFrameOptionKey_ForceKeyFrame : @YES} : nil;
		OSStatus st = VTCompressionSessionEncodeFrame(e->s, pb, pts, kCMTimeInvalid, (__bridge CFDictionaryRef)props, NULL, NULL);
		CVPixelBufferRelease(pb);
		if (st != noErr) {
			return (int)st;
		}
		return (int)VTCompressionSessionCompleteFrames(e->s, pts);
	}
}

void clack_vt_bitrate(void *p, int bps) {
	clackVT *e = p;
	setInt(e->s, kVTCompressionPropertyKey_AverageBitRate, bps);
}

void clack_vt_free(void *p) {
	clackVT *e = p;
	VTCompressionSessionCompleteFrames(e->s, kCMTimeInvalid);
	VTCompressionSessionInvalidate(e->s);
	CFRelease(e->s);
	free(e);
}
