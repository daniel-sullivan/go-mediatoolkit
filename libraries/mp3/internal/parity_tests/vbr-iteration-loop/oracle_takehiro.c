// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Second translation unit of the vbr-iteration-loop oracle: the GENUINE vendored
 * LAME 3.100 bit-counting machinery (libmp3lame/takehiro.c + tables.c) that the
 * VBR iteration loops reach via iteration_finish_one (best_scalefac_store /
 * best_huffman_divide), outer_loop (scale_bitcount / count_bits) and
 * VBR_encode_frame (in the sibling oracle_vbrquantize.c TU). tables.c also
 * defines the bitrate_table the getframebits hand-twin (oracle.c) reads extern.
 *
 * Why a separate .c: vbrquantize.c (oracle_vbrquantize.c) and takehiro.c each
 * define their OWN file-local `union fi_union` + MAGIC_INT / MAGIC_FLOAT for the
 * TAKEHIRO_IEEE754_HACK, so they cannot share a TU. cgo compiles each package .c
 * separately, keeping the statics private while the linker resolves the NON-
 * static bit-counters takehiro.c exports. The precompute tables (pow43,
 * adj43asm, pretab, nr_of_sfb_block) are defined in oracle.c's TU (via
 * quantize_pvt.c) and referenced extern here through quantize_pvt.h.
 */

#include <config.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include <math.h>

#include "libmp3lame/tables.c"
#include "libmp3lame/takehiro.c"
