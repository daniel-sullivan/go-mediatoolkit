/* FLAC native-vs-Cgo benchmark — shared libFLAC support TU.
 *
 * This translation unit compiles every libFLAC source EXCEPT
 * stream_encoder.c and stream_decoder.c, which carry file-static helpers
 * (read_callback_, set_defaults_, …) that collide between the two. Those
 * two live in their own TUs (bench_encoder.c / bench_decoder.c), mirroring
 * the amalgamation split the e2e parity packages use.
 */

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/bitreader.c"
#include "src/libFLAC/bitwriter.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/fixed.c"
#include "src/libFLAC/float.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/lpc.c"
#include "src/libFLAC/md5.c"
#include "src/libFLAC/memory.c"
#include "src/libFLAC/stream_encoder_framing.c"

/* FP-parity transcendental shim for window.c: it is the only libFLAC TU
 * that calls single-precision libm (cosf, fabsf), which are neither
 * correctly-rounded nor portable. Redirect each to its double kernel
 * narrowed to float so the oracle matches the Go port's
 * float32(math.Cos(float64(x))) bit-for-bit on every platform. <math.h>
 * is included first so its own (un-macroed) declarations are processed
 * before the macros rewrite the call sites in window.c's body. */
#include <math.h>
#define cosf(x) ((float)cos((double)(x)))
#define fabsf(x) ((float)fabs((double)(x)))

#include "src/libFLAC/window.c"

/* Intrinsic kernels — empty TUs in the scalar baseline (config.h leaves
 * FLAC__HAS_X86INTRIN / FLAC__HAS_NEONINTRIN undefined). */
#include "src/libFLAC/fixed_intrin_avx2.c"
#include "src/libFLAC/fixed_intrin_sse2.c"
#include "src/libFLAC/fixed_intrin_sse42.c"
#include "src/libFLAC/fixed_intrin_ssse3.c"
#include "src/libFLAC/lpc_intrin_avx2.c"
#include "src/libFLAC/lpc_intrin_fma.c"
#include "src/libFLAC/lpc_intrin_neon.c"
#include "src/libFLAC/lpc_intrin_sse2.c"
#include "src/libFLAC/lpc_intrin_sse41.c"
#include "src/libFLAC/stream_encoder_intrin_avx2.c"
#include "src/libFLAC/stream_encoder_intrin_sse2.c"
#include "src/libFLAC/stream_encoder_intrin_ssse3.c"

/* On Windows (incl. mingw) libFLAC's compat.h redirects flac_fprintf/
 * flac_fopen to fprintf_utf8/fopen_utf8 (lpc.c / stream_*coder.c call
 * them), which live in share/win_utf8_io.c. That TU is not otherwise
 * compiled into this parity binary, and this support TU is the single
 * aggregator for the benchcmp package, so pull it in here to satisfy
 * the link. No-op on non-Windows, where compat.h maps the flac_* macros
 * straight to the stdio functions. */
#ifdef _WIN32
#include "src/share/win_utf8_io/win_utf8_io.c"
#endif
