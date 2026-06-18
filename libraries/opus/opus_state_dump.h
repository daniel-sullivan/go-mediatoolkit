/* Shared struct layout for the Opus drift-bisection state dump.
 * Included by both opus_cgo_src.c (which writes OpusEncoder scalars,
 * TonalityAnalysisState, and silk_encoder fields) and opus_cgo_celt.c
 * (which writes the CELT fields). The benchcmp package's
 * debug_state_dump_cgo.go mirrors the layout for Go-side access.
 *
 * This is non-vendored glue; the vendored libopus sources are
 * untouched.
 */

#ifndef OPUS_STATE_DUMP_H
#define OPUS_STATE_DUMP_H

#include <stdint.h>

struct opus_test_encoder_full_dump {
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

    /* --- TonalityAnalysisState (analysis.h) -------------------- */
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
    uint32_t an_angle_fp_crc;
    uint32_t an_d_angle_fp_crc;
    uint32_t an_d2_angle_fp_crc;
    uint32_t an_inmem_fp_crc;
    uint32_t an_E_fp_crc;
    uint32_t an_logE_fp_crc;
    uint32_t an_rnn_state_fp_crc;
    uint32_t an_info_fp_crc;

    /* --- CELT encoder (OpusCustomEncoder) --------------------- */
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

    uint32_t celt_oldBandE_fp_crc;
    uint32_t celt_oldLogE_fp_crc;
    uint32_t celt_oldLogE2_fp_crc;
    uint32_t celt_energyError_fp_crc;
    uint32_t celt_in_mem_fp_crc;
    uint32_t celt_prefilter_mem_fp_crc;

    uint32_t celt_oldBandE_f16[16];
    uint32_t celt_oldLogE_f16[16];
    uint32_t celt_oldLogE2_f16[16];
    uint32_t celt_energyError_f16[16];
    uint32_t celt_preemph_memE_f16[2];

    /* --- silk_encoder top-level ------------------------------- */
    int32_t silk_super_nBitsUsedLBRR;
    int32_t silk_super_nBitsExceeded;
    int32_t silk_super_nChannelsAPI;
    int32_t silk_super_nChannelsInternal;
    int32_t silk_super_nPrevChannelsInternal;
    int32_t silk_super_timeSinceSwitchAllowed_ms;
    int32_t silk_super_allowBandwidthSwitch;
    int32_t silk_super_prev_decode_only_middle;

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
    uint32_t silk_ch0_inputBuf_crc;
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

#endif /* OPUS_STATE_DUMP_H */
