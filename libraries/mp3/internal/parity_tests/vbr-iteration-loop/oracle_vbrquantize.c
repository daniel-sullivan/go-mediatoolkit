// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Third translation unit of the vbr-iteration-loop oracle: the GENUINE vendored
 * LAME 3.100 whole-frame VBR quantizer (libmp3lame/vbrquantize.c) that
 * VBR_new_iteration_loop delegates to via VBR_encode_frame. It runs block_sf,
 * the allocator, quantize_x34 + the bit-counter (resolved into the takehiro TU),
 * and the out-of-budget redistribution.
 *
 * Why a separate .c: vbrquantize.c carries its OWN file-local `union fi_union` +
 * MAGIC for the TAKEHIRO_IEEE754_HACK, which collides with takehiro.c's; the two
 * therefore live in disjoint TUs. The precompute tables (pow43 / adj43asm etc.)
 * are defined
 * in oracle.c's TU (via quantize_pvt.c) and referenced extern here through
 * quantize_pvt.h. lame_errorf (the ERRORF sink) is defined once in oracle.c.
 */

#include <config.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <stdarg.h>
#include <math.h>

#include "lame.h"
#include "machine.h"
#include "encoder.h"
#include "util.h"
#include "quantize_pvt.h"

#include "libmp3lame/vbrquantize.c"
