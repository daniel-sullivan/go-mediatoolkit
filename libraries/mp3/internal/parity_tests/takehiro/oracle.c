// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Compiles a PRIVATE copy of the vendored LAME 3.100 takehiro.c bit-counting
 * routines and surfaces them to Go-cgo via mp3parity_tk_* trampolines, for the
 * "takehiro" parity slice.
 *
 * Scope. This oracle pins nativemp3's takehiro bit-counting slice
 * (takehiro.go): the INTEGER Huffman-table-selection / bit-counting core —
 * ix_max, count_bit_ESC / count_bit_noESC / from2 / from3, choose_table_nonMMX,
 * noquant_count_bits, best_huffman_divide (recalc_divide_init / sub),
 * scfsi_calc, best_scalefac_store, mpeg1_scale_bitcount /
 * mpeg2_scale_bitcount (scale_bitcount_lsf) / scale_bitcount, and huffman_init.
 * Every one is exercised through the genuine vendored code below.
 *
 * The floating-point quantizer front-end of takehiro.c (count_bits /
 * quantize_xrpow / quantize_lines_xrpow*) is compiled — it lives in the same TU
 * — but never CALLED by this oracle: it is the quantize_pvt / rate-loop slice's
 * target, not this one. Its external table references (ipow20 / adj43asm /
 * adj43 / pow43) are satisfied by link-only zeroed definitions below; the
 * integer routines this slice pins never read them.
 *
 * Discipline (per CONTRIBUTING.md "parity oracle per
 * slice"): each go-test binary is one package wide, so this private LAME copy
 * never collides with the one libraries/mp3 compiles for production. This
 * package never imports libraries/mp3; it imports only internal/nativemp3.
 *
 * This slice is integer-only — pure table lookups and shifting — so it is
 * bit-identical regardless of build tag or vectorization. The scalar FP flags
 * (-ffp-contract=off, ...) still come from the mise task env, never from the
 * in-source #cgo block, matching the rest of the suite; they have no effect on
 * the integer routines but keep the build uniform.
 */

#include <config.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>

#include "libmp3lame/tables.c"
#include "libmp3lame/takehiro.c"

#include "oracle.h"

/* ---- link-only definitions for symbols takehiro.c's compiled-but-unreached
 * FP front-end (count_bits / quantize_xrpow) references. The integer routines
 * this oracle calls never read them. pretab / nr_of_sfb_block carry the
 * verbatim quantize_pvt.c values (so even an accidental read is faithful);
 * ipow20 / adj43asm / adj43 / pow43 are zeroed (unreached). ---- */

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

FLOAT ipow20[Q_MAX];
FLOAT adj43asm[PRECALC_SIZE];
FLOAT adj43[PRECALC_SIZE];
FLOAT pow43[PRECALC_SIZE];

/* ERRORF in mpeg2_scale_bitcount expands to lame_errorf; provide an inert
 * stub (the "intensity stereo not implemented" path this oracle never hits). */
void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    (void) gfc;
    (void) format;
}

/* ---- oracle state plumbing ----
 *
 * lame_internal_flags is large; we calloc one zeroed instance and touch only
 * the fields the bit-counting routines read/write: cfg.mode_gr,
 * cfg.use_best_huffman, scalefac_band.l/.s, sv_qnt.bv_scf, the l3_side gr_info
 * (l3_enc, scalefac, width, window, block geometry, the filled side info), and
 * l3_side.scfsi. gfc->choose_table is installed by huffman_init. */

struct mp3parity_tk_t {
    lame_internal_flags *gfc;
};

mp3parity_tk_t *mp3parity_tk_new(void) {
    mp3parity_tk_t *h = (mp3parity_tk_t *) calloc(1, sizeof(*h));
    h->gfc = (lame_internal_flags *) calloc(1, sizeof(lame_internal_flags));
    /* default choose_table so callers that skip huffman_init still work */
    h->gfc->choose_table = choose_table_nonMMX;
    return h;
}

void mp3parity_tk_free(mp3parity_tk_t *h) {
    if (!h) return;
    free(h->gfc);
    free(h);
}

static gr_info *gi_of(mp3parity_tk_t *h, int gr, int ch) {
    return &h->gfc->l3_side.tt[gr][ch];
}

void mp3parity_tk_set_cfg(mp3parity_tk_t *h, int mode_gr, int use_best_huffman) {
    h->gfc->cfg.mode_gr = mode_gr;
    h->gfc->cfg.use_best_huffman = use_best_huffman;
}

void mp3parity_tk_set_sfb_long(mp3parity_tk_t *h, const int *l, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->scalefac_band.l[i] = l[i];
}

void mp3parity_tk_set_sfb_short(mp3parity_tk_t *h, const int *s, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->scalefac_band.s[i] = s[i];
}

void mp3parity_tk_set_l3enc(mp3parity_tk_t *h, int gr, int ch, const int *ix, int n) {
    int i;
    gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->l3_enc[i] = ix[i];
}

void mp3parity_tk_set_scalefac(mp3parity_tk_t *h, int gr, int ch, const int *sf, int n) {
    int i;
    gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->scalefac[i] = sf[i];
}

void mp3parity_tk_set_width(mp3parity_tk_t *h, int gr, int ch, const int *w, int n) {
    int i;
    gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->width[i] = w[i];
}

void mp3parity_tk_set_window(mp3parity_tk_t *h, int gr, int ch, const int *win, int n) {
    int i;
    gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->window[i] = win[i];
}

void mp3parity_tk_set_geom(mp3parity_tk_t *h, int gr, int ch,
                           int block_type, int mixed_block_flag, int global_gain,
                           int scalefac_scale, int preflag, int sfbmax, int sfbdivide,
                           int max_nonzero_coeff, int part2_3_length) {
    gr_info *gi = gi_of(h, gr, ch);
    gi->block_type = block_type;
    gi->mixed_block_flag = mixed_block_flag;
    gi->global_gain = global_gain;
    gi->scalefac_scale = scalefac_scale;
    gi->preflag = preflag;
    gi->sfbmax = sfbmax;
    gi->sfbdivide = sfbdivide;
    gi->max_nonzero_coeff = max_nonzero_coeff;
    gi->part2_3_length = part2_3_length;
}

void mp3parity_tk_huffman_init(mp3parity_tk_t *h) {
    huffman_init(h->gfc);
}

int mp3parity_tk_choose_table(mp3parity_tk_t *h, int gr, int ch, int begin, int end, int *bits) {
    gr_info *gi = gi_of(h, gr, ch);
    return h->gfc->choose_table(gi->l3_enc + begin, gi->l3_enc + end, bits);
}

int mp3parity_tk_noquant_count_bits(mp3parity_tk_t *h, int gr, int ch) {
    return noquant_count_bits(h->gfc, gi_of(h, gr, ch), NULL);
}

int mp3parity_tk_scale_bitcount(mp3parity_tk_t *h, int gr, int ch) {
    return scale_bitcount(h->gfc, gi_of(h, gr, ch));
}

void mp3parity_tk_best_huffman_divide(mp3parity_tk_t *h, int gr, int ch) {
    best_huffman_divide(h->gfc, gi_of(h, gr, ch));
}

void mp3parity_tk_best_scalefac_store(mp3parity_tk_t *h, int gr, int ch) {
    best_scalefac_store(h->gfc, gr, ch, &h->gfc->l3_side);
}

int mp3parity_tk_bv_scf(const mp3parity_tk_t *h, int i) {
    return (int) h->gfc->sv_qnt.bv_scf[i];
}
int mp3parity_tk_big_values(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].big_values;
}
int mp3parity_tk_count1(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].count1;
}
int mp3parity_tk_count1bits(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].count1bits;
}
int mp3parity_tk_count1table_select(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].count1table_select;
}
int mp3parity_tk_region0_count(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].region0_count;
}
int mp3parity_tk_region1_count(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].region1_count;
}
int mp3parity_tk_table_select(const mp3parity_tk_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].table_select[i];
}
int mp3parity_tk_part2_3_length(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].part2_3_length;
}
int mp3parity_tk_part2_length(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].part2_length;
}
int mp3parity_tk_scalefac_compress(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].scalefac_compress;
}
int mp3parity_tk_scalefac_scale(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].scalefac_scale;
}
int mp3parity_tk_preflag(const mp3parity_tk_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].preflag;
}
int mp3parity_tk_scalefac(const mp3parity_tk_t *h, int gr, int ch, int sfb) {
    return h->gfc->l3_side.tt[gr][ch].scalefac[sfb];
}
int mp3parity_tk_slen(const mp3parity_tk_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].slen[i];
}
int mp3parity_tk_scfsi(const mp3parity_tk_t *h, int ch, int i) {
    return h->gfc->l3_side.scfsi[ch][i];
}
