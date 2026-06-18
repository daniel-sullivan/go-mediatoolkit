//go:build cgo

package benchcmp

// debug_state_dump_cgo.go — cgo glue for the drift-bisection diagnostic
// harness. This file wires Go-side consumers to the C-side
// opus_test_dump_encoder_full helper defined in the parent package's
// opus_cgo_src.c + opus_cgo_celt.c amalgamation TUs. The shared struct
// layout lives in libraries/opus/opus_state_dump.h.
//
// Intent: given a *CEncoder, extract every tracked field of the C
// OpusEncoder (plus nested silk_encoder, OpusCustomEncoder, and
// TonalityAnalysisState) into a Go-side CEncoderStateDump value.
//
// The benchmark here is NOT 100% exhaustive field-for-field coverage —
// large float arrays are summarised as FNV-1a CRCs with the first 16
// values broken out for surgical diffing. The categories cover:
//
//   - OpusEncoder proper (top-level scalars, hp_mem, width_mem,
//     peak_signal_energy, rangeFinal, nb_no_activity_ms_Q1, etc.).
//   - TonalityAnalysisState (scalars + CRCs of angle/d_angle/d2_angle/
//     inmem/E/logE/rnn_state/info).
//   - CELT OpusCustomEncoder (every scalar including rng,
//     spread_decision, delayedIntra, tonal_average, lastCodedBands,
//     hf_average, tapset_decision, prefilter_*, consec_transient,
//     preemph_memE/D, vbr_*, overlap_max, stereo_saving, intensity,
//     spec_avg — plus CRCs + first-16 values of oldBandE/oldLogE/
//     oldLogE2/energyError/in_mem/prefilter_mem).
//   - silk_encoder (sStereo-scalars + per-channel sCmn subset +
//     sShape + large-array CRCs).

/*
#cgo CFLAGS: -I${SRCDIR}/../../..
#cgo LDFLAGS: -lm
#include <stdint.h>
#include "opus_state_dump.h"

extern void opus_test_dump_encoder_full(void *enc, struct opus_test_encoder_full_dump *dst);
extern int  opus_test_encode_and_dump(void *enc, const float *pcm, int frame_size,
                                      unsigned char *pkt, int32_t max_pkt_bytes,
                                      struct opus_test_encoder_full_dump *dst);
*/
import "C"
import "unsafe"

// CEncoderStateDump mirrors the C-side opus_test_encoder_full_dump.
// Each field is either a raw integer or a uint32 holding a float's IEEE
// bit pattern (fields suffixed `_bits`). Large array fields are
// summarised via FNV-1a CRCs (`_fp_crc`, `_crc`) and the first 16
// scalar elements are broken out in `_f16` arrays for diffing.
type CEncoderStateDump struct {
	// OpusEncoder top-level
	CeltEncOffset           int32
	SilkEncOffset           int32
	Application             int32
	Channels                int32
	DelayCompensation       int32
	ForceChannels           int32
	SignalType              int32
	UserBandwidth           int32
	MaxBandwidth            int32
	UserForcedMode          int32
	VoiceRatio              int32
	Fs                      int32
	UseVbr                  int32
	VbrConstraint           int32
	VariableDuration        int32
	BitrateBps              int32
	UserBitrateBps          int32
	LsbDepth                int32
	EncoderBuffer           int32
	Lfe                     int32
	Arch                    int32
	UseDtx                  int32
	FecConfig               int32
	StreamChannels          int32
	HybridStereoWidthQ14    int32
	VariableHPSmth2Q15      int32
	PrevHBGainBits          uint32
	HPMemBits               [4]uint32
	Mode                    int32
	PrevMode                int32
	PrevChannels            int32
	PrevFramesize           int32
	Bandwidth               int32
	AutoBandwidth           int32
	SilkBwSwitch            int32
	First                   int32
	WidthMemXXBits          uint32
	WidthMemXYBits          uint32
	WidthMemYYBits          uint32
	WidthMemSmoothedBits    uint32
	WidthMemMaxFollowerBits uint32
	DetectedBandwidth       int32
	NbNoActivityMsQ1        int32
	PeakSignalEnergyBits    uint32
	NonfinalFrame           int32
	RangeFinal              uint32

	// TonalityAnalysisState
	AnArch             int32
	AnApplication      int32
	AnFs               int32
	AnMemFill          int32
	AnPrevTonalityBits uint32
	AnPrevBandwidth    int32
	AnEtrackerBits     uint32
	AnLowECountBits    uint32
	AnECount           int32
	AnCount            int32
	AnAnalysisOffset   int32
	AnWritePos         int32
	AnReadPos          int32
	AnReadSubframe     int32
	AnHpEnerAccumBits  uint32
	AnInitialized      int32
	AnAngleFpCrc       uint32
	AnDAngleFpCrc      uint32
	AnD2AngleFpCrc     uint32
	AnInmemFpCrc       uint32
	AnEFpCrc           uint32
	AnLogEFpCrc        uint32
	AnRnnStateFpCrc    uint32
	AnInfoFpCrc        uint32

	// CELT encoder
	CeltChannels       int32
	CeltStreamChannels int32
	CeltForceIntra     int32
	CeltClip           int32
	CeltDisablePf      int32
	CeltComplexity     int32
	CeltUpsample       int32
	CeltStart          int32
	CeltEnd            int32
	CeltBitrate        int32
	CeltVbr            int32
	CeltSignalling     int32
	CeltConstrainedVbr int32
	CeltLossRate       int32
	CeltLsbDepth       int32
	CeltLfe            int32
	CeltDisableInv     int32
	CeltArch           int32

	CeltRng               uint32
	CeltSpreadDecision    int32
	CeltDelayedIntraBits  uint32
	CeltTonalAverage      int32
	CeltLastCodedBands    int32
	CeltHfAverage         int32
	CeltTapsetDecision    int32
	CeltPrefilterPeriod   int32
	CeltPrefilterGainBits uint32
	CeltPrefilterTapset   int32
	CeltConsecTransient   int32

	CeltPreemphMemEBits [2]uint32
	CeltPreemphMemDBits [2]uint32

	CeltVbrReservoir     int32
	CeltVbrDrift         int32
	CeltVbrOffset        int32
	CeltVbrCount         int32
	CeltOverlapMaxBits   uint32
	CeltStereoSavingBits uint32
	CeltIntensity        int32
	CeltSpecAvgBits      uint32

	CeltOldBandEFpCrc     uint32
	CeltOldLogEFpCrc      uint32
	CeltOldLogE2FpCrc     uint32
	CeltEnergyErrorFpCrc  uint32
	CeltInMemFpCrc        uint32
	CeltPrefilterMemFpCrc uint32

	CeltOldBandEF16    [16]uint32
	CeltOldLogEF16     [16]uint32
	CeltOldLogE2F16    [16]uint32
	CeltEnergyErrorF16 [16]uint32
	CeltPreemphMemEF16 [2]uint32

	// silk_encoder top-level
	SilkSuperNBitsUsedLBRR            int32
	SilkSuperNBitsExceeded            int32
	SilkSuperNChannelsAPI             int32
	SilkSuperNChannelsInternal        int32
	SilkSuperNPrevChannelsInternal    int32
	SilkSuperTimeSinceSwitchAllowedMs int32
	SilkSuperAllowBandwidthSwitch     int32
	SilkSuperPrevDecodeOnlyMiddle     int32

	// silk channel 0 scalars
	SilkCh0FsKHz                       int32
	SilkCh0NbSubfr                     int32
	SilkCh0FrameLength                 int32
	SilkCh0SubfrLength                 int32
	SilkCh0LtpMemLength                int32
	SilkCh0LaPitch                     int32
	SilkCh0LaShape                     int32
	SilkCh0PrevLag                     int32
	SilkCh0PrevSignalType              int32
	SilkCh0InputBufIx                  int32
	SilkCh0NFramesEncoded              int32
	SilkCh0NFramesPerPacket            int32
	SilkCh0TargetRateBps               int32
	SilkCh0PacketSizeMs                int32
	SilkCh0PacketLossPerc              int32
	SilkCh0FrameCounter                int32
	SilkCh0Complexity                  int32
	SilkCh0PredictLPCOrder             int32
	SilkCh0ShapingLPCOrder             int32
	SilkCh0WarpingQ16                  int32
	SilkCh0UseCBR                      int32
	SilkCh0PrefillFlag                 int32
	SilkCh0SpeechActivityQ8            int32
	SilkCh0AllowBandwidthSwitch        int32
	SilkCh0LBRRprevLastGainIndex       int32
	SilkCh0FirstFrameAfterReset        int32
	SilkCh0ControlledSinceLastPayload  int32
	SilkCh0NStatesDelayedDecision      int32
	SilkCh0UseInterpolatedNLSFs        int32
	SilkCh0VariableHPSmth1Q15          int32
	SilkCh0VariableHPSmth2Q15          int32
	SilkCh0SumLogGainQ7                int32
	SilkCh0NLSFMSVQSurvivors           int32
	SilkCh0PitchLPCWinLength           int32
	SilkCh0MaxPitchLag                 int32
	SilkCh0PitchEstComplexity          int32
	SilkCh0PitchEstLPCOrder            int32
	SilkCh0PitchEstThresholdQ16        int32
	SilkCh0NSQLagPrev                  int32
	SilkCh0NSQSLTPBufIdx               int32
	SilkCh0NSQSLTPShpBufIdx            int32
	SilkCh0NSQRandSeed                 int32
	SilkCh0NSQPrevGainQ16              int32
	SilkCh0NSQRewhiteFlag              int32
	SilkCh0NSQSLFARShpQ14              int32
	SilkCh0NSQSDiffShpQ14              int32
	SilkCh0SShapeLastGainIndex         int32
	SilkCh0SShapeHarmShapeGainSmthBits uint32
	SilkCh0SShapeTiltSmthBits          uint32
	SilkCh0LTPCorrBits                 uint32
	SilkCh0InputBufCrc                 uint32
	SilkCh0PrevNLSFqQ15Crc             uint32
	SilkCh0XBufFpCrc                   uint32
	SilkCh0InHPStateCrc                uint32

	// silk channel 1 (stereo only)
	SilkCh1FsKHz          int32
	SilkCh1NbSubfr        int32
	SilkCh1FrameLength    int32
	SilkCh1PrevSignalType int32
	SilkCh1FrameCounter   int32
	SilkCh1NSQRandSeed    int32
	SilkCh1XBufFpCrc      uint32
}

// DumpCEncoderState extracts the full tracked state of the C encoder
// into a CEncoderStateDump. Safe to call any time — the encoder is not
// mutated.
func DumpCEncoderState(enc *CEncoder) CEncoderStateDump {
	var dst C.struct_opus_test_encoder_full_dump
	C.opus_test_dump_encoder_full(unsafe.Pointer(enc.p), &dst)
	return cDumpToGo(&dst)
}

// CEncodeAndDump encodes one frame and returns (bytes, post-frame dump).
// Matches EncodeFrame's signature apart from the extra dump return.
func CEncodeAndDump(enc *CEncoder, pcm []float32, frameSize int, pkt []byte) (int, CEncoderStateDump) {
	var dst C.struct_opus_test_encoder_full_dump
	n := C.opus_test_encode_and_dump(
		unsafe.Pointer(enc.p),
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.int32_t(len(pkt)),
		&dst)
	return int(n), cDumpToGo(&dst)
}

func cDumpToGo(src *C.struct_opus_test_encoder_full_dump) CEncoderStateDump {
	var d CEncoderStateDump
	d.CeltEncOffset = int32(src.celt_enc_offset)
	d.SilkEncOffset = int32(src.silk_enc_offset)
	d.Application = int32(src.application)
	d.Channels = int32(src.channels)
	d.DelayCompensation = int32(src.delay_compensation)
	d.ForceChannels = int32(src.force_channels)
	d.SignalType = int32(src.signal_type)
	d.UserBandwidth = int32(src.user_bandwidth)
	d.MaxBandwidth = int32(src.max_bandwidth)
	d.UserForcedMode = int32(src.user_forced_mode)
	d.VoiceRatio = int32(src.voice_ratio)
	d.Fs = int32(src.Fs)
	d.UseVbr = int32(src.use_vbr)
	d.VbrConstraint = int32(src.vbr_constraint)
	d.VariableDuration = int32(src.variable_duration)
	d.BitrateBps = int32(src.bitrate_bps)
	d.UserBitrateBps = int32(src.user_bitrate_bps)
	d.LsbDepth = int32(src.lsb_depth)
	d.EncoderBuffer = int32(src.encoder_buffer)
	d.Lfe = int32(src.lfe)
	d.Arch = int32(src.arch)
	d.UseDtx = int32(src.use_dtx)
	d.FecConfig = int32(src.fec_config)
	d.StreamChannels = int32(src.stream_channels)
	d.HybridStereoWidthQ14 = int32(src.hybrid_stereo_width_Q14)
	d.VariableHPSmth2Q15 = int32(src.variable_HP_smth2_Q15)
	d.PrevHBGainBits = uint32(src.prev_HB_gain_bits)
	for i := 0; i < 4; i++ {
		d.HPMemBits[i] = uint32(src.hp_mem_bits[i])
	}
	d.Mode = int32(src.mode)
	d.PrevMode = int32(src.prev_mode)
	d.PrevChannels = int32(src.prev_channels)
	d.PrevFramesize = int32(src.prev_framesize)
	d.Bandwidth = int32(src.bandwidth)
	d.AutoBandwidth = int32(src.auto_bandwidth)
	d.SilkBwSwitch = int32(src.silk_bw_switch)
	d.First = int32(src.first)
	d.WidthMemXXBits = uint32(src.width_mem_XX_bits)
	d.WidthMemXYBits = uint32(src.width_mem_XY_bits)
	d.WidthMemYYBits = uint32(src.width_mem_YY_bits)
	d.WidthMemSmoothedBits = uint32(src.width_mem_smoothed_width_bits)
	d.WidthMemMaxFollowerBits = uint32(src.width_mem_max_follower_bits)
	d.DetectedBandwidth = int32(src.detected_bandwidth)
	d.NbNoActivityMsQ1 = int32(src.nb_no_activity_ms_Q1)
	d.PeakSignalEnergyBits = uint32(src.peak_signal_energy_bits)
	d.NonfinalFrame = int32(src.nonfinal_frame)
	d.RangeFinal = uint32(src.rangeFinal)

	// Analysis
	d.AnArch = int32(src.an_arch)
	d.AnApplication = int32(src.an_application)
	d.AnFs = int32(src.an_Fs)
	d.AnMemFill = int32(src.an_mem_fill)
	d.AnPrevTonalityBits = uint32(src.an_prev_tonality_bits)
	d.AnPrevBandwidth = int32(src.an_prev_bandwidth)
	d.AnEtrackerBits = uint32(src.an_Etracker_bits)
	d.AnLowECountBits = uint32(src.an_lowECount_bits)
	d.AnECount = int32(src.an_E_count)
	d.AnCount = int32(src.an_count)
	d.AnAnalysisOffset = int32(src.an_analysis_offset)
	d.AnWritePos = int32(src.an_write_pos)
	d.AnReadPos = int32(src.an_read_pos)
	d.AnReadSubframe = int32(src.an_read_subframe)
	d.AnHpEnerAccumBits = uint32(src.an_hp_ener_accum_bits)
	d.AnInitialized = int32(src.an_initialized)
	d.AnAngleFpCrc = uint32(src.an_angle_fp_crc)
	d.AnDAngleFpCrc = uint32(src.an_d_angle_fp_crc)
	d.AnD2AngleFpCrc = uint32(src.an_d2_angle_fp_crc)
	d.AnInmemFpCrc = uint32(src.an_inmem_fp_crc)
	d.AnEFpCrc = uint32(src.an_E_fp_crc)
	d.AnLogEFpCrc = uint32(src.an_logE_fp_crc)
	d.AnRnnStateFpCrc = uint32(src.an_rnn_state_fp_crc)
	d.AnInfoFpCrc = uint32(src.an_info_fp_crc)

	// CELT
	d.CeltChannels = int32(src.celt_channels)
	d.CeltStreamChannels = int32(src.celt_stream_channels)
	d.CeltForceIntra = int32(src.celt_force_intra)
	d.CeltClip = int32(src.celt_clip)
	d.CeltDisablePf = int32(src.celt_disable_pf)
	d.CeltComplexity = int32(src.celt_complexity)
	d.CeltUpsample = int32(src.celt_upsample)
	d.CeltStart = int32(src.celt_start)
	d.CeltEnd = int32(src.celt_end)
	d.CeltBitrate = int32(src.celt_bitrate)
	d.CeltVbr = int32(src.celt_vbr)
	d.CeltSignalling = int32(src.celt_signalling)
	d.CeltConstrainedVbr = int32(src.celt_constrained_vbr)
	d.CeltLossRate = int32(src.celt_loss_rate)
	d.CeltLsbDepth = int32(src.celt_lsb_depth)
	d.CeltLfe = int32(src.celt_lfe)
	d.CeltDisableInv = int32(src.celt_disable_inv)
	d.CeltArch = int32(src.celt_arch)
	d.CeltRng = uint32(src.celt_rng)
	d.CeltSpreadDecision = int32(src.celt_spread_decision)
	d.CeltDelayedIntraBits = uint32(src.celt_delayedIntra_bits)
	d.CeltTonalAverage = int32(src.celt_tonal_average)
	d.CeltLastCodedBands = int32(src.celt_lastCodedBands)
	d.CeltHfAverage = int32(src.celt_hf_average)
	d.CeltTapsetDecision = int32(src.celt_tapset_decision)
	d.CeltPrefilterPeriod = int32(src.celt_prefilter_period)
	d.CeltPrefilterGainBits = uint32(src.celt_prefilter_gain_bits)
	d.CeltPrefilterTapset = int32(src.celt_prefilter_tapset)
	d.CeltConsecTransient = int32(src.celt_consec_transient)
	for i := 0; i < 2; i++ {
		d.CeltPreemphMemEBits[i] = uint32(src.celt_preemph_memE_bits[i])
		d.CeltPreemphMemDBits[i] = uint32(src.celt_preemph_memD_bits[i])
		d.CeltPreemphMemEF16[i] = uint32(src.celt_preemph_memE_f16[i])
	}
	d.CeltVbrReservoir = int32(src.celt_vbr_reservoir)
	d.CeltVbrDrift = int32(src.celt_vbr_drift)
	d.CeltVbrOffset = int32(src.celt_vbr_offset)
	d.CeltVbrCount = int32(src.celt_vbr_count)
	d.CeltOverlapMaxBits = uint32(src.celt_overlap_max_bits)
	d.CeltStereoSavingBits = uint32(src.celt_stereo_saving_bits)
	d.CeltIntensity = int32(src.celt_intensity)
	d.CeltSpecAvgBits = uint32(src.celt_spec_avg_bits)
	d.CeltOldBandEFpCrc = uint32(src.celt_oldBandE_fp_crc)
	d.CeltOldLogEFpCrc = uint32(src.celt_oldLogE_fp_crc)
	d.CeltOldLogE2FpCrc = uint32(src.celt_oldLogE2_fp_crc)
	d.CeltEnergyErrorFpCrc = uint32(src.celt_energyError_fp_crc)
	d.CeltInMemFpCrc = uint32(src.celt_in_mem_fp_crc)
	d.CeltPrefilterMemFpCrc = uint32(src.celt_prefilter_mem_fp_crc)
	for i := 0; i < 16; i++ {
		d.CeltOldBandEF16[i] = uint32(src.celt_oldBandE_f16[i])
		d.CeltOldLogEF16[i] = uint32(src.celt_oldLogE_f16[i])
		d.CeltOldLogE2F16[i] = uint32(src.celt_oldLogE2_f16[i])
		d.CeltEnergyErrorF16[i] = uint32(src.celt_energyError_f16[i])
	}

	// silk super
	d.SilkSuperNBitsUsedLBRR = int32(src.silk_super_nBitsUsedLBRR)
	d.SilkSuperNBitsExceeded = int32(src.silk_super_nBitsExceeded)
	d.SilkSuperNChannelsAPI = int32(src.silk_super_nChannelsAPI)
	d.SilkSuperNChannelsInternal = int32(src.silk_super_nChannelsInternal)
	d.SilkSuperNPrevChannelsInternal = int32(src.silk_super_nPrevChannelsInternal)
	d.SilkSuperTimeSinceSwitchAllowedMs = int32(src.silk_super_timeSinceSwitchAllowed_ms)
	d.SilkSuperAllowBandwidthSwitch = int32(src.silk_super_allowBandwidthSwitch)
	d.SilkSuperPrevDecodeOnlyMiddle = int32(src.silk_super_prev_decode_only_middle)

	// silk ch0
	d.SilkCh0FsKHz = int32(src.silk_ch0_fs_kHz)
	d.SilkCh0NbSubfr = int32(src.silk_ch0_nb_subfr)
	d.SilkCh0FrameLength = int32(src.silk_ch0_frame_length)
	d.SilkCh0SubfrLength = int32(src.silk_ch0_subfr_length)
	d.SilkCh0LtpMemLength = int32(src.silk_ch0_ltp_mem_length)
	d.SilkCh0LaPitch = int32(src.silk_ch0_la_pitch)
	d.SilkCh0LaShape = int32(src.silk_ch0_la_shape)
	d.SilkCh0PrevLag = int32(src.silk_ch0_prevLag)
	d.SilkCh0PrevSignalType = int32(src.silk_ch0_prevSignalType)
	d.SilkCh0InputBufIx = int32(src.silk_ch0_inputBufIx)
	d.SilkCh0NFramesEncoded = int32(src.silk_ch0_nFramesEncoded)
	d.SilkCh0NFramesPerPacket = int32(src.silk_ch0_nFramesPerPacket)
	d.SilkCh0TargetRateBps = int32(src.silk_ch0_TargetRate_bps)
	d.SilkCh0PacketSizeMs = int32(src.silk_ch0_PacketSize_ms)
	d.SilkCh0PacketLossPerc = int32(src.silk_ch0_PacketLoss_perc)
	d.SilkCh0FrameCounter = int32(src.silk_ch0_frameCounter)
	d.SilkCh0Complexity = int32(src.silk_ch0_Complexity)
	d.SilkCh0PredictLPCOrder = int32(src.silk_ch0_predictLPCOrder)
	d.SilkCh0ShapingLPCOrder = int32(src.silk_ch0_shapingLPCOrder)
	d.SilkCh0WarpingQ16 = int32(src.silk_ch0_warping_Q16)
	d.SilkCh0UseCBR = int32(src.silk_ch0_useCBR)
	d.SilkCh0PrefillFlag = int32(src.silk_ch0_prefillFlag)
	d.SilkCh0SpeechActivityQ8 = int32(src.silk_ch0_speech_activity_Q8)
	d.SilkCh0AllowBandwidthSwitch = int32(src.silk_ch0_allow_bandwidth_switch)
	d.SilkCh0LBRRprevLastGainIndex = int32(src.silk_ch0_LBRRprevLastGainIndex)
	d.SilkCh0FirstFrameAfterReset = int32(src.silk_ch0_first_frame_after_reset)
	d.SilkCh0ControlledSinceLastPayload = int32(src.silk_ch0_controlled_since_last_payload)
	d.SilkCh0NStatesDelayedDecision = int32(src.silk_ch0_nStatesDelayedDecision)
	d.SilkCh0UseInterpolatedNLSFs = int32(src.silk_ch0_useInterpolatedNLSFs)
	d.SilkCh0VariableHPSmth1Q15 = int32(src.silk_ch0_variable_HP_smth1_Q15)
	d.SilkCh0VariableHPSmth2Q15 = int32(src.silk_ch0_variable_HP_smth2_Q15)
	d.SilkCh0SumLogGainQ7 = int32(src.silk_ch0_sum_log_gain_Q7)
	d.SilkCh0NLSFMSVQSurvivors = int32(src.silk_ch0_NLSF_MSVQ_Survivors)
	d.SilkCh0PitchLPCWinLength = int32(src.silk_ch0_pitch_LPC_win_length)
	d.SilkCh0MaxPitchLag = int32(src.silk_ch0_max_pitch_lag)
	d.SilkCh0PitchEstComplexity = int32(src.silk_ch0_pitchEstimationComplexity)
	d.SilkCh0PitchEstLPCOrder = int32(src.silk_ch0_pitchEstimationLPCOrder)
	d.SilkCh0PitchEstThresholdQ16 = int32(src.silk_ch0_pitchEstimationThreshold_Q16)
	d.SilkCh0NSQLagPrev = int32(src.silk_ch0_NSQ_lagPrev)
	d.SilkCh0NSQSLTPBufIdx = int32(src.silk_ch0_NSQ_sLTP_buf_idx)
	d.SilkCh0NSQSLTPShpBufIdx = int32(src.silk_ch0_NSQ_sLTP_shp_buf_idx)
	d.SilkCh0NSQRandSeed = int32(src.silk_ch0_NSQ_rand_seed)
	d.SilkCh0NSQPrevGainQ16 = int32(src.silk_ch0_NSQ_prev_gain_Q16)
	d.SilkCh0NSQRewhiteFlag = int32(src.silk_ch0_NSQ_rewhite_flag)
	d.SilkCh0NSQSLFARShpQ14 = int32(src.silk_ch0_NSQ_sLF_AR_shp_Q14)
	d.SilkCh0NSQSDiffShpQ14 = int32(src.silk_ch0_NSQ_sDiff_shp_Q14)
	d.SilkCh0SShapeLastGainIndex = int32(src.silk_ch0_sShape_LastGainIndex)
	d.SilkCh0SShapeHarmShapeGainSmthBits = uint32(src.silk_ch0_sShape_HarmShapeGain_smth_bits)
	d.SilkCh0SShapeTiltSmthBits = uint32(src.silk_ch0_sShape_Tilt_smth_bits)
	d.SilkCh0LTPCorrBits = uint32(src.silk_ch0_LTPCorr_bits)
	d.SilkCh0InputBufCrc = uint32(src.silk_ch0_inputBuf_crc)
	d.SilkCh0PrevNLSFqQ15Crc = uint32(src.silk_ch0_prev_NLSFq_Q15_crc)
	d.SilkCh0XBufFpCrc = uint32(src.silk_ch0_x_buf_fp_crc)
	d.SilkCh0InHPStateCrc = uint32(src.silk_ch0_In_HP_State_crc)

	// silk ch1
	d.SilkCh1FsKHz = int32(src.silk_ch1_fs_kHz)
	d.SilkCh1NbSubfr = int32(src.silk_ch1_nb_subfr)
	d.SilkCh1FrameLength = int32(src.silk_ch1_frame_length)
	d.SilkCh1PrevSignalType = int32(src.silk_ch1_prevSignalType)
	d.SilkCh1FrameCounter = int32(src.silk_ch1_frameCounter)
	d.SilkCh1NSQRandSeed = int32(src.silk_ch1_NSQ_rand_seed)
	d.SilkCh1XBufFpCrc = uint32(src.silk_ch1_x_buf_fp_crc)

	return d
}
