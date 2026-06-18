/* Amalgamation file: compiles all CELT sources as a single
 * translation unit for the Go/Cgo build.
 *
 * celt.c MUST come first: it defines CELT_C, which gates the
 * definition of celt_fatal() in arch.h. If another file includes
 * arch.h first (via the header guard), the definition is skipped.
 */

#include "libopus/celt/celt.c"
#include "libopus/celt/bands.c"
#include "libopus/celt/celt_encoder.c"
#include "libopus/celt/celt_decoder.c"
#include "libopus/celt/cwrs.c"
#include "libopus/celt/entcode.c"
#include "libopus/celt/entdec.c"
#include "libopus/celt/entenc.c"
#include "libopus/celt/kiss_fft.c"
#include "libopus/celt/laplace.c"
#include "libopus/celt/mathops.c"
#include "libopus/celt/mdct.c"
#include "libopus/celt/modes.c"
#include "libopus/celt/pitch.c"
#include "libopus/celt/celt_lpc.c"
#include "libopus/celt/quant_bands.c"
#include "libopus/celt/rate.c"
#include "libopus/celt/vq.c"

/* NEON/SSE intrinsic sources. Included only under OPUS_PRODUCTION_C
 * (Go build tag `opus_production_c`). The default scalar oracle build
 * omits them for bit-exact parity with the Go port — see
 * libopus/config.h for the full rationale. */
#ifdef OPUS_PRODUCTION_C
#  if defined(__aarch64__) || defined(_M_ARM64)
#    include "libopus/celt/arm/armcpu.c"
#    include "libopus/celt/arm/arm_celt_map.c"
#    include "libopus/celt/arm/celt_neon_intr.c"
#    include "libopus/celt/arm/pitch_neon_intr.c"
#  endif
#  if defined(__x86_64__) || defined(_M_X64)
#    include "libopus/celt/x86/x86cpu.c"
#    include "libopus/celt/x86/x86_celt_map.c"
#    include "libopus/celt/x86/pitch_sse.c"
#    include "libopus/celt/x86/pitch_sse2.c"
#    include "libopus/celt/x86/vq_sse2.c"
/* pitch_sse4_1.c is only present in fixed-point builds; float build
 * uses the SSE2 variants above. */
#  endif
#endif

/* ─── Drift-bisection state-dump helper for CELT ──────────────────
 * Fills the CELT subfields of opus_test_encoder_full_dump (declared in
 * opus_state_dump.h). Layout is shared via the header; this TU can
 * see the full struct OpusCustomEncoder layout because celt_encoder.c
 * is compiled here. */

#include <string.h>
#include <stdint.h>
#include "opus_state_dump.h"

static uint32_t _ct_f32_bits(float v) {
    uint32_t out;
    memcpy(&out, &v, sizeof(out));
    return out;
}

static uint32_t _ct_fnv1a(const void *p, size_t n) {
    const unsigned char *b = (const unsigned char*)p;
    uint32_t h = 2166136261u;
    size_t i;
    for (i = 0; i < n; i++) { h ^= b[i]; h *= 16777619u; }
    return h;
}

void opus_test_dump_celt_encoder(void *celt_enc_v,
                                 struct opus_test_encoder_full_dump *dst) {
    CELTEncoder *st = (CELTEncoder*)celt_enc_v;
    int i;

    dst->celt_channels        = st->channels;
    dst->celt_stream_channels = st->stream_channels;
    dst->celt_force_intra     = st->force_intra;
    dst->celt_clip            = st->clip;
    dst->celt_disable_pf      = st->disable_pf;
    dst->celt_complexity      = st->complexity;
    dst->celt_upsample        = st->upsample;
    dst->celt_start           = st->start;
    dst->celt_end             = st->end;
    dst->celt_bitrate         = st->bitrate;
    dst->celt_vbr             = st->vbr;
    dst->celt_signalling      = st->signalling;
    dst->celt_constrained_vbr = st->constrained_vbr;
    dst->celt_loss_rate       = st->loss_rate;
    dst->celt_lsb_depth       = st->lsb_depth;
    dst->celt_lfe             = st->lfe;
    dst->celt_disable_inv     = st->disable_inv;
    dst->celt_arch            = st->arch;

    dst->celt_rng             = st->rng;
    dst->celt_spread_decision = st->spread_decision;
    dst->celt_delayedIntra_bits = _ct_f32_bits((float)st->delayedIntra);
    dst->celt_tonal_average   = st->tonal_average;
    dst->celt_lastCodedBands  = st->lastCodedBands;
    dst->celt_hf_average      = st->hf_average;
    dst->celt_tapset_decision = st->tapset_decision;
    dst->celt_prefilter_period = st->prefilter_period;
    dst->celt_prefilter_gain_bits = _ct_f32_bits((float)st->prefilter_gain);
    dst->celt_prefilter_tapset = st->prefilter_tapset;
    dst->celt_consec_transient = st->consec_transient;

    for (i = 0; i < 2; i++) {
        dst->celt_preemph_memE_bits[i] = _ct_f32_bits((float)st->preemph_memE[i]);
        dst->celt_preemph_memD_bits[i] = _ct_f32_bits((float)st->preemph_memD[i]);
    }

    dst->celt_vbr_reservoir   = st->vbr_reservoir;
    dst->celt_vbr_drift       = st->vbr_drift;
    dst->celt_vbr_offset      = st->vbr_offset;
    dst->celt_vbr_count       = st->vbr_count;
    dst->celt_overlap_max_bits = _ct_f32_bits((float)st->overlap_max);
    dst->celt_stereo_saving_bits = _ct_f32_bits((float)st->stereo_saving);
    dst->celt_intensity       = st->intensity;
    dst->celt_spec_avg_bits   = _ct_f32_bits((float)st->spec_avg);

    {
        const CELTMode *mode = st->mode;
        int nbE = mode ? mode->nbEBands : 21;
        int overlap = mode ? mode->overlap : 120;
        int C = st->channels;
        celt_sig *in_mem = st->in_mem;
        celt_sig *prefilter_mem = in_mem + C*overlap;
        celt_glog *oldBandE = (celt_glog*)(prefilter_mem + C*COMBFILTER_MAXPERIOD);
        celt_glog *oldLogE  = oldBandE + C*nbE;
        celt_glog *oldLogE2 = oldLogE + C*nbE;
        celt_glog *energyError = oldLogE2 + C*nbE;

        dst->celt_in_mem_fp_crc        = _ct_fnv1a(in_mem, C*overlap*sizeof(celt_sig));
        dst->celt_prefilter_mem_fp_crc = _ct_fnv1a(prefilter_mem, C*COMBFILTER_MAXPERIOD*sizeof(celt_sig));
        dst->celt_oldBandE_fp_crc      = _ct_fnv1a(oldBandE, C*nbE*sizeof(celt_glog));
        dst->celt_oldLogE_fp_crc       = _ct_fnv1a(oldLogE, C*nbE*sizeof(celt_glog));
        dst->celt_oldLogE2_fp_crc      = _ct_fnv1a(oldLogE2, C*nbE*sizeof(celt_glog));
        dst->celt_energyError_fp_crc   = _ct_fnv1a(energyError, C*nbE*sizeof(celt_glog));

        for (i = 0; i < 16 && i < C*nbE; i++) {
            dst->celt_oldBandE_f16[i]    = _ct_f32_bits((float)oldBandE[i]);
            dst->celt_oldLogE_f16[i]     = _ct_f32_bits((float)oldLogE[i]);
            dst->celt_oldLogE2_f16[i]    = _ct_f32_bits((float)oldLogE2[i]);
            dst->celt_energyError_f16[i] = _ct_f32_bits((float)energyError[i]);
        }
        dst->celt_preemph_memE_f16[0] = _ct_f32_bits((float)st->preemph_memE[0]);
        dst->celt_preemph_memE_f16[1] = _ct_f32_bits((float)st->preemph_memE[1]);
    }
}
