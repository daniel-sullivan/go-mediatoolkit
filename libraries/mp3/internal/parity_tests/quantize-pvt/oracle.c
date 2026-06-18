// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/*
 * oracle.c — compiles the vendored LAME 3.100 quantizer-support source and
 * re-exports its ATH / distortion-budget / noise-measure kernels for the
 * quantize-pvt parity tests.
 *
 * This translation unit #includes the committed libmp3lame/quantize_pvt.c
 * (bringing the genuine calc_xmin / calc_noise / athAdjust plus the file-static
 * ATHmdct / compute_ath / calc_noise_core_c into scope) and libmp3lame/util.c
 * (so ATHformula, which ATHmdct/compute_ath call, is the genuine vendored
 * formula). The oracle_* trampolines below forward straight through to those
 * functions, so the C side of every parity assertion is the real reference,
 * never a hand twin. See oracle.h for the full discipline note.
 *
 * STAGE SEAM. iteration_init (which oracle_fill_tables drives to populate the
 * genuine pow43[]/pow20[] file globals calc_noise indexes) calls two extern
 * table-init helpers that live in other TUs — huffman_init (takehiro.c) and
 * init_xrpow_core_init (quantize.c). This slice does not exercise their bodies,
 * so inert stub definitions are provided here to satisfy the link, exactly as
 * the frame-encode-dispatch oracle stubs its extern callees. The pow43/pow20/
 * ipow20/adj43 and longfact/shortfact fills inside iteration_init, and
 * compute_ath, run genuinely.
 *
 * quantize_pvt.c is compiled in isolation as its own TU (one .c per parity
 * binary) so each go-test binary's symbol set is self-contained — no
 * cross-package static-symbol clash. This package never imports libraries/mp3
 * (which would duplicate the LAME symbols at link time); it only imports the
 * pure-Go internal/nativemp3 port.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS
 * (cgo.go). The FP-determinism flags come from the mise task env (see oracle.h).
 */

#include <stdlib.h>
#include <string.h>
#include <assert.h>

#include "tables.c"
#include "util.c"
#include "quantize_pvt.c"

#include "oracle.h"

/* --- extern table-init callees iteration_init reaches in other TUs. The bodies
 * are not exercised by this slice; stub them so the genuine table-fill loops in
 * iteration_init still link and run. --------------------------------------- */
void huffman_init(lame_internal_flags * const gfc) { (void) gfc; }
void init_xrpow_core_init(lame_internal_flags * const gfc) { (void) gfc; }

/* ResvMaxBits (reservoir.c) is referenced by quantize_pvt.c's on_pe, which this
 * slice compiles but does not exercise (on_pe/reduce_side are the integer
 * bit-allocation surface, not the FP quantize-pvt kernels under test). Stub it
 * so the TU links; its body is never called here. tables.c supplies the genuine
 * const bitrate_table / samplerate_table / huffman tables util.c references. */
void ResvMaxBits(lame_internal_flags * gfc, int mean_bits, int *targ_bits,
                 int *max_bits, int cbr) {
    (void) gfc; (void) mean_bits; (void) cbr;
    if (targ_bits) *targ_bits = 0;
    if (max_bits) *max_bits = 0;
}

/* Compile-time layout cross-checks: the flat-mirror sizes in oracle.h must equal
 * the genuine LAME values pulled in via the headers above. */
typedef char orc_assert_sbmaxl[(ORC_SBMAX_L == SBMAX_l) ? 1 : -1];
typedef char orc_assert_sbmaxs[(ORC_SBMAX_S == SBMAX_s) ? 1 : -1];
typedef char orc_assert_psfb21[(ORC_PSFB21 == PSFB21) ? 1 : -1];
typedef char orc_assert_psfb12[(ORC_PSFB12 == PSFB12) ? 1 : -1];
typedef char orc_assert_sfbmax[(ORC_SFBMAX == SFBMAX) ? 1 : -1];

/* fill_cfg populates the cfg fields ATHmdct/compute_ath/calc_xmin read. */
static void fill_cfg(SessionConfig_t *cfg, int samplerate_out, int athtype,
                     float athcurve, float ath_offset_db, float athfixpoint,
                     int noath) {
    memset(cfg, 0, sizeof(*cfg));
    cfg->samplerate_out = samplerate_out;
    cfg->ATHtype = athtype;
    cfg->ATHcurve = athcurve;
    cfg->ATH_offset_db = ath_offset_db;
    cfg->ATHfixpoint = athfixpoint;
    cfg->noATH = noath;
}

/* fill_scalefac_band copies the long/short (and optional psfb) sfb boundaries
 * into gfc->scalefac_band. The psfb arrays are optional (NULL leaves them 0). */
static void fill_scalefac_band(lame_internal_flags *gfc, const int *sbL,
                               const int *sbS, const int *psfb21,
                               const int *psfb12) {
    int i;
    for (i = 0; i < SBMAX_l + 1; i++) gfc->scalefac_band.l[i] = sbL[i];
    for (i = 0; i < SBMAX_s + 1; i++) gfc->scalefac_band.s[i] = sbS[i];
    if (psfb21) for (i = 0; i < PSFB21 + 1; i++) gfc->scalefac_band.psfb21[i] = psfb21[i];
    if (psfb12) for (i = 0; i < PSFB12 + 1; i++) gfc->scalefac_band.psfb12[i] = psfb12[i];
}

void oracle_fill_tables(int samplerate_out, int athtype, float athcurve,
                        float ath_offset_db, float athfixpoint, int noath,
                        const int *sbL, const int *sbS,
                        const int *psfb21, const int *psfb12,
                        const float *adjust_long, const float *adjust_short) {
    lame_internal_flags *gfc = (lame_internal_flags *) calloc(1, sizeof(*gfc));
    gfc->ATH = (ATH_t *) calloc(1, sizeof(ATH_t));
    fill_cfg(&gfc->cfg, samplerate_out, athtype, athcurve, ath_offset_db,
             athfixpoint, noath);
    /* longfact/shortfact in iteration_init are derived from these dB knobs +
     * the payload tables; the parity test passes the bass/alto/treble/sfb21 dB
     * adjustments so both sides compute identical longfact/shortfact. */
    gfc->cfg.adjust_bass_db   = adjust_long[0];
    gfc->cfg.adjust_alto_db   = adjust_long[1];
    gfc->cfg.adjust_treble_db = adjust_long[2];
    gfc->cfg.adjust_sfb21_db  = adjust_long[3];
    (void) adjust_short; /* iteration_init uses the same four cfg dB knobs for
                          * short via the static payload_short table */
    fill_scalefac_band(gfc, sbL, sbS, psfb21, psfb12);
    gfc->iteration_init_init = 0;
    iteration_init(gfc);
    free(gfc->ATH);
    free(gfc);
}

float oracle_athadjust(float a, float x, float athFloor, float athFixpoint) {
    return athAdjust(a, x, athFloor, athFixpoint);
}

float oracle_athmdct(int athtype, float athcurve, float ath_offset_db,
                     float athfixpoint, float f) {
    SessionConfig_t cfg;
    fill_cfg(&cfg, 44100, athtype, athcurve, ath_offset_db, athfixpoint, 0);
    return ATHmdct(&cfg, f);
}

void oracle_compute_ath(int samplerate_out, int athtype, float athcurve,
                        float ath_offset_db, float athfixpoint, int noath,
                        const int *sbL, const int *sbS,
                        const int *psfb21, const int *psfb12,
                        float *out_l, float *out_psfb21,
                        float *out_s, float *out_psfb12, float *out_floor) {
    int i;
    lame_internal_flags *gfc = (lame_internal_flags *) calloc(1, sizeof(*gfc));
    gfc->ATH = (ATH_t *) calloc(1, sizeof(ATH_t));
    fill_cfg(&gfc->cfg, samplerate_out, athtype, athcurve, ath_offset_db,
             athfixpoint, noath);
    fill_scalefac_band(gfc, sbL, sbS, psfb21, psfb12);
    compute_ath(gfc);
    for (i = 0; i < SBMAX_l; i++) out_l[i] = gfc->ATH->l[i];
    for (i = 0; i < PSFB21; i++) out_psfb21[i] = gfc->ATH->psfb21[i];
    for (i = 0; i < SBMAX_s; i++) out_s[i] = gfc->ATH->s[i];
    for (i = 0; i < PSFB12; i++) out_psfb12[i] = gfc->ATH->psfb12[i];
    *out_floor = gfc->ATH->floor;
    free(gfc->ATH);
    free(gfc);
}

int oracle_calc_xmin(
    int samplerate_out, float athfixpoint, int sfb21_extra, int use_temporal,
    const int *sbL, const int *sbS,
    float ath_adjust_factor, float ath_floor, const float *ath_l, const float *ath_s,
    const float *longfact, const float *shortfact,
    float decay,
    const float *xr, const int *width,
    int psy_lmax, int psymax, int sfb_smin, int block_type,
    const float *en_l, const float *thm_l, const float *en_s, const float *thm_s,
    float *out_xmin, signed char *out_eac, int *out_max_nonzero) {
    int i, ret;
    lame_internal_flags *gfc = (lame_internal_flags *) calloc(1, sizeof(*gfc));
    gfc->ATH = (ATH_t *) calloc(1, sizeof(ATH_t));
    gfc->cd_psy = (PsyConst_t *) calloc(1, sizeof(PsyConst_t));
    gr_info *gi = (gr_info *) calloc(1, sizeof(gr_info));
    III_psy_ratio *ratio = (III_psy_ratio *) calloc(1, sizeof(III_psy_ratio));
    FLOAT *pxmin = (FLOAT *) calloc(SFBMAX, sizeof(FLOAT));

    gfc->cfg.samplerate_out = samplerate_out;
    gfc->cfg.ATHfixpoint = athfixpoint;
    gfc->sv_qnt.sfb21_extra = sfb21_extra;
    gfc->cfg.use_temporal_masking_effect = use_temporal;
    for (i = 0; i < SBMAX_l + 1; i++) gfc->scalefac_band.l[i] = sbL[i];
    for (i = 0; i < SBMAX_s + 1; i++) gfc->scalefac_band.s[i] = sbS[i];

    gfc->ATH->adjust_factor = ath_adjust_factor;
    gfc->ATH->floor = ath_floor;
    for (i = 0; i < SBMAX_l; i++) gfc->ATH->l[i] = ath_l[i];
    for (i = 0; i < SBMAX_s; i++) gfc->ATH->s[i] = ath_s[i];
    for (i = 0; i < SBMAX_l; i++) gfc->sv_qnt.longfact[i] = longfact[i];
    for (i = 0; i < SBMAX_s; i++) gfc->sv_qnt.shortfact[i] = shortfact[i];
    gfc->cd_psy->decay = decay;

    for (i = 0; i < 576; i++) gi->xr[i] = xr[i];
    for (i = 0; i < SFBMAX; i++) gi->width[i] = width[i];
    gi->psy_lmax = psy_lmax;
    gi->psymax = psymax;
    gi->sfb_smin = sfb_smin;
    gi->block_type = block_type;

    for (i = 0; i < SBMAX_l; i++) {
        ratio->en.l[i] = en_l[i];
        ratio->thm.l[i] = thm_l[i];
    }
    for (i = 0; i < SBMAX_s; i++) {
        int b;
        for (b = 0; b < 3; b++) {
            ratio->en.s[i][b] = en_s[i * 3 + b];
            ratio->thm.s[i][b] = thm_s[i * 3 + b];
        }
    }

    ret = calc_xmin(gfc, ratio, gi, pxmin);

    for (i = 0; i < SFBMAX; i++) out_xmin[i] = pxmin[i];
    for (i = 0; i < SFBMAX; i++) out_eac[i] = gi->energy_above_cutoff[i];
    *out_max_nonzero = gi->max_nonzero_coeff;

    free(pxmin);
    free(ratio);
    free(gi);
    free(gfc->cd_psy);
    free(gfc->ATH);
    free(gfc);
    return ret;
}

int oracle_calc_noise(
    const float *xr, const int *l3_enc, const int *scalefac,
    const int *width, const int *window,
    const int *subblock_gain, int global_gain, int scalefac_scale,
    int preflag, int psymax, int max_nonzero_coeff, int count1, int big_values,
    const float *l3_xmin,
    float *out_distort,
    float *out_over_noise, float *out_tot_noise, float *out_max_noise,
    int *out_over_count, int *out_over_ssd) {
    int i, ret;
    gr_info *gi = (gr_info *) calloc(1, sizeof(gr_info));
    FLOAT *distort = (FLOAT *) calloc(SFBMAX, sizeof(FLOAT));
    calc_noise_result res;
    memset(&res, 0, sizeof(res));

    for (i = 0; i < 576; i++) gi->xr[i] = xr[i];
    for (i = 0; i < 576; i++) gi->l3_enc[i] = l3_enc[i];
    for (i = 0; i < SFBMAX; i++) gi->scalefac[i] = scalefac[i];
    for (i = 0; i < SFBMAX; i++) gi->width[i] = width[i];
    for (i = 0; i < SFBMAX; i++) gi->window[i] = window[i];
    for (i = 0; i < 4; i++) gi->subblock_gain[i] = subblock_gain[i];
    gi->global_gain = global_gain;
    gi->scalefac_scale = scalefac_scale;
    gi->preflag = preflag;
    gi->psymax = psymax;
    gi->max_nonzero_coeff = max_nonzero_coeff;
    gi->count1 = count1;
    gi->big_values = big_values;

    ret = calc_noise(gi, l3_xmin, distort, &res, NULL);

    for (i = 0; i < SFBMAX; i++) out_distort[i] = distort[i];
    *out_over_noise = res.over_noise;
    *out_tot_noise = res.tot_noise;
    *out_max_noise = res.max_noise;
    *out_over_count = res.over_count;
    *out_over_ssd = res.over_SSD;

    free(distort);
    free(gi);
    return ret;
}
