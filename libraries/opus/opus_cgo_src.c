/* Amalgamation file: compiles all Opus src/ sources as a single
 * translation unit for the Go/Cgo build. This file is only compiled
 * when a Go file in this package contains `import "C"`.
 */

#include "libopus/src/opus.c"
#include "libopus/src/opus_decoder.c"
#include "libopus/src/opus_encoder.c"
#include "libopus/src/extensions.c"
#include "libopus/src/opus_multistream.c"
#include "libopus/src/opus_multistream_encoder.c"
#include "libopus/src/opus_multistream_decoder.c"
#include "libopus/src/repacketizer.c"
#include "libopus/src/opus_projection_encoder.c"
#include "libopus/src/opus_projection_decoder.c"
#include "libopus/src/mapping_matrix.c"

/* Float-only sources (analysis, MLP) */
#include "libopus/src/analysis.c"
#include "libopus/src/mlp.c"
#include "libopus/src/mlp_data.c"

/* ─────────────────────────────────────────────────────────────────
 * Test-only helpers. These live in the amalgamation TU because the
 * OpusEncoder struct is defined statically inside opus_encoder.c;
 * outside it, only the opaque forward declaration is visible.
 *
 * Used by libraries/opus/internal/parity_tests/benchcmp parity tests.
 * ───────────────────────────────────────────────────────────────── */

#include <string.h>

struct opus_test_enc_snapshot {
    int32_t celt_enc_offset;
    int32_t silk_enc_offset;
    int32_t application;
    int32_t channels;
    int32_t delay_compensation;
    int32_t force_channels;
    int32_t signal_type;
    int32_t user_bandwidth;
    int32_t max_bandwidth;
    int32_t user_forced_mode;
    int32_t voice_ratio;
    int32_t Fs;
    int32_t use_vbr;
    int32_t vbr_constraint;
    int32_t variable_duration;
    int32_t bitrate_bps;
    int32_t user_bitrate_bps;
    int32_t lsb_depth;
    int32_t encoder_buffer;
    int32_t lfe;
    int32_t arch;
    int32_t use_dtx;
    int32_t fec_config;

    int32_t silk_nChannelsAPI;
    int32_t silk_nChannelsInternal;
    int32_t silk_API_sampleRate;
    int32_t silk_maxInternalSampleRate;
    int32_t silk_minInternalSampleRate;
    int32_t silk_desiredInternalSampleRate;
    int32_t silk_payloadSize_ms;
    int32_t silk_bitRate;
    int32_t silk_packetLossPercentage;
    int32_t silk_complexity;
    int32_t silk_useInBandFEC;
    int32_t silk_useDTX;
    int32_t silk_useCBR;
    int32_t silk_reducedDependency;

    int32_t stream_channels;
    int32_t hybrid_stereo_width_Q14;
    int32_t variable_HP_smth2_Q15;
    uint32_t prev_HB_gain_bits;
    int32_t mode;
    int32_t prev_mode;
    int32_t prev_channels;
    int32_t prev_framesize;
    int32_t bandwidth;
    int32_t first;
    int32_t nb_no_activity_ms_Q1;
};

int opus_test_encoder_init_and_snapshot(int32_t Fs, int channels, int application,
                                        struct opus_test_enc_snapshot *snap) {
    int size = opus_encoder_get_size(channels);
    if (size <= 0) return OPUS_INTERNAL_ERROR;
    OpusEncoder *st = (OpusEncoder*)opus_alloc(size);
    if (st == NULL) return OPUS_ALLOC_FAIL;
    int ret = opus_encoder_init(st, Fs, channels, application);
    if (ret == OPUS_OK && snap != NULL) {
        memset(snap, 0, sizeof(*snap));
        snap->celt_enc_offset     = st->celt_enc_offset;
        snap->silk_enc_offset     = st->silk_enc_offset;
        snap->application         = st->application;
        snap->channels            = st->channels;
        snap->delay_compensation  = st->delay_compensation;
        snap->force_channels      = st->force_channels;
        snap->signal_type         = st->signal_type;
        snap->user_bandwidth      = st->user_bandwidth;
        snap->max_bandwidth       = st->max_bandwidth;
        snap->user_forced_mode    = st->user_forced_mode;
        snap->voice_ratio         = st->voice_ratio;
        snap->Fs                  = st->Fs;
        snap->use_vbr             = st->use_vbr;
        snap->vbr_constraint      = st->vbr_constraint;
        snap->variable_duration   = st->variable_duration;
        snap->bitrate_bps         = st->bitrate_bps;
        snap->user_bitrate_bps    = st->user_bitrate_bps;
        snap->lsb_depth           = st->lsb_depth;
        snap->encoder_buffer      = st->encoder_buffer;
        snap->lfe                 = st->lfe;
        snap->arch                = st->arch;
        snap->use_dtx             = st->use_dtx;
        snap->fec_config          = st->fec_config;

        snap->silk_nChannelsAPI              = st->silk_mode.nChannelsAPI;
        snap->silk_nChannelsInternal         = st->silk_mode.nChannelsInternal;
        snap->silk_API_sampleRate            = st->silk_mode.API_sampleRate;
        snap->silk_maxInternalSampleRate     = st->silk_mode.maxInternalSampleRate;
        snap->silk_minInternalSampleRate     = st->silk_mode.minInternalSampleRate;
        snap->silk_desiredInternalSampleRate = st->silk_mode.desiredInternalSampleRate;
        snap->silk_payloadSize_ms            = st->silk_mode.payloadSize_ms;
        snap->silk_bitRate                   = st->silk_mode.bitRate;
        snap->silk_packetLossPercentage      = st->silk_mode.packetLossPercentage;
        snap->silk_complexity                = st->silk_mode.complexity;
        snap->silk_useInBandFEC              = st->silk_mode.useInBandFEC;
        snap->silk_useDTX                    = st->silk_mode.useDTX;
        snap->silk_useCBR                    = st->silk_mode.useCBR;
        snap->silk_reducedDependency         = st->silk_mode.reducedDependency;

        snap->stream_channels         = st->stream_channels;
        snap->hybrid_stereo_width_Q14 = st->hybrid_stereo_width_Q14;
        snap->variable_HP_smth2_Q15   = st->variable_HP_smth2_Q15;
        {
            float g = (float)st->prev_HB_gain;
            uint32_t bits;
            memcpy(&bits, &g, sizeof(bits));
            snap->prev_HB_gain_bits = bits;
        }
        snap->mode                 = st->mode;
        snap->prev_mode            = st->prev_mode;
        snap->prev_channels        = st->prev_channels;
        snap->prev_framesize       = st->prev_framesize;
        snap->bandwidth            = st->bandwidth;
        snap->first                = st->first;
        snap->nb_no_activity_ms_Q1 = st->nb_no_activity_ms_Q1;
    }
    opus_free(st);
    return ret;
}

/* 9f-B: opus_encoder_ctl parity helpers */

static void opus_test_fill_snapshot(OpusEncoder *st, struct opus_test_enc_snapshot *snap) {
    memset(snap, 0, sizeof(*snap));
    snap->celt_enc_offset     = st->celt_enc_offset;
    snap->silk_enc_offset     = st->silk_enc_offset;
    snap->application         = st->application;
    snap->channels            = st->channels;
    snap->delay_compensation  = st->delay_compensation;
    snap->force_channels      = st->force_channels;
    snap->signal_type         = st->signal_type;
    snap->user_bandwidth      = st->user_bandwidth;
    snap->max_bandwidth       = st->max_bandwidth;
    snap->user_forced_mode    = st->user_forced_mode;
    snap->voice_ratio         = st->voice_ratio;
    snap->Fs                  = st->Fs;
    snap->use_vbr             = st->use_vbr;
    snap->vbr_constraint      = st->vbr_constraint;
    snap->variable_duration   = st->variable_duration;
    snap->bitrate_bps         = st->bitrate_bps;
    snap->user_bitrate_bps    = st->user_bitrate_bps;
    snap->lsb_depth           = st->lsb_depth;
    snap->encoder_buffer      = st->encoder_buffer;
    snap->lfe                 = st->lfe;
    snap->arch                = st->arch;
    snap->use_dtx             = st->use_dtx;
    snap->fec_config          = st->fec_config;

    snap->silk_nChannelsAPI              = st->silk_mode.nChannelsAPI;
    snap->silk_nChannelsInternal         = st->silk_mode.nChannelsInternal;
    snap->silk_API_sampleRate            = st->silk_mode.API_sampleRate;
    snap->silk_maxInternalSampleRate     = st->silk_mode.maxInternalSampleRate;
    snap->silk_minInternalSampleRate     = st->silk_mode.minInternalSampleRate;
    snap->silk_desiredInternalSampleRate = st->silk_mode.desiredInternalSampleRate;
    snap->silk_payloadSize_ms            = st->silk_mode.payloadSize_ms;
    snap->silk_bitRate                   = st->silk_mode.bitRate;
    snap->silk_packetLossPercentage      = st->silk_mode.packetLossPercentage;
    snap->silk_complexity                = st->silk_mode.complexity;
    snap->silk_useInBandFEC              = st->silk_mode.useInBandFEC;
    snap->silk_useDTX                    = st->silk_mode.useDTX;
    snap->silk_useCBR                    = st->silk_mode.useCBR;
    snap->silk_reducedDependency         = st->silk_mode.reducedDependency;

    snap->stream_channels         = st->stream_channels;
    snap->hybrid_stereo_width_Q14 = st->hybrid_stereo_width_Q14;
    snap->variable_HP_smth2_Q15   = st->variable_HP_smth2_Q15;
    {
        float g = (float)st->prev_HB_gain;
        uint32_t bits;
        memcpy(&bits, &g, sizeof(bits));
        snap->prev_HB_gain_bits = bits;
    }
    snap->mode                 = st->mode;
    snap->prev_mode            = st->prev_mode;
    snap->prev_channels        = st->prev_channels;
    snap->prev_framesize       = st->prev_framesize;
    snap->bandwidth            = st->bandwidth;
    snap->first                = st->first;
    snap->nb_no_activity_ms_Q1 = st->nb_no_activity_ms_Q1;
}

/* Applies opus_encoder_ctl(request, value) on a freshly-initialised
 * encoder and writes the resulting state into *snap. Returns the CTL
 * return code. initRet, if non-NULL, receives the init return code. */
int opus_test_encoder_ctl_set_and_snapshot(int32_t Fs, int channels, int application,
                                           int request, int32_t value,
                                           struct opus_test_enc_snapshot *snap,
                                           int *initRet) {
    int size = opus_encoder_get_size(channels);
    if (size <= 0) {
        if (initRet) *initRet = OPUS_INTERNAL_ERROR;
        return OPUS_INTERNAL_ERROR;
    }
    OpusEncoder *st = (OpusEncoder*)opus_alloc(size);
    if (st == NULL) {
        if (initRet) *initRet = OPUS_ALLOC_FAIL;
        return OPUS_ALLOC_FAIL;
    }
    int ir = opus_encoder_init(st, Fs, channels, application);
    if (initRet) *initRet = ir;
    if (ir != OPUS_OK) {
        opus_free(st);
        return ir;
    }
    int ctlRet;
    switch (request) {
        case OPUS_RESET_STATE:
            ctlRet = opus_encoder_ctl(st, request);
            break;
        case OPUS_GET_LOOKAHEAD_REQUEST:
        case OPUS_GET_SAMPLE_RATE_REQUEST:
        {
            opus_int32 tmp;
            ctlRet = opus_encoder_ctl(st, request, &tmp);
            break;
        }
        case OPUS_GET_FINAL_RANGE_REQUEST:
        {
            opus_uint32 tmp;
            ctlRet = opus_encoder_ctl(st, request, &tmp);
            break;
        }
        default:
            ctlRet = opus_encoder_ctl(st, request, (opus_int32)value);
    }
    if (snap) opus_test_fill_snapshot(st, snap);
    opus_free(st);
    return ctlRet;
}

/* Applies an optional pre-set CTL, then runs a GET CTL expecting
 * opus_int32; writes out through *outValue. */
int opus_test_encoder_ctl_get_i32(int32_t Fs, int channels, int application,
                                  int preSetRequest, int32_t preSetValue,
                                  int getRequest,
                                  int *initRet, int *preSetRet,
                                  int32_t *outValue) {
    int size = opus_encoder_get_size(channels);
    if (size <= 0) {
        if (initRet) *initRet = OPUS_INTERNAL_ERROR;
        return OPUS_INTERNAL_ERROR;
    }
    OpusEncoder *st = (OpusEncoder*)opus_alloc(size);
    if (st == NULL) {
        if (initRet) *initRet = OPUS_ALLOC_FAIL;
        return OPUS_ALLOC_FAIL;
    }
    int ir = opus_encoder_init(st, Fs, channels, application);
    if (initRet) *initRet = ir;
    if (ir != OPUS_OK) {
        opus_free(st);
        return ir;
    }
    int pr = 0;
    if (preSetRequest != 0) {
        pr = opus_encoder_ctl(st, preSetRequest, (opus_int32)preSetValue);
    }
    if (preSetRet) *preSetRet = pr;
    opus_int32 out = 0;
    int getRet = opus_encoder_ctl(st, getRequest, &out);
    if (outValue) *outValue = (int32_t)out;
    opus_free(st);
    return getRet;
}

/* Same shape but opus_uint32 out value (OPUS_GET_FINAL_RANGE). */
int opus_test_encoder_ctl_get_u32(int32_t Fs, int channels, int application,
                                  int preSetRequest, int32_t preSetValue,
                                  int getRequest,
                                  int *initRet, int *preSetRet,
                                  uint32_t *outValue) {
    int size = opus_encoder_get_size(channels);
    if (size <= 0) {
        if (initRet) *initRet = OPUS_INTERNAL_ERROR;
        return OPUS_INTERNAL_ERROR;
    }
    OpusEncoder *st = (OpusEncoder*)opus_alloc(size);
    if (st == NULL) {
        if (initRet) *initRet = OPUS_ALLOC_FAIL;
        return OPUS_ALLOC_FAIL;
    }
    int ir = opus_encoder_init(st, Fs, channels, application);
    if (initRet) *initRet = ir;
    if (ir != OPUS_OK) {
        opus_free(st);
        return ir;
    }
    int pr = 0;
    if (preSetRequest != 0) {
        pr = opus_encoder_ctl(st, preSetRequest, (opus_int32)preSetValue);
    }
    if (preSetRet) *preSetRet = pr;
    opus_uint32 out = 0;
    int getRet = opus_encoder_ctl(st, getRequest, &out);
    if (outValue) *outValue = (uint32_t)out;
    opus_free(st);
    return getRet;
}

int32_t opus_test_user_bitrate_to_bitrate(int32_t Fs, int channels, int application,
                                          int32_t user_bitrate_bps,
                                          int frame_size, int max_data_bytes) {
    int size = opus_encoder_get_size(channels);
    if (size <= 0) return 0;
    OpusEncoder *st = (OpusEncoder*)opus_alloc(size);
    if (st == NULL) return 0;
    if (opus_encoder_init(st, Fs, channels, application) != OPUS_OK) {
        opus_free(st);
        return 0;
    }
    st->user_bitrate_bps = user_bitrate_bps;
    int32_t result = user_bitrate_to_bitrate(st, frame_size, max_data_bytes);
    opus_free(st);
    return result;
}

/* ─── 9f-D helpers: threshold tables + leaf static functions ─────── */

int opus_test_decide_fec(int useInBandFEC, int PacketLoss_perc, int last_fec,
                         int mode, int *bandwidth_io, int32_t rate) {
    return decide_fec(useInBandFEC, PacketLoss_perc, last_fec, mode,
                      bandwidth_io, (opus_int32)rate);
}

int opus_test_compute_silk_rate_for_hybrid(int rate, int bandwidth, int frame20ms,
                                           int vbr, int fec, int channels) {
    return compute_silk_rate_for_hybrid(rate, bandwidth, frame20ms, vbr, fec, channels);
}

int32_t opus_test_compute_equiv_rate(int32_t bitrate, int channels, int frame_rate,
                                     int vbr, int mode, int complexity, int loss) {
    return (int32_t)compute_equiv_rate((opus_int32)bitrate, channels, frame_rate,
                                       vbr, mode, complexity, loss);
}

float opus_test_compute_frame_energy(const float *pcm, int frame_size, int channels, int arch) {
    return (float)compute_frame_energy((const opus_res*)pcm, frame_size, channels, arch);
}

int opus_test_decide_dtx_mode(int activity, int *nb_no_activity_ms_Q1, int frame_size_ms_Q1) {
    return decide_dtx_mode(activity, nb_no_activity_ms_Q1, frame_size_ms_Q1);
}

int opus_test_compute_redundancy_bytes(int32_t max_data_bytes, int32_t bitrate_bps,
                                        int frame_rate, int channels) {
    return compute_redundancy_bytes((opus_int32)max_data_bytes,
                                    (opus_int32)bitrate_bps, frame_rate, channels);
}

struct opus_test_bandwidth_thresholds {
    int32_t mono_voice[8];
    int32_t mono_music[8];
    int32_t stereo_voice[8];
    int32_t stereo_music[8];
};

void opus_test_get_bandwidth_thresholds(struct opus_test_bandwidth_thresholds *out) {
    int i;
    for (i = 0; i < 8; i++) {
        out->mono_voice[i]   = (int32_t)mono_voice_bandwidth_thresholds[i];
        out->mono_music[i]   = (int32_t)mono_music_bandwidth_thresholds[i];
        out->stereo_voice[i] = (int32_t)stereo_voice_bandwidth_thresholds[i];
        out->stereo_music[i] = (int32_t)stereo_music_bandwidth_thresholds[i];
    }
}

void opus_test_get_stereo_thresholds(int32_t *voice, int32_t *music) {
    *voice = (int32_t)stereo_voice_threshold;
    *music = (int32_t)stereo_music_threshold;
}

void opus_test_get_mode_thresholds(int32_t *out /* [2][2] flattened */) {
    int i, j;
    for (i = 0; i < 2; i++)
        for (j = 0; j < 2; j++)
            out[2*i + j] = (int32_t)mode_thresholds[i][j];
}

void opus_test_get_fec_thresholds(int32_t *out /* [10] */) {
    int i;
    int n = (int)(sizeof(fec_thresholds)/sizeof(fec_thresholds[0]));
    for (i = 0; i < 10; i++) {
        out[i] = (i < n) ? (int32_t)fec_thresholds[i] : 0;
    }
}

/* ─── Drift-bisection state-dump helpers ────────────────────────────
 * These helpers provide in-TU access to the full layout of the C
 * OpusEncoder + nested silk/CELT sub-encoders. They are driven by the
 * benchcmp package's debug_state_dump_cgo.go harness.
 *
 * The two structs below (opus_test_full_dump, which flattens all the
 * fields we currently care about) must be kept in 1:1 lockstep with
 * the Go-side mirror in
 * libraries/opus/internal/nativeopus/export_for_testing_state_dump.go
 * and the ordering in the benchcmp DumpEncoderStateC walker.
 * ─────────────────────────────────────────────────────────────────── */

#include "celt.h"
#include "silk/float/structs_FLP.h"
#include "opus_state_dump.h"
/* OpusCustomEncoder's full layout is defined inside celt_encoder.c
 * (file-local to the CELT amalgamation TU opus_cgo_celt.c). This TU
 * only sees the opaque forward declaration via celt.h. The CELT-side
 * dump helper (opus_test_dump_celt_encoder) therefore lives in the
 * CELT TU; this TU dumps OpusEncoder scalars + silk_encoder +
 * TonalityAnalysisState and delegates CELT fields to the other TU via
 * the forward declaration below. */

#ifndef OPUS_TEST_STATE_DUMP_DEFINED
#define OPUS_TEST_STATE_DUMP_DEFINED

/* Forward decl — implementation lives in opus_cgo_celt.c where the
 * full OpusCustomEncoder struct layout is visible. */
extern void opus_test_dump_celt_encoder(void *celt_enc,
                                        struct opus_test_encoder_full_dump *dst);

#if 0
/* Layout historically defined inline here — moved to opus_state_dump.h
 * so opus_cgo_celt.c can write CELT fields with full struct visibility. */
struct opus_test_encoder_full_dump_IGNORED_placeholder {
    /* --- OpusEncoder top-level --------------------------------- */
    int32_t celt_enc_offset;
    int32_t silk_enc_offset;
    int32_t application;
    int32_t channels;
    int32_t delay_compensation;
    int32_t force_channels;
    int32_t signal_type;
    int32_t user_bandwidth;
    int32_t max_bandwidth;
    int32_t user_forced_mode;
    int32_t voice_ratio;
    int32_t Fs;
    int32_t use_vbr;
    int32_t vbr_constraint;
    int32_t variable_duration;
    int32_t bitrate_bps;
    int32_t user_bitrate_bps;
    int32_t lsb_depth;
    int32_t encoder_buffer;
    int32_t lfe;
    int32_t arch;
    int32_t use_dtx;
    int32_t fec_config;
    int32_t stream_channels;
    int32_t hybrid_stereo_width_Q14;
    int32_t variable_HP_smth2_Q15;
    uint32_t prev_HB_gain_bits;
    uint32_t hp_mem_bits[4];
    int32_t mode;
    int32_t prev_mode;
    int32_t prev_channels;
    int32_t prev_framesize;
    int32_t bandwidth;
    int32_t auto_bandwidth;
    int32_t silk_bw_switch;
    int32_t first;
    uint32_t width_mem_XX_bits;
    uint32_t width_mem_XY_bits;
    uint32_t width_mem_YY_bits;
    uint32_t width_mem_smoothed_width_bits;
    uint32_t width_mem_max_follower_bits;
    int32_t detected_bandwidth;
    int32_t nb_no_activity_ms_Q1;
    uint32_t peak_signal_energy_bits;
    int32_t nonfinal_frame;
    uint32_t rangeFinal;

    /* --- TonalityAnalysisState (analysis.h) ---------------------- */
    int32_t an_arch;
    int32_t an_application;
    int32_t an_Fs;
    int32_t an_mem_fill;
    uint32_t an_prev_tonality_bits;
    int32_t an_prev_bandwidth;
    uint32_t an_Etracker_bits;
    uint32_t an_lowECount_bits;
    int32_t an_E_count;
    int32_t an_count;
    int32_t an_analysis_offset;
    int32_t an_write_pos;
    int32_t an_read_pos;
    int32_t an_read_subframe;
    uint32_t an_hp_ener_accum_bits;
    int32_t an_initialized;
    /* Reduced set of TonalityAnalysisState arrays — we dump checksums
     * of the large float arrays to keep the wire format small, plus
     * a few full arrays that are known-small and important. */
    uint32_t an_angle_fp_crc;    /* FNV-1a of [240]float */
    uint32_t an_d_angle_fp_crc;
    uint32_t an_d2_angle_fp_crc;
    uint32_t an_inmem_fp_crc;
    uint32_t an_E_fp_crc;
    uint32_t an_logE_fp_crc;
    uint32_t an_rnn_state_fp_crc;
    uint32_t an_info_fp_crc;

    /* --- CELT encoder (OpusCustomEncoder) ----------------------- */
    int32_t celt_channels;
    int32_t celt_stream_channels;
    int32_t celt_force_intra;
    int32_t celt_clip;
    int32_t celt_disable_pf;
    int32_t celt_complexity;
    int32_t celt_upsample;
    int32_t celt_start;
    int32_t celt_end;
    int32_t celt_bitrate;
    int32_t celt_vbr;
    int32_t celt_signalling;
    int32_t celt_constrained_vbr;
    int32_t celt_loss_rate;
    int32_t celt_lsb_depth;
    int32_t celt_lfe;
    int32_t celt_disable_inv;
    int32_t celt_arch;

    uint32_t celt_rng;
    int32_t celt_spread_decision;
    uint32_t celt_delayedIntra_bits;
    int32_t celt_tonal_average;
    int32_t celt_lastCodedBands;
    int32_t celt_hf_average;
    int32_t celt_tapset_decision;
    int32_t celt_prefilter_period;
    uint32_t celt_prefilter_gain_bits;
    int32_t celt_prefilter_tapset;
    int32_t celt_consec_transient;

    uint32_t celt_preemph_memE_bits[2];
    uint32_t celt_preemph_memD_bits[2];

    int32_t celt_vbr_reservoir;
    int32_t celt_vbr_drift;
    int32_t celt_vbr_offset;
    int32_t celt_vbr_count;
    uint32_t celt_overlap_max_bits;
    uint32_t celt_stereo_saving_bits;
    int32_t celt_intensity;
    uint32_t celt_spec_avg_bits;

    /* Arrays — length depends on channels/mode; we dump CRC + first16
     * values for diffing. nbEBands=21 for the built-in 48k/960 mode. */
    uint32_t celt_oldBandE_fp_crc;
    uint32_t celt_oldLogE_fp_crc;
    uint32_t celt_oldLogE2_fp_crc;
    uint32_t celt_energyError_fp_crc;
    uint32_t celt_in_mem_fp_crc;
    uint32_t celt_prefilter_mem_fp_crc;

    /* First-16 float values of each critical CELT array — useful for
     * pinpointing the first index that diverges. */
    uint32_t celt_oldBandE_f16[16];
    uint32_t celt_oldLogE_f16[16];
    uint32_t celt_oldLogE2_f16[16];
    uint32_t celt_energyError_f16[16];
    uint32_t celt_preemph_memE_f16[2];

    /* --- silk_encoder top-level (float/structs_FLP.h:91-103) ---- */
    int32_t silk_super_nBitsUsedLBRR;
    int32_t silk_super_nBitsExceeded;
    int32_t silk_super_nChannelsAPI;
    int32_t silk_super_nChannelsInternal;
    int32_t silk_super_nPrevChannelsInternal;
    int32_t silk_super_timeSinceSwitchAllowed_ms;
    int32_t silk_super_allowBandwidthSwitch;
    int32_t silk_super_prev_decode_only_middle;

    /* Per-channel silk_encoder_state_FLP — up to 2 */
    /* sCmn scalar subset */
    int32_t silk_ch0_fs_kHz;
    int32_t silk_ch0_nb_subfr;
    int32_t silk_ch0_frame_length;
    int32_t silk_ch0_subfr_length;
    int32_t silk_ch0_ltp_mem_length;
    int32_t silk_ch0_la_pitch;
    int32_t silk_ch0_la_shape;
    int32_t silk_ch0_prevLag;
    int32_t silk_ch0_prevSignalType;
    int32_t silk_ch0_inputBufIx;
    int32_t silk_ch0_nFramesEncoded;
    int32_t silk_ch0_nFramesPerPacket;
    int32_t silk_ch0_TargetRate_bps;
    int32_t silk_ch0_PacketSize_ms;
    int32_t silk_ch0_PacketLoss_perc;
    int32_t silk_ch0_frameCounter;
    int32_t silk_ch0_Complexity;
    int32_t silk_ch0_predictLPCOrder;
    int32_t silk_ch0_shapingLPCOrder;
    int32_t silk_ch0_warping_Q16;
    int32_t silk_ch0_useCBR;
    int32_t silk_ch0_prefillFlag;
    int32_t silk_ch0_speech_activity_Q8;
    int32_t silk_ch0_allow_bandwidth_switch;
    int32_t silk_ch0_LBRRprevLastGainIndex;
    int32_t silk_ch0_first_frame_after_reset;
    int32_t silk_ch0_controlled_since_last_payload;
    int32_t silk_ch0_nStatesDelayedDecision;
    int32_t silk_ch0_useInterpolatedNLSFs;
    int32_t silk_ch0_variable_HP_smth1_Q15;
    int32_t silk_ch0_variable_HP_smth2_Q15;
    int32_t silk_ch0_sum_log_gain_Q7;
    int32_t silk_ch0_NLSF_MSVQ_Survivors;
    int32_t silk_ch0_pitch_LPC_win_length;
    int32_t silk_ch0_max_pitch_lag;
    int32_t silk_ch0_pitchEstimationComplexity;
    int32_t silk_ch0_pitchEstimationLPCOrder;
    int32_t silk_ch0_pitchEstimationThreshold_Q16;
    int32_t silk_ch0_NSQ_lagPrev;
    int32_t silk_ch0_NSQ_sLTP_buf_idx;
    int32_t silk_ch0_NSQ_sLTP_shp_buf_idx;
    int32_t silk_ch0_NSQ_rand_seed;
    int32_t silk_ch0_NSQ_prev_gain_Q16;
    int32_t silk_ch0_NSQ_rewhite_flag;
    int32_t silk_ch0_NSQ_sLF_AR_shp_Q14;
    int32_t silk_ch0_NSQ_sDiff_shp_Q14;
    int32_t silk_ch0_sShape_LastGainIndex;
    uint32_t silk_ch0_sShape_HarmShapeGain_smth_bits;
    uint32_t silk_ch0_sShape_Tilt_smth_bits;
    uint32_t silk_ch0_LTPCorr_bits;
    /* CRCs for the large array fields: */
    uint32_t silk_ch0_inputBuf_crc;      /* opus_int16 */
    uint32_t silk_ch0_prev_NLSFq_Q15_crc;
    uint32_t silk_ch0_x_buf_fp_crc;
    uint32_t silk_ch0_In_HP_State_crc;

    int32_t silk_ch1_fs_kHz;
    int32_t silk_ch1_nb_subfr;
    int32_t silk_ch1_frame_length;
    int32_t silk_ch1_prevSignalType;
    int32_t silk_ch1_frameCounter;
    int32_t silk_ch1_NSQ_rand_seed;
    uint32_t silk_ch1_x_buf_fp_crc;
};
#endif /* inline layout placeholder — real def is in opus_state_dump.h */

/* Small FNV-1a helper for arbitrary byte buffers. */
static uint32_t opus_test_fnv1a(const void *p, size_t n) {
    const unsigned char *b = (const unsigned char*)p;
    uint32_t h = 2166136261u;
    size_t i;
    for (i = 0; i < n; i++) {
        h ^= b[i];
        h *= 16777619u;
    }
    return h;
}

static uint32_t opus_test_f32_bits(float v) {
    uint32_t out;
    memcpy(&out, &v, sizeof(out));
    return out;
}

/* Dump all tracked state of the encoder into dst. */
void opus_test_dump_encoder_full(OpusEncoder *st, struct opus_test_encoder_full_dump *dst) {
    int i;
    silk_encoder *silk_enc;
    CELTEncoder *celt_enc;
    memset(dst, 0, sizeof(*dst));
    silk_enc = (silk_encoder*)(void*)((char*)st + st->silk_enc_offset);
    celt_enc = (CELTEncoder*)(void*)((char*)st + st->celt_enc_offset);

    /* OpusEncoder scalar fields ----------------------------------- */
    dst->celt_enc_offset     = st->celt_enc_offset;
    dst->silk_enc_offset     = st->silk_enc_offset;
    dst->application         = st->application;
    dst->channels            = st->channels;
    dst->delay_compensation  = st->delay_compensation;
    dst->force_channels      = st->force_channels;
    dst->signal_type         = st->signal_type;
    dst->user_bandwidth      = st->user_bandwidth;
    dst->max_bandwidth       = st->max_bandwidth;
    dst->user_forced_mode    = st->user_forced_mode;
    dst->voice_ratio         = st->voice_ratio;
    dst->Fs                  = st->Fs;
    dst->use_vbr             = st->use_vbr;
    dst->vbr_constraint      = st->vbr_constraint;
    dst->variable_duration   = st->variable_duration;
    dst->bitrate_bps         = st->bitrate_bps;
    dst->user_bitrate_bps    = st->user_bitrate_bps;
    dst->lsb_depth           = st->lsb_depth;
    dst->encoder_buffer      = st->encoder_buffer;
    dst->lfe                 = st->lfe;
    dst->arch                = st->arch;
    dst->use_dtx             = st->use_dtx;
    dst->fec_config          = st->fec_config;
    dst->stream_channels     = st->stream_channels;
    dst->hybrid_stereo_width_Q14 = st->hybrid_stereo_width_Q14;
    dst->variable_HP_smth2_Q15   = st->variable_HP_smth2_Q15;
    dst->prev_HB_gain_bits   = opus_test_f32_bits((float)st->prev_HB_gain);
    for (i = 0; i < 4; i++) dst->hp_mem_bits[i] = opus_test_f32_bits((float)st->hp_mem[i]);
    dst->mode                = st->mode;
    dst->prev_mode           = st->prev_mode;
    dst->prev_channels       = st->prev_channels;
    dst->prev_framesize      = st->prev_framesize;
    dst->bandwidth           = st->bandwidth;
    dst->auto_bandwidth      = st->auto_bandwidth;
    dst->silk_bw_switch      = st->silk_bw_switch;
    dst->first               = st->first;
    dst->width_mem_XX_bits   = opus_test_f32_bits((float)st->width_mem.XX);
    dst->width_mem_XY_bits   = opus_test_f32_bits((float)st->width_mem.XY);
    dst->width_mem_YY_bits   = opus_test_f32_bits((float)st->width_mem.YY);
    dst->width_mem_smoothed_width_bits = opus_test_f32_bits((float)st->width_mem.smoothed_width);
    dst->width_mem_max_follower_bits   = opus_test_f32_bits((float)st->width_mem.max_follower);
#ifndef DISABLE_FLOAT_API
    dst->detected_bandwidth  = st->detected_bandwidth;
#endif
    dst->nb_no_activity_ms_Q1 = st->nb_no_activity_ms_Q1;
    dst->peak_signal_energy_bits = opus_test_f32_bits((float)st->peak_signal_energy);
    dst->nonfinal_frame      = st->nonfinal_frame;
    dst->rangeFinal          = st->rangeFinal;

    /* TonalityAnalysisState ---------------------------------------- */
#ifndef DISABLE_FLOAT_API
    dst->an_arch            = st->analysis.arch;
    dst->an_application     = st->analysis.application;
    dst->an_Fs              = st->analysis.Fs;
    dst->an_mem_fill        = st->analysis.mem_fill;
    dst->an_prev_tonality_bits = opus_test_f32_bits(st->analysis.prev_tonality);
    dst->an_prev_bandwidth  = st->analysis.prev_bandwidth;
    dst->an_Etracker_bits   = opus_test_f32_bits(st->analysis.Etracker);
    dst->an_lowECount_bits  = opus_test_f32_bits(st->analysis.lowECount);
    dst->an_E_count         = st->analysis.E_count;
    dst->an_count           = st->analysis.count;
    dst->an_analysis_offset = st->analysis.analysis_offset;
    dst->an_write_pos       = st->analysis.write_pos;
    dst->an_read_pos        = st->analysis.read_pos;
    dst->an_read_subframe   = st->analysis.read_subframe;
    dst->an_hp_ener_accum_bits = opus_test_f32_bits(st->analysis.hp_ener_accum);
    dst->an_initialized     = st->analysis.initialized;
    dst->an_angle_fp_crc    = opus_test_fnv1a(st->analysis.angle, sizeof(st->analysis.angle));
    dst->an_d_angle_fp_crc  = opus_test_fnv1a(st->analysis.d_angle, sizeof(st->analysis.d_angle));
    dst->an_d2_angle_fp_crc = opus_test_fnv1a(st->analysis.d2_angle, sizeof(st->analysis.d2_angle));
    dst->an_inmem_fp_crc    = opus_test_fnv1a(st->analysis.inmem, sizeof(st->analysis.inmem));
    dst->an_E_fp_crc        = opus_test_fnv1a(st->analysis.E, sizeof(st->analysis.E));
    dst->an_logE_fp_crc     = opus_test_fnv1a(st->analysis.logE, sizeof(st->analysis.logE));
    dst->an_rnn_state_fp_crc = opus_test_fnv1a(st->analysis.rnn_state, sizeof(st->analysis.rnn_state));
    dst->an_info_fp_crc     = opus_test_fnv1a(st->analysis.info, sizeof(st->analysis.info));
#endif

    /* CELT encoder (OpusCustomEncoder) ----------------------------- */
    if (celt_enc) {
        /* Delegate to the CELT TU which can see the full layout. */
        opus_test_dump_celt_encoder((void*)celt_enc, dst);
    }

    /* silk_encoder ------------------------------------------------- */
    if (silk_enc) {
        dst->silk_super_nBitsUsedLBRR             = silk_enc->nBitsUsedLBRR;
        dst->silk_super_nBitsExceeded             = silk_enc->nBitsExceeded;
        dst->silk_super_nChannelsAPI              = silk_enc->nChannelsAPI;
        dst->silk_super_nChannelsInternal         = silk_enc->nChannelsInternal;
        dst->silk_super_nPrevChannelsInternal     = silk_enc->nPrevChannelsInternal;
        dst->silk_super_timeSinceSwitchAllowed_ms = silk_enc->timeSinceSwitchAllowed_ms;
        dst->silk_super_allowBandwidthSwitch      = silk_enc->allowBandwidthSwitch;
        dst->silk_super_prev_decode_only_middle   = silk_enc->prev_decode_only_middle;

        {
            silk_encoder_state_FLP *ch0 = &silk_enc->state_Fxx[0];
            dst->silk_ch0_fs_kHz             = ch0->sCmn.fs_kHz;
            dst->silk_ch0_nb_subfr           = ch0->sCmn.nb_subfr;
            dst->silk_ch0_frame_length       = ch0->sCmn.frame_length;
            dst->silk_ch0_subfr_length       = ch0->sCmn.subfr_length;
            dst->silk_ch0_ltp_mem_length     = ch0->sCmn.ltp_mem_length;
            dst->silk_ch0_la_pitch           = ch0->sCmn.la_pitch;
            dst->silk_ch0_la_shape           = ch0->sCmn.la_shape;
            dst->silk_ch0_prevLag            = ch0->sCmn.prevLag;
            dst->silk_ch0_prevSignalType     = ch0->sCmn.prevSignalType;
            dst->silk_ch0_inputBufIx         = ch0->sCmn.inputBufIx;
            dst->silk_ch0_nFramesEncoded     = ch0->sCmn.nFramesEncoded;
            dst->silk_ch0_nFramesPerPacket   = ch0->sCmn.nFramesPerPacket;
            dst->silk_ch0_TargetRate_bps     = ch0->sCmn.TargetRate_bps;
            dst->silk_ch0_PacketSize_ms      = ch0->sCmn.PacketSize_ms;
            dst->silk_ch0_PacketLoss_perc    = ch0->sCmn.PacketLoss_perc;
            dst->silk_ch0_frameCounter       = ch0->sCmn.frameCounter;
            dst->silk_ch0_Complexity         = ch0->sCmn.Complexity;
            dst->silk_ch0_predictLPCOrder    = ch0->sCmn.predictLPCOrder;
            dst->silk_ch0_shapingLPCOrder    = ch0->sCmn.shapingLPCOrder;
            dst->silk_ch0_warping_Q16        = ch0->sCmn.warping_Q16;
            dst->silk_ch0_useCBR             = ch0->sCmn.useCBR;
            dst->silk_ch0_prefillFlag        = ch0->sCmn.prefillFlag;
            dst->silk_ch0_speech_activity_Q8 = ch0->sCmn.speech_activity_Q8;
            dst->silk_ch0_allow_bandwidth_switch = ch0->sCmn.allow_bandwidth_switch;
            dst->silk_ch0_LBRRprevLastGainIndex  = ch0->sCmn.LBRRprevLastGainIndex;
            dst->silk_ch0_first_frame_after_reset = ch0->sCmn.first_frame_after_reset;
            dst->silk_ch0_controlled_since_last_payload = ch0->sCmn.controlled_since_last_payload;
            dst->silk_ch0_nStatesDelayedDecision = ch0->sCmn.nStatesDelayedDecision;
            dst->silk_ch0_useInterpolatedNLSFs   = ch0->sCmn.useInterpolatedNLSFs;
            dst->silk_ch0_variable_HP_smth1_Q15  = ch0->sCmn.variable_HP_smth1_Q15;
            dst->silk_ch0_variable_HP_smth2_Q15  = ch0->sCmn.variable_HP_smth2_Q15;
            dst->silk_ch0_sum_log_gain_Q7        = ch0->sCmn.sum_log_gain_Q7;
            dst->silk_ch0_NLSF_MSVQ_Survivors    = ch0->sCmn.NLSF_MSVQ_Survivors;
            dst->silk_ch0_pitch_LPC_win_length   = ch0->sCmn.pitch_LPC_win_length;
            dst->silk_ch0_max_pitch_lag          = ch0->sCmn.max_pitch_lag;
            dst->silk_ch0_pitchEstimationComplexity = ch0->sCmn.pitchEstimationComplexity;
            dst->silk_ch0_pitchEstimationLPCOrder   = ch0->sCmn.pitchEstimationLPCOrder;
            dst->silk_ch0_pitchEstimationThreshold_Q16 = ch0->sCmn.pitchEstimationThreshold_Q16;

            dst->silk_ch0_NSQ_lagPrev         = ch0->sCmn.sNSQ.lagPrev;
            dst->silk_ch0_NSQ_sLTP_buf_idx    = ch0->sCmn.sNSQ.sLTP_buf_idx;
            dst->silk_ch0_NSQ_sLTP_shp_buf_idx = ch0->sCmn.sNSQ.sLTP_shp_buf_idx;
            dst->silk_ch0_NSQ_rand_seed       = ch0->sCmn.sNSQ.rand_seed;
            dst->silk_ch0_NSQ_prev_gain_Q16   = ch0->sCmn.sNSQ.prev_gain_Q16;
            dst->silk_ch0_NSQ_rewhite_flag    = ch0->sCmn.sNSQ.rewhite_flag;
            dst->silk_ch0_NSQ_sLF_AR_shp_Q14  = ch0->sCmn.sNSQ.sLF_AR_shp_Q14;
            dst->silk_ch0_NSQ_sDiff_shp_Q14   = ch0->sCmn.sNSQ.sDiff_shp_Q14;

            dst->silk_ch0_sShape_LastGainIndex         = ch0->sShape.LastGainIndex;
            dst->silk_ch0_sShape_HarmShapeGain_smth_bits = opus_test_f32_bits((float)ch0->sShape.HarmShapeGain_smth);
            dst->silk_ch0_sShape_Tilt_smth_bits        = opus_test_f32_bits((float)ch0->sShape.Tilt_smth);
            dst->silk_ch0_LTPCorr_bits                 = opus_test_f32_bits((float)ch0->LTPCorr);

            dst->silk_ch0_inputBuf_crc        = opus_test_fnv1a(ch0->sCmn.inputBuf, sizeof(ch0->sCmn.inputBuf));
            dst->silk_ch0_prev_NLSFq_Q15_crc  = opus_test_fnv1a(ch0->sCmn.prev_NLSFq_Q15, sizeof(ch0->sCmn.prev_NLSFq_Q15));
            dst->silk_ch0_x_buf_fp_crc        = opus_test_fnv1a(ch0->x_buf, sizeof(ch0->x_buf));
            dst->silk_ch0_In_HP_State_crc     = opus_test_fnv1a(ch0->sCmn.In_HP_State, sizeof(ch0->sCmn.In_HP_State));
        }

        if (silk_enc->nChannelsInternal >= 2) {
            silk_encoder_state_FLP *ch1 = &silk_enc->state_Fxx[1];
            dst->silk_ch1_fs_kHz            = ch1->sCmn.fs_kHz;
            dst->silk_ch1_nb_subfr          = ch1->sCmn.nb_subfr;
            dst->silk_ch1_frame_length      = ch1->sCmn.frame_length;
            dst->silk_ch1_prevSignalType    = ch1->sCmn.prevSignalType;
            dst->silk_ch1_frameCounter      = ch1->sCmn.frameCounter;
            dst->silk_ch1_NSQ_rand_seed     = ch1->sCmn.sNSQ.rand_seed;
            dst->silk_ch1_x_buf_fp_crc      = opus_test_fnv1a(ch1->x_buf, sizeof(ch1->x_buf));
        }
    }
}

/* Encode one frame via C, then fill dst with the post-frame state.
 * Returns bytes written to pkt (or negative error). */
int opus_test_encode_and_dump(OpusEncoder *st, const float *pcm, int frame_size,
                              unsigned char *pkt, int32_t max_pkt_bytes,
                              struct opus_test_encoder_full_dump *dst) {
    int n = opus_encode_float(st, pcm, frame_size, pkt, max_pkt_bytes);
    if (n >= 0 && dst != NULL) {
        opus_test_dump_encoder_full(st, dst);
    }
    return n;
}

#endif /* OPUS_TEST_STATE_DUMP_DEFINED */

