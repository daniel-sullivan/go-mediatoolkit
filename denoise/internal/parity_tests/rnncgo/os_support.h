#ifndef OS_SUPPORT_H
#define OS_SUPPORT_H

/* Harness-only shim. RNNoise's vec.h generic (scalar) branch — which we
 * force via -DDISABLE_NEON so tanh/sigmoid/exp are reproducible in Go —
 * #includes "os_support.h" for OPUS_CLEAR. That header lives in the
 * parent opus/celt tree and is NOT part of RNNoise's own vendored src
 * (upstream only ever compiles the SIMD branches, which do not need it).
 * We provide the standard Xiph definitions here; they are plain
 * memset/memcpy/memmove wrappers, bit-exact by construction. This is a
 * harness header on the cgo include path, not a modification of the
 * vendored source (same spirit as the FLAC cosf shim). */

#include <string.h>
#include <stdlib.h>

#define OPUS_COPY(dst, src, n)  (memcpy((dst), (src), (n)*sizeof(*(dst)) + 0*((dst)-(src))))
#define OPUS_MOVE(dst, src, n)  (memmove((dst), (src), (n)*sizeof(*(dst)) + 0*((dst)-(src))))
#define OPUS_CLEAR(dst, n)      (memset((dst), 0, (n)*sizeof(*(dst))))

#endif
