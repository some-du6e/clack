//go:build darwin && cgo

// AVFoundation playing a file Go reads: an AVURLAsset of a made-up URL
// (clack-media://<token>/<id>.<ext>) whose resource loader hands every
// byte range to Go (media.RangeSource over slack.Client.DownloadRange),
// so AVFoundation never fetches anything itself. Frames come from an
// AVPlayerItemVideoOutput (32BGRA, Metal-compatible IOSurfaces at the
// drawn size) as BGRA8Unorm_sRGB textures of Gio's own MTLDevice through
// a CVMetalTextureCache, which Gio draws (patch 51). Everything of
// AVFoundation's runs on the player's serial queue; Gio's ask, on the
// thread drawing the frame, only reads the newest frame under a lock.
//
// A frame handed to Gio at frame N is kept until the ask of frame N+1:
// Gio's Metal BeginFrame waited for frame N's command buffer by then
// (CVMetalTextureCacheCreateTextureFromImage: keep the texture until the
// GPU is done with it).

#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Metal/Metal.h>
#import <QuartzCore/QuartzCore.h>
#include <math.h>
#include <os/lock.h>
#include <stdatomic.h>
#include <time.h>

#include "_cgo_export.h"
#include "avf_darwin.h"

static _Atomic int32_t live[CLACK_AV_LIVE_N];

void clack_avf_live(int32_t *out) {
	for (int i = 0; i < CLACK_AV_LIVE_N; i++) {
		out[i] = atomic_load(&live[i]);
	}
}

static int64_t nowNS(void) { return (int64_t)clock_gettime_nsec_np(CLOCK_UPTIME_RAW); }

// clackEntry is a frame: its pixel buffer and its Metal texture.
typedef struct {
	CVPixelBufferRef pb;
	CVMetalTextureRef tex;
	uint64_t seq;
} clackEntry;

static void releaseEntry(clackEntry *e) {
	if (e->tex != NULL) {
		CFRelease(e->tex);
		e->tex = NULL;
		atomic_fetch_sub(&live[CLACK_AV_LIVE_TEXTURES], 1);
	}
	if (e->pb != NULL) {
		CVPixelBufferRelease(e->pb);
		e->pb = NULL;
		atomic_fetch_sub(&live[CLACK_AV_LIVE_BUFFERS], 1);
	}
	e->seq = 0;
}

static void *kStatusCtx = &kStatusCtx;
static void *kTimeCtx = &kTimeCtx;

// mapError is an NSError's HTML5 MediaError code: 2 network, 4 a format
// or codec AVFoundation can't play, 3 a decode error.
static int mapError(NSError *e) {
	if (e == nil) {
		return 3;
	}
	if ([e.domain isEqualToString:NSURLErrorDomain]) {
		return 2;
	}
	if ([e.domain isEqualToString:AVFoundationErrorDomain]) {
		switch (e.code) {
		case -11828: // AVErrorFileFormatNotRecognized
		case -11829: // AVErrorFileFailedToParse
		case -11833: // AVErrorDecoderNotFound
		case -11838: // AVErrorOperationNotSupportedForAsset
			return 4;
		case -11821: // AVErrorDecodeFailed
			return 3;
		}
		NSError *u = e.userInfo[NSUnderlyingErrorKey];
		if (u != nil && u != e) {
			return mapError(u);
		}
	}
	return 3;
}

@interface ClackAVPlayer : NSObject <AVAssetResourceLoaderDelegate>
- (instancetype)initWithHandle:(uintptr_t)h total:(int64_t)total seekable:(BOOL)seekable uti:(NSString *)uti url:(NSURL *)url drawW:(int)w drawH:(int)hh;
@end

@implementation ClackAVPlayer {
	uintptr_t _h;
	dispatch_queue_t _q;
	NSOperationQueue *_opq;
	AVURLAsset *_asset;
	AVPlayerItem *_item;
	AVPlayer *_player;
	AVPlayerItemVideoOutput *_output;
	int64_t _total;
	BOOL _seekable;
	NSString *_uti;
	NSMutableDictionary<NSNumber *, AVAssetResourceLoadingRequest *> *_requests;
	uint64_t _reqSeq;
	id _endObs, _failObs;
	dispatch_source_t _pull, _watch;
	CVMetalTextureCacheRef _cache;
	id<MTLDevice> _device;
	// shared with the ask, under _lock
	os_unfair_lock _lock;
	clackEntry _newest, _handed;
	uintptr_t _askDevice;
	uint64_t _seq;
	_Atomic int64_t _lastAsk;
	// the queue's
	BOOL _closed, _ready, _wantPlay, _loop, _wantFrame, _firstFrame, _ended, _seekPending;
	double _rate, _volume, _pendingSeek;
	// the frame asked for (_wantFrame): the one at _wantAt (NAN: the next
	// one), until _wantUntilNS (0: no end); _lastPts is the display time of
	// the frame pulled last (-1 none), _seekTarget the seek's
	double _wantAt, _lastPts, _seekTarget;
	int64_t _wantUntilNS;
	BOOL _muted;
	int _drawW, _drawH;
	size_t _outW, _outH;
	NSInteger _lastTime;
	AVPlayerItemStatus _lastStatus;
	double _fps;
	int _err;
	int32_t _errCode;
	int64_t _lastStateNS;
	_Atomic int64_t _pulls, _frames;
	_Atomic uint32_t _pbW, _pbH;
}

- (instancetype)initWithHandle:(uintptr_t)h total:(int64_t)total seekable:(BOOL)seekable uti:(NSString *)uti url:(NSURL *)url drawW:(int)w drawH:(int)hh {
	self = [super init];
	if (self == nil) {
		return nil;
	}
	_h = h;
	_total = total;
	_seekable = seekable;
	_uti = uti;
	_drawW = w;
	_drawH = hh;
	_rate = 1;
	_volume = 1;
	_lastTime = -1;
	_lastPts = -1;
	_wantAt = NAN;
	_lastStatus = AVPlayerItemStatusUnknown;
	_lock = OS_UNFAIR_LOCK_INIT;
	atomic_store(&_lastAsk, nowNS());
	_q = dispatch_queue_create("club.getclack.media", DISPATCH_QUEUE_SERIAL);
	_opq = [[NSOperationQueue alloc] init];
	_opq.underlyingQueue = _q;
	_opq.maxConcurrentOperationCount = 1;
	_requests = [NSMutableDictionary dictionary];
	atomic_fetch_add(&live[CLACK_AV_LIVE_PLAYERS], 1);
	dispatch_async(_q, ^{
		[self setUp:url];
	});
	return self;
}

- (void)dealloc {
	atomic_fetch_sub(&live[CLACK_AV_LIVE_PLAYERS], 1);
}

- (void)setUp:(NSURL *)url {
	_asset = [AVURLAsset URLAssetWithURL:url options:nil];
	[_asset.resourceLoader setDelegate:self queue:_q];
	_item = [AVPlayerItem playerItemWithAsset:_asset];
	_player = [AVPlayer playerWithPlayerItem:_item];
	_player.allowsExternalPlayback = NO;
	_player.actionAtItemEnd = AVPlayerActionAtItemEndPause;
	_player.volume = (float)_volume;
	_player.muted = _muted;
	[_item addObserver:self forKeyPath:@"status" options:0 context:kStatusCtx];
	[_player addObserver:self forKeyPath:@"timeControlStatus" options:0 context:kTimeCtx];
	__weak ClackAVPlayer *w = self;
	NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
	_endObs = [nc addObserverForName:AVPlayerItemDidPlayToEndTimeNotification
	                          object:_item
	                           queue:_opq
	                      usingBlock:^(NSNotification *n) {
		                      [w reachedEnd];
	                      }];
	_failObs = [nc addObserverForName:AVPlayerItemFailedToPlayToEndTimeNotification
	                           object:_item
	                            queue:_opq
	                       usingBlock:^(NSNotification *n) {
		                       [w failedWith:n.userInfo[AVPlayerItemFailedToPlayToEndTimeErrorKey]];
	                       }];
	[self updateTimers];
}

- (void)observeValueForKeyPath:(NSString *)kp ofObject:(id)o change:(NSDictionary *)c context:(void *)ctx {
	if (ctx == kStatusCtx || ctx == kTimeCtx) {
		__weak ClackAVPlayer *w = self;
		dispatch_async(_q, ^{
			[w check];
		});
		return;
	}
	[super observeValueForKeyPath:kp ofObject:o change:c context:ctx];
}

// nativeSize is the video's size: the item's presentation size, or (before
// it's known) the video track's natural size as it's shown.
- (CGSize)nativeSize {
	CGSize ps = _item.presentationSize;
	if (ps.width >= 1 && ps.height >= 1) {
		return ps;
	}
	for (AVPlayerItemTrack *t in _item.tracks) {
		AVAssetTrack *a = t.assetTrack;
		if ([a.mediaType isEqualToString:AVMediaTypeVideo]) {
			CGSize n = CGSizeApplyAffineTransform(a.naturalSize, a.preferredTransform);
			return CGSizeMake(fabs(n.width), fabs(n.height));
		}
	}
	return CGSizeZero;
}

- (clackAVState)state {
	clackAVState s;
	memset(&s, 0, sizeof s);
	CMTime t = _player != nil ? _player.currentTime : kCMTimeInvalid;
	if (CMTIME_IS_NUMERIC(t)) {
		s.pos = CMTimeGetSeconds(t);
	}
	CMTime d = _item != nil ? _item.duration : kCMTimeInvalid;
	if (CMTIME_IS_NUMERIC(d)) {
		s.dur = CMTimeGetSeconds(d);
	}
	s.paused = !_wantPlay || _ended;
	s.ended = _ended;
	s.waiting = _wantPlay && _player != nil && _player.timeControlStatus == AVPlayerTimeControlStatusWaitingToPlayAtSpecifiedRate;
	s.ready = _ready ? 4 : 0;
	s.err = _err;
	s.errCode = _errCode;
	if (_ready) {
		CGSize ps = [self nativeSize];
		s.w = (uint32_t)ps.width;
		s.h = (uint32_t)ps.height;
	}
	s.fps = _fps;
	s.rate = _rate;
	return s;
}

- (void)emit:(int)code {
	if (_closed) {
		return;
	}
	clackAVState s = [self state];
	_lastStateNS = nowNS();
	clackAVFEvent(_h, code, &s);
}

// check reads the item's status and the player's, and fires what changed:
// from KVO, and from the watch timer (AVFoundation may post its changes on
// the main queue, which a test binary doesn't run).
- (void)check {
	if (_closed || _item == nil) {
		return;
	}
	AVPlayerItemStatus st = _item.status;
	if (st != _lastStatus) {
		_lastStatus = st;
		if (st == AVPlayerItemStatusReadyToPlay && !_ready) {
			[self becameReady];
		} else if (st == AVPlayerItemStatusFailed) {
			[self failedWith:_item.error];
		}
	}
	if (_ready && _output == nil) {
		[self makeOutput];
	}
	if (_ready) {
		NSInteger tc = _player.timeControlStatus;
		if (tc != _lastTime) {
			_lastTime = tc;
			if (tc == AVPlayerTimeControlStatusPlaying && _wantPlay) {
				[self emit:CLACK_AV_PLAYING];
			} else if (tc == AVPlayerTimeControlStatusWaitingToPlayAtSpecifiedRate && _wantPlay) {
				[self emit:CLACK_AV_WAITING];
			}
		}
		// the end, should its notification not come
		CMTime d = _item.duration, t = _player.currentTime;
		if (_wantPlay && !_ended && !_seekPending && _player.rate == 0 && CMTIME_IS_NUMERIC(d) && CMTIME_IS_NUMERIC(t) &&
		    CMTimeGetSeconds(t) >= CMTimeGetSeconds(d) - 0.001) {
			[self reachedEnd];
		}
		if (_wantPlay && nowNS() - _lastStateNS > 250000000) {
			[self emit:CLACK_AV_STATE];
		}
	}
	[self updateTimers];
}

- (void)becameReady {
	_ready = YES;
	for (AVPlayerItemTrack *t in _item.tracks) {
		if ([t.assetTrack.mediaType isEqualToString:AVMediaTypeVideo]) {
			_fps = t.assetTrack.nominalFrameRate;
			break;
		}
	}
	[self makeOutput];
	[self emit:CLACK_AV_DURATION];
	[self emit:CLACK_AV_METADATA];
	[self emit:CLACK_AV_DATA];
	if (_seekPending) {
		[self seekTo:_pendingSeek]; // asks for the frame there
	} else {
		[self wantFrameAt:NAN];
	}
	_lastTime = -1;
	[self check];
}

// makeOutput (re)makes the video output at the video's size fitted into
// the drawn size, never bigger than the video.
- (void)makeOutput {
	if (!_ready) {
		return;
	}
	CGSize n = [self nativeSize];
	if (n.width < 1 || n.height < 1) {
		return; // again from check, once the size is known
	}
	size_t w = (size_t)n.width, h = (size_t)n.height;
	if (_drawW > 0 && _drawH > 0 && ((size_t)_drawW < w || (size_t)_drawH < h)) {
		double s = fmin((double)_drawW / (double)w, (double)_drawH / (double)h);
		w = (size_t)MAX(2, lround((double)w * s));
		h = (size_t)MAX(2, lround((double)h * s));
	}
	if (_output != nil && w == _outW && h == _outH) {
		return;
	}
	NSDictionary *attrs = @{
		(id)kCVPixelBufferPixelFormatTypeKey : @(kCVPixelFormatType_32BGRA),
		(id)kCVPixelBufferMetalCompatibilityKey : @YES,
		(id)kCVPixelBufferIOSurfacePropertiesKey : @{},
		(id)kCVPixelBufferWidthKey : @(w),
		(id)kCVPixelBufferHeightKey : @(h),
	};
	AVPlayerItemVideoOutput *o = [[AVPlayerItemVideoOutput alloc] initWithPixelBufferAttributes:attrs];
	if (_output != nil) {
		[_item removeOutput:_output];
	}
	[_item addOutput:o];
	_output = o;
	_outW = w;
	_outH = h;
	if (!_wantFrame) {
		[self wantFrameAt:NAN];
	}
}

- (void)play {
	if (_closed || _err) {
		return;
	}
	BOOL restart = _ended;
	_wantPlay = YES;
	_ended = NO;
	atomic_store(&_lastAsk, nowNS()); // the click: it's on screen
	if (restart) {
		[self seekTo:0];
	}
	_player.preventsDisplaySleepDuringVideoPlayback = YES;
	[_player play];
	if (_rate != 1) {
		_player.rate = (float)_rate;
	}
	_lastTime = -1;
	[self emit:CLACK_AV_PLAY];
	[self check];
}

- (void)pause {
	if (_closed || !_wantPlay) {
		return;
	}
	_wantPlay = NO;
	[_player pause];
	_player.preventsDisplaySleepDuringVideoPlayback = NO;
	[self emit:CLACK_AV_PAUSE];
	[self updateTimers];
}

- (void)seekTo:(double)t {
	if (_closed) {
		return;
	}
	if (!_ready) {
		_seekPending = YES;
		_pendingSeek = t;
		return;
	}
	_seekPending = YES;
	_ended = NO;
	_seekTarget = t;
	[self emit:CLACK_AV_SEEKING];
	__weak ClackAVPlayer *w = self;
	[_player seekToTime:CMTimeMakeWithSeconds(t, 90000)
	      toleranceBefore:kCMTimeZero
	       toleranceAfter:kCMTimeZero
	    completionHandler:^(BOOL finished) {
		    ClackAVPlayer *s = w;
		    if (s == nil) {
			    return;
		    }
		    dispatch_async(s->_q, ^{
			    [s seeked:finished];
		    });
	    }];
	// pulled from now on: the frame there shows once it's decoded, not once
	// the completion comes (AVFoundation runs it on the main queue, which
	// a busy main thread holds up)
	[self wantFrameAt:t];
	[self updateTimers];
}

// wantFrameAt asks for pulls until the frame at t is pulled (NAN: the next
// frame, whatever it is), a seek's for 2 s at most.
- (void)wantFrameAt:(double)t {
	_wantFrame = YES;
	_wantAt = t;
	_wantUntilNS = isnan(t) ? 0 : nowNS() + 2000000000;
}

// frameAt is whether the frame pulled last is the one at t: its display
// time within a frame and a half of it.
- (BOOL)frameAt:(double)t {
	double fps = _fps > 0 ? _fps : 30;
	return _lastPts >= 0 && fabs(_lastPts - t) <= 1.5 / fps;
}

- (void)seeked:(BOOL)finished {
	if (_closed) {
		return;
	}
	_seekPending = NO;
	if (finished) {
		if (![self frameAt:_seekTarget]) {
			[self wantFrameAt:_seekTarget]; // not pulled while it ran
		}
		[self emit:CLACK_AV_SEEKED];
	}
	[self updateTimers];
}

- (void)reachedEnd {
	if (_closed || _ended || !_ready) {
		return;
	}
	if (_loop) {
		[self seekTo:0];
		if (_wantPlay) { // not when a pause came first
			[_player play];
		}
		return;
	}
	_ended = YES;
	_wantPlay = NO;
	_player.preventsDisplaySleepDuringVideoPlayback = NO;
	[self emit:CLACK_AV_PAUSE];
	[self emit:CLACK_AV_ENDED];
	[self updateTimers];
}

- (void)failedWith:(NSError *)e {
	if (_closed || _err) {
		return;
	}
	_err = mapError(e);
	_errCode = (int32_t)e.code;
	[self emit:CLACK_AV_ERROR];
	[self updateTimers];
}

static dispatch_source_t timerOn(dispatch_queue_t q, int64_t every, dispatch_block_t f) {
	dispatch_source_t t = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, q);
	dispatch_source_set_timer(t, dispatch_time(DISPATCH_TIME_NOW, every), (uint64_t)every, (uint64_t)every / 10);
	dispatch_source_set_event_handler(t, f);
	dispatch_resume(t);
	return t;
}

// updateTimers runs the frame pull only while playing and asked for within
// the last second (or for the one frame after the data came or a seek),
// and the state watch only while there is state to watch.
- (void)updateTimers {
	BOOL asked = nowNS() - atomic_load(&_lastAsk) < 1000000000;
	BOOL pull = !_closed && _ready && _output != nil && _cache != NULL && !_err && ((_wantPlay && !_ended && asked) || _wantFrame);
	__weak ClackAVPlayer *w = self;
	if (pull && _pull == nil) {
		double fps = _fps > 0 ? _fps : 30;
		int64_t every = (int64_t)(1e9 / fps / 2);
		every = every < 4000000 ? 4000000 : every > 20000000 ? 20000000 : every;
		_pull = timerOn(_q, every, ^{
			[w pullFrame];
		});
	} else if (!pull && _pull != nil) {
		dispatch_source_cancel(_pull);
		_pull = nil;
	}
	BOOL watch = !_closed && !_err && (!_ready || _output == nil || _wantPlay || _seekPending);
	if (watch && _watch == nil) {
		_watch = timerOn(_q, 50000000, ^{
			[w check];
		});
	} else if (!watch && _watch != nil) {
		dispatch_source_cancel(_watch);
		_watch = nil;
	}
}

- (void)pullFrame {
	if (_closed || _output == nil || _cache == NULL) {
		return;
	}
	atomic_fetch_add(&_pulls, 1);
	CMTime t = [_output itemTimeForHostTime:CACurrentMediaTime()];
	if (![_output hasNewPixelBufferForItemTime:t]) {
		if (_wantFrame && _wantUntilNS != 0 && nowNS() > _wantUntilNS) {
			_wantFrame = NO; // the seek's frame never came
			[self updateTimers];
		} else if (nowNS() - atomic_load(&_lastAsk) >= 1000000000) {
			[self updateTimers];
		}
		return;
	}
	CMTime disp = kCMTimeInvalid;
	CVPixelBufferRef pb = [_output copyPixelBufferForItemTime:t itemTimeForDisplay:&disp];
	if (pb == NULL) {
		return;
	}
	atomic_fetch_add(&live[CLACK_AV_LIVE_BUFFERS], 1);
	size_t w = CVPixelBufferGetWidth(pb), h = CVPixelBufferGetHeight(pb);
	atomic_store(&_pbW, (uint32_t)w);
	atomic_store(&_pbH, (uint32_t)h);
	CVMetalTextureRef tex = NULL;
	CVReturn r = CVMetalTextureCacheCreateTextureFromImage(kCFAllocatorDefault, _cache, pb, NULL, MTLPixelFormatBGRA8Unorm_sRGB, w, h, 0, &tex);
	if (r != kCVReturnSuccess || tex == NULL) {
		CVPixelBufferRelease(pb);
		atomic_fetch_sub(&live[CLACK_AV_LIVE_BUFFERS], 1);
		return;
	}
	atomic_fetch_add(&live[CLACK_AV_LIVE_TEXTURES], 1);
	clackEntry e = {pb, tex, 0};
	os_unfair_lock_lock(&_lock);
	e.seq = ++_seq;
	clackEntry old = _newest;
	_newest = e;
	BOOL handed = old.tex != NULL && old.tex == _handed.tex;
	os_unfair_lock_unlock(&_lock);
	if (old.tex != NULL && !handed) {
		releaseEntry(&old); // superseded before Gio took it
	}
	int64_t n = atomic_fetch_add(&_frames, 1) + 1;
	_lastPts = CMTIME_IS_NUMERIC(disp) ? CMTimeGetSeconds(disp) : -1;
	// the frame asked for (an older one, pulled while a seek ran, isn't),
	// or any once a seek done while playing ended: playing on brings the
	// frames from there (the first one pulled can be a frame or two past
	// the target, and the ask would keep pulls going after a pause)
	if (isnan(_wantAt) || _lastPts < 0 || [self frameAt:_wantAt] || (_wantPlay && !_seekPending) ||
	    (_wantUntilNS != 0 && nowNS() > _wantUntilNS)) {
		_wantFrame = NO;
	}
	if (!_firstFrame) {
		_firstFrame = YES;
		[self emit:CLACK_AV_FIRSTFRAME];
	}
	clackAVFFrame(_h);
	if (n % 120 == 0) {
		CVMetalTextureCacheFlush(_cache, 0);
	}
	[self updateTimers];
}

// useDevice makes the texture cache on the device Gio draws with (each
// Gio window has its own; a dual-GPU Mac's can change).
- (void)useDevice:(id<MTLDevice>)dev {
	if (_closed) {
		return;
	}
	if (_device == dev && _cache != NULL) {
		return;
	}
	if (_cache != NULL) {
		CVMetalTextureCacheFlush(_cache, 0);
		CFRelease(_cache);
		_cache = NULL;
	}
	_device = dev;
	if (CVMetalTextureCacheCreate(kCFAllocatorDefault, NULL, dev, NULL, &_cache) != kCVReturnSuccess) {
		_cache = NULL;
	}
	os_unfair_lock_lock(&_lock);
	clackEntry a = _newest, b = _handed;
	memset(&_newest, 0, sizeof _newest);
	memset(&_handed, 0, sizeof _handed);
	os_unfair_lock_unlock(&_lock);
	if (b.tex != a.tex) {
		releaseEntry(&b);
	}
	releaseEntry(&a);
	[self wantFrameAt:NAN];
	[self updateTimers];
}

// ask is Gio's, on the thread drawing the frame.
- (int)askDevice:(uintptr_t)device tex:(uintptr_t *)tex key:(uint64_t *)key {
	int64_t now = nowNS();
	int64_t last = atomic_exchange(&_lastAsk, now);
	if (now - last >= 1000000000) {
		__weak ClackAVPlayer *w = self;
		dispatch_async(_q, ^{
			[w updateTimers]; // asked again after a gap: the frames go on
		});
	}
	os_unfair_lock_lock(&_lock);
	if (device != _askDevice) {
		_askDevice = device;
		os_unfair_lock_unlock(&_lock);
		id<MTLDevice> dev = (__bridge id<MTLDevice>)(void *)device;
		__weak ClackAVPlayer *w = self;
		dispatch_async(_q, ^{
			[w useDevice:dev];
		});
		return 0;
	}
	if (_newest.tex == NULL) {
		os_unfair_lock_unlock(&_lock);
		return 0;
	}
	clackEntry done = {NULL, NULL, 0};
	if (_handed.tex != _newest.tex) {
		done = _handed; // handed out at the frame before: its GPU work is done
		_handed = _newest;
	}
	// a reference Gio takes over, taken under the lock: a close releasing
	// the frame can't come between
	*tex = (uintptr_t)CFBridgingRetain(CVMetalTextureGetTexture(_handed.tex));
	*key = _handed.seq;
	os_unfair_lock_unlock(&_lock);
	if (done.tex != NULL) {
		releaseEntry(&done);
	}
	return 1;
}

// The resource loader, on the queue: every byte the asset reads is Go's.
- (BOOL)resourceLoader:(AVAssetResourceLoader *)loader shouldWaitForLoadingOfRequestedResource:(AVAssetResourceLoadingRequest *)req {
	if (_closed) {
		return NO;
	}
	AVAssetResourceLoadingContentInformationRequest *ci = req.contentInformationRequest;
	if (ci != nil) {
		ci.contentType = _uti;
		ci.contentLength = _total;
		ci.byteRangeAccessSupported = _seekable;
	}
	AVAssetResourceLoadingDataRequest *dr = req.dataRequest;
	if (dr == nil) {
		[req finishLoading];
		return YES;
	}
	uint64_t rid = ++_reqSeq;
	_requests[@(rid)] = req;
	atomic_fetch_add(&live[CLACK_AV_LIVE_REQUESTS], 1);
	clackAVFLoad(_h, rid, dr.requestedOffset, (int64_t)dr.requestedLength, dr.requestsAllDataToEndOfResource ? 1 : 0);
	return YES;
}

- (void)resourceLoader:(AVAssetResourceLoader *)loader didCancelLoadingRequest:(AVAssetResourceLoadingRequest *)req {
	for (NSNumber *k in [_requests allKeysForObject:req]) {
		[_requests removeObjectForKey:k];
		atomic_fetch_sub(&live[CLACK_AV_LIVE_REQUESTS], 1);
		clackAVFCancel(_h, k.unsignedLongLongValue);
	}
}

- (void)respond:(uint64_t)rid data:(NSData *)d {
	dispatch_async(_q, ^{
		AVAssetResourceLoadingRequest *r = self->_requests[@(rid)];
		if (r != nil) {
			[r.dataRequest respondWithData:d];
		}
		clackAVFResponded(self->_h); // Go may queue its next
	});
}

- (void)finish:(uint64_t)rid failed:(int)failed {
	dispatch_async(_q, ^{
		AVAssetResourceLoadingRequest *r = self->_requests[@(rid)];
		if (r == nil) {
			return;
		}
		[self->_requests removeObjectForKey:@(rid)];
		atomic_fetch_sub(&live[CLACK_AV_LIVE_REQUESTS], 1);
		if (failed) {
			[r finishLoadingWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorNetworkConnectionLost userInfo:nil]];
		} else {
			[r finishLoading];
		}
	});
}

- (void)closeNow {
	if (_closed) {
		return;
	}
	_closed = YES;
	if (_pull != nil) {
		dispatch_source_cancel(_pull);
		_pull = nil;
	}
	if (_watch != nil) {
		dispatch_source_cancel(_watch);
		_watch = nil;
	}
	if (_item != nil) {
		[_item removeObserver:self forKeyPath:@"status" context:kStatusCtx];
		[_player removeObserver:self forKeyPath:@"timeControlStatus" context:kTimeCtx];
		NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
		[nc removeObserver:_endObs];
		[nc removeObserver:_failObs];
		[_player pause];
		if (_output != nil) {
			[_item removeOutput:_output];
		}
		[_player replaceCurrentItemWithPlayerItem:nil];
	}
	for (AVAssetResourceLoadingRequest *r in _requests.allValues) {
		[r finishLoadingWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorCancelled userInfo:nil]];
		atomic_fetch_sub(&live[CLACK_AV_LIVE_REQUESTS], 1);
	}
	[_requests removeAllObjects];
	[_asset.resourceLoader setDelegate:nil queue:_q];
	os_unfair_lock_lock(&_lock);
	clackEntry a = _newest, b = _handed;
	memset(&_newest, 0, sizeof _newest);
	memset(&_handed, 0, sizeof _handed);
	os_unfair_lock_unlock(&_lock);
	if (b.tex != a.tex) {
		releaseEntry(&b);
	}
	releaseEntry(&a);
	if (_cache != NULL) {
		CVMetalTextureCacheFlush(_cache, 0);
		CFRelease(_cache);
		_cache = NULL;
	}
	_output = nil;
	_player = nil;
	_item = nil;
	_asset = nil;
	_device = nil;
}

- (dispatch_queue_t)queue {
	return _q;
}

- (void)counts:(int64_t *)pulls frames:(int64_t *)frames w:(uint32_t *)w h:(uint32_t *)h {
	*pulls = atomic_load(&_pulls);
	*frames = atomic_load(&_frames);
	*w = atomic_load(&_pbW);
	*h = atomic_load(&_pbH);
}

// The setters, run on the queue.
- (void)setVolumeTo:(double)v {
	_volume = v;
	if (_player != nil) {
		_player.volume = (float)v;
	}
}
- (void)setMutedTo:(BOOL)m {
	_muted = m;
	if (_player != nil) {
		_player.muted = m;
	}
}
- (void)setLoopTo:(BOOL)l {
	_loop = l;
}
- (void)setRateTo:(double)r {
	_rate = r;
	if (_player != nil && _wantPlay) {
		_player.rate = (float)r;
	}
}
- (void)setDrawW:(int)w h:(int)h {
	_drawW = w;
	_drawH = h;
	[self makeOutput];
	[self updateTimers];
}
// snapshot copies the newest frame's pixels (32BGRA) into dst as RGBA,
// opaque, when dst holds them: 1; else 0, *pw and *ph its size (0 with no
// frame). Any thread: the frame is retained under the lock.
- (int)snapshot:(uint8_t *)dst cap:(size_t)cap w:(int *)pw h:(int *)ph {
	*pw = 0;
	*ph = 0;
	os_unfair_lock_lock(&_lock);
	CVPixelBufferRef pb = _newest.pb;
	if (pb != NULL) {
		CVPixelBufferRetain(pb);
	}
	os_unfair_lock_unlock(&_lock);
	if (pb == NULL) {
		return 0;
	}
	size_t w = CVPixelBufferGetWidth(pb), h = CVPixelBufferGetHeight(pb);
	*pw = (int)w;
	*ph = (int)h;
	int ok = 0;
	if (dst != NULL && w * h * 4 <= cap && CVPixelBufferLockBaseAddress(pb, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess) {
		const uint8_t *base = CVPixelBufferGetBaseAddress(pb);
		size_t stride = CVPixelBufferGetBytesPerRow(pb);
		if (base != NULL) {
			for (size_t y = 0; y < h; y++) {
				const uint8_t *s = base + y * stride;
				uint8_t *o = dst + y * w * 4;
				for (size_t x = 0; x < w; x++, s += 4, o += 4) {
					o[0] = s[2];
					o[1] = s[1];
					o[2] = s[0];
					o[3] = 255;
				}
			}
			ok = 1;
		}
		CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
	}
	CVPixelBufferRelease(pb);
	return ok;
}
@end

static ClackAVPlayer *P(void *p) { return (__bridge ClackAVPlayer *)p; }

void *clack_avf_new(uintptr_t h, int64_t total, int seekable, const char *uti, const char *url, int drawW, int drawH) {
	@autoreleasepool {
		NSURL *u = [NSURL URLWithString:@(url)];
		if (u == nil || ![u.scheme isEqualToString:@"clack-media"]) {
			return NULL; // AVFoundation is never given a URL it would fetch itself
		}
		ClackAVPlayer *o = [[ClackAVPlayer alloc] initWithHandle:h total:total seekable:seekable != 0 uti:@(uti) url:u drawW:drawW drawH:drawH];
		return (__bridge_retained void *)o;
	}
}

#define ON_QUEUE(p, body)                                                                                                                  \
	do {                                                                                                                                   \
		ClackAVPlayer *o_ = P(p);                                                                                                          \
		dispatch_async([o_ queue], ^{                                                                                                      \
			body;                                                                                                                          \
		});                                                                                                                                \
	} while (0)

void clack_avf_play(void *p) { ON_QUEUE(p, [o_ play]); }
void clack_avf_pause(void *p) { ON_QUEUE(p, [o_ pause]); }
void clack_avf_seek(void *p, double t) { ON_QUEUE(p, [o_ seekTo:t]); }
void clack_avf_volume(void *p, double v) { ON_QUEUE(p, [o_ setVolumeTo:v]); }
void clack_avf_muted(void *p, int m) { ON_QUEUE(p, [o_ setMutedTo:m != 0]); }
void clack_avf_loop(void *p, int l) { ON_QUEUE(p, [o_ setLoopTo:l != 0]); }
void clack_avf_rate(void *p, double r) { ON_QUEUE(p, [o_ setRateTo:r]); }
void clack_avf_drawsize(void *p, int w, int h) { ON_QUEUE(p, [o_ setDrawW:w h:h]); }
void clack_avf_asked(void *p) { ON_QUEUE(p, [o_ updateTimers]); }

int clack_avf_ask(void *p, uintptr_t device, uintptr_t *tex, uint64_t *key) { return [P(p) askDevice:device tex:tex key:key]; }
int clack_avf_snapshot(void *p, uint8_t *dst, size_t cap, int *w, int *h) { return [P(p) snapshot:dst cap:cap w:w h:h]; }

void clack_avf_respond(void *p, uint64_t req, const void *bytes, int n) {
	@autoreleasepool {
		[P(p) respond:req data:[NSData dataWithBytes:bytes length:(NSUInteger)n]];
	}
}

void clack_avf_finish(void *p, uint64_t req, int failed) { [P(p) finish:req failed:failed]; }

void clack_avf_counts(void *p, int64_t *pulls, int64_t *frames, uint32_t *w, uint32_t *h) { [P(p) counts:pulls frames:frames w:w h:h]; }

// clack_avf_close ends the player: its queue lets go of everything, then
// of the object itself (the reference clack_avf_new handed out).
void clack_avf_close(void *p) {
	ClackAVPlayer *o = (__bridge_transfer ClackAVPlayer *)p;
	dispatch_async([o queue], ^{
		[o closeNow];
	});
}

// The main run loop, for tests: AVFoundation posts some of its changes on
// the main queue, which Clack's NSApplication runs and a test binary
// doesn't. Call it on the main thread; it returns after stop.
void clack_avf_run_main_loop(void) {
	CFRunLoopTimerRef t = CFRunLoopTimerCreateWithHandler(NULL, CFAbsoluteTimeGetCurrent() + 3600, 3600, 0, 0, ^(CFRunLoopTimerRef x) {
	});
	CFRunLoopAddTimer(CFRunLoopGetCurrent(), t, kCFRunLoopCommonModes);
	CFRunLoopRun();
	CFRunLoopRemoveTimer(CFRunLoopGetCurrent(), t, kCFRunLoopCommonModes);
	CFRelease(t);
}

void clack_avf_stop_main_loop(void) { CFRunLoopStop(CFRunLoopGetMain()); }
