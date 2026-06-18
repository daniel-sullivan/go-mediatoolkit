// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Second translation unit of the vbrquantize-frame oracle: the GENUINE vendored
 * LAME 3.100 bit-counting machinery (libmp3lame/takehiro.c + tables.c) that
 * VBR_encode_frame's bitcount / quantizeAndCountBits / reduce_bit_usage call.
 *
 * Why a separate .c: vbrquantize.c (compiled in oracle.c) and takehiro.c each
 * define their OWN file-local `union fi_union` + MAGIC_INT / MAGIC_FLOAT for the
 * TAKEHIRO_IEEE754_HACK, so #including both in one TU is a typedef/macro
 * redefinition error. Compiling them in disjoint TUs (cgo compiles each package
 * .c separately) keeps each file's statics private while the linker resolves the
 * NON-static bit-counters takehiro.c exports — best_scalefac_store,
 * best_huffman_divide, scale_bitcount, noquant_count_bits, huffman_init,
 * choose_table_nonMMX — into vbrquantize.c's references. This mirrors the per-TU
 * split the FLAC oracle uses (add-audio-format SKILL).
 *
 * The shared precompute tables (pretab / nr_of_sfb_block / pow* / adj43*) and the
 * lame_errorf ERRORF stub are defined ONCE in oracle.c; this TU references them
 * extern (via quantize_pvt.h / the natural externs takehiro.c declares).
 */

#include <config.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include <math.h>

#include "libmp3lame/tables.c"
#include "libmp3lame/takehiro.c"
