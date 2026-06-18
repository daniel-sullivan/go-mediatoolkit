package nativeopus

import (
	"hash/fnv"
	"math"
	"unsafe"
)

// export_for_testing_state_dump.go — Go-side mirror of the C
// opus_test_encoder_full_dump helper. Produces a structurally identical
// per-encoder snapshot of every field the benchcmp drift-bisection
// harness tracks. Field names match the C-side CEncoderStateDump
// verbatim so the diff routine can iterate both by index.
//
// Array fields too large to dump verbatim are summarised as FNV-1a
// CRCs over their on-wire byte representation (little-endian raw float
// bits or raw integer bytes). The first-16 scalar elements of the
// critical CELT arrays are additionally broken out for surgical
// diffing.
//
// IMPORTANT: the C helper writes float values as their IEEE bit
// patterns (opus_test_f32_bits). We do the same here using
// math.Float32bits so bit-exact parity tests see identical uint32s.

// GoEncoderStateDump — Go-side mirror of benchcmp.CEncoderStateDump.
type GoEncoderStateDump struct {
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

	SilkCh1FsKHz          int32
	SilkCh1NbSubfr        int32
	SilkCh1FrameLength    int32
	SilkCh1PrevSignalType int32
	SilkCh1FrameCounter   int32
	SilkCh1NSQRandSeed    int32
	SilkCh1XBufFpCrc      uint32
}

// ExportDumpGoEncoderState returns a GoEncoderStateDump of the given
// Go OpusEncoder. Used by the benchcmp harness for drift bisection.
func ExportDumpGoEncoderState(st *OpusEncoder) GoEncoderStateDump {
	var d GoEncoderStateDump
	if st == nil {
		return d
	}
	d.CeltEncOffset = int32(st.celt_enc_offset)
	d.SilkEncOffset = int32(st.silk_enc_offset)
	d.Application = int32(st.application)
	d.Channels = int32(st.channels)
	d.DelayCompensation = int32(st.delay_compensation)
	d.ForceChannels = int32(st.force_channels)
	d.SignalType = int32(st.signal_type)
	d.UserBandwidth = int32(st.user_bandwidth)
	d.MaxBandwidth = int32(st.max_bandwidth)
	d.UserForcedMode = int32(st.user_forced_mode)
	d.VoiceRatio = int32(st.voice_ratio)
	d.Fs = int32(st.Fs)
	d.UseVbr = int32(st.use_vbr)
	d.VbrConstraint = int32(st.vbr_constraint)
	d.VariableDuration = int32(st.variable_duration)
	d.BitrateBps = int32(st.bitrate_bps)
	d.UserBitrateBps = int32(st.user_bitrate_bps)
	d.LsbDepth = int32(st.lsb_depth)
	d.EncoderBuffer = int32(st.encoder_buffer)
	d.Lfe = int32(st.lfe)
	d.Arch = int32(st.arch)
	d.UseDtx = int32(st.use_dtx)
	d.FecConfig = int32(st.fec_config)
	d.StreamChannels = int32(st.stream_channels)
	d.HybridStereoWidthQ14 = int32(st.hybrid_stereo_width_Q14)
	d.VariableHPSmth2Q15 = int32(st.variable_HP_smth2_Q15)
	d.PrevHBGainBits = math.Float32bits(float32(st.prev_HB_gain))
	for i := 0; i < 4; i++ {
		d.HPMemBits[i] = math.Float32bits(float32(st.hp_mem[i]))
	}
	d.Mode = int32(st.mode)
	d.PrevMode = int32(st.prev_mode)
	d.PrevChannels = int32(st.prev_channels)
	d.PrevFramesize = int32(st.prev_framesize)
	d.Bandwidth = int32(st.bandwidth)
	d.AutoBandwidth = int32(st.auto_bandwidth)
	d.SilkBwSwitch = int32(st.silk_bw_switch)
	d.First = int32(st.first)
	d.WidthMemXXBits = math.Float32bits(float32(st.width_mem.XX))
	d.WidthMemXYBits = math.Float32bits(float32(st.width_mem.XY))
	d.WidthMemYYBits = math.Float32bits(float32(st.width_mem.YY))
	d.WidthMemSmoothedBits = math.Float32bits(float32(st.width_mem.smoothed_width))
	d.WidthMemMaxFollowerBits = math.Float32bits(float32(st.width_mem.max_follower))
	d.DetectedBandwidth = int32(st.detected_bandwidth)
	d.NbNoActivityMsQ1 = int32(st.nb_no_activity_ms_Q1)
	d.PeakSignalEnergyBits = math.Float32bits(float32(st.peak_signal_energy))
	d.NonfinalFrame = int32(st.nonfinal_frame)
	d.RangeFinal = uint32(st.rangeFinal)

	// Analysis
	d.AnArch = int32(st.analysis.arch)
	d.AnApplication = int32(st.analysis.application)
	d.AnFs = int32(st.analysis.Fs)
	d.AnMemFill = int32(st.analysis.mem_fill)
	d.AnPrevTonalityBits = math.Float32bits(st.analysis.prev_tonality)
	d.AnPrevBandwidth = int32(st.analysis.prev_bandwidth)
	d.AnEtrackerBits = math.Float32bits(st.analysis.Etracker)
	d.AnLowECountBits = math.Float32bits(st.analysis.lowECount)
	d.AnECount = int32(st.analysis.E_count)
	d.AnCount = int32(st.analysis.count)
	d.AnAnalysisOffset = int32(st.analysis.analysis_offset)
	d.AnWritePos = int32(st.analysis.write_pos)
	d.AnReadPos = int32(st.analysis.read_pos)
	d.AnReadSubframe = int32(st.analysis.read_subframe)
	d.AnHpEnerAccumBits = math.Float32bits(st.analysis.hp_ener_accum)
	d.AnInitialized = int32(st.analysis.initialized)
	d.AnAngleFpCrc = fnvF32Slice(st.analysis.angle[:])
	d.AnDAngleFpCrc = fnvF32Slice(st.analysis.d_angle[:])
	d.AnD2AngleFpCrc = fnvF32Slice(st.analysis.d2_angle[:])
	// inmem is []opus_val32 (float32)
	{
		buf := make([]float32, len(st.analysis.inmem))
		for i, v := range st.analysis.inmem {
			buf[i] = float32(v)
		}
		d.AnInmemFpCrc = fnvF32Slice(buf)
	}
	// E/logE are 2D arrays
	{
		var flat [NB_FRAMES * NB_TBANDS]float32
		idx := 0
		for i := 0; i < NB_FRAMES; i++ {
			for j := 0; j < NB_TBANDS; j++ {
				flat[idx] = st.analysis.E[i][j]
				idx++
			}
		}
		d.AnEFpCrc = fnvF32Slice(flat[:])
		idx = 0
		for i := 0; i < NB_FRAMES; i++ {
			for j := 0; j < NB_TBANDS; j++ {
				flat[idx] = st.analysis.logE[i][j]
				idx++
			}
		}
		d.AnLogEFpCrc = fnvF32Slice(flat[:])
	}
	d.AnRnnStateFpCrc = fnvF32Slice(st.analysis.rnn_state[:])
	d.AnInfoFpCrc = fnvAnalysisInfos(st.analysis.info[:])

	// CELT encoder
	if st.celt_enc != nil {
		c := st.celt_enc
		d.CeltChannels = int32(c.channels)
		d.CeltStreamChannels = int32(c.stream_channels)
		d.CeltForceIntra = int32(c.force_intra)
		d.CeltClip = int32(c.clip)
		d.CeltDisablePf = int32(c.disable_pf)
		d.CeltComplexity = int32(c.complexity)
		d.CeltUpsample = int32(c.upsample)
		d.CeltStart = int32(c.start)
		d.CeltEnd = int32(c.end)
		d.CeltBitrate = int32(c.bitrate)
		d.CeltVbr = int32(c.vbr)
		d.CeltSignalling = int32(c.signalling)
		d.CeltConstrainedVbr = int32(c.constrained_vbr)
		d.CeltLossRate = int32(c.loss_rate)
		d.CeltLsbDepth = int32(c.lsb_depth)
		d.CeltLfe = int32(c.lfe)
		d.CeltDisableInv = int32(c.disable_inv)
		d.CeltArch = int32(c.arch)
		d.CeltRng = uint32(c.rng)
		d.CeltSpreadDecision = int32(c.spread_decision)
		d.CeltDelayedIntraBits = math.Float32bits(float32(c.delayedIntra))
		d.CeltTonalAverage = int32(c.tonal_average)
		d.CeltLastCodedBands = int32(c.lastCodedBands)
		d.CeltHfAverage = int32(c.hf_average)
		d.CeltTapsetDecision = int32(c.tapset_decision)
		d.CeltPrefilterPeriod = int32(c.prefilter_period)
		d.CeltPrefilterGainBits = math.Float32bits(float32(c.prefilter_gain))
		d.CeltPrefilterTapset = int32(c.prefilter_tapset)
		d.CeltConsecTransient = int32(c.consec_transient)
		for i := 0; i < 2; i++ {
			d.CeltPreemphMemEBits[i] = math.Float32bits(float32(c.preemph_memE[i]))
			d.CeltPreemphMemDBits[i] = math.Float32bits(float32(c.preemph_memD[i]))
			d.CeltPreemphMemEF16[i] = d.CeltPreemphMemEBits[i]
		}
		d.CeltVbrReservoir = int32(c.vbr_reservoir)
		d.CeltVbrDrift = int32(c.vbr_drift)
		d.CeltVbrOffset = int32(c.vbr_offset)
		d.CeltVbrCount = int32(c.vbr_count)
		d.CeltOverlapMaxBits = math.Float32bits(float32(c.overlap_max))
		d.CeltStereoSavingBits = math.Float32bits(float32(c.stereo_saving))
		d.CeltIntensity = int32(c.intensity)
		d.CeltSpecAvgBits = math.Float32bits(float32(c.spec_avg))

		// Array CRCs (glog / sig are float32 under our float build)
		d.CeltInMemFpCrc = fnvSigSlice(c.in_mem)
		d.CeltPrefilterMemFpCrc = fnvSigSlice(c.prefilter_mem)
		d.CeltOldBandEFpCrc = fnvGlogSlice(c.oldBandE)
		d.CeltOldLogEFpCrc = fnvGlogSlice(c.oldLogE)
		d.CeltOldLogE2FpCrc = fnvGlogSlice(c.oldLogE2)
		d.CeltEnergyErrorFpCrc = fnvGlogSlice(c.energyError)

		for i := 0; i < 16; i++ {
			if i < len(c.oldBandE) {
				d.CeltOldBandEF16[i] = math.Float32bits(float32(c.oldBandE[i]))
			}
			if i < len(c.oldLogE) {
				d.CeltOldLogEF16[i] = math.Float32bits(float32(c.oldLogE[i]))
			}
			if i < len(c.oldLogE2) {
				d.CeltOldLogE2F16[i] = math.Float32bits(float32(c.oldLogE2[i]))
			}
			if i < len(c.energyError) {
				d.CeltEnergyErrorF16[i] = math.Float32bits(float32(c.energyError[i]))
			}
		}
	}

	// silk_encoder
	if st.silk_enc != nil {
		s := st.silk_enc
		d.SilkSuperNBitsUsedLBRR = int32(s.nBitsUsedLBRR)
		d.SilkSuperNBitsExceeded = int32(s.nBitsExceeded)
		d.SilkSuperNChannelsAPI = int32(s.nChannelsAPI)
		d.SilkSuperNChannelsInternal = int32(s.nChannelsInternal)
		d.SilkSuperNPrevChannelsInternal = int32(s.nPrevChannelsInternal)
		d.SilkSuperTimeSinceSwitchAllowedMs = int32(s.timeSinceSwitchAllowed_ms)
		d.SilkSuperAllowBandwidthSwitch = int32(s.allowBandwidthSwitch)
		d.SilkSuperPrevDecodeOnlyMiddle = int32(s.prev_decode_only_middle)

		ch0 := &s.state_Fxx[0]
		d.SilkCh0FsKHz = int32(ch0.sCmn.fs_kHz)
		d.SilkCh0NbSubfr = int32(ch0.sCmn.nb_subfr)
		d.SilkCh0FrameLength = int32(ch0.sCmn.frame_length)
		d.SilkCh0SubfrLength = int32(ch0.sCmn.subfr_length)
		d.SilkCh0LtpMemLength = int32(ch0.sCmn.ltp_mem_length)
		d.SilkCh0LaPitch = int32(ch0.sCmn.la_pitch)
		d.SilkCh0LaShape = int32(ch0.sCmn.la_shape)
		d.SilkCh0PrevLag = int32(ch0.sCmn.prevLag)
		d.SilkCh0PrevSignalType = int32(ch0.sCmn.prevSignalType)
		d.SilkCh0InputBufIx = int32(ch0.sCmn.inputBufIx)
		d.SilkCh0NFramesEncoded = int32(ch0.sCmn.nFramesEncoded)
		d.SilkCh0NFramesPerPacket = int32(ch0.sCmn.nFramesPerPacket)
		d.SilkCh0TargetRateBps = int32(ch0.sCmn.TargetRate_bps)
		d.SilkCh0PacketSizeMs = int32(ch0.sCmn.PacketSize_ms)
		d.SilkCh0PacketLossPerc = int32(ch0.sCmn.PacketLoss_perc)
		d.SilkCh0FrameCounter = int32(ch0.sCmn.frameCounter)
		d.SilkCh0Complexity = int32(ch0.sCmn.Complexity)
		d.SilkCh0PredictLPCOrder = int32(ch0.sCmn.predictLPCOrder)
		d.SilkCh0ShapingLPCOrder = int32(ch0.sCmn.shapingLPCOrder)
		d.SilkCh0WarpingQ16 = int32(ch0.sCmn.warping_Q16)
		d.SilkCh0UseCBR = int32(ch0.sCmn.useCBR)
		d.SilkCh0PrefillFlag = int32(ch0.sCmn.prefillFlag)
		d.SilkCh0SpeechActivityQ8 = int32(ch0.sCmn.speech_activity_Q8)
		d.SilkCh0AllowBandwidthSwitch = int32(ch0.sCmn.allow_bandwidth_switch)
		d.SilkCh0LBRRprevLastGainIndex = int32(ch0.sCmn.LBRRprevLastGainIndex)
		d.SilkCh0FirstFrameAfterReset = int32(ch0.sCmn.first_frame_after_reset)
		d.SilkCh0ControlledSinceLastPayload = int32(ch0.sCmn.controlled_since_last_payload)
		d.SilkCh0NStatesDelayedDecision = int32(ch0.sCmn.nStatesDelayedDecision)
		d.SilkCh0UseInterpolatedNLSFs = int32(ch0.sCmn.useInterpolatedNLSFs)
		d.SilkCh0VariableHPSmth1Q15 = int32(ch0.sCmn.variable_HP_smth1_Q15)
		d.SilkCh0VariableHPSmth2Q15 = int32(ch0.sCmn.variable_HP_smth2_Q15)
		d.SilkCh0SumLogGainQ7 = int32(ch0.sCmn.sum_log_gain_Q7)
		d.SilkCh0NLSFMSVQSurvivors = int32(ch0.sCmn.NLSF_MSVQ_Survivors)
		d.SilkCh0PitchLPCWinLength = int32(ch0.sCmn.pitch_LPC_win_length)
		d.SilkCh0MaxPitchLag = int32(ch0.sCmn.max_pitch_lag)
		d.SilkCh0PitchEstComplexity = int32(ch0.sCmn.pitchEstimationComplexity)
		d.SilkCh0PitchEstLPCOrder = int32(ch0.sCmn.pitchEstimationLPCOrder)
		d.SilkCh0PitchEstThresholdQ16 = int32(ch0.sCmn.pitchEstimationThreshold_Q16)
		d.SilkCh0NSQLagPrev = int32(ch0.sCmn.sNSQ.lagPrev)
		d.SilkCh0NSQSLTPBufIdx = int32(ch0.sCmn.sNSQ.sLTP_buf_idx)
		d.SilkCh0NSQSLTPShpBufIdx = int32(ch0.sCmn.sNSQ.sLTP_shp_buf_idx)
		d.SilkCh0NSQRandSeed = int32(ch0.sCmn.sNSQ.rand_seed)
		d.SilkCh0NSQPrevGainQ16 = int32(ch0.sCmn.sNSQ.prev_gain_Q16)
		d.SilkCh0NSQRewhiteFlag = int32(ch0.sCmn.sNSQ.rewhite_flag)
		d.SilkCh0NSQSLFARShpQ14 = int32(ch0.sCmn.sNSQ.sLF_AR_shp_Q14)
		d.SilkCh0NSQSDiffShpQ14 = int32(ch0.sCmn.sNSQ.sDiff_shp_Q14)
		d.SilkCh0SShapeLastGainIndex = int32(ch0.sShape.LastGainIndex)
		d.SilkCh0SShapeHarmShapeGainSmthBits = math.Float32bits(float32(ch0.sShape.HarmShapeGain_smth))
		d.SilkCh0SShapeTiltSmthBits = math.Float32bits(float32(ch0.sShape.Tilt_smth))
		d.SilkCh0LTPCorrBits = math.Float32bits(float32(ch0.LTPCorr))

		d.SilkCh0InputBufCrc = fnvInt16Slice(ch0.sCmn.inputBuf[:])
		d.SilkCh0PrevNLSFqQ15Crc = fnvInt16Slice(ch0.sCmn.prev_NLSFq_Q15[:])
		d.SilkCh0XBufFpCrc = fnvF32Slice(ch0.x_buf[:])
		d.SilkCh0InHPStateCrc = fnvInt32Slice(ch0.sCmn.In_HP_State[:])

		if s.nChannelsInternal >= 2 {
			ch1 := &s.state_Fxx[1]
			d.SilkCh1FsKHz = int32(ch1.sCmn.fs_kHz)
			d.SilkCh1NbSubfr = int32(ch1.sCmn.nb_subfr)
			d.SilkCh1FrameLength = int32(ch1.sCmn.frame_length)
			d.SilkCh1PrevSignalType = int32(ch1.sCmn.prevSignalType)
			d.SilkCh1FrameCounter = int32(ch1.sCmn.frameCounter)
			d.SilkCh1NSQRandSeed = int32(ch1.sCmn.sNSQ.rand_seed)
			d.SilkCh1XBufFpCrc = fnvF32Slice(ch1.x_buf[:])
		}
	}
	return d
}

// ── FNV-1a helpers. Inputs are viewed as raw little-endian bytes
//    (same interpretation as C memcmp over the underlying storage).

func fnvF32Slice(xs []float32) uint32 {
	h := fnv.New32a()
	// Write the raw float bits as a byte slice.
	// 4 bytes per element, little-endian.
	buf := make([]byte, 4*len(xs))
	for i, v := range xs {
		b := math.Float32bits(v)
		buf[4*i+0] = byte(b)
		buf[4*i+1] = byte(b >> 8)
		buf[4*i+2] = byte(b >> 16)
		buf[4*i+3] = byte(b >> 24)
	}
	_, _ = h.Write(buf)
	return h.Sum32()
}

func fnvGlogSlice(xs []celt_glog) uint32 {
	buf := make([]float32, len(xs))
	for i, v := range xs {
		buf[i] = float32(v)
	}
	return fnvF32Slice(buf)
}

func fnvSigSlice(xs []celt_sig) uint32 {
	buf := make([]float32, len(xs))
	for i, v := range xs {
		buf[i] = float32(v)
	}
	return fnvF32Slice(buf)
}

func fnvInt16Slice(xs []opus_int16) uint32 {
	h := fnv.New32a()
	buf := make([]byte, 2*len(xs))
	for i, v := range xs {
		buf[2*i+0] = byte(uint16(v))
		buf[2*i+1] = byte(uint16(v) >> 8)
	}
	_, _ = h.Write(buf)
	return h.Sum32()
}

func fnvInt32Slice(xs []opus_int32) uint32 {
	h := fnv.New32a()
	buf := make([]byte, 4*len(xs))
	for i, v := range xs {
		u := uint32(v)
		buf[4*i+0] = byte(u)
		buf[4*i+1] = byte(u >> 8)
		buf[4*i+2] = byte(u >> 16)
		buf[4*i+3] = byte(u >> 24)
	}
	_, _ = h.Write(buf)
	return h.Sum32()
}

// fnvAnalysisInfos FNVs a slice of AnalysisInfo structs byte-for-byte
// using their current in-memory layout. The C side writes the same
// byte range via memcpy against its struct; the struct field order
// (valid, tonality, tonality_slope, noisiness, activity, music_prob,
// music_prob_min, music_prob_max, bandwidth, activity_probability,
// max_pitch_ratio, leak_boost[LEAK_BANDS]) matches between C and Go
// because we maintain layout parity in the Go port.
//
// However Go struct alignment may differ from C. We FNV the canonical
// field bytes in order rather than memcpy the raw struct to keep the
// hash portable across platforms.
func fnvAnalysisInfos(xs []AnalysisInfo) uint32 {
	h := fnv.New32a()
	var buf [4]byte
	put32 := func(u uint32) {
		buf[0] = byte(u)
		buf[1] = byte(u >> 8)
		buf[2] = byte(u >> 16)
		buf[3] = byte(u >> 24)
		_, _ = h.Write(buf[:])
	}
	for _, a := range xs {
		put32(uint32(a.valid))
		put32(math.Float32bits(a.tonality))
		put32(math.Float32bits(a.tonality_slope))
		put32(math.Float32bits(a.noisiness))
		put32(math.Float32bits(a.activity))
		put32(math.Float32bits(a.music_prob))
		put32(math.Float32bits(a.music_prob_min))
		put32(math.Float32bits(a.music_prob_max))
		put32(uint32(a.bandwidth))
		put32(math.Float32bits(a.activity_probability))
		put32(math.Float32bits(a.max_pitch_ratio))
		for _, b := range a.leak_boost {
			_, _ = h.Write([]byte{byte(b)})
		}
	}
	_ = unsafe.Sizeof(xs) // keep "unsafe" import live for future use
	return h.Sum32()
}
