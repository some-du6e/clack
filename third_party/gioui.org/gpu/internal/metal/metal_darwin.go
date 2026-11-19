// SPDX-License-Identifier: Unlicense OR MIT

package metal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/shader"
)

/*
#cgo CFLAGS: -Werror -xobjective-c -fobjc-arc
#cgo LDFLAGS: -framework CoreGraphics -framework Metal -framework Foundation

#include <CoreFoundation/CoreFoundation.h>
#include <Metal/Metal.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	void *addr;
	NSUInteger size;
} slice;

typedef struct {
	CFTypeRef obj;
	char *err;
} CFTypeAndError;

static CFTypeAndError error(NSError *err) {
	char *errStr = strdup(err.localizedDescription.UTF8String);
	if (errStr == NULL) {
		abort();
	}
	CFTypeAndError res = {
		.err = errStr,
	};
	return res;
}

static CFTypeAndError ok(id obj) {
	CFTypeAndError res = {
		.obj = CFBridgingRetain(obj),
	};
	return res;
}

static CFTypeRef queueNewBuffer(CFTypeRef queueRef) {
	@autoreleasepool {
		id<MTLCommandQueue> queue = (__bridge id<MTLCommandQueue>)queueRef;
		return CFBridgingRetain([queue commandBuffer]);
	}
}

static void cmdBufferCommit(CFTypeRef cmdBufRef) {
	@autoreleasepool {
		id<MTLCommandBuffer> cmdBuf = (__bridge id<MTLCommandBuffer>)cmdBufRef;
		[cmdBuf commit];
	}
}

static void cmdBufferWaitUntilCompleted(CFTypeRef cmdBufRef) {
	@autoreleasepool {
		id<MTLCommandBuffer> cmdBuf = (__bridge id<MTLCommandBuffer>)cmdBufRef;
		[cmdBuf waitUntilCompleted];
	}
}

static CFTypeRef cmdBufferRenderEncoder(CFTypeRef cmdBufRef, CFTypeRef textureRef, MTLLoadAction act, float r, float g, float b, float a) {
	@autoreleasepool {
		id<MTLCommandBuffer> cmdBuf = (__bridge id<MTLCommandBuffer>)cmdBufRef;
		MTLRenderPassDescriptor *desc = [MTLRenderPassDescriptor new];
		desc.colorAttachments[0].texture = (__bridge id<MTLTexture>)textureRef;
		desc.colorAttachments[0].loadAction = act;
		desc.colorAttachments[0].clearColor = MTLClearColorMake(r, g, b, a);
		return CFBridgingRetain([cmdBuf renderCommandEncoderWithDescriptor:desc]);
	}
}

static CFTypeRef cmdBufferComputeEncoder(CFTypeRef cmdBufRef) {
	@autoreleasepool {
		id<MTLCommandBuffer> cmdBuf = (__bridge id<MTLCommandBuffer>)cmdBufRef;
		return CFBridgingRetain([cmdBuf computeCommandEncoder]);
	}
}

static CFTypeRef cmdBufferBlitEncoder(CFTypeRef cmdBufRef) {
	@autoreleasepool {
		id<MTLCommandBuffer> cmdBuf = (__bridge id<MTLCommandBuffer>)cmdBufRef;
		return CFBridgingRetain([cmdBuf blitCommandEncoder]);
	}
}

static void renderEncEnd(CFTypeRef renderEncRef) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		[enc endEncoding];
	}
}

static void renderEncViewport(CFTypeRef renderEncRef, MTLViewport viewport) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		[enc setViewport:viewport];
	}
}

// Clack patch 46: a drawable's size (the frame's copy stays inside it).
static void textureSize(CFTypeRef texRef, NSUInteger *w, NSUInteger *h) {
	@autoreleasepool {
		id<MTLTexture> tex = (__bridge id<MTLTexture>)texRef;
		*w = tex.width;
		*h = tex.height;
	}
}

// Clack patch 46: whether a texture is framebufferOnly (a drawable of a
// layer that is): no blit can read it.
static bool textureFramebufferOnly(CFTypeRef texRef) {
	@autoreleasepool {
		id<MTLTexture> tex = (__bridge id<MTLTexture>)texRef;
		return tex.framebufferOnly;
	}
}

// Clack patch 46: a partial frame's scissor.
static void renderEncScissor(CFTypeRef renderEncRef, NSUInteger x, NSUInteger y, NSUInteger w, NSUInteger h) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		MTLScissorRect r = {x, y, w, h};
		[enc setScissorRect:r];
	}
}

static void renderEncSetFragmentTexture(CFTypeRef renderEncRef, NSUInteger index, CFTypeRef texRef) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		id<MTLTexture> tex = (__bridge id<MTLTexture>)texRef;
		[enc setFragmentTexture:tex atIndex:index];
	}
}

static void renderEncSetFragmentSamplerState(CFTypeRef renderEncRef, NSUInteger index, CFTypeRef samplerRef) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		id<MTLSamplerState> sampler = (__bridge id<MTLSamplerState>)samplerRef;
		[enc setFragmentSamplerState:sampler atIndex:index];
	}
}

static void renderEncSetVertexBuffer(CFTypeRef renderEncRef, CFTypeRef bufRef, NSUInteger idx, NSUInteger offset) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		id<MTLBuffer> buf = (__bridge id<MTLBuffer>)bufRef;
		[enc setVertexBuffer:buf offset:offset atIndex:idx];
	}
}

static void renderEncSetVertexBufferOffset(CFTypeRef renderEncRef, NSUInteger idx, NSUInteger offset) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		[enc setVertexBufferOffset:offset atIndex:idx];
	}
}

// Clack patch 37: a render pass's commands as the backend recorded them
// (Backend.rec), run in one cgo call under one autorelease pool instead of
// one each. Every word is 8 bytes; bytes are padded to 8.
enum {
	cmdViewport = 1,
	cmdPipeline,
	cmdFragTexture,
	cmdFragSampler,
	cmdVertBuffer,
	cmdVertBufferOffset,
	cmdFragBuffer,
	cmdVertBytes,
	cmdFragBytes,
	cmdDraw,
	cmdDrawIndexed,
	cmdScissor, // Clack patch 46
};

static uint64_t cmdWord(const uint8_t **p) {
	uint64_t v;
	memcpy(&v, *p, 8);
	*p += 8;
	return v;
}

static void renderEncReplay(CFTypeRef renderEncRef, const void *cmds, NSUInteger n) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		const uint8_t *p = (const uint8_t *)cmds;
		const uint8_t *end = p + n;
		while (p < end) {
			uint64_t op = cmdWord(&p);
			switch (op) {
			case cmdViewport: {
				MTLViewport v;
				memcpy(&v, p, sizeof(v));
				p += sizeof(v);
				[enc setViewport:v];
				break;
			}
			case cmdPipeline: {
				CFTypeRef o = (CFTypeRef)(uintptr_t)cmdWord(&p);
				[enc setRenderPipelineState:(__bridge id<MTLRenderPipelineState>)o];
				break;
			}
			case cmdFragTexture: {
				NSUInteger idx = (NSUInteger)cmdWord(&p);
				CFTypeRef o = (CFTypeRef)(uintptr_t)cmdWord(&p);
				[enc setFragmentTexture:(__bridge id<MTLTexture>)o atIndex:idx];
				break;
			}
			case cmdFragSampler: {
				NSUInteger idx = (NSUInteger)cmdWord(&p);
				CFTypeRef o = (CFTypeRef)(uintptr_t)cmdWord(&p);
				[enc setFragmentSamplerState:(__bridge id<MTLSamplerState>)o atIndex:idx];
				break;
			}
			case cmdVertBuffer:
			case cmdFragBuffer: {
				NSUInteger idx = (NSUInteger)cmdWord(&p);
				CFTypeRef o = (CFTypeRef)(uintptr_t)cmdWord(&p);
				NSUInteger off = (NSUInteger)cmdWord(&p);
				if (op == cmdVertBuffer) {
					[enc setVertexBuffer:(__bridge id<MTLBuffer>)o offset:off atIndex:idx];
				} else {
					[enc setFragmentBuffer:(__bridge id<MTLBuffer>)o offset:off atIndex:idx];
				}
				break;
			}
			case cmdVertBufferOffset: {
				NSUInteger idx = (NSUInteger)cmdWord(&p);
				NSUInteger off = (NSUInteger)cmdWord(&p);
				[enc setVertexBufferOffset:off atIndex:idx];
				break;
			}
			case cmdVertBytes:
			case cmdFragBytes: {
				NSUInteger idx = (NSUInteger)cmdWord(&p);
				NSUInteger len = (NSUInteger)cmdWord(&p);
				if (op == cmdVertBytes) {
					[enc setVertexBytes:p length:len atIndex:idx];
				} else {
					[enc setFragmentBytes:p length:len atIndex:idx];
				}
				p += (len + 7) & ~(NSUInteger)7;
				break;
			}
			case cmdDraw: {
				MTLPrimitiveType t = (MTLPrimitiveType)cmdWord(&p);
				NSUInteger start = (NSUInteger)cmdWord(&p);
				NSUInteger count = (NSUInteger)cmdWord(&p);
				[enc drawPrimitives:t vertexStart:start vertexCount:count];
				break;
			}
			case cmdDrawIndexed: {
				MTLPrimitiveType t = (MTLPrimitiveType)cmdWord(&p);
				NSUInteger count = (NSUInteger)cmdWord(&p);
				CFTypeRef o = (CFTypeRef)(uintptr_t)cmdWord(&p);
				NSUInteger off = (NSUInteger)cmdWord(&p);
				[enc drawIndexedPrimitives:t indexCount:count indexType:MTLIndexTypeUInt16 indexBuffer:(__bridge id<MTLBuffer>)o indexBufferOffset:off];
				break;
			}
			case cmdScissor: {
				MTLScissorRect r;
				r.x = (NSUInteger)cmdWord(&p);
				r.y = (NSUInteger)cmdWord(&p);
				r.width = (NSUInteger)cmdWord(&p);
				r.height = (NSUInteger)cmdWord(&p);
				[enc setScissorRect:r];
				break;
			}
			default:
				abort();
			}
		}
	}
}

static void renderEncSetFragmentBuffer(CFTypeRef renderEncRef, CFTypeRef bufRef, NSUInteger idx, NSUInteger offset) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		id<MTLBuffer> buf = (__bridge id<MTLBuffer>)bufRef;
		[enc setFragmentBuffer:buf offset:offset atIndex:idx];
	}
}

static void renderEncSetFragmentBytes(CFTypeRef renderEncRef, const void *bytes, NSUInteger length, NSUInteger idx) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		[enc setFragmentBytes:bytes length:length atIndex:idx];
	}
}

static void renderEncSetVertexBytes(CFTypeRef renderEncRef, const void *bytes, NSUInteger length, NSUInteger idx) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		[enc setVertexBytes:bytes length:length atIndex:idx];
	}
}

static void renderEncSetRenderPipelineState(CFTypeRef renderEncRef, CFTypeRef pipeRef) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		id<MTLRenderPipelineState> pipe = (__bridge id<MTLRenderPipelineState>)pipeRef;
		[enc setRenderPipelineState:pipe];
	}
}

static void renderEncDrawPrimitives(CFTypeRef renderEncRef, MTLPrimitiveType type, NSUInteger start, NSUInteger count) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		[enc drawPrimitives:type vertexStart:start vertexCount:count];
	}
}

static void renderEncDrawIndexedPrimitives(CFTypeRef renderEncRef, MTLPrimitiveType type, CFTypeRef bufRef, NSUInteger offset, NSUInteger count) {
	@autoreleasepool {
		id<MTLRenderCommandEncoder> enc = (__bridge id<MTLRenderCommandEncoder>)renderEncRef;
		id<MTLBuffer> buf = (__bridge id<MTLBuffer>)bufRef;
		[enc drawIndexedPrimitives:type indexCount:count indexType:MTLIndexTypeUInt16 indexBuffer:buf indexBufferOffset:offset];
	}
}

static void computeEncSetPipeline(CFTypeRef computeEncRef, CFTypeRef pipeRef) {
	@autoreleasepool {
		id<MTLComputeCommandEncoder> enc = (__bridge id<MTLComputeCommandEncoder>)computeEncRef;
		id<MTLComputePipelineState> pipe = (__bridge id<MTLComputePipelineState>)pipeRef;
		[enc setComputePipelineState:pipe];
	}
}

static void computeEncSetTexture(CFTypeRef computeEncRef, NSUInteger index, CFTypeRef texRef) {
	@autoreleasepool {
		id<MTLComputeCommandEncoder> enc = (__bridge id<MTLComputeCommandEncoder>)computeEncRef;
		id<MTLTexture> tex = (__bridge id<MTLTexture>)texRef;
		[enc setTexture:tex atIndex:index];
	}
}

static void computeEncEnd(CFTypeRef computeEncRef) {
	@autoreleasepool {
		id<MTLComputeCommandEncoder> enc = (__bridge id<MTLComputeCommandEncoder>)computeEncRef;
		[enc endEncoding];
	}
}

static void computeEncSetBuffer(CFTypeRef computeEncRef, NSUInteger index, CFTypeRef bufRef) {
	@autoreleasepool {
		id<MTLComputeCommandEncoder> enc = (__bridge id<MTLComputeCommandEncoder>)computeEncRef;
		id<MTLBuffer> buf = (__bridge id<MTLBuffer>)bufRef;
		[enc setBuffer:buf offset:0 atIndex:index];
	}
}

static void computeEncDispatch(CFTypeRef computeEncRef, MTLSize threadgroupsPerGrid, MTLSize threadsPerThreadgroup) {
	@autoreleasepool {
		id<MTLComputeCommandEncoder> enc = (__bridge id<MTLComputeCommandEncoder>)computeEncRef;
		[enc dispatchThreadgroups:threadgroupsPerGrid threadsPerThreadgroup:threadsPerThreadgroup];
	}
}

static void computeEncSetBytes(CFTypeRef computeEncRef, const void *bytes, NSUInteger length, NSUInteger index) {
	@autoreleasepool {
		id<MTLComputeCommandEncoder> enc = (__bridge id<MTLComputeCommandEncoder>)computeEncRef;
		[enc setBytes:bytes length:length atIndex:index];
	}
}

static void blitEncEnd(CFTypeRef blitEncRef) {
	@autoreleasepool {
		id<MTLBlitCommandEncoder> enc = (__bridge id<MTLBlitCommandEncoder>)blitEncRef;
		[enc endEncoding];
	}
}

static void blitEncCopyFromTexture(CFTypeRef blitEncRef, CFTypeRef srcRef, MTLOrigin srcOrig, MTLSize srcSize, CFTypeRef dstRef, MTLOrigin dstOrig) {
	@autoreleasepool {
		id<MTLBlitCommandEncoder> enc = (__bridge id<MTLBlitCommandEncoder>)blitEncRef;
		id<MTLTexture> src = (__bridge id<MTLTexture>)srcRef;
		id<MTLTexture> dst = (__bridge id<MTLTexture>)dstRef;
		[enc copyFromTexture:src
				 sourceSlice:0
				 sourceLevel:0
			    sourceOrigin:srcOrig
				  sourceSize:srcSize
				   toTexture:dst
			destinationSlice:0
			destinationLevel:0
		   destinationOrigin:dstOrig];
	}
}

static void blitEncCopyBufferToTexture(CFTypeRef blitEncRef, CFTypeRef bufRef, CFTypeRef texRef, NSUInteger offset, NSUInteger stride, NSUInteger length, MTLSize dims, MTLOrigin orig) {
	@autoreleasepool {
		id<MTLBlitCommandEncoder> enc = (__bridge id<MTLBlitCommandEncoder>)blitEncRef;
		id<MTLBuffer> src = (__bridge id<MTLBuffer>)bufRef;
		id<MTLTexture> dst = (__bridge id<MTLTexture>)texRef;
		[enc copyFromBuffer:src
			   sourceOffset:offset
		  sourceBytesPerRow:stride
		sourceBytesPerImage:length
				 sourceSize:dims
				  toTexture:dst
		   destinationSlice:0
		   destinationLevel:0
		  destinationOrigin:orig];
	}
}

static void blitEncGenerateMipmapsForTexture(CFTypeRef blitEncRef, CFTypeRef texRef) {
	@autoreleasepool {
		id<MTLBlitCommandEncoder> enc = (__bridge id<MTLBlitCommandEncoder>)blitEncRef;
		id<MTLTexture> tex = (__bridge id<MTLTexture>)texRef;
		[enc generateMipmapsForTexture: tex];
	}
}

static void blitEncCopyTextureToBuffer(CFTypeRef blitEncRef, CFTypeRef texRef, CFTypeRef bufRef, NSUInteger offset, NSUInteger stride, NSUInteger length, MTLSize dims, MTLOrigin orig) {
	@autoreleasepool {
		id<MTLBlitCommandEncoder> enc = (__bridge id<MTLBlitCommandEncoder>)blitEncRef;
		id<MTLTexture> src = (__bridge id<MTLTexture>)texRef;
		id<MTLBuffer> dst = (__bridge id<MTLBuffer>)bufRef;
		[enc		 copyFromTexture:src
						 sourceSlice:0
						 sourceLevel:0
						sourceOrigin:orig
						  sourceSize:dims
							toBuffer:dst
				   destinationOffset:offset
			  destinationBytesPerRow:stride
			destinationBytesPerImage:length];
	}
}

static void blitEncCopyBufferToBuffer(CFTypeRef blitEncRef, CFTypeRef srcRef, CFTypeRef dstRef, NSUInteger srcOff, NSUInteger dstOff, NSUInteger size) {
	@autoreleasepool {
		id<MTLBlitCommandEncoder> enc = (__bridge id<MTLBlitCommandEncoder>)blitEncRef;
		id<MTLBuffer> src = (__bridge id<MTLBuffer>)srcRef;
		id<MTLBuffer> dst = (__bridge id<MTLBuffer>)dstRef;
		[enc   copyFromBuffer:src
				 sourceOffset:srcOff
					 toBuffer:dst
			destinationOffset:dstOff
						 size:size];
	}
}

static CFTypeRef newTexture(CFTypeRef devRef, NSUInteger width, NSUInteger height, MTLPixelFormat format, MTLTextureUsage usage, int mipmapped) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		MTLTextureDescriptor *mtlDesc = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat: format
																						   width: width
																						  height: height
																				   	   mipmapped: mipmapped ? YES : NO];
		mtlDesc.usage = usage;
		mtlDesc.storageMode =  MTLStorageModePrivate;
		return CFBridgingRetain([dev newTextureWithDescriptor:mtlDesc]);
	}
}

static CFTypeRef newSampler(CFTypeRef devRef, MTLSamplerMinMagFilter minFilter, MTLSamplerMinMagFilter magFilter, MTLSamplerMipFilter mipFilter) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		MTLSamplerDescriptor *desc = [MTLSamplerDescriptor new];
		desc.minFilter = minFilter;
		desc.magFilter = magFilter;
		desc.mipFilter = mipFilter;
		return CFBridgingRetain([dev newSamplerStateWithDescriptor:desc]);
	}
}

static CFTypeRef newBuffer(CFTypeRef devRef, NSUInteger size, MTLResourceOptions opts) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		id<MTLBuffer> buf = [dev newBufferWithLength:size
											 options:opts];
		return CFBridgingRetain(buf);
	}
}

static slice bufferContents(CFTypeRef bufRef) {
	@autoreleasepool {
		id<MTLBuffer> buf = (__bridge id<MTLBuffer>)bufRef;
		slice s = {.addr = [buf contents], .size = [buf length]};
		return s;
	}
}

static CFTypeAndError newLibrary(CFTypeRef devRef, char *name, void *mtllib, size_t size) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		dispatch_data_t data = dispatch_data_create(mtllib, size, DISPATCH_TARGET_QUEUE_DEFAULT, DISPATCH_DATA_DESTRUCTOR_DEFAULT);
		NSError *err = NULL;
		id<MTLLibrary> lib = [dev newLibraryWithData:data error:&err];
		if (lib == nil) {
			return error(err);
		}
		lib.label = [NSString stringWithUTF8String:name];
		return ok(lib);
	}
}

static CFTypeRef libraryNewFunction(CFTypeRef libRef, char *funcName) {
	@autoreleasepool {
		id<MTLLibrary> lib = (__bridge id<MTLLibrary>)libRef;
		NSString *name = [NSString stringWithUTF8String:funcName];
		return CFBridgingRetain([lib newFunctionWithName:name]);
	}
}

static CFTypeAndError newComputePipeline(CFTypeRef devRef, CFTypeRef funcRef) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		id<MTLFunction> func = (__bridge id<MTLFunction>)funcRef;
		NSError *err = NULL;
		id<MTLComputePipelineState> s = [dev newComputePipelineStateWithFunction:func error:&err];
		if (s == nil) {
			return error(err);
		}
		return ok(s);
	}
}

static CFTypeAndError newRenderPipeline(CFTypeRef devRef, CFTypeRef vertFunc, CFTypeRef fragFunc, MTLPixelFormat pixelFormat, NSUInteger bufIdx, NSUInteger nverts, MTLVertexFormat *fmts, NSUInteger *offsets, NSUInteger stride, int blend, MTLBlendFactor srcFactor, MTLBlendFactor dstFactor, NSUInteger nvertBufs, NSUInteger nfragBufs) {
	@autoreleasepool {
		id<MTLDevice> dev = (__bridge id<MTLDevice>)devRef;
		id<MTLFunction> vfunc = (__bridge id<MTLFunction>)vertFunc;
		id<MTLFunction> ffunc = (__bridge id<MTLFunction>)fragFunc;
		MTLVertexDescriptor *vdesc = [MTLVertexDescriptor vertexDescriptor];
		vdesc.layouts[bufIdx].stride = stride;
		for (NSUInteger i = 0; i < nverts; i++) {
			vdesc.attributes[i].format = fmts[i];
			vdesc.attributes[i].offset = offsets[i];
			vdesc.attributes[i].bufferIndex = bufIdx;
		}
		MTLRenderPipelineDescriptor *desc = [MTLRenderPipelineDescriptor new];
		desc.vertexFunction = vfunc;
		desc.fragmentFunction = ffunc;
		desc.vertexDescriptor = vdesc;
		for (NSUInteger i = 0; i < nvertBufs; i++) {
			if (@available(iOS 11.0, *)) {
				desc.vertexBuffers[i].mutability = MTLMutabilityImmutable;
			}
		}
		for (NSUInteger i = 0; i < nfragBufs; i++) {
			if (@available(iOS 11.0, *)) {
				desc.fragmentBuffers[i].mutability = MTLMutabilityImmutable;
			}
		}
		desc.colorAttachments[0].pixelFormat = pixelFormat;
		desc.colorAttachments[0].blendingEnabled = blend ? YES : NO;
		desc.colorAttachments[0].sourceAlphaBlendFactor = srcFactor;
		desc.colorAttachments[0].sourceRGBBlendFactor = srcFactor;
		desc.colorAttachments[0].destinationAlphaBlendFactor = dstFactor;
		desc.colorAttachments[0].destinationRGBBlendFactor = dstFactor;
		NSError *err = NULL;
		id<MTLRenderPipelineState> s = [dev newRenderPipelineStateWithDescriptor:desc
											error:&err];
		if (s == nil) {
			return error(err);
		}
		return ok(s);
	}
}
*/
import "C"

type Backend struct {
	dev      C.CFTypeRef
	queue    C.CFTypeRef
	pixelFmt C.MTLPixelFormat

	cmdBuffer     C.CFTypeRef
	lastCmdBuffer C.CFTypeRef
	renderEnc     C.CFTypeRef
	computeEnc    C.CFTypeRef
	blitEnc       C.CFTypeRef

	prog     *Program
	topology C.MTLPrimitiveType

	stagingBuf C.CFTypeRef
	stagingOff int

	indexBuf *Buffer

	// bufSizes is scratch space for filling out the spvBufferSizeConstants
	// that spirv-cross generates for emulating buffer.length expressions in
	// shaders.
	bufSizes []uint32

	// Clack patch 37: the open render encoder's state, and one sampler
	// state per filter for all textures.
	enc      encState
	samplers map[samplerKey]C.CFTypeRef
	// Clack patch 36: the cgo calls made into Metal, and the ones enc
	// left out.
	calls driver.Calls
	// rec is the open render pass's commands while batch is on (Clack
	// patch 37, driver.ClackBatchCommands): run in one cgo call
	// (renderEncReplay) when the pass ends.
	rec   []byte
	batch bool
	// rtSize is the open render pass's target's size, where known (Clack
	// patch 46: the scissor stays inside it)
	rtSize image.Point
}

// The render commands' opcodes, as renderEncReplay's enum.
const (
	cmdViewport = 1 + iota
	cmdPipeline
	cmdFragTexture
	cmdFragSampler
	cmdVertBuffer
	cmdVertBufferOffset
	cmdFragBuffer
	cmdVertBytes
	cmdFragBytes
	cmdDraw
	cmdDrawIndexed
	cmdScissor // Clack patch 46
)

// recMax is how big rec may grow before it is run, mid-pass.
const recMax = 1 << 20

// record adds a render command (op and its words) to rec.
func (b *Backend) record(op uint64, words ...uint64) {
	b.rec = binary.NativeEndian.AppendUint64(b.rec, op)
	for _, w := range words {
		b.rec = binary.NativeEndian.AppendUint64(b.rec, w)
	}
	if len(b.rec) >= recMax {
		b.runRecorded()
	}
}

// recordBytes adds a command sending data to a stage's buffer index.
func (b *Backend) recordBytes(op uint64, index int, data []byte) {
	b.rec = binary.NativeEndian.AppendUint64(b.rec, op)
	b.rec = binary.NativeEndian.AppendUint64(b.rec, uint64(index))
	b.rec = binary.NativeEndian.AppendUint64(b.rec, uint64(len(data)))
	b.rec = append(b.rec, data...)
	for len(b.rec)%8 != 0 {
		b.rec = append(b.rec, 0)
	}
	if len(b.rec) >= recMax {
		b.runRecorded()
	}
}

// runRecorded runs the recorded commands on the render encoder.
func (b *Backend) runRecorded() {
	if len(b.rec) == 0 {
		return
	}
	C.renderEncReplay(b.renderEnc, unsafe.Pointer(unsafe.SliceData(b.rec)), C.NSUInteger(len(b.rec)))
	b.calls.Crossings++
	b.rec = b.rec[:0]
}

// encState is what the open render encoder was last given (Clack patch
// 37). Gio binds the pipeline, the quad, the textures, the uniforms and
// the viewport again for every op; each was a cgo call into Metal under
// an autorelease pool of its own, ~11,400 a frame on a busy window. A
// bind of what the encoder already has is left out. A new encoder starts
// with nothing bound.
type encState struct {
	pipeline *Pipeline
	// the attribute buffer: a Metal buffer at an offset, or a Go one's
	// bytes as of its upload generation
	vert struct {
		buf *Buffer
		off int
		gen uint64
	}
	// the uniform bytes each stage was last sent (setVertexBytes,
	// setFragmentBytes), or the Metal buffer bound for them
	vertUniforms, fragUniforms uniformBinding
	tex                        [texUnits]C.CFTypeRef
	sampler                    [texUnits]C.CFTypeRef
	viewport                   C.MTLViewport
	hasViewport                bool
}

type uniformBinding struct {
	buf   C.CFTypeRef // a Metal buffer, or 0 for bytes
	bytes []byte
	set   bool
}

// same reports whether the stage, reading the bytes in r of data, would
// see what it was last sent, and the bytes bound are as long as data (the
// shader's argument struct may reach past the last member it reads).
func (u *uniformBinding) same(data []byte, r [2]int) bool {
	return u.set && u.buf == 0 && len(u.bytes) >= len(data) && len(data) >= r[1] && string(u.bytes[r[0]:r[1]]) == string(data[r[0]:r[1]])
}

func (u *uniformBinding) sent(data []byte) {
	u.buf, u.set = 0, true
	u.bytes = append(u.bytes[:0], data...)
}

type samplerKey struct {
	min, mag C.MTLSamplerMinMagFilter
	mip      C.MTLSamplerMipFilter
}

type Texture struct {
	backend *Backend
	texture C.CFTypeRef
	sampler C.CFTypeRef
	width   int
	height  int
	mipmap  bool
	foreign bool
}

type Shader struct {
	function C.CFTypeRef
	inputs   []shader.InputLocation
	uniforms shader.UniformsReflection
}

// uniformRange is the bytes of the uniform buffer a shader reads; zero for
// none (Clack patch 37).
func (s *Shader) uniformRange() [2]int {
	r := [2]int{-1, 0}
	for _, l := range s.uniforms.Locations {
		if r[0] < 0 || l.Offset < r[0] {
			r[0] = l.Offset
		}
		if e := l.Offset + 4*l.Size; e > r[1] {
			r[1] = e
		}
	}
	if r[0] < 0 {
		return [2]int{}
	}
	return r
}

type Program struct {
	pipeline  C.CFTypeRef
	groupSize [3]int
}

type Pipeline struct {
	pipeline C.CFTypeRef
	topology C.MTLPrimitiveType
	// the uniform bytes each stage reads (Clack patch 37): a stage that
	// reads none isn't sent any
	vertUniforms, fragUniforms [2]int
}

type Buffer struct {
	backend *Backend
	size    int
	buffer  C.CFTypeRef

	// store is the buffer contents for buffers not allocated on the GPU.
	store []byte
	// gen counts store's uploads: bytes sent to an encoder are still its
	// contents while gen is unchanged (Clack patch 37)
	gen uint64
}

const (
	uniformBufferIndex   = 0
	attributeBufferIndex = 1

	spvBufferSizeConstantsBinding = 25
)

const (
	texUnits    = 4
	bufferUnits = 4
)

func init() {
	driver.NewMetalDevice = newMetalDevice
}

func newMetalDevice(api driver.Metal) (driver.Device, error) {
	dev := C.CFTypeRef(api.Device)
	C.CFRetain(dev)
	queue := C.CFTypeRef(api.Queue)
	C.CFRetain(queue)
	b := &Backend{
		dev:      dev,
		queue:    queue,
		pixelFmt: C.MTLPixelFormat(api.PixelFormat),
		bufSizes: make([]uint32, bufferUnits),
	}
	return b, nil
}

func (b *Backend) BeginFrame(target driver.RenderTarget, clear bool, viewport image.Point) driver.Texture {
	if b.lastCmdBuffer != 0 {
		C.cmdBufferWaitUntilCompleted(b.lastCmdBuffer)
		b.apiCall()
		b.stagingOff = 0
	}
	if target == nil {
		return nil
	}
	switch t := target.(type) {
	case driver.MetalRenderTarget:
		texture := C.CFTypeRef(t.Texture)
		// its size: the frame texture's copy stays inside it (Clack patch 46)
		var w, h C.NSUInteger
		C.textureSize(texture, &w, &h)
		b.apiCall()
		// backend: ReadPixels can read it back (gpu.ClackVerifyCopy's tests
		// make the layer's drawables readable)
		return &Texture{backend: b, texture: texture, foreign: true, width: int(w), height: int(h)}
	case *Texture:
		return t
	default:
		panic(fmt.Sprintf("metal: unsupported render target type: %T", t))
	}
}

func (b *Backend) startBlit() C.CFTypeRef {
	if b.blitEnc != 0 {
		return b.blitEnc
	}
	b.endEncoder()
	b.ensureCmdBuffer()
	b.blitEnc = C.cmdBufferBlitEncoder(b.cmdBuffer)
	b.apiCall()
	if b.blitEnc == 0 {
		panic("metal: [MTLCommandBuffer blitCommandEncoder:] failed")
	}
	return b.blitEnc
}

func (b *Backend) CopyTexture(dst driver.Texture, dorig image.Point, src driver.Texture, srect image.Rectangle) {
	enc := b.startBlit()
	dstTex := dst.(*Texture).texture
	srcTex := src.(*Texture).texture
	ssz := srect.Size()
	C.blitEncCopyFromTexture(
		enc,
		srcTex,
		C.MTLOrigin{
			x: C.NSUInteger(srect.Min.X),
			y: C.NSUInteger(srect.Min.Y),
		},
		C.MTLSize{
			width:  C.NSUInteger(ssz.X),
			height: C.NSUInteger(ssz.Y),
			depth:  1,
		},
		dstTex,
		C.MTLOrigin{
			x: C.NSUInteger(dorig.X),
			y: C.NSUInteger(dorig.Y),
		},
	)
}

func (b *Backend) EndFrame() {
	b.endCmdBuffer(false)
}

// ClackPartialOK implements driver.ClackPartial (Clack patch 46).
func (b *Backend) ClackPartialOK() bool { return true }

// ClackCopyDraws implements driver.ClackPartial: the drawable is
// framebufferOnly, so the frame is drawn into it.
func (b *Backend) ClackCopyDraws() bool { return true }

// ClackScissor implements driver.ClackPartial: the render encoder's
// scissor rectangle, inside its target (a new encoder's is the whole
// target).
func (b *Backend) ClackScissor(r image.Rectangle) {
	if b.renderEnc == 0 {
		return
	}
	full := image.Rectangle{Max: b.rtSize}
	if full.Empty() {
		if r.Empty() {
			return
		}
	} else if r.Empty() {
		r = full
	} else {
		r = r.Intersect(full)
	}
	b.calls.API++
	if b.batch {
		b.record(cmdScissor, uint64(r.Min.X), uint64(r.Min.Y), uint64(r.Dx()), uint64(r.Dy()))
		return
	}
	C.renderEncScissor(b.renderEnc, C.NSUInteger(r.Min.X), C.NSUInteger(r.Min.Y), C.NSUInteger(r.Dx()), C.NSUInteger(r.Dy()))
	b.calls.Crossings++
}

// ClackCopyToTarget implements driver.ClackPartial: a blit encoder's copy
// into the target's texture (a drawable's when the layer isn't
// framebufferOnly, app/metal_darwin.go).
func (b *Backend) ClackCopyToTarget(target, src driver.Texture, size image.Point) {
	if t := target.(*Texture); t.width > 0 && t.height > 0 {
		size.X, size.Y = min(size.X, t.width), min(size.Y, t.height)
	}
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	b.CopyTexture(target, image.Point{}, src, image.Rectangle{Max: size})
}

func (b *Backend) endCmdBuffer(wait bool) {
	b.endEncoder()
	if b.cmdBuffer == 0 {
		return
	}
	C.cmdBufferCommit(b.cmdBuffer)
	b.apiCall()
	if wait {
		C.cmdBufferWaitUntilCompleted(b.cmdBuffer)
		b.apiCall()
	}
	if b.lastCmdBuffer != 0 {
		C.CFRelease(b.lastCmdBuffer)
		b.apiCall()
	}
	b.lastCmdBuffer = b.cmdBuffer
	b.cmdBuffer = 0
}

func (b *Backend) Caps() driver.Caps {
	return driver.Caps{
		MaxTextureSize: 8192,
		Features:       driver.FeatureSRGB | driver.FeatureCompute | driver.FeatureFloatRenderTargets,
	}
}

func (b *Backend) NewTimer() driver.Timer {
	panic("timers not supported")
}

func (b *Backend) IsTimeContinuous() bool {
	panic("timers not supported")
}

func (b *Backend) Release() {
	if b.cmdBuffer != 0 {
		C.CFRelease(b.cmdBuffer)
	}
	if b.lastCmdBuffer != 0 {
		C.CFRelease(b.lastCmdBuffer)
	}
	if b.stagingBuf != 0 {
		C.CFRelease(b.stagingBuf)
	}
	for _, s := range b.samplers {
		C.CFRelease(s)
	}
	C.CFRelease(b.queue)
	C.CFRelease(b.dev)
	*b = Backend{}
}

// ClackCalls implements driver.CallCounter (Clack patch 36): the cgo
// calls into Metal.
func (b *Backend) ClackCalls() driver.Calls {
	return b.calls
}

// sampler is the sampler state for a filter, shared by the textures that
// use it (Clack patch 37): a bind of another texture with the same filter
// keeps the sampler bound.
func (b *Backend) sampler(min, mag C.MTLSamplerMinMagFilter, mip C.MTLSamplerMipFilter) C.CFTypeRef {
	k := samplerKey{min, mag, mip}
	if s, ok := b.samplers[k]; ok {
		return s
	}
	s := C.newSampler(b.dev, min, mag, mip)
	b.apiCall()
	if s == 0 {
		return 0
	}
	if b.samplers == nil {
		b.samplers = make(map[samplerKey]C.CFTypeRef)
	}
	b.samplers[k] = s
	return s
}

func (b *Backend) NewTexture(format driver.TextureFormat, width, height int, minFilter, magFilter driver.TextureFilter, bindings driver.BufferBinding) (driver.Texture, error) {
	var mformat C.MTLPixelFormat
	if format == driver.TextureFormatOutput {
		mformat = b.pixelFmt // the layer's (Clack patch 46's frame texture)
	} else {
		mformat = pixelFormatFor(format)
	}
	var usage C.MTLTextureUsage
	if bindings&(driver.BufferBindingTexture|driver.BufferBindingShaderStorageRead) != 0 {
		usage |= C.MTLTextureUsageShaderRead
	}
	if bindings&driver.BufferBindingFramebuffer != 0 {
		usage |= C.MTLTextureUsageRenderTarget
	}
	if bindings&driver.BufferBindingShaderStorageWrite != 0 {
		usage |= C.MTLTextureUsageShaderWrite
	}
	min, mip := samplerFilterFor(minFilter)
	max, _ := samplerFilterFor(magFilter)
	mipmap := mip != C.MTLSamplerMipFilterNotMipmapped
	mipmapped := C.int(0)
	if mipmap {
		mipmapped = 1
	}
	tex := C.newTexture(b.dev, C.NSUInteger(width), C.NSUInteger(height), mformat, usage, mipmapped)
	b.apiCall()
	if tex == 0 {
		return nil, errors.New("metal: [MTLDevice newTextureWithDescriptor:] failed")
	}
	s := b.sampler(min, max, mip)
	if s == 0 {
		C.CFRelease(tex)
		return nil, errors.New("metal: [MTLDevice newSamplerStateWithDescriptor:] failed")
	}
	return &Texture{backend: b, texture: tex, sampler: s, width: width, height: height, mipmap: mipmap}, nil
}

func samplerFilterFor(f driver.TextureFilter) (C.MTLSamplerMinMagFilter, C.MTLSamplerMipFilter) {
	switch f {
	case driver.FilterNearest:
		return C.MTLSamplerMinMagFilterNearest, C.MTLSamplerMipFilterNotMipmapped
	case driver.FilterLinear:
		return C.MTLSamplerMinMagFilterLinear, C.MTLSamplerMipFilterNotMipmapped
	case driver.FilterLinearMipmapLinear:
		return C.MTLSamplerMinMagFilterLinear, C.MTLSamplerMipFilterLinear
	default:
		panic("invalid texture filter")
	}
}

func (b *Backend) NewPipeline(desc driver.PipelineDesc) (driver.Pipeline, error) {
	vsh, fsh := desc.VertexShader.(*Shader), desc.FragmentShader.(*Shader)
	layout := desc.VertexLayout.Inputs
	if got, exp := len(layout), len(vsh.inputs); got != exp {
		return nil, fmt.Errorf("metal: number of input descriptors (%d) doesn't match number of inputs (%d)", got, exp)
	}
	formats := make([]C.MTLVertexFormat, len(layout))
	offsets := make([]C.NSUInteger, len(layout))
	for i, inp := range layout {
		index := vsh.inputs[i].Location
		formats[index] = vertFormatFor(vsh.inputs[i])
		offsets[index] = C.NSUInteger(inp.Offset)
	}
	srcFactor := blendFactorFor(desc.BlendDesc.SrcFactor)
	dstFactor := blendFactorFor(desc.BlendDesc.DstFactor)
	blend := C.int(0)
	if desc.BlendDesc.IsEnabled() {
		blend = 1
	}
	pf := b.pixelFmt
	if f := desc.PixelFormat; f != driver.TextureFormatOutput {
		pf = pixelFormatFor(f)
	}
	pipe, err := splitErr(C.newRenderPipeline(
		b.dev,
		vsh.function,
		fsh.function,
		pf,
		attributeBufferIndex,
		C.NSUInteger(len(layout)), unsafe.SliceData(formats), unsafe.SliceData(offsets),
		C.NSUInteger(desc.VertexLayout.Stride),
		blend, srcFactor, dstFactor,
		2, // Number of vertex buffers.
		1, // Number of fragment buffers.
	))
	if err != nil {
		return nil, fmt.Errorf("metal: pipeline construction failed: %w", err)
	}
	return &Pipeline{pipeline: pipe, topology: primitiveFor(desc.Topology), vertUniforms: vsh.uniformRange(), fragUniforms: fsh.uniformRange()}, nil
}

func blendFactorFor(f driver.BlendFactor) C.MTLBlendFactor {
	switch f {
	case driver.BlendFactorZero:
		return C.MTLBlendFactorZero
	case driver.BlendFactorOne:
		return C.MTLBlendFactorOne
	case driver.BlendFactorOneMinusSrcAlpha:
		return C.MTLBlendFactorOneMinusSourceAlpha
	case driver.BlendFactorDstColor:
		return C.MTLBlendFactorDestinationColor
	default:
		panic("unsupported blend factor")
	}
}

func vertFormatFor(f shader.InputLocation) C.MTLVertexFormat {
	t := f.Type
	s := f.Size
	switch {
	case t == shader.DataTypeFloat && s == 1:
		return C.MTLVertexFormatFloat
	case t == shader.DataTypeFloat && s == 2:
		return C.MTLVertexFormatFloat2
	case t == shader.DataTypeFloat && s == 3:
		return C.MTLVertexFormatFloat3
	case t == shader.DataTypeFloat && s == 4:
		return C.MTLVertexFormatFloat4
	default:
		panic("unsupported data type")
	}
}

func pixelFormatFor(f driver.TextureFormat) C.MTLPixelFormat {
	switch f {
	case driver.TextureFormatFloat:
		return C.MTLPixelFormatR16Float
	case driver.TextureFormatRGBA8:
		return C.MTLPixelFormatRGBA8Unorm
	case driver.TextureFormatSRGBA:
		return C.MTLPixelFormatRGBA8Unorm_sRGB
	default:
		panic("unsupported pixel format")
	}
}

func (b *Backend) NewBuffer(typ driver.BufferBinding, size int) (driver.Buffer, error) {
	// Transfer buffer contents in command encoders on every use for
	// smaller buffers. The advantage is that buffer re-use during a frame
	// won't occur a GPU wait.
	// We can't do this for buffers written to by the GPU and read by the client,
	// and Metal doesn't require a buffer for indexed draws.
	if size <= 4096 && typ&(driver.BufferBindingShaderStorageWrite|driver.BufferBindingIndices) == 0 {
		return &Buffer{size: size, store: make([]byte, size)}, nil
	}
	buf := C.newBuffer(b.dev, C.NSUInteger(size), C.MTLResourceStorageModePrivate)
	b.apiCall()
	return &Buffer{backend: b, size: size, buffer: buf}, nil
}

func (b *Backend) NewImmutableBuffer(typ driver.BufferBinding, data []byte) (driver.Buffer, error) {
	buf, err := b.NewBuffer(typ, len(data))
	if err != nil {
		return nil, err
	}
	buf.Upload(data)
	return buf, nil
}

func (b *Backend) NewComputeProgram(src shader.Sources) (driver.Program, error) {
	sh, err := b.newShader(src)
	if err != nil {
		return nil, err
	}
	defer sh.Release()
	pipe, err := splitErr(C.newComputePipeline(b.dev, sh.function))
	if err != nil {
		return nil, fmt.Errorf("metal: compute program %q load failed: %w", src.Name, err)
	}
	return &Program{pipeline: pipe, groupSize: src.WorkgroupSize}, nil
}

func (b *Backend) NewVertexShader(src shader.Sources) (driver.VertexShader, error) {
	return b.newShader(src)
}

func (b *Backend) NewFragmentShader(src shader.Sources) (driver.FragmentShader, error) {
	return b.newShader(src)
}

func splitErr(o C.CFTypeAndError) (C.CFTypeRef, error) {
	if o.err != nil {
		defer C.free(unsafe.Pointer(o.err))
		return 0, errors.New(C.GoString(o.err))
	}
	return o.obj, nil
}

func (b *Backend) newShader(src shader.Sources) (*Shader, error) {
	vsrc := src.MetalLib
	cname := C.CString(src.Name)
	defer C.free(unsafe.Pointer(cname))
	vlib, err := splitErr(C.newLibrary(b.dev, cname, unsafe.Pointer(unsafe.StringData(vsrc)), C.size_t(len(vsrc))))
	if err != nil {
		return nil, fmt.Errorf("metal: shader %q load failed: %w", src.Name, err)
	}
	defer C.CFRelease(vlib)
	funcName := C.CString("main0")
	defer C.free(unsafe.Pointer(funcName))
	f := C.libraryNewFunction(vlib, funcName)
	if f == 0 {
		return nil, fmt.Errorf("metal: main function not found in %q", src.Name)
	}
	return &Shader{function: f, inputs: src.Inputs, uniforms: src.Uniforms}, nil
}

func (b *Backend) Viewport(x, y, width, height int) {
	enc := b.renderEnc
	if enc == 0 {
		panic("no active render pass")
	}
	v := C.MTLViewport{
		originX: C.double(x),
		originY: C.double(y),
		width:   C.double(width),
		height:  C.double(height),
		znear:   0.0,
		zfar:    1.0,
	}
	if driver.ClackStateCache && b.enc.hasViewport && b.enc.viewport == v {
		b.calls.Skipped++
		return
	}
	b.enc.viewport, b.enc.hasViewport = v, true
	b.calls.API++
	if b.batch {
		f := math.Float64bits
		b.record(cmdViewport, f(float64(v.originX)), f(float64(v.originY)), f(float64(v.width)), f(float64(v.height)), f(float64(v.znear)), f(float64(v.zfar)))
		return
	}
	C.renderEncViewport(enc, v)
	b.calls.Crossings++
}

func (b *Backend) DrawArrays(off, count int) {
	enc := b.renderEnc
	if enc == 0 {
		panic("no active render pass")
	}
	b.calls.API++
	if b.batch {
		b.record(cmdDraw, uint64(b.topology), uint64(off), uint64(count))
		return
	}
	C.renderEncDrawPrimitives(enc, b.topology, C.NSUInteger(off), C.NSUInteger(count))
	b.calls.Crossings++
}

func (b *Backend) DrawElements(off, count int) {
	enc := b.renderEnc
	if enc == 0 {
		panic("no active render pass")
	}
	b.calls.API++
	if b.batch {
		b.record(cmdDrawIndexed, uint64(b.topology), uint64(count), uint64(b.indexBuf.buffer), uint64(off))
		return
	}
	C.renderEncDrawIndexedPrimitives(enc, b.topology, b.indexBuf.buffer, C.NSUInteger(off), C.NSUInteger(count))
	b.calls.Crossings++
}

func primitiveFor(mode driver.Topology) C.MTLPrimitiveType {
	switch mode {
	case driver.TopologyTriangles:
		return C.MTLPrimitiveTypeTriangle
	case driver.TopologyTriangleStrip:
		return C.MTLPrimitiveTypeTriangleStrip
	default:
		panic("metal: unknown draw mode")
	}
}

func (b *Backend) BindImageTexture(unit int, tex driver.Texture) {
	b.BindTexture(unit, tex)
}

func (b *Backend) BeginCompute() {
	b.endEncoder()
	b.ensureCmdBuffer()
	for i := range b.bufSizes {
		b.bufSizes[i] = 0
	}
	b.computeEnc = C.cmdBufferComputeEncoder(b.cmdBuffer)
	if b.computeEnc == 0 {
		panic("metal: [MTLCommandBuffer computeCommandEncoder:] failed")
	}
}

func (b *Backend) EndCompute() {
	if b.computeEnc == 0 {
		panic("no active compute pass")
	}
	C.computeEncEnd(b.computeEnc)
	C.CFRelease(b.computeEnc)
	b.computeEnc = 0
}

func (b *Backend) DispatchCompute(x, y, z int) {
	enc := b.computeEnc
	if enc == 0 {
		panic("no active compute pass")
	}
	C.computeEncSetBytes(enc, unsafe.Pointer(unsafe.SliceData(b.bufSizes)), C.NSUInteger(len(b.bufSizes)*4), spvBufferSizeConstantsBinding)
	threadgroupsPerGrid := C.MTLSize{
		width: C.NSUInteger(x), height: C.NSUInteger(y), depth: C.NSUInteger(z),
	}
	sz := b.prog.groupSize
	threadsPerThreadgroup := C.MTLSize{
		width: C.NSUInteger(sz[0]), height: C.NSUInteger(sz[1]), depth: C.NSUInteger(sz[2]),
	}
	C.computeEncDispatch(enc, threadgroupsPerGrid, threadsPerThreadgroup)
}

func (b *Backend) stagingBuffer(size int) (C.CFTypeRef, int) {
	if b.stagingBuf == 0 || b.stagingOff+size > len(bufferStore(b.stagingBuf)) {
		if b.stagingBuf != 0 {
			C.CFRelease(b.stagingBuf)
		}
		cap := 2 * (b.stagingOff + size)
		b.stagingBuf = C.newBuffer(b.dev, C.NSUInteger(cap), C.MTLResourceStorageModeShared|C.MTLResourceCPUCacheModeWriteCombined)
		if b.stagingBuf == 0 {
			panic(fmt.Errorf("metal: failed to allocate %d bytes of staging buffer", cap))
		}
		b.stagingOff = 0
	}
	off := b.stagingOff
	b.stagingOff += size
	return b.stagingBuf, off
}

func (t *Texture) Upload(offset, size image.Point, pixels []byte, stride int) {
	if len(pixels) == 0 {
		return
	}
	if stride == 0 {
		stride = size.X * 4
	}
	dstStride := size.X * 4
	n := size.Y * dstStride
	buf, off := t.backend.stagingBuffer(n)
	store := bufferSlice(buf, off, n)
	var srcOff, dstOff int
	for y := 0; y < size.Y; y++ {
		srcRow := pixels[srcOff : srcOff+dstStride]
		dstRow := store[dstOff : dstOff+dstStride]
		copy(dstRow, srcRow)
		dstOff += dstStride
		srcOff += stride
	}
	enc := t.backend.startBlit()
	orig := C.MTLOrigin{
		x: C.NSUInteger(offset.X),
		y: C.NSUInteger(offset.Y),
	}
	msize := C.MTLSize{
		width:  C.NSUInteger(size.X),
		height: C.NSUInteger(size.Y),
		depth:  1,
	}
	C.blitEncCopyBufferToTexture(enc, buf, t.texture, C.NSUInteger(off), C.NSUInteger(dstStride), C.NSUInteger(len(store)), msize, orig)
	t.backend.apiCall()
	if t.mipmap {
		C.blitEncGenerateMipmapsForTexture(enc, t.texture)
		t.backend.apiCall()
	}
}

func (t *Texture) Release() {
	if t.foreign {
		panic("metal: release of external texture")
	}
	C.CFRelease(t.texture)
	// the sampler is the backend's, shared (Clack patch 37)
	*t = Texture{}
}

func (p *Pipeline) Release() {
	C.CFRelease(p.pipeline)
	*p = Pipeline{}
}

func (b *Backend) PrepareTexture(tex driver.Texture) {}

func (b *Backend) BindTexture(unit int, tex driver.Texture) {
	t := tex.(*Texture)
	if enc := b.renderEnc; enc != 0 {
		if driver.ClackStateCache && b.enc.tex[unit] == t.texture {
			b.calls.Skipped++
		} else {
			b.enc.tex[unit] = t.texture
			b.calls.API++
			if b.batch {
				b.record(cmdFragTexture, uint64(unit), uint64(t.texture))
			} else {
				C.renderEncSetFragmentTexture(enc, C.NSUInteger(unit), t.texture)
				b.calls.Crossings++
			}
		}
		if driver.ClackStateCache && b.enc.sampler[unit] == t.sampler {
			b.calls.Skipped++
		} else {
			b.enc.sampler[unit] = t.sampler
			b.calls.API++
			if b.batch {
				b.record(cmdFragSampler, uint64(unit), uint64(t.sampler))
			} else {
				C.renderEncSetFragmentSamplerState(enc, C.NSUInteger(unit), t.sampler)
				b.calls.Crossings++
			}
		}
	} else if enc := b.computeEnc; enc != 0 {
		C.computeEncSetTexture(enc, C.NSUInteger(unit), t.texture)
	} else {
		panic("no active render nor compute pass")
	}
}

func (b *Backend) ensureCmdBuffer() {
	if b.cmdBuffer != 0 {
		return
	}
	b.cmdBuffer = C.queueNewBuffer(b.queue)
	b.apiCall()
	if b.cmdBuffer == 0 {
		panic("metal: [MTLCommandQueue cmdBuffer] failed")
	}
}

func (b *Backend) BindPipeline(pipe driver.Pipeline) {
	p := pipe.(*Pipeline)
	enc := b.renderEnc
	if enc == 0 {
		panic("no active render pass")
	}
	b.topology = p.topology
	if driver.ClackStateCache && b.enc.pipeline == p {
		b.calls.Skipped++
		return
	}
	b.enc.pipeline = p
	b.calls.API++
	if b.batch {
		b.record(cmdPipeline, uint64(p.pipeline))
		return
	}
	C.renderEncSetRenderPipelineState(enc, p.pipeline)
	b.calls.Crossings++
}

func (b *Backend) BindProgram(prog driver.Program) {
	enc := b.computeEnc
	if enc == 0 {
		panic("no active compute pass")
	}
	p := prog.(*Program)
	C.computeEncSetPipeline(enc, p.pipeline)
	b.prog = p
}

func (s *Shader) Release() {
	C.CFRelease(s.function)
	*s = Shader{}
}

func (p *Program) Release() {
	C.CFRelease(p.pipeline)
	*p = Program{}
}

func (b *Backend) BindStorageBuffer(binding int, buffer driver.Buffer) {
	buf := buffer.(*Buffer)
	b.bufSizes[binding] = uint32(buf.size)
	enc := b.computeEnc
	if enc == 0 {
		panic("no active compute pass")
	}
	if buf.buffer != 0 {
		C.computeEncSetBuffer(enc, C.NSUInteger(binding), buf.buffer)
	} else if buf.size > 0 {
		C.computeEncSetBytes(enc, unsafe.Pointer(unsafe.SliceData(buf.store)), C.NSUInteger(buf.size), C.NSUInteger(binding))
	}
}

// BindUniforms sends the uniforms to the stages of the bound pipeline that
// read them, unless a stage would see the bytes it was last sent (Clack
// patch 37). Shaders that read no uniforms (the stencil and intersection
// fragment shaders, the texture materials') aren't sent any.
func (b *Backend) BindUniforms(buf driver.Buffer) {
	bf := buf.(*Buffer)
	enc := b.renderEnc
	if enc == 0 {
		panic("no active render pass")
	}
	p := b.enc.pipeline
	vert, frag := true, true
	cache := driver.ClackStateCache
	if p != nil && cache {
		vert, frag = p.vertUniforms[1] > 0, p.fragUniforms[1] > 0
	}
	if bf.buffer != 0 {
		if vert {
			if u := &b.enc.vertUniforms; cache && u.set && u.buf == bf.buffer {
				b.calls.Skipped++
			} else {
				b.calls.API++
				if b.batch {
					b.record(cmdVertBuffer, uniformBufferIndex, uint64(bf.buffer), 0)
				} else {
					C.renderEncSetVertexBuffer(enc, bf.buffer, uniformBufferIndex, 0)
					b.calls.Crossings++
				}
				u.buf, u.set = bf.buffer, true
			}
		}
		if frag {
			if u := &b.enc.fragUniforms; cache && u.set && u.buf == bf.buffer {
				b.calls.Skipped++
			} else {
				b.calls.API++
				if b.batch {
					b.record(cmdFragBuffer, uniformBufferIndex, uint64(bf.buffer), 0)
				} else {
					C.renderEncSetFragmentBuffer(enc, bf.buffer, uniformBufferIndex, 0)
					b.calls.Crossings++
				}
				u.buf, u.set = bf.buffer, true
			}
		}
	} else if bf.size > 0 {
		data := bf.store[:bf.size]
		if vert {
			if cache && p != nil && b.enc.vertUniforms.same(data, p.vertUniforms) {
				b.calls.Skipped++
			} else {
				b.calls.API++
				if b.batch {
					b.recordBytes(cmdVertBytes, uniformBufferIndex, data)
				} else {
					C.renderEncSetVertexBytes(enc, unsafe.Pointer(unsafe.SliceData(bf.store)), C.NSUInteger(bf.size), uniformBufferIndex)
					b.calls.Crossings++
				}
				b.enc.vertUniforms.sent(data)
			}
		} else {
			b.calls.Skipped++
		}
		if frag {
			if cache && p != nil && b.enc.fragUniforms.same(data, p.fragUniforms) {
				b.calls.Skipped++
			} else {
				b.calls.API++
				if b.batch {
					b.recordBytes(cmdFragBytes, uniformBufferIndex, data)
				} else {
					C.renderEncSetFragmentBytes(enc, unsafe.Pointer(unsafe.SliceData(bf.store)), C.NSUInteger(bf.size), uniformBufferIndex)
					b.calls.Crossings++
				}
				b.enc.fragUniforms.sent(data)
			}
		} else {
			b.calls.Skipped++
		}
	}
}

// BindVertexBuffer binds the attribute buffer, unless the encoder has it
// (Clack patch 37): the same Metal buffer at the same offset, or the same
// Go buffer's bytes not uploaded again since.
func (b *Backend) BindVertexBuffer(buf driver.Buffer, offset int) {
	bf := buf.(*Buffer)
	enc := b.renderEnc
	if enc == 0 {
		panic("no active render pass")
	}
	v := &b.enc.vert
	cache := driver.ClackStateCache
	if bf.buffer != 0 {
		switch {
		case cache && v.buf == bf && v.off == offset:
			b.calls.Skipped++
		case cache && v.buf == bf:
			b.calls.API++
			if b.batch {
				b.record(cmdVertBufferOffset, attributeBufferIndex, uint64(offset))
			} else {
				C.renderEncSetVertexBufferOffset(enc, attributeBufferIndex, C.NSUInteger(offset))
				b.calls.Crossings++
			}
		default:
			b.calls.API++
			if b.batch {
				b.record(cmdVertBuffer, attributeBufferIndex, uint64(bf.buffer), uint64(offset))
			} else {
				C.renderEncSetVertexBuffer(enc, bf.buffer, attributeBufferIndex, C.NSUInteger(offset))
				b.calls.Crossings++
			}
		}
		v.buf, v.off = bf, offset
	} else if n := bf.size - offset; n > 0 {
		if cache && v.buf == bf && v.off == offset && v.gen == bf.gen {
			b.calls.Skipped++
			return
		}
		b.calls.API++
		if b.batch {
			b.recordBytes(cmdVertBytes, attributeBufferIndex, bf.store[offset:bf.size])
		} else {
			C.renderEncSetVertexBytes(enc, unsafe.Pointer(&bf.store[offset]), C.NSUInteger(n), attributeBufferIndex)
			b.calls.Crossings++
		}
		v.buf, v.off, v.gen = bf, offset, bf.gen
	}
}

func (b *Backend) BindIndexBuffer(buf driver.Buffer) {
	b.indexBuf = buf.(*Buffer)
}

func (b *Buffer) Download(data []byte) error {
	if len(data) > b.size {
		panic(fmt.Errorf("len(data) (%d) larger than len(content) (%d)", len(data), b.size))
	}
	buf, off := b.backend.stagingBuffer(len(data))
	enc := b.backend.startBlit()
	C.blitEncCopyBufferToBuffer(enc, b.buffer, buf, 0, C.NSUInteger(off), C.NSUInteger(len(data)))
	b.backend.endCmdBuffer(true)
	store := bufferSlice(buf, off, len(data))
	copy(data, store)
	return nil
}

func (b *Buffer) Upload(data []byte) {
	if len(data) > b.size {
		panic(fmt.Errorf("len(data) (%d) larger than len(content) (%d)", len(data), b.size))
	}
	if b.buffer == 0 {
		copy(b.store, data)
		b.gen++
		return
	}
	buf, off := b.backend.stagingBuffer(len(data))
	store := bufferSlice(buf, off, len(data))
	copy(store, data)
	enc := b.backend.startBlit()
	C.blitEncCopyBufferToBuffer(enc, buf, b.buffer, C.NSUInteger(off), 0, C.NSUInteger(len(store)))
	b.backend.apiCall()
}

func bufferStore(buf C.CFTypeRef) []byte {
	contents := C.bufferContents(buf)
	return (*(*[1 << 30]byte)(contents.addr))[:contents.size:contents.size]
}

func bufferSlice(buf C.CFTypeRef, off, len int) []byte {
	store := bufferStore(buf)
	return store[off : off+len]
}

func (b *Buffer) Release() {
	if b.buffer != 0 {
		C.CFRelease(b.buffer)
		if b.backend != nil {
			b.backend.apiCall()
		}
	}
	*b = Buffer{}
}

func (t *Texture) ReadPixels(src image.Rectangle, pixels []byte, stride int) error {
	if len(pixels) == 0 {
		return nil
	}
	if t.foreign && bool(C.textureFramebufferOnly(t.texture)) {
		// Clack patch 46: a window's drawable, which no blit may read
		// (gpu.ClackVerifyCopy reads one only when the layer's drawables
		// were made readable)
		return errors.New("metal: the texture is framebufferOnly")
	}
	sz := src.Size()
	orig := C.MTLOrigin{
		x: C.NSUInteger(src.Min.X),
		y: C.NSUInteger(src.Min.Y),
	}
	msize := C.MTLSize{
		width:  C.NSUInteger(sz.X),
		height: C.NSUInteger(sz.Y),
		depth:  1,
	}
	stageStride := sz.X * 4
	n := sz.Y * stageStride
	buf, off := t.backend.stagingBuffer(n)
	enc := t.backend.startBlit()
	C.blitEncCopyTextureToBuffer(enc, t.texture, buf, C.NSUInteger(off), C.NSUInteger(stageStride), C.NSUInteger(n), msize, orig)
	t.backend.endCmdBuffer(true)
	store := bufferSlice(buf, off, n)
	var srcOff, dstOff int
	for y := 0; y < sz.Y; y++ {
		dstRow := pixels[srcOff : srcOff+stageStride]
		srcRow := store[dstOff : dstOff+stageStride]
		copy(dstRow, srcRow)
		dstOff += stageStride
		srcOff += stride
	}
	return nil
}

func (b *Backend) BeginRenderPass(tex driver.Texture, d driver.LoadDesc) {
	b.endEncoder()
	b.ensureCmdBuffer()
	f := tex.(*Texture)
	col := d.ClearColor
	var act C.MTLLoadAction
	switch d.Action {
	case driver.LoadActionKeep:
		act = C.MTLLoadActionLoad
	case driver.LoadActionClear:
		act = C.MTLLoadActionClear
	case driver.LoadActionInvalidate:
		act = C.MTLLoadActionDontCare
	}
	b.renderEnc = C.cmdBufferRenderEncoder(b.cmdBuffer, f.texture, act, C.float(col.R), C.float(col.G), C.float(col.B), C.float(col.A))
	b.apiCall()
	if b.renderEnc == 0 {
		panic("metal: [MTLCommandBuffer renderCommandEncoderWithDescriptor:] failed")
	}
	// a new encoder has nothing bound; its commands are recorded and run
	// in one cgo call when it ends (Clack patch 37). It's unscissored
	// (patch 46).
	b.enc.reset()
	b.rtSize = image.Pt(f.width, f.height)
	b.batch = driver.ClackBatchCommands
	b.rec = b.rec[:0]
}

func (b *Backend) EndRenderPass() {
	if b.renderEnc == 0 {
		panic("no active render pass")
	}
	b.runRecorded()
	C.renderEncEnd(b.renderEnc)
	C.CFRelease(b.renderEnc)
	b.apiCall()
	b.apiCall()
	b.renderEnc = 0
	b.enc.reset()
	b.batch = false
}

// apiCall counts a call into Metal that is a cgo call of its own.
func (b *Backend) apiCall() {
	b.calls.API++
	b.calls.Crossings++
}

func (e *encState) reset() {
	vb, fb := e.vertUniforms.bytes[:0], e.fragUniforms.bytes[:0]
	*e = encState{}
	e.vertUniforms.bytes, e.fragUniforms.bytes = vb, fb
}

func (b *Backend) endEncoder() {
	if b.renderEnc != 0 {
		panic("active render pass")
	}
	if b.computeEnc != 0 {
		panic("active compute pass")
	}
	if b.blitEnc != 0 {
		C.blitEncEnd(b.blitEnc)
		C.CFRelease(b.blitEnc)
		b.apiCall()
		b.apiCall()
		b.blitEnc = 0
	}
}

func (f *Texture) ImplementsRenderTarget() {}
