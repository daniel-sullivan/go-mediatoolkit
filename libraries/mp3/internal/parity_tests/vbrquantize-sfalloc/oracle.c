// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Compiles a PRIVATE copy of the vendored LAME 3.100 vbrquantize.c VBR
 * quantizer and surfaces its scalefactor-ALLOCATION tier to Go-cgo via
 * mp3parity_vbrsf_* trampolines, for the "vbrquantize-sfalloc" parity slice.
 *
 * Scope. This oracle pins nativemp3's VBR allocation tier
 * (vbrquantize_sfalloc.go): block_sf, quantize_x34, set_subblock_gain,
 * set_scalefacs, checkScalefactor, short_block_constrain and
 * long_block_constrain. Each is exercised through the genuine vendored code
 * #included below. The higher VBR drivers in the same TU (tryGlobalStepsize ..
 * outOfBitsStrategy, VBR_encode_frame) are a later slice — they are compiled
 * (they share the file) but never CALLED by this oracle, so their unresolved
 * callees (ResvFrameBegin / etc.) are satisfied by inert link-only stubs; the
 * functions this oracle calls never reach them.
 *
 * The bitcount / quantizeAndCountBits wrappers (vbrquantize.c:984/999) delegate
 * to takehiro.c's scale_bitcount / noquant_count_bits. To keep this oracle TU
 * focused on the ALLOCATION outputs (the task's parity target) without pulling
 * the whole bit-counting + tables TU into the link, scale_bitcount /
 * noquant_count_bits are inert stubs here and bitcount / quantizeAndCountBits are
 * NOT pinned by this slice — they are thin wrappers over the already-verified
 * takehiro slice (parity_tests/takehiro). The allocation + quantize_x34 outputs
 * this oracle DOES pin are self-contained.
 *
 * Discipline (per CONTRIBUTING.md "parity oracle per
 * slice"): each go-test binary is one package wide, so this private LAME copy
 * never collides with the one libraries/mp3 compiles for production. This
 * package never imports libraries/mp3; it imports only internal/nativemp3.
 *
 * This slice IS floating-point-bearing (block_sf's find dispatch and
 * quantize_x34's float32 product + magic-float quantize round separately), so the
 * result is only bit-exact under the mp3_strict build (FMA-free Go) against the
 * -ffp-contract=off cgo oracle. The scalar FP flags come from the mise task env,
 * never the in-source #cgo block.
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

/* Bring the mp3parity_vbrsf_t typedef + trampoline prototypes into scope so the
 * handle struct and the trampolines below agree with the Go-visible signatures
 * (cgo.go #includes the same oracle.h). */
#include "oracle.h"

/* ---- precompute tables the kernels read. Defined here and filled with the
 * verbatim iteration_init table-fill loop (quantize_pvt.c:351-367, the
 * TAKEHIRO_IEEE754_HACK branch the vendored config selects), as the
 * vbrquantize-leaf oracle does. The Go side runs the identical loop
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

/* pretab carries the verbatim quantize_pvt.c:159-162 values; block_sf does not
 * read it, but set_scalefacs / long_block_constrain / quantize_x34 do (when
 * preflag is set), so it must be the genuine table. */
const int pretab[SBMAX_l] = {
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    1, 1, 1, 1, 2, 2, 3, 3, 3, 2, 0
};

/* ---- the genuine vendored VBR quantizer. #include brings the file-static
 * allocation routines into this TU so the trampolines below call the real LAME
 * code. ---- */
#include "libmp3lame/vbrquantize.c"

/* ---- link-only stubs for symbols vbrquantize.c's compiled-but-unreached
 * drivers reference. The allocation routines this oracle calls never invoke
 * them. ---- */

void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    (void) gfc;
    (void) format;
}

/* bitcount / quantizeAndCountBits (compiled, never called by this oracle)
 * reference these takehiro.c routines; inert stubs satisfy the link. */
int noquant_count_bits(lame_internal_flags const *const gfc, gr_info *const cod_info,
                       calc_noise_data *prev_noise) {
    (void) gfc; (void) cod_info; (void) prev_noise;
    return 0;
}
int scale_bitcount(const lame_internal_flags *gfc, gr_info *cod_info) {
    (void) gfc; (void) cod_info;
    return 0;
}

/* VBR_encode_frame (compiled, never called by this oracle) also references the
 * best_* finishers in quantize.c; inert stubs satisfy the link. */
void best_huffman_divide(const lame_internal_flags *const gfc, gr_info *const cod_info) {
    (void) gfc; (void) cod_info;
}
void best_scalefac_store(const lame_internal_flags *gfc, const int gr, const int ch,
                         III_side_info_t *const l3_side) {
    (void) gfc; (void) gr; (void) ch; (void) l3_side;
}

/* ---- handle plumbing ----
 *
 * lame_internal_flags is large; calloc one zeroed instance and touch only the
 * fields the allocation routines read/write: cfg.mode_gr, cfg.noise_shaping, the
 * l3_side gr_info (xr, width, window, energy_above_cutoff, scalefac, l3_enc,
 * subblock_gain, block geometry, the resolved side info). gr is fixed at [0][0].
 */

struct mp3parity_vbrsf_t {
    lame_internal_flags *gfc;
};

static gr_info *gi_of(mp3parity_vbrsf_t *h) {
    return &h->gfc->l3_side.tt[0][0];
}

mp3parity_vbrsf_t *mp3parity_vbrsf_new(void) {
    mp3parity_vbrsf_t *h = (mp3parity_vbrsf_t *) calloc(1, sizeof(*h));
    h->gfc = (lame_internal_flags *) calloc(1, sizeof(lame_internal_flags));
    return h;
}

void mp3parity_vbrsf_free(mp3parity_vbrsf_t *h) {
    if (!h) return;
    free(h->gfc);
    free(h);
}

void mp3parity_vbrsf_set_cfg(mp3parity_vbrsf_t *h, int mode_gr, int noise_shaping) {
    h->gfc->cfg.mode_gr = mode_gr;
    h->gfc->cfg.noise_shaping = noise_shaping;
}

void mp3parity_vbrsf_set_xr(mp3parity_vbrsf_t *h, const float *xr, int n) {
    int i; gr_info *gi = gi_of(h);
    for (i = 0; i < n; i++) gi->xr[i] = xr[i];
}
void mp3parity_vbrsf_set_width(mp3parity_vbrsf_t *h, const int *w, int n) {
    int i; gr_info *gi = gi_of(h);
    for (i = 0; i < n; i++) gi->width[i] = w[i];
}
void mp3parity_vbrsf_set_window(mp3parity_vbrsf_t *h, const int *win, int n) {
    int i; gr_info *gi = gi_of(h);
    for (i = 0; i < n; i++) gi->window[i] = win[i];
}
void mp3parity_vbrsf_set_eac(mp3parity_vbrsf_t *h, const char *eac, int n) {
    int i; gr_info *gi = gi_of(h);
    for (i = 0; i < n; i++) gi->energy_above_cutoff[i] = eac[i];
}
void mp3parity_vbrsf_set_scalefac(mp3parity_vbrsf_t *h, const int *sf, int n) {
    int i; gr_info *gi = gi_of(h);
    for (i = 0; i < n; i++) gi->scalefac[i] = sf[i];
}
void mp3parity_vbrsf_set_subblock_gain(mp3parity_vbrsf_t *h, const int *sbg, int n) {
    int i; gr_info *gi = gi_of(h);
    for (i = 0; i < n; i++) gi->subblock_gain[i] = sbg[i];
}
void mp3parity_vbrsf_set_geom(mp3parity_vbrsf_t *h, int block_type, int global_gain,
                              int scalefac_scale, int preflag, int sfbmax, int psymax,
                              int max_nonzero_coeff) {
    gr_info *gi = gi_of(h);
    gi->block_type = block_type;
    gi->global_gain = global_gain;
    gi->scalefac_scale = scalefac_scale;
    gi->preflag = preflag;
    gi->sfbmax = sfbmax;
    gi->psymax = psymax;
    gi->max_nonzero_coeff = max_nonzero_coeff;
}

int mp3parity_vbrsf_global_gain(const mp3parity_vbrsf_t *h) {
    return h->gfc->l3_side.tt[0][0].global_gain;
}
int mp3parity_vbrsf_scalefac_scale(const mp3parity_vbrsf_t *h) {
    return h->gfc->l3_side.tt[0][0].scalefac_scale;
}
int mp3parity_vbrsf_preflag(const mp3parity_vbrsf_t *h) {
    return h->gfc->l3_side.tt[0][0].preflag;
}
int mp3parity_vbrsf_scalefac(const mp3parity_vbrsf_t *h, int sfb) {
    return h->gfc->l3_side.tt[0][0].scalefac[sfb];
}
int mp3parity_vbrsf_subblock_gain(const mp3parity_vbrsf_t *h, int i) {
    return h->gfc->l3_side.tt[0][0].subblock_gain[i];
}
int mp3parity_vbrsf_l3enc(const mp3parity_vbrsf_t *h, int i) {
    return h->gfc->l3_side.tt[0][0].l3_enc[i];
}

/* ---- trampolines: each fills an algo_t and calls the genuine static kernel. */

int mp3parity_vbrsf_block_sf(mp3parity_vbrsf_t *h, const float *xr34orig,
                             const float *l3_xmin, int find_sel,
                             int *vbrsf, int *vbrsfmin,
                             int *mingain_l, int *mingain_s) {
    algo_t that;
    int vbrmax;
    memset(&that, 0, sizeof(that));
    that.gfc = h->gfc;
    that.cod_info = gi_of(h);
    that.xr34orig = xr34orig;
    that.find = find_sel ? find_scalefac_x34 : guess_scalefac_x34;
    vbrmax = block_sf(&that, l3_xmin, vbrsf, vbrsfmin);
    *mingain_l = that.mingain_l;
    mingain_s[0] = that.mingain_s[0];
    mingain_s[1] = that.mingain_s[1];
    mingain_s[2] = that.mingain_s[2];
    return vbrmax;
}

void mp3parity_vbrsf_quantize_x34(mp3parity_vbrsf_t *h, const float *xr34orig) {
    algo_t that;
    memset(&that, 0, sizeof(that));
    that.gfc = h->gfc;
    that.cod_info = gi_of(h);
    that.xr34orig = xr34orig;
    quantize_x34(&that);
}

void mp3parity_vbrsf_run_set_subblock_gain(mp3parity_vbrsf_t *h, const int *mingain_s,
                                           int *sf) {
    set_subblock_gain(gi_of(h), mingain_s, sf);
}

void mp3parity_vbrsf_set_scalefacs(mp3parity_vbrsf_t *h, const int *vbrsfmin,
                                   int *sf, int max_range_sel) {
    const uint8_t *mr = max_range_short;
    if (max_range_sel == 1) mr = max_range_long;
    else if (max_range_sel == 2) mr = max_range_long_lsf_pretab;
    set_scalefacs(gi_of(h), vbrsfmin, sf, mr);
}

int mp3parity_vbrsf_check_scalefactor(mp3parity_vbrsf_t *h, const int *vbrsfmin) {
    return checkScalefactor(gi_of(h), vbrsfmin);
}

void mp3parity_vbrsf_short_constrain(mp3parity_vbrsf_t *h, const int *vbrsf,
                                     const int *vbrsfmin, int vbrmax,
                                     int mingain_l, const int *mingain_s) {
    algo_t that;
    memset(&that, 0, sizeof(that));
    that.gfc = h->gfc;
    that.cod_info = gi_of(h);
    that.mingain_l = mingain_l;
    that.mingain_s[0] = mingain_s[0];
    that.mingain_s[1] = mingain_s[1];
    that.mingain_s[2] = mingain_s[2];
    short_block_constrain(&that, vbrsf, vbrsfmin, vbrmax);
}

void mp3parity_vbrsf_long_constrain(mp3parity_vbrsf_t *h, const int *vbrsf,
                                    const int *vbrsfmin, int vbrmax,
                                    int mingain_l, const int *mingain_s) {
    algo_t that;
    memset(&that, 0, sizeof(that));
    that.gfc = h->gfc;
    that.cod_info = gi_of(h);
    that.mingain_l = mingain_l;
    that.mingain_s[0] = mingain_s[0];
    that.mingain_s[1] = mingain_s[1];
    that.mingain_s[2] = mingain_s[2];
    long_block_constrain(&that, vbrsf, vbrsfmin, vbrmax);
}
