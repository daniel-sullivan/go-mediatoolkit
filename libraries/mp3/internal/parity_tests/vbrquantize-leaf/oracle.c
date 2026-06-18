// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Compiles a PRIVATE copy of the vendored LAME 3.100 vbrquantize.c VBR
 * quantizer and surfaces its floating-point leaf kernels to Go-cgo via oracle_*
 * trampolines, for the "vbrquantize-leaf" parity slice.
 *
 * Scope. This oracle pins nativemp3's VBR leaf kernels (vbrquantize.go):
 * vec_max_c, find_lowest_scalefac, k_34_4 (the TAKEHIRO_IEEE754_HACK
 * magic-float quantize), calc_sfb_noise_x34, tri_calc_sfb_noise_x34,
 * calc_scalefac, guess_scalefac_x34 and find_scalefac_x34. Each is exercised
 * through the genuine vendored code #included below. The block_sf /
 * calc_*_block_vbr_sf / VBR_* drivers in the same TU are compiled (they share
 * the file) but never CALLED by this oracle — they are a later slice — so their
 * unresolved external callees (set_scalefactors, bitcount, …) are satisfied by
 * the inert link-only stubs at the bottom; the leaf kernels this oracle calls
 * never reach them.
 *
 * Discipline (per CONTRIBUTING.md "parity oracle per
 * slice"): each go-test binary is one package wide, so this private LAME copy
 * never collides with the one libraries/mp3 compiles for production. This
 * package never imports libraries/mp3; it imports only internal/nativemp3.
 *
 * This slice IS floating-point-bearing: every band-noise multiply/add and the
 * magic-float add round separately, so the result is only bit-exact under the
 * mp3_strict build (FMA-free Go) against the -ffp-contract=off cgo oracle. The
 * scalar FP flags come from the mise task env, never from the in-source #cgo
 * block, matching the rest of the suite.
 */

#include <config.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include <math.h>

#include "lame.h"
#include "machine.h"
#include "encoder.h"
#include "util.h"
#include "quantize_pvt.h"

/* ---- precompute tables the kernels read.
 *
 * vbrquantize.c declares pow20 / ipow20 / pow43 / adj43asm extern (via
 * quantize_pvt.h) and iteration_init (quantize_pvt.c) fills them. Rather than
 * link the whole iteration_init dependency tree, define them here and fill them
 * with a VERBATIM copy of iteration_init's table-fill loop (quantize_pvt.c:351-
 * 367, the TAKEHIRO_IEEE754_HACK branch the vendored config selects). This is a
 * documented hand-twin of the genuine fill; the formulas and the double->float
 * narrowings are copied line-for-line, and the table sizes are the genuine
 * #define values (PRECALC_SIZE / Q_MAX / Q_MAX2) so any drift would fail to
 * compile. The Go side runs the identical loop (InitQuantizePvtTables +
 * InitVbrQuantizeTables). ---- */

FLOAT   pow43[PRECALC_SIZE];
FLOAT   adj43asm[PRECALC_SIZE];
FLOAT   adj43[PRECALC_SIZE]; /* unused by the hack kernels; defined for the TU */
FLOAT   pow20[Q_MAX + Q_MAX2 + 1];
FLOAT   ipow20[Q_MAX];

void
oracle_fill_tables(void)
{
    int     i;
    static int filled = 0;
    if (filled)
        return;
    filled = 1;

    /* quantize_pvt.c:351-353 */
    pow43[0] = 0.0;
    for (i = 1; i < PRECALC_SIZE; i++)
        pow43[i] = pow((FLOAT) i, 4.0 / 3.0);

    /* quantize_pvt.c:355-358 (TAKEHIRO_IEEE754_HACK branch) */
    adj43asm[0] = 0.0;
    for (i = 1; i < PRECALC_SIZE; i++)
        adj43asm[i] = i - 0.5 - pow(0.5 * (pow43[i - 1] + pow43[i]), 0.75);

    /* quantize_pvt.c:364-367 */
    for (i = 0; i < Q_MAX; i++)
        ipow20[i] = pow(2.0, (double) (i - 210) * -0.1875);
    for (i = 0; i <= Q_MAX + Q_MAX2; i++)
        pow20[i] = pow(2.0, (double) (i - 210 - Q_MAX2) * 0.25);
}

/* ---- the genuine vendored VBR quantizer. #include brings the file-static leaf
 * kernels into this TU so the trampolines below call the real LAME code. ---- */
#include "libmp3lame/vbrquantize.c"

/* ---- link-only stubs for symbols vbrquantize.c's compiled-but-unreached
 * drivers reference (block_sf -> the gfc->find dispatch and the alloc helpers,
 * VBR_* -> bitcount / ResvFrameBegin / etc.). The leaf kernels this oracle
 * calls never invoke them. lame_errorf is the only varargs one. ---- */

void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    (void) gfc;
    (void) format;
}

/* pretab carries the verbatim quantize_pvt.c:159-162 values (so even an
 * accidental read by an unreached driver is faithful). The leaf kernels this
 * oracle calls never index it. */
const int pretab[SBMAX_l] = {
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    1, 1, 1, 1, 2, 2, 3, 3, 3, 2, 0
};

/* Inert stubs for the takehiro.c bit-counting routines the compiled-but-
 * unreached VBR drivers (VBR_encode_frame / quantizeAndCountBits) reference.
 * The leaf kernels this oracle calls never invoke them; returning 0 keeps the
 * link satisfied without pulling in the whole bit-counting TU. */
int noquant_count_bits(lame_internal_flags const *const gfc, gr_info *const cod_info,
                       calc_noise_data *prev_noise) {
    (void) gfc; (void) cod_info; (void) prev_noise;
    return 0;
}
int scale_bitcount(const lame_internal_flags *gfc, gr_info *cod_info) {
    (void) gfc; (void) cod_info;
    return 0;
}
void best_huffman_divide(const lame_internal_flags *const gfc, gr_info *const cod_info) {
    (void) gfc; (void) cod_info;
}
void best_scalefac_store(const lame_internal_flags *gfc, const int gr, const int ch,
                         III_side_info_t *const l3_side) {
    (void) gfc; (void) gr; (void) ch; (void) l3_side;
}

/* ---- trampolines: each calls the genuine static kernel above. ---- */

float oracle_vec_max_c(const float *xr34, unsigned int bw) {
    return vec_max_c(xr34, bw);
}

unsigned char oracle_find_lowest_scalefac(float xr34) {
    return find_lowest_scalefac(xr34);
}

void oracle_k_34_4(double *x, int *l3) {
    /* DOUBLEX == double under TAKEHIRO_IEEE754_HACK; x is double[4]. */
    k_34_4(x, l3);
}

float oracle_calc_sfb_noise_x34(const float *xr, const float *xr34,
                                unsigned int bw, unsigned char sf) {
    return calc_sfb_noise_x34(xr, xr34, bw, sf);
}

unsigned char oracle_tri_calc_sfb_noise_x34(const float *xr, const float *xr34,
                                            float l3_xmin, unsigned int bw,
                                            unsigned char sf) {
    calc_noise_cache_t did_it[256];
    memset(did_it, 0, sizeof(did_it));
    return tri_calc_sfb_noise_x34(xr, xr34, l3_xmin, bw, sf, did_it);
}

int oracle_calc_scalefac(float l3_xmin, int bw) {
    return calc_scalefac(l3_xmin, bw);
}

unsigned char oracle_guess_scalefac_x34(const float *xr, const float *xr34,
                                        float l3_xmin, unsigned int bw,
                                        unsigned char sf_min) {
    return guess_scalefac_x34(xr, xr34, l3_xmin, bw, sf_min);
}

unsigned char oracle_find_scalefac_x34(const float *xr, const float *xr34,
                                       float l3_xmin, unsigned int bw,
                                       unsigned char sf_min) {
    return find_scalefac_x34(xr, xr34, l3_xmin, bw, sf_min);
}
