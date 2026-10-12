// Shared Metal device / pipeline / encode. Platform files acquire the
// destination texture (IOSurface vs CAMetalLayer drawable).
#ifndef SHIREI_GPURENDER_METAL_COMMON_H
#define SHIREI_GPURENDER_METAL_COMMON_H

#import <Metal/Metal.h>
#include "metal.h"

void metal_seterr(const char *fmt, ...);
int64_t metal_now_ns(void);

// Takes ownership of device (+1). No-op success if already initialized.
int metal_common_init(id<MTLDevice> device);

id<MTLDevice> metal_device(void);
id<MTLCommandQueue> metal_queue(void);

// Copies quads, blits pending uploads, and encodes draws onto tex.
// Does not commit cmd — caller presents / adds handlers, then commits.
int metal_encode(
	id<MTLCommandBuffer> cmd,
	id<MTLTexture> tex,
	int w, int h,
	const GPUQuad *quads, int nquads,
	const GPUBatch *batches, int nbatches,
	const GPUUpload *uploads, int nuploads,
	int do_clear, double clearR, double clearG, double clearB, double clearA);

#endif
