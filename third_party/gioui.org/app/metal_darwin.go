// SPDX-License-Identifier: Unlicense OR MIT

//go:build !nometal

package app

import (
	"errors"
	"log"
	"os"
	"sync/atomic"
	"time"

	"gioui.org/gpu"
)

/*
#cgo CFLAGS: -Werror -xobjective-c -fobjc-arc
#cgo LDFLAGS: -framework QuartzCore -framework Metal

#import <Metal/Metal.h>
#import <QuartzCore/CAMetalLayer.h>

#include <CoreFoundation/CoreFoundation.h>
#include <stdbool.h>

static CFTypeRef createMetalDevice(void) {
	@autoreleasepool {
		id<MTLDevice> dev = MTLCreateSystemDefaultDevice();
		return CFBridgingRetain(dev);
	}
}

static void setupLayer(CFTypeRef layerRef, CFTypeRef devRef, bool readable) {
	@autoreleasepool {
		CAMetalLayer *layer = (__bridge CAMetalLayer *)layerRef;
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		layer.device = dev;
		// Package gpu assumes an sRGB-encoded framebuffer.
		layer.pixelFormat = MTLPixelFormatBGRA8Unorm_sRGB;
		if (readable) {
			// Clack patch (CLACK_PATCHES.md §46), tests only: drawables a
			// blit can read back (gpu.ClackVerifyCopy).
			layer.framebufferOnly = NO;
		}
		if (@available(iOS 11.0, *)) {
			// Clack patch (CLACK_PATCHES.md §32): nextDrawable waits a second
			// at most (CAMetalLayer's default) and gives nil when no drawable
			// came back in time; RenderTarget skips that frame and the window
			// tries again. Upstream turned the timeout off, and the call held
			// the main thread for as long as the compositor kept the
			// drawables: seconds as a display woke, minutes while it slept.
			layer.allowsNextDrawableTimeout = YES;
		}
	}
}

static CFTypeRef nextDrawable(CFTypeRef layerRef) {
	@autoreleasepool {
		CAMetalLayer *layer = (__bridge CAMetalLayer *)layerRef;
		return CFBridgingRetain([layer nextDrawable]);
	}
}

static CFTypeRef drawableTexture(CFTypeRef drawableRef) {
	@autoreleasepool {
		id<CAMetalDrawable> drawable = (__bridge id<CAMetalDrawable>)drawableRef;
		return CFBridgingRetain(drawable.texture);
	}
}

static void presentDrawable(CFTypeRef queueRef, CFTypeRef drawableRef) {
	@autoreleasepool {
		id<MTLDrawable> drawable = (__bridge id<MTLDrawable>)drawableRef;
		id<MTLCommandQueue> queue = (__bridge id<MTLCommandQueue>)queueRef;
		id<MTLCommandBuffer> cmdBuffer = [queue commandBuffer];
		[cmdBuffer commit];
		[cmdBuffer waitUntilScheduled];
		[drawable present];
	}
}

static CFTypeRef newCommandQueue(CFTypeRef devRef) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		return CFBridgingRetain([dev newCommandQueue]);
	}
}
*/
import "C"

// metalPresented and metalSkipped count the frames every Metal context
// presented, and skipped for want of a drawable (Clack patch,
// CLACK_PATCHES.md §32; the macOS window tests read them).
var metalPresented, metalSkipped atomic.Int64

type mtlContext struct {
	dev      C.CFTypeRef
	view     C.CFTypeRef
	layer    C.CFTypeRef
	queue    C.CFTypeRef
	drawable C.CFTypeRef
	texture  C.CFTypeRef

	// Clack patch (CLACK_PATCHES.md §32): skipped frames in a row for
	// want of a drawable, since skipSince; the slow waits on the layer.
	skipped                   int
	skipSince                 time.Time
	slowDrawable, slowPresent slowWaits
}

// slowMetalWait is a wait on the Metal layer long enough to log (Clack
// patch, CLACK_PATCHES.md §32): the main thread does nothing else.
const slowMetalWait = 250 * time.Millisecond

// slowWaits logs waits of slowMetalWait or more: the first at once, then
// at most a line a minute with how many came and the longest.
type slowWaits struct {
	last  time.Time
	n     int
	worst time.Duration
}

func (s *slowWaits) note(what string, d time.Duration) {
	if d < slowMetalWait {
		return
	}
	s.n++
	s.worst = max(s.worst, d)
	if now := time.Now(); s.last.IsZero() || now.Sub(s.last) >= time.Minute {
		log.Printf("gio: %s held the main thread %v (%d such waits since the last line, the longest %v)", what, d.Round(time.Millisecond), s.n, s.worst.Round(time.Millisecond))
		s.last, s.n, s.worst = now, 0, 0
	}
}

func newMtlContext(w *window) (*mtlContext, error) {
	dev := C.createMetalDevice()
	if dev == 0 {
		return nil, errors.New("metal: MTLCreateSystemDefaultDevice failed")
	}
	view := w.contextView()
	layer := getMetalLayer(view)
	if layer == 0 {
		C.CFRelease(dev)
		return nil, errors.New("metal: CAMetalLayer construction failed")
	}
	queue := C.newCommandQueue(dev)
	if queue == 0 {
		C.CFRelease(dev)
		C.CFRelease(layer)
		return nil, errors.New("metal: [MTLDevice newCommandQueue] failed")
	}
	// Clack patch (CLACK_PATCHES.md §46): CLACK_METAL_READBACK makes the
	// drawables readable, for tests that read the window back.
	C.setupLayer(layer, dev, C.bool(os.Getenv("CLACK_METAL_READBACK") != ""))
	c := &mtlContext{
		dev:   dev,
		view:  view,
		layer: layer,
		queue: queue,
	}
	return c, nil
}

func (c *mtlContext) RenderTarget() (gpu.RenderTarget, error) {
	if c.drawable != 0 || c.texture != 0 {
		return nil, errors.New("metal:a previous RenderTarget wasn't Presented")
	}
	start := time.Now()
	c.drawable = C.nextDrawable(c.layer)
	waited := time.Since(start)
	if c.drawable == 0 {
		// Clack patch (CLACK_PATCHES.md §32): none within the layer's
		// second. The frame is skipped (validateAndProcess), not an error
		// that ends the window, and the window tries again.
		metalSkipped.Add(1)
		if c.skipped == 0 {
			c.skipSince = start
			log.Printf("gio: no Metal drawable within %v ([CAMetalLayer nextDrawable] gave nil): frame skipped, the window tries again", waited.Round(time.Millisecond))
		}
		c.skipped++
		return nil, errNoRenderTarget
	}
	if c.skipped > 0 {
		log.Printf("gio: Metal drawables again after %d frames skipped in %v", c.skipped, time.Since(c.skipSince).Round(time.Millisecond))
		c.skipped = 0
	}
	c.slowDrawable.note("[CAMetalLayer nextDrawable]", waited)
	c.texture = C.drawableTexture(c.drawable)
	if c.texture == 0 {
		return nil, errors.New("metal: CADrawable.texture is nil")
	}
	return gpu.MetalRenderTarget{
		Texture: uintptr(c.texture),
	}, nil
}

func (c *mtlContext) API() gpu.API {
	return gpu.Metal{
		Device:      uintptr(c.dev),
		Queue:       uintptr(c.queue),
		PixelFormat: int(C.MTLPixelFormatBGRA8Unorm_sRGB),
	}
}

func (c *mtlContext) Release() {
	C.CFRelease(c.queue)
	C.CFRelease(c.dev)
	C.CFRelease(c.layer)
	if c.drawable != 0 {
		C.CFRelease(c.drawable)
	}
	if c.texture != 0 {
		C.CFRelease(c.texture)
	}
	*c = mtlContext{}
}

func (c *mtlContext) Present() error {
	C.CFRelease(c.texture)
	c.texture = 0
	start := time.Now()
	C.presentDrawable(c.queue, c.drawable)
	// Clack patch (CLACK_PATCHES.md §32): waitUntilScheduled, which a
	// layer that presents with its transaction needs (Chromium's
	// HDRCopierLayer does the same), holds the main thread too.
	c.slowPresent.note("presenting a frame (waitUntilScheduled)", time.Since(start))
	C.CFRelease(c.drawable)
	c.drawable = 0
	metalPresented.Add(1)
	return nil
}

func (c *mtlContext) Lock() error {
	return nil
}

func (c *mtlContext) Unlock() {}

func (c *mtlContext) Refresh() error {
	resizeDrawable(c.view, c.layer)
	return nil
}

func (w *window) NewContext() (context, error) {
	return newMtlContext(w)
}
