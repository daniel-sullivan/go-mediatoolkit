/* Amalgamation translation unit #1: shared libFLAC support code that
 * does not redefine the static `read_callback_`, `set_defaults_`,
 * `init_stream_internal_`, … helper names. The four files that DO
 * redefine those helpers each get their own cgo translation unit
 * (flac_cgo_decoder.c, flac_cgo_encoder.c, flac_cgo_meta_iter.c,
 * flac_cgo_meta_obj.c) so static linkage stays per-file as intended.
 */

#include "libflac/src/libFLAC/bitmath.c"
#include "libflac/src/libFLAC/bitreader.c"
#include "libflac/src/libFLAC/bitwriter.c"
#include "libflac/src/libFLAC/cpu.c"
#include "libflac/src/libFLAC/crc.c"
#include "libflac/src/libFLAC/fixed.c"
#include "libflac/src/libFLAC/float.c"
#include "libflac/src/libFLAC/format.c"
#include "libflac/src/libFLAC/lpc.c"
#include "libflac/src/libFLAC/md5.c"
#include "libflac/src/libFLAC/memory.c"
#include "libflac/src/libFLAC/stream_encoder_framing.c"
#include "libflac/src/libFLAC/window.c"

/* Intrinsic kernels — empty TUs in the scalar baseline (config.h leaves
 * FLAC__HAS_X86INTRIN / FLAC__HAS_NEONINTRIN undefined). */
#include "libflac/src/libFLAC/fixed_intrin_avx2.c"
#include "libflac/src/libFLAC/fixed_intrin_sse2.c"
#include "libflac/src/libFLAC/fixed_intrin_sse42.c"
#include "libflac/src/libFLAC/fixed_intrin_ssse3.c"
#include "libflac/src/libFLAC/lpc_intrin_avx2.c"
#include "libflac/src/libFLAC/lpc_intrin_fma.c"
#include "libflac/src/libFLAC/lpc_intrin_neon.c"
#include "libflac/src/libFLAC/lpc_intrin_sse2.c"
#include "libflac/src/libFLAC/lpc_intrin_sse41.c"
#include "libflac/src/libFLAC/stream_encoder_intrin_avx2.c"
#include "libflac/src/libFLAC/stream_encoder_intrin_sse2.c"
#include "libflac/src/libFLAC/stream_encoder_intrin_ssse3.c"
