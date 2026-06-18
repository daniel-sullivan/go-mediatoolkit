// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Compiles a PRIVATE copy of the vendored LAME 3.100 VBR iteration drivers and
 * surfaces VBR_{new,old}_iteration_loop (plus the static prepares /
 * bitpressure_strategy / VBR_encode_granule they run) to Go-cgo via
 * mp3parity_vbrit_* trampolines, for the "vbr-iteration-loop" parity slice.
 *
 * See oracle.h for the full scope, the per-TU split rationale, and the FP / LGPL
 * notes. This TU #includes quantize.c (the drivers + their long callee list:
 * init_xrpow / outer_loop / init_outer_loop / iteration_finish_one /
 * trancate_smallspectrums / ms_convert / get_framebits), quantize_pvt.c (on_pe /
 * reduce_side / calc_xmin + the precompute-table DEFINITIONS) and reservoir.c
 * (ResvFrameBegin / ResvMaxBits / ResvAdjust / ResvFrameEnd). None of these carry
 * the takehiro/vbrquantize fi_union+MAGIC statics, so they coexist in one TU; the
 * bit-counter (takehiro.c + tables.c) and VBR_encode_frame (vbrquantize.c) live
 * in sibling TUs (oracle_takehiro.c / oracle_vbrquantize.c).
 */

#include <config.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <stdarg.h>
#include <math.h>
#include <assert.h>

#include "lame.h"
#include "machine.h"
#include "encoder.h"
#include "util.h"
#include "l3side.h"
#include "quantize_pvt.h"

#include "oracle.h"

/* ---- ERRORF sink (see vbrquantize-frame oracle): fires only on degenerate
 * inputs the tests avoid (over-amplification / frame overflow). Prints then the
 * genuine code exits. ---- */
void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    va_list ap;
    (void) gfc;
    va_start(ap, format);
    vfprintf(stderr, format, ap);
    va_end(ap);
}

/* ---- getframebits / calcFrameLength: VERBATIM hand-twins of bitstream.c:61/71
 * (static calcFrameLength + non-static getframebits). Pulled in here so the loop
 * + ResvFrameBegin resolve them without dragging the whole bitstream huffman
 * emitter + util.c tree. The Go getframebits / calcFrameLength port mirrors these
 * exactly (bitstream_format.go). bitrate_table is defined in the takehiro TU
 * (tables.c) and referenced extern here. ---- */
extern const int bitrate_table[3][16];

static int
calcFrameLength(SessionConfig_t const *const cfg, int kbps, int pad)
{
    return 8 * ((cfg->version + 1) * 72000 * kbps / cfg->samplerate_out + pad);
}

int
getframebits(const lame_internal_flags * gfc)
{
    SessionConfig_t const *const cfg = &gfc->cfg;
    EncResult_t const *const eov = &gfc->ov_enc;
    int     bit_rate;
    if (eov->bitrate_index)
        bit_rate = bitrate_table[cfg->version][eov->bitrate_index];
    else
        bit_rate = cfg->avg_bitrate;
    assert(8 <= bit_rate && bit_rate <= 640);
    return calcFrameLength(cfg, bit_rate, eov->padding);
}

/* ATHformula (util.c) is referenced by quantize_pvt.c's compute_ath/ATHmdct,
 * which the VBR iteration loops never call (calc_xmin reads the pre-filled
 * ATH->l/.s the test supplies). Stubbed so the TU links; never executed. */
FLOAT ATHformula(SessionConfig_t const *cfg, FLOAT f) {
    (void) cfg; (void) f;
    return 0.0;
}

/* ---- the genuine vendored VBR iteration drivers + their callees ---- */
#include "libmp3lame/quantize.c"
#include "libmp3lame/quantize_pvt.c"
#include "libmp3lame/reservoir.c"

/* ---- handle plumbing ----
 *
 * lame_internal_flags is large; calloc one zeroed instance and touch only the
 * fields the VBR loops + their closure read/write. */

struct mp3parity_vbrit_t {
    lame_internal_flags *gfc;
    ATH_t                ath;
    III_psy_ratio        ratio[2][2];
};

/* oracle_fill_tables: VERBATIM iteration_init table fill (quantize_pvt.c, the
 * TAKEHIRO_IEEE754_HACK adj43asm branch). quantize_pvt.c DEFINES the table
 * storage; here we only populate it, idempotently, so the bit-counter (takehiro
 * TU) and quantize_x34 (vbrquantize TU) read the same values the Go
 * FillVbrQuantizeTables fills. */
void oracle_fill_tables(void) {
    int i;
    static int filled = 0;
    if (filled) return;
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

mp3parity_vbrit_t *mp3parity_vbrit_new(void) {
    mp3parity_vbrit_t *h = (mp3parity_vbrit_t *) calloc(1, sizeof(*h));
    h->gfc = (lame_internal_flags *) calloc(1, sizeof(lame_internal_flags));
    h->gfc->ATH = &h->ath;
    return h;
}

void mp3parity_vbrit_free(mp3parity_vbrit_t *h) {
    if (!h) return;
    free(h->gfc);
    free(h);
}

static gr_info *gi_of(mp3parity_vbrit_t *h, int gr, int ch) {
    return &h->gfc->l3_side.tt[gr][ch];
}

void mp3parity_vbrit_set_cfg(mp3parity_vbrit_t *h, int mode_gr, int channels_out,
                             int version, int samplerate_out, int avg_bitrate,
                             int sideinfo_len, int buffer_constraint,
                             int vbr_min_bitrate_index, int vbr_max_bitrate_index,
                             int disable_reservoir, int free_format,
                             int enforce_min_bitrate, int mode_ext) {
    SessionConfig_t *cfg = &h->gfc->cfg;
    cfg->mode_gr = mode_gr;
    cfg->channels_out = channels_out;
    cfg->version = version;
    cfg->samplerate_out = samplerate_out;
    cfg->avg_bitrate = avg_bitrate;
    cfg->sideinfo_len = sideinfo_len;
    cfg->buffer_constraint = buffer_constraint;
    cfg->vbr_min_bitrate_index = vbr_min_bitrate_index;
    cfg->vbr_max_bitrate_index = vbr_max_bitrate_index;
    cfg->disable_reservoir = disable_reservoir;
    cfg->free_format = free_format;
    cfg->enforce_min_bitrate = enforce_min_bitrate;
    h->gfc->ov_enc.mode_ext = mode_ext;
}

void mp3parity_vbrit_set_cfg_quant(mp3parity_vbrit_t *h, int noise_shaping,
                                   int full_outer_loop, int use_best_huffman,
                                   float ATHfixpoint, float athcurve, int athtype) {
    SessionConfig_t *cfg = &h->gfc->cfg;
    cfg->noise_shaping = noise_shaping;
    cfg->full_outer_loop = full_outer_loop;
    cfg->use_best_huffman = use_best_huffman;
    cfg->ATHfixpoint = ATHfixpoint;
    cfg->ATHcurve = athcurve;
    cfg->ATHtype = athtype;
}

void mp3parity_vbrit_set_resv(mp3parity_vbrit_t *h, int resv_size, int resv_max) {
    h->gfc->sv_enc.ResvSize = resv_size;
    h->gfc->sv_enc.ResvMax = resv_max;
}

/* Seed the bin_search_StepSize per-channel carry the encoder primes once in
 * lame_init_params (CurrentStep=4, OldValue=180). VBR_old_iteration_loop's
 * outer_loop -> bin_search_StepSize asserts CurrentStep != 0; the VBR-new path
 * doesn't use it. The Go encode_driver.go primes the same values. */
void mp3parity_vbrit_set_binsearch(mp3parity_vbrit_t *h, int old_value, int current_step) {
    int ch;
    for (ch = 0; ch < 2; ch++) {
        h->gfc->sv_qnt.OldValue[ch] = old_value;
        h->gfc->sv_qnt.CurrentStep[ch] = current_step;
    }
}

void mp3parity_vbrit_set_svqnt(mp3parity_vbrit_t *h, float mask_adjust,
                               float mask_adjust_short, int substep_shaping,
                               int sfb21_extra) {
    h->gfc->sv_qnt.mask_adjust = mask_adjust;
    h->gfc->sv_qnt.mask_adjust_short = mask_adjust_short;
    h->gfc->sv_qnt.substep_shaping = substep_shaping;
    h->gfc->sv_qnt.sfb21_extra = sfb21_extra;
}

void mp3parity_vbrit_set_longfact(mp3parity_vbrit_t *h, const float *lf, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->sv_qnt.longfact[i] = lf[i];
}
void mp3parity_vbrit_set_shortfact(mp3parity_vbrit_t *h, const float *sf, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->sv_qnt.shortfact[i] = sf[i];
}

void mp3parity_vbrit_set_ath(mp3parity_vbrit_t *h, float adjust_factor, float floor,
                             const float *l, int nl, const float *s, int ns) {
    int i;
    h->ath.adjust_factor = adjust_factor;
    h->ath.floor = floor;
    for (i = 0; i < nl; i++) h->ath.l[i] = l[i];
    for (i = 0; i < ns; i++) h->ath.s[i] = s[i];
}

void mp3parity_vbrit_set_sfb_long(mp3parity_vbrit_t *h, const int *l, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->scalefac_band.l[i] = l[i];
}
void mp3parity_vbrit_set_sfb_short(mp3parity_vbrit_t *h, const int *s, int n) {
    int i;
    for (i = 0; i < n; i++) h->gfc->scalefac_band.s[i] = s[i];
}
void mp3parity_vbrit_huffman_init(mp3parity_vbrit_t *h) {
    huffman_init(h->gfc);
    /* init_xrpow_core_init (quantize.c:93) installs the gfc->init_xrpow_core
     * SIMD-dispatch function pointer init_xrpow sucks through. iteration_init
     * normally calls it; the loop's init_xrpow would deref a NULL fnptr without
     * it. The Go initXrpow calls initXrpowCoreC directly (no fnptr), so it needs
     * no analogue — this only restores the C path the genuine loop expects. */
    init_xrpow_core_init(h->gfc);
}

void mp3parity_vbrit_set_xr(mp3parity_vbrit_t *h, int gr, int ch, const float *xr, int n) {
    int i; gr_info *gi = gi_of(h, gr, ch);
    for (i = 0; i < n; i++) gi->xr[i] = xr[i];
}
void mp3parity_vbrit_set_geom(mp3parity_vbrit_t *h, int gr, int ch, int block_type,
                              int mixed_block_flag) {
    gr_info *gi = gi_of(h, gr, ch);
    gi->block_type = block_type;
    gi->mixed_block_flag = mixed_block_flag;
    /* the rest of the geometry (sfb_lmax/sfb_smin/psy_lmax/sfbmax/psymax/
     * sfbdivide/width/window) is filled by init_outer_loop inside the loop, so
     * the test only seeds the block type, exactly as a real frame arrives. */
}

void mp3parity_vbrit_set_ratio_l(mp3parity_vbrit_t *h, int gr, int ch,
                                 const float *en_l, const float *thm_l, int n) {
    int i;
    /* ratio lives on the handle so run_* can hand the loop a stable
     * (const III_psy_ratio (*)[2]) pointer, matching the encoder's masking[2][2]. */
    for (i = 0; i < n; i++) {
        h->ratio[gr][ch].en.l[i] = en_l[i];
        h->ratio[gr][ch].thm.l[i] = thm_l[i];
    }
}
void mp3parity_vbrit_set_ratio_s(mp3parity_vbrit_t *h, int gr, int ch,
                                 const float *en_s, const float *thm_s, int n) {
    int i, b;
    for (i = 0; i < n; i++) {
        for (b = 0; b < 3; b++) {
            h->ratio[gr][ch].en.s[i][b] = en_s[i * 3 + b];
            h->ratio[gr][ch].thm.s[i][b] = thm_s[i * 3 + b];
        }
    }
}

/* ---- the driver trampolines ---- */
void mp3parity_vbrit_run_new(mp3parity_vbrit_t *h, const float *pe, const float *mer) {
    FLOAT pe_[2][2], mer_[2];
    int gr, ch;
    for (gr = 0; gr < 2; gr++) {
        for (ch = 0; ch < 2; ch++) pe_[gr][ch] = pe[gr * 2 + ch];
        mer_[gr] = mer[gr];
    }
    VBR_new_iteration_loop(h->gfc, (const FLOAT (*)[2]) pe_, mer_,
                           (const III_psy_ratio (*)[2]) h->ratio);
}
void mp3parity_vbrit_run_old(mp3parity_vbrit_t *h, const float *pe, const float *mer) {
    FLOAT pe_[2][2], mer_[2];
    int gr, ch;
    for (gr = 0; gr < 2; gr++) {
        for (ch = 0; ch < 2; ch++) pe_[gr][ch] = pe[gr * 2 + ch];
        mer_[gr] = mer[gr];
    }
    VBR_old_iteration_loop(h->gfc, (const FLOAT (*)[2]) pe_, mer_,
                           (const III_psy_ratio (*)[2]) h->ratio);
}

/* ---- frame-level output getters ---- */
int mp3parity_vbrit_bitrate_index(const mp3parity_vbrit_t *h) {
    return h->gfc->ov_enc.bitrate_index;
}
int mp3parity_vbrit_resv_size(const mp3parity_vbrit_t *h) {
    return h->gfc->sv_enc.ResvSize;
}
int mp3parity_vbrit_mode_ext(const mp3parity_vbrit_t *h) {
    return h->gfc->ov_enc.mode_ext;
}

/* ---- per-[gr][ch] gr_info output getters ---- */
int mp3parity_vbrit_global_gain(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].global_gain;
}
int mp3parity_vbrit_scalefac_scale(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].scalefac_scale;
}
int mp3parity_vbrit_preflag(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].preflag;
}
int mp3parity_vbrit_scalefac(const mp3parity_vbrit_t *h, int gr, int ch, int sfb) {
    return h->gfc->l3_side.tt[gr][ch].scalefac[sfb];
}
int mp3parity_vbrit_subblock_gain(const mp3parity_vbrit_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].subblock_gain[i];
}
int mp3parity_vbrit_l3enc(const mp3parity_vbrit_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].l3_enc[i];
}
int mp3parity_vbrit_part2_3_length(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].part2_3_length;
}
int mp3parity_vbrit_part2_length(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].part2_length;
}
int mp3parity_vbrit_scalefac_compress(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].scalefac_compress;
}
int mp3parity_vbrit_big_values(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].big_values;
}
int mp3parity_vbrit_table_select(const mp3parity_vbrit_t *h, int gr, int ch, int i) {
    return h->gfc->l3_side.tt[gr][ch].table_select[i];
}
int mp3parity_vbrit_region0_count(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].region0_count;
}
int mp3parity_vbrit_region1_count(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].region1_count;
}
int mp3parity_vbrit_block_type(const mp3parity_vbrit_t *h, int gr, int ch) {
    return h->gfc->l3_side.tt[gr][ch].block_type;
}
