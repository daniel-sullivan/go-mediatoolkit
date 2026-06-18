// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* oracle.c — its own TU AND the psymodel TU for the vbrpsy-multigran slice. It
 * #includes libmp3lame/psymodel.c directly so it can call the static
 * L3psycho_anal_vbr; the rest of the vendored encoder (lame_init_params + the
 * init helpers psymodel depends on) comes from the per-TU lame_*.c wrappers
 * compiled alongside (see oracle.h / cgo.go for the parity rationale and the
 * per-TU split). Build config + include paths come from cgo.go.
 *
 * It reproduces the encoder's first-frame mfbuf exactly (528 primed-silence
 * samples then the pcm_transform-widened synthetic PCM), then drives the genuine
 * L3psycho_anal_vbr for gr=0 and gr=1 and captures the mfbuf + per-granule
 * energy / pe / pe_MS / masking en+thm so the Go side can run nativemp3's vbrpsy
 * over the byte-identical mfbuf and compare bit-for-bit. */

#include <config.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

/* Pull the static L3psycho_anal_vbr + all of psymodel.c into THIS TU. */
#include "libmp3lame/psymodel.c"

#include "lame.h"
#include "machine.h"
#include "encoder.h"
#include "util.h"
#include "lame_global_flags.h"

#include "oracle.h"

struct mp3parity_mg_t {
    int    channels_out;
    int    n_chn_psy;
    int    mf_size;

    float *mfbuf[2];      /* the shared first-frame mfbuf, len = mf_size */

    float  energy[MP3PARITY_MG_NGR][4];
    float  pe[MP3PARITY_MG_NGR][2];
    float  pe_ms[MP3PARITY_MG_NGR][2];

    /* masking en/thm: [which: 0=LR,1=MS][gr][ch] */
    float  en_l[2][MP3PARITY_MG_NGR][2][MP3PARITY_MG_SBMAXL];
    float  thm_l[2][MP3PARITY_MG_NGR][2][MP3PARITY_MG_SBMAXL];
    float  en_s[2][MP3PARITY_MG_NGR][2][MP3PARITY_MG_SBMAXS * 3];
    float  thm_s[2][MP3PARITY_MG_NGR][2][MP3PARITY_MG_SBMAXS * 3];

    /* long-block PsyConst_CB2SB_t init tables (gdl). */
    int    gdl_npart;
    int    gdl_s3_len;
    float *gdl_s3;                   /* [gdl_s3_len] */
    float  gdl_masking_lower[CBANDS];
    float  gdl_minval[CBANDS];
    float  gdl_rnumlines[CBANDS];
    float  gdl_bo_weight[MP3PARITY_MG_SBMAXL];
    float  gdl_mld[MP3PARITY_MG_SBMAXL];

    /* short-block PsyConst_CB2SB_t init tables (gds). */
    int    gds_s3_len;
    float *gds_s3;
    float  gds_masking_lower[CBANDS];
    float  gds_minval[CBANDS];
    float  gds_rnumlines[CBANDS];
    float  gds_mld[MP3PARITY_MG_SBMAXS];
    float  gds_mld_cb[CBANDS];
    float  ath_cb_s[CBANDS];
    float  ath_cb_l[CBANDS];
};

/* synth_pcm mirrors the vbr-encode-e2e oracle's deterministic generator so the
 * inputs line up with the e2e slice this localizes. */
static short
synth_sample(int i, int samplerate, unsigned seed, double *phase)
{
    double f = 220.0 + 660.0 * (0.5 + 0.5 * sin((double) i / 4096.0 + (double) seed));
    double s = 0.6 * sin(*phase) + 0.25 * sin(3.0 * *phase) + 0.1 * sin(7.0 * *phase);
    short v = (short) (s * 26000.0);
    *phase += 2.0 * 3.14159265358979323846 * f / (double) samplerate;
    return v;
}

mp3parity_mg_t *
mp3parity_mg_run(int samplerate, int channels, int nsamples_per_ch, unsigned seed)
{
    lame_global_flags *gfp;
    lame_internal_flags *gfc;
    SessionConfig_t *cfg;
    mp3parity_mg_t *h;
    int i, ch, gr;
    int mf_prime, mf_need;
    double phase;

    gfp = lame_init();
    if (gfp == NULL)
        return NULL;
    lame_set_in_samplerate(gfp, samplerate);
    lame_set_num_channels(gfp, channels);
    lame_set_quality(gfp, 5);
    if (channels == 1)
        lame_set_mode(gfp, MONO);
    else
        lame_set_mode(gfp, JOINT_STEREO);
    lame_set_VBR(gfp, vbr_default); /* vbr_mtrh */
    lame_set_VBR_q(gfp, 2);

    if (lame_init_params(gfp) < 0) {
        lame_close(gfp);
        return NULL;
    }
    gfc = gfp->internal_flags;
    cfg = &gfc->cfg;

    h = (mp3parity_mg_t *) calloc(1, sizeof(*h));
    if (h == NULL) {
        lame_close(gfp);
        return NULL;
    }
    h->channels_out = cfg->channels_out;
    h->n_chn_psy = (cfg->mode == JOINT_STEREO) ? 4 : cfg->channels_out;

    /* first-frame mfbuf: mf_size primed to ENCDELAY-MDCTDELAY leading zeros, then
     * fill to the FFT need. framesize = 576*mode_gr. */
    mf_prime = ENCDELAY - MDCTDELAY;                      /* 528 */
    mf_need = BLKSIZE + 576 * cfg->mode_gr - FFTOFFSET;   /* the FFT need */
    h->mf_size = mf_need;

    h->mfbuf[0] = (float *) calloc((size_t) mf_need, sizeof(float));
    h->mfbuf[1] = (float *) calloc((size_t) mf_need, sizeof(float));
    if (h->mfbuf[0] == NULL || h->mfbuf[1] == NULL) {
        free(h->mfbuf[0]);
        free(h->mfbuf[1]);
        free(h);
        lame_close(gfp);
        return NULL;
    }

    /* Widen the synthetic int16 PCM through cfg->pcm_transform exactly as
     * lame_copy_inbuffer does (s = 1.0, so m == pcm_transform), into mfbuf at
     * the primed offset. The encoder fills until mf_size >= mf_need; we fill
     * mf_need-mf_prime samples (the rest of the granule pair is irrelevant to the
     * first frame). The synthetic generator runs from sample 0 (the FIRST PCM
     * sample lands at mfbuf[mf_prime]). */
    phase = 0.0;
    for (i = 0; i + mf_prime < mf_need; i++) {
        short v = synth_sample(i, samplerate, seed, &phase);
        float xl, xr;
        if (channels == 1) {
            xl = (float) v;
            xr = (float) v; /* mono: r aliases l (lame_encode_buffer_template) */
        } else {
            xl = (float) v;
            xr = (float) ((short) (v * 0.85));
        }
        /* u = xl*m00 + xr*m01 ; v = xl*m10 + xr*m11 */
        h->mfbuf[0][mf_prime + i] =
            xl * cfg->pcm_transform[0][0] + xr * cfg->pcm_transform[0][1];
        if (cfg->channels_out == 2) {
            h->mfbuf[1][mf_prime + i] =
                xl * cfg->pcm_transform[1][0] + xr * cfg->pcm_transform[1][1];
        }
    }
    (void) nsamples_per_ch;

    /* Run the genuine L3psycho_anal_vbr per granule, exactly as
     * lame_encode_mp3_frame does (encoder.c:356-393). */
    {
        III_psy_ratio masking_LR[2][2];
        III_psy_ratio masking_MS[2][2];
        FLOAT tot_ener[2][4];
        FLOAT pe[2][2];
        FLOAT pe_MS[2][2];
        int   blocktype[2];
        const sample_t *bufp[2];

        memset(masking_LR, 0, sizeof(masking_LR));
        memset(masking_MS, 0, sizeof(masking_MS));
        memset(tot_ener, 0, sizeof(tot_ener));
        memset(pe, 0, sizeof(pe));
        memset(pe_MS, 0, sizeof(pe_MS));

        for (gr = 0; gr < cfg->mode_gr; gr++) {
            for (ch = 0; ch < cfg->channels_out; ch++) {
                bufp[ch] = &h->mfbuf[ch][576 + gr * 576 - FFTOFFSET];
            }
            (void) L3psycho_anal_vbr(gfc, bufp, gr, masking_LR, masking_MS,
                                     pe[gr], pe_MS[gr], tot_ener[gr], blocktype);

            for (i = 0; i < 4; i++)
                h->energy[gr][i] = (float) tot_ener[gr][i];
            for (i = 0; i < 2; i++) {
                h->pe[gr][i] = (float) pe[gr][i];
                h->pe_ms[gr][i] = (float) pe_MS[gr][i];
            }
            for (ch = 0; ch < 2; ch++) {
                int sb, sub;
                for (sb = 0; sb < SBMAX_l; sb++) {
                    h->en_l[0][gr][ch][sb] = (float) masking_LR[gr][ch].en.l[sb];
                    h->thm_l[0][gr][ch][sb] = (float) masking_LR[gr][ch].thm.l[sb];
                    h->en_l[1][gr][ch][sb] = (float) masking_MS[gr][ch].en.l[sb];
                    h->thm_l[1][gr][ch][sb] = (float) masking_MS[gr][ch].thm.l[sb];
                }
                for (sb = 0; sb < SBMAX_s; sb++) {
                    for (sub = 0; sub < 3; sub++) {
                        int k = sb * 3 + sub;
                        h->en_s[0][gr][ch][k] = (float) masking_LR[gr][ch].en.s[sb][sub];
                        h->thm_s[0][gr][ch][k] = (float) masking_LR[gr][ch].thm.s[sb][sub];
                        h->en_s[1][gr][ch][k] = (float) masking_MS[gr][ch].en.s[sb][sub];
                        h->thm_s[1][gr][ch][k] = (float) masking_MS[gr][ch].thm.s[sb][sub];
                    }
                }
            }
        }
    }

    /* Capture the long-block init tables before lame_close frees cd_psy. */
    {
        PsyConst_CB2SB_t const *const gdl = &gfc->cd_psy->l;
        int b, s3len = 0;
        h->gdl_npart = gdl->npart;
        for (b = 0; b < gdl->npart; b++) {
            s3len += gdl->s3ind[b][1] - gdl->s3ind[b][0] + 1;
        }
        h->gdl_s3_len = s3len;
        h->gdl_s3 = (float *) malloc(sizeof(float) * (size_t) (s3len > 0 ? s3len : 1));
        if (h->gdl_s3 != NULL) {
            for (b = 0; b < s3len; b++)
                h->gdl_s3[b] = (float) gdl->s3[b];
        }
        for (b = 0; b < CBANDS; b++) {
            h->gdl_masking_lower[b] = (float) gdl->masking_lower[b];
            h->gdl_minval[b] = (float) gdl->minval[b];
            h->gdl_rnumlines[b] = (float) gdl->rnumlines[b];
        }
        for (b = 0; b < SBMAX_l; b++) {
            h->gdl_bo_weight[b] = (float) gdl->bo_weight[b];
            h->gdl_mld[b] = (float) gdl->mld[b];
        }
        {
            PsyConst_CB2SB_t const *const gds = &gfc->cd_psy->s;
            int s = 0;
            for (b = 0; b < gds->npart; b++)
                s += gds->s3ind[b][1] - gds->s3ind[b][0] + 1;
            h->gds_s3_len = s;
            h->gds_s3 = (float *) malloc(sizeof(float) * (size_t) (s > 0 ? s : 1));
            if (h->gds_s3 != NULL)
                for (b = 0; b < s; b++)
                    h->gds_s3[b] = (float) gds->s3[b];
            for (b = 0; b < CBANDS; b++) {
                h->gds_masking_lower[b] = (float) gds->masking_lower[b];
                h->gds_minval[b] = (float) gds->minval[b];
                h->gds_rnumlines[b] = (float) gds->rnumlines[b];
            }
            for (b = 0; b < SBMAX_s; b++)
                h->gds_mld[b] = (float) gds->mld[b];
            for (b = 0; b < CBANDS; b++)
                h->gds_mld_cb[b] = (float) gds->mld_cb[b];
        }
        for (b = 0; b < CBANDS; b++) {
            h->ath_cb_s[b] = (float) gfc->ATH->cb_s[b];
            h->ath_cb_l[b] = (float) gfc->ATH->cb_l[b];
        }
    }

    lame_close(gfp);
    return h;
}

void
mp3parity_mg_free(mp3parity_mg_t *h)
{
    if (h == NULL)
        return;
    free(h->mfbuf[0]);
    free(h->mfbuf[1]);
    free(h->gdl_s3);
    free(h->gds_s3);
    free(h);
}

int mp3parity_mg_mf_size(const mp3parity_mg_t *h) { return h->mf_size; }
int mp3parity_mg_channels_out(const mp3parity_mg_t *h) { return h->channels_out; }
int mp3parity_mg_n_chn_psy(const mp3parity_mg_t *h) { return h->n_chn_psy; }

const float *mp3parity_mg_mfbuf_ptr(const mp3parity_mg_t *h, int ch) { return h->mfbuf[ch]; }
int          mp3parity_mg_mfbuf_len(const mp3parity_mg_t *h) { return h->mf_size; }

const float *mp3parity_mg_energy(const mp3parity_mg_t *h, int gr) { return h->energy[gr]; }
const float *mp3parity_mg_pe(const mp3parity_mg_t *h, int gr) { return h->pe[gr]; }
const float *mp3parity_mg_pe_ms(const mp3parity_mg_t *h, int gr) { return h->pe_ms[gr]; }

const float *mp3parity_mg_en_l(const mp3parity_mg_t *h, int which, int gr, int ch) { return h->en_l[which][gr][ch]; }
const float *mp3parity_mg_thm_l(const mp3parity_mg_t *h, int which, int gr, int ch) { return h->thm_l[which][gr][ch]; }
const float *mp3parity_mg_en_s(const mp3parity_mg_t *h, int which, int gr, int ch) { return h->en_s[which][gr][ch]; }
const float *mp3parity_mg_thm_s(const mp3parity_mg_t *h, int which, int gr, int ch) { return h->thm_s[which][gr][ch]; }

int          mp3parity_mg_gdl_npart(const mp3parity_mg_t *h) { return h->gdl_npart; }
int          mp3parity_mg_gdl_s3_len(const mp3parity_mg_t *h) { return h->gdl_s3_len; }
const float *mp3parity_mg_gdl_s3(const mp3parity_mg_t *h) { return h->gdl_s3; }
const float *mp3parity_mg_gdl_masking_lower(const mp3parity_mg_t *h) { return h->gdl_masking_lower; }
const float *mp3parity_mg_gdl_minval(const mp3parity_mg_t *h) { return h->gdl_minval; }
const float *mp3parity_mg_gdl_rnumlines(const mp3parity_mg_t *h) { return h->gdl_rnumlines; }
const float *mp3parity_mg_gdl_bo_weight(const mp3parity_mg_t *h) { return h->gdl_bo_weight; }
const float *mp3parity_mg_gdl_mld(const mp3parity_mg_t *h) { return h->gdl_mld; }

int          mp3parity_mg_gds_s3_len(const mp3parity_mg_t *h) { return h->gds_s3_len; }
const float *mp3parity_mg_gds_s3(const mp3parity_mg_t *h) { return h->gds_s3; }
const float *mp3parity_mg_gds_masking_lower(const mp3parity_mg_t *h) { return h->gds_masking_lower; }
const float *mp3parity_mg_gds_minval(const mp3parity_mg_t *h) { return h->gds_minval; }
const float *mp3parity_mg_gds_rnumlines(const mp3parity_mg_t *h) { return h->gds_rnumlines; }
const float *mp3parity_mg_gds_mld(const mp3parity_mg_t *h) { return h->gds_mld; }

const float *mp3parity_mg_gds_mld_cb(const mp3parity_mg_t *h) { return h->gds_mld_cb; }
const float *mp3parity_mg_ath_cb_s(const mp3parity_mg_t *h) { return h->ath_cb_s; }
const float *mp3parity_mg_ath_cb_l(const mp3parity_mg_t *h) { return h->ath_cb_l; }
