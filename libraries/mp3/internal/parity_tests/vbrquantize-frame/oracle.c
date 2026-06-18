// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Compiles a PRIVATE copy of the vendored LAME 3.100 VBR quantizer top tier and
 * surfaces VBR_encode_frame (plus the static bit-search drivers it runs) to
 * Go-cgo via mp3parity_vbrfr_* trampolines, for the "vbrquantize-frame" parity
 * slice.
 *
 * Scope. This oracle pins nativemp3's VBR bit-search orchestration tier
 * (vbrquantize_frame.go): tryGlobalStepsize .. outOfBitsStrategy, reduce_bit_-
 * usage and VBR_encode_frame. Unlike the vbrquantize-sfalloc oracle (which
 * stubbed the bit-counter to isolate the allocation outputs), VBR_encode_frame's
 * whole job is to drive the FULL pipeline and report bit usage, so this TU must
 * supply the GENUINE bit-counting machinery: it #includes libmp3lame/takehiro.c
 * + libmp3lame/tables.c alongside libmp3lame/vbrquantize.c. The three files
 * define disjoint non-static symbols; the file-static drivers in vbrquantize.c
 * (tryGlobalStepsize etc.) and the static bit-counters in takehiro.c stay TU-
 * local and never collide. Every assertion is therefore against vendored LAME
 * code end-to-end.
 *
 * Discipline (per CONTRIBUTING.md "parity oracle per
 * slice"): each go-test binary is one package wide, so this private LAME copy
 * never collides with the one libraries/mp3 compiles for production. This
 * package never imports libraries/mp3; it imports only internal/nativemp3.
 *
 * This slice IS floating-point-bearing (the 'as is' quantize and the
 * out-of-budget bit redistribution round float32 products separately), so the
 * result is only bit-exact under the mp3_strict build (FMA-free Go) against the
 * -ffp-contract=off cgo oracle. The scalar FP flags come from the mise task env,
 * never the in-source #cgo block.
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

#include "oracle.h"

/* ---- precompute tables the kernels read. Defined here (vbrquantize.c /
 * takehiro.c reference them extern) and filled with the verbatim iteration_init
 * table-fill loop (quantize_pvt.c:351-367, the TAKEHIRO_IEEE754_HACK branch the
 * vendored config selects). The Go side runs the identical loop
 * (FillVbrQuantizeTables). ---- */

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

    pow43[0] = 0.0;
    for (i = 1; i < PRECALC_SIZE; i++)
        pow43[i] = pow((FLOAT) i, 4.0 / 3.0);

    adj43asm[0] = 0.0;
    for (i = 1; i < PRECALC_SIZE; i++)
        adj43asm[i] = i - 0.5 - pow(0.5 * (pow43[i - 1] + pow43[i]), 0.75);

    for (i = 0; i < Q_MAX; i++)
        ipow20[i] = pow(2.0, (double) (i - 210) * -0.1875);
    for (i = 0; i <= Q_MAX + Q_MAX2; i++)
        pow20[i] = pow(2.0, (double) (i - 210 - Q_MAX2) * 0.25);
}

/* pretab / nr_of_sfb_block carry the verbatim quantize_pvt.c values that
 * set_scalefacs / long_block_constrain / quantize_x34 (preflag) and
 * scale_bitcount_lsf read. */
const int pretab[SBMAX_l] = {
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    1, 1, 1, 1, 2, 2, 3, 3, 3, 2, 0
};

const int nr_of_sfb_block[6][3][4] = {
    {{6, 5, 5, 5}, {9, 9, 9, 9}, {6, 9, 9, 9}},
    {{6, 5, 7, 3}, {9, 9, 12, 6}, {6, 9, 12, 6}},
    {{11, 10, 0, 0}, {18, 18, 0, 0}, {15, 18, 0, 0}},
    {{7, 7, 7, 0}, {12, 12, 12, 0}, {6, 15, 12, 0}},
    {{6, 6, 6, 3}, {12, 9, 9, 6}, {6, 12, 9, 6}},
    {{8, 8, 5, 0}, {15, 12, 9, 0}, {6, 18, 9, 0}}
};

/* ERRORF expands to lame_errorf. It fires from bitcount's "should never happen"
 * over-amplification exit (vbrquantize.c:986), mpeg2_scale_bitcount's unreached
 * intensity-stereo path, and VBR_encode_frame's final frame-overflow exit
 * (vbrquantize.c:1577) — all of which indicate a degenerate input the parity
 * tests deliberately avoid (realistic decaying spectra inside a feasible bit
 * budget). The stub prints the message to stderr before LAME's own exit(-1) so a
 * bad test input surfaces a diagnostic rather than a silent SIGABRT. */
void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    va_list ap;
    (void) gfc;
    va_start(ap, format);
    vfprintf(stderr, format, ap);
    va_end(ap);
}

/* ---- the genuine vendored VBR quantizer. vbrquantize.c supplies the VBR
 * drivers and VBR_encode_frame; its bitcount / quantizeAndCountBits /
 * reduce_bit_usage call the bit-counting machinery (best_scalefac_store /
 * best_huffman_divide / scale_bitcount / noquant_count_bits / huffman_init /
 * choose_table_nonMMX) which lives in a SEPARATE translation unit
 * (oracle_takehiro.c, which #includes takehiro.c + tables.c). The split is
 * REQUIRED: vbrquantize.c and takehiro.c each define their own file-local
 * `union fi_union` + MAGIC_INT / MAGIC_FLOAT for the TAKEHIRO_IEEE754_HACK, so
 * #including both in one TU collides. Compiling them in disjoint TUs keeps the
 * statics private and links the non-static bit-counters across (per the FLAC
 * per-TU split the SKILL documents). The shared precompute tables (pretab /
 * nr_of_sfb_block / pow* / adj43*) and lame_errorf are defined HERE; the takehiro
 * TU references them extern. ---- */
#include "libmp3lame/vbrquantize.c"

/* ---- handle plumbing ----
 *
 * lame_internal_flags is large; calloc one zeroed instance and touch only the
 * fields VBR_encode_frame + its closure read/write: cfg (mode_gr, channels_out,
 * noise_shaping, full_outer_loop, use_best_huffman), scalefac_band.l/.s,
 * sv_qnt.bv_scf (via huffman_init), choose_table (via huffman_init), and the 2x2
 * l3_side.tt gr_info grid (xr, width, window, energy_above_cutoff, block
 * geometry, xrpow_max, and the resolved side info). */

struct mp3parity_vbrfr_t {
    lame_internal_flags *gfc;
};

mp3parity_vbrfr_t *mp3parity_vbrfr_new(void) {
    mp3parity_vbrfr_t *h = (mp3parity_vbrfr_t *) calloc(1, sizeof(*h));
    h->gfc = (lame_internal_flags *) calloc(1, sizeof(lame_internal_flags));
    /* choose_table is installed by huffman_init (mp3parity_vbrfr_huffman_init,
     * which every test calls); choose_table_nonMMX is static in takehiro.c's TU
     * so it cannot be referenced here. */
    return h;
}

void mp3parity_vbrfr_free(mp3parity_vbrfr_t *h) {
    if (!h) return;
    free(h->gfc);
    free(h);
}

static gr_info *gi_of(mp3parity_vbrfr_t *h, int gr, int ch) {
    return &h->gfc->l3_side.tt[gr][ch];
}

void mp3parity_vbrfr_set_cfg(mp3parity_vbrfr_t *h, int mode_gr, int channels_out,
                             int noise_shaping, int full_outer_loop, int use_best_huffman) {
    h->gfc->cfg.mode_gr = mode_gr;
    h->gfc->cfg.channels_out = channels_out;
    h->gfc->cfg.noise_shaping = noise_shaping;
    h->gfc->cfg.full_outer_loop = full_outer_loop;
    h->gfc->cfg.use_best_huffman = use_best_huffman;
}

void mp3parity_vbrfr_set_sfb_long(mp3parity_vbrfr_t *h, const int *l, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->scalefac_band.l[i] = l[i];
}
void mp3parity_vbrfr_set_sfb_short(mp3parity_vbrfr_t *h, const int *s, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->scalefac_band.s[i] = s[i];
}
void mp3parity_vbrfr_huffman_init(mp3parity_vbrfr_t *h) {
    huffman_init(h->gfc);
}

void mp3parity_vbrfr_set_xr(mp3parity_vbrfr_t *h, int gr, int ch, const float *xr, int n) {
    int i; gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->xr[i] = xr[i];
}
void mp3parity_vbrfr_set_width(mp3parity_vbrfr_t *h, int gr, int ch, const int *w, int n) {
    int i; gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->width[i] = w[i];
}
void mp3parity_vbrfr_set_window(mp3parity_vbrfr_t *h, int gr, int ch, const int *win, int n) {
    int i; gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->window[i] = win[i];
}
void mp3parity_vbrfr_set_eac(mp3parity_vbrfr_t *h, int gr, int ch, const char *eac, int n) {
    int i; gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->energy_above_cutoff[i] = eac[i];
}
void mp3parity_vbrfr_set_geom(mp3parity_vbrfr_t *h, int gr, int ch, int block_type,
                              int mixed_block_flag, int sfbmax, int sfbdivide, int psymax,
                              int max_nonzero_coeff, float xrpow_max) {
    gr_info *gi = gi_of(h, gr, ch);
    gi->block_type = block_type;
    gi->mixed_block_flag = mixed_block_flag;
    gi->sfbmax = sfbmax;
    gi->sfbdivide = sfbdivide;
    gi->psymax = psymax;
    gi->max_nonzero_coeff = max_nonzero_coeff;
    gi->xrpow_max = xrpow_max;
}

/* ---- the driver trampoline. Reshapes the flat input arrays into the C
 * [2][2][...] view VBR_encode_frame expects and calls the genuine entry. ---- */
int mp3parity_vbrfr_encode(mp3parity_vbrfr_t *h, const float *xr34orig,
                           const float *l3_xmin, const int *max_bits) {
    FLOAT   x34[2][2][576];
    FLOAT   xmin[2][2][SFBMAX];
    int     mb[2][2];
    int     gr, ch, i;
    for (gr = 0; gr < 2; gr++) {
        for (ch = 0; ch < 2; ch++) {
            for (i = 0; i < 576; i++)
                x34[gr][ch][i] = xr34orig[(gr * 2 + ch) * 576 + i];
            for (i = 0; i < SFBMAX; i++)
                xmin[gr][ch][i] = l3_xmin[(gr * 2 + ch) * SFBMAX + i];
            mb[gr][ch] = max_bits[gr * 2 + ch];
        }
    }
    return VBR_encode_frame(h->gfc, x34, xmin, mb);
}

/* ---- gr_info output getters ---- */
int mp3parity_vbrfr_global_gain(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].global_gain;
}
int mp3parity_vbrfr_scalefac_scale(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].scalefac_scale;
}
int mp3parity_vbrfr_preflag(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].preflag;
}
int mp3parity_vbrfr_scalefac(const mp3parity_vbrfr_t *h, int gr, int ch, int sfb) {
    return h->gfc->l3_side.tt[gr][ch].scalefac[sfb];
}
int mp3parity_vbrfr_subblock_gain(const mp3parity_vbrfr_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].subblock_gain[i];
}
int mp3parity_vbrfr_l3enc(const mp3parity_vbrfr_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].l3_enc[i];
}
int mp3parity_vbrfr_part2_3_length(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].part2_3_length;
}
int mp3parity_vbrfr_part2_length(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].part2_length;
}
int mp3parity_vbrfr_scalefac_compress(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].scalefac_compress;
}
int mp3parity_vbrfr_big_values(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].big_values;
}
int mp3parity_vbrfr_table_select(const mp3parity_vbrfr_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].table_select[i];
}
int mp3parity_vbrfr_region0_count(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].region0_count;
}
int mp3parity_vbrfr_region1_count(const mp3parity_vbrfr_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].region1_count;
}
int mp3parity_vbrfr_scfsi(const mp3parity_vbrfr_t *h, int ch, int band) {
    return h->gfc->l3_side.scfsi[ch][band];
}
