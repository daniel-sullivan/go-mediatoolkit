package nativeopus

import "math"

// Thin parity-test exports for opus_encoder.c helper functions.
// The production symbols are package-private (lowercase) so tests in
// benchcmp reach them through these shims.

func f32bits(f float32) uint32 { return math.Float32bits(f) }

// ExportTestHpCutoff runs the high-pass biquad filter across all
// channels. hp_mem must have at least 4 entries (mirrors the C
// encoder field). Returns the filtered output and the updated state.
func ExportTestHpCutoff(in_ []opus_res, cutoff_Hz int32, hp_mem []opus_val32, length, channels int, Fs int32) (out []opus_res, memOut []opus_val32) {
	out = make([]opus_res, len(in_))
	mem := make([]opus_val32, 4)
	copy(mem, hp_mem)
	hp_cutoff(in_, opus_int32(cutoff_Hz), out, mem, length, channels, opus_int32(Fs), 0)
	return out, mem
}

// ExportTestDcReject runs the DC-reject filter. hp_mem must have at
// least 4 entries.
func ExportTestDcReject(in_ []opus_val16, cutoff_Hz int32, hp_mem []opus_val32, length, channels int, Fs int32) (out []opus_val16, memOut []opus_val32) {
	out = make([]opus_val16, len(in_))
	mem := make([]opus_val32, 4)
	copy(mem, hp_mem)
	dc_reject(in_, opus_int32(cutoff_Hz), out, mem, length, channels, opus_int32(Fs))
	return out, mem
}

// ExportTestStereoFade runs the stereo→mono crossfade helper over the
// provided interleaved buffer. Returns the mutated output.
func ExportTestStereoFade(in_ []opus_res, g1, g2 opus_val16, overlap48, frame_size, channels int, window []celt_coef, Fs int32) (out []opus_res) {
	out = append([]opus_res(nil), in_...)
	stereo_fade(in_, out, g1, g2, overlap48, frame_size, channels, window, opus_int32(Fs))
	return out
}

// ExportTestGainFade runs the gain-fade window ramp.
func ExportTestGainFade(in_ []opus_res, g1, g2 opus_val16, overlap48, frame_size, channels int, window []celt_coef, Fs int32) (out []opus_res) {
	out = make([]opus_res, len(in_))
	gain_fade(in_, out, g1, g2, overlap48, frame_size, channels, window, opus_int32(Fs))
	return out
}

// ExportTestComputeStereoWidth runs compute_stereo_width with the
// provided initial smoothing state. Returns the return value and the
// updated state fields.
func ExportTestComputeStereoWidth(pcm []opus_res, frame_size int, Fs int32,
	memXX, memXY, memYY opus_val32, memSmoothed, memMax opus_val16,
) (ret opus_val16, outXX, outXY, outYY opus_val32, outSmoothed, outMax opus_val16) {
	mem := StereoWidthState{
		XX: memXX, XY: memXY, YY: memYY,
		smoothed_width: memSmoothed, max_follower: memMax,
	}
	ret = compute_stereo_width(pcm, frame_size, opus_int32(Fs), &mem)
	return ret, mem.XX, mem.XY, mem.YY, mem.smoothed_width, mem.max_follower
}

// ─────────────────────────────────────────────────────────────────────
// 9f-A test shims: struct + lifecycle + TOC + frame_size_select
// ─────────────────────────────────────────────────────────────────────

// OpusEncoderStateSnapshot captures the deterministically-initialised
// fields of an OpusEncoder immediately after opus_encoder_init. Tests
// compare this struct against a matching snapshot from the C oracle.
type OpusEncoderStateSnapshot struct {
	CeltEncOffset     int32
	SilkEncOffset     int32
	Application       int32
	Channels          int32
	DelayCompensation int32
	ForceChannels     int32
	SignalType        int32
	UserBandwidth     int32
	MaxBandwidth      int32
	UserForcedMode    int32
	VoiceRatio        int32
	Fs                int32
	UseVBR            int32
	VBRConstraint     int32
	VariableDuration  int32
	BitrateBps        int32
	UserBitrateBps    int32
	LsbDepth          int32
	EncoderBuffer     int32
	Lfe               int32
	Arch              int32
	UseDTX            int32
	FecConfig         int32

	SilkNChannelsAPI              int32
	SilkNChannelsInternal         int32
	SilkAPISampleRate             int32
	SilkMaxInternalSampleRate     int32
	SilkMinInternalSampleRate     int32
	SilkDesiredInternalSampleRate int32
	SilkPayloadSizeMs             int32
	SilkBitRate                   int32
	SilkPacketLossPercentage      int32
	SilkComplexity                int32
	SilkUseInBandFEC              int32
	SilkUseDTX                    int32
	SilkUseCBR                    int32
	SilkReducedDependency         int32

	StreamChannels       int32
	HybridStereoWidthQ14 int32
	VariableHPSmth2Q15   int32
	PrevHBGainBits       uint32 // float32 bit pattern
	Mode                 int32
	PrevMode             int32
	PrevChannels         int32
	PrevFramesize        int32
	Bandwidth            int32
	First                int32
	NbNoActivityMsQ1     int32
}

// ExportOpusEncoderInitAndSnapshot zeroes a new Go encoder, runs
// opus_encoder_init, and returns (ret, snapshot). Tests compare the
// snapshot against a matching field-for-field capture of a C encoder
// initialised the same way.
func ExportOpusEncoderInitAndSnapshot(Fs int32, channels, application int) (int, OpusEncoderStateSnapshot) {
	st := &OpusEncoder{}
	ret := opus_encoder_init(st, opus_int32(Fs), channels, application)
	return ret, snapshotOpusEncoder(st)
}

func snapshotOpusEncoder(st *OpusEncoder) OpusEncoderStateSnapshot {
	return OpusEncoderStateSnapshot{
		CeltEncOffset:     int32(st.celt_enc_offset),
		SilkEncOffset:     int32(st.silk_enc_offset),
		Application:       int32(st.application),
		Channels:          int32(st.channels),
		DelayCompensation: int32(st.delay_compensation),
		ForceChannels:     int32(st.force_channels),
		SignalType:        int32(st.signal_type),
		UserBandwidth:     int32(st.user_bandwidth),
		MaxBandwidth:      int32(st.max_bandwidth),
		UserForcedMode:    int32(st.user_forced_mode),
		VoiceRatio:        int32(st.voice_ratio),
		Fs:                int32(st.Fs),
		UseVBR:            int32(st.use_vbr),
		VBRConstraint:     int32(st.vbr_constraint),
		VariableDuration:  int32(st.variable_duration),
		BitrateBps:        int32(st.bitrate_bps),
		UserBitrateBps:    int32(st.user_bitrate_bps),
		LsbDepth:          int32(st.lsb_depth),
		EncoderBuffer:     int32(st.encoder_buffer),
		Lfe:               int32(st.lfe),
		Arch:              int32(st.arch),
		UseDTX:            int32(st.use_dtx),
		FecConfig:         int32(st.fec_config),

		SilkNChannelsAPI:              int32(st.silk_mode.nChannelsAPI),
		SilkNChannelsInternal:         int32(st.silk_mode.nChannelsInternal),
		SilkAPISampleRate:             int32(st.silk_mode.API_sampleRate),
		SilkMaxInternalSampleRate:     int32(st.silk_mode.maxInternalSampleRate),
		SilkMinInternalSampleRate:     int32(st.silk_mode.minInternalSampleRate),
		SilkDesiredInternalSampleRate: int32(st.silk_mode.desiredInternalSampleRate),
		SilkPayloadSizeMs:             int32(st.silk_mode.payloadSize_ms),
		SilkBitRate:                   int32(st.silk_mode.bitRate),
		SilkPacketLossPercentage:      int32(st.silk_mode.packetLossPercentage),
		SilkComplexity:                int32(st.silk_mode.complexity),
		SilkUseInBandFEC:              int32(st.silk_mode.useInBandFEC),
		SilkUseDTX:                    int32(st.silk_mode.useDTX),
		SilkUseCBR:                    int32(st.silk_mode.useCBR),
		SilkReducedDependency:         int32(st.silk_mode.reducedDependency),

		StreamChannels:       int32(st.stream_channels),
		HybridStereoWidthQ14: int32(st.hybrid_stereo_width_Q14),
		VariableHPSmth2Q15:   int32(st.variable_HP_smth2_Q15),
		PrevHBGainBits:       f32bits(float32(st.prev_HB_gain)),
		Mode:                 int32(st.mode),
		PrevMode:             int32(st.prev_mode),
		PrevChannels:         int32(st.prev_channels),
		PrevFramesize:        int32(st.prev_framesize),
		Bandwidth:            int32(st.bandwidth),
		First:                int32(st.first),
		NbNoActivityMsQ1:     int32(st.nb_no_activity_ms_Q1),
	}
}

// ExportOpusEncoderGetSize wraps opus_encoder_get_size.
func ExportOpusEncoderGetSize(channels int) int {
	return opus_encoder_get_size(channels)
}

// ExportGenToc wraps gen_toc.
func ExportGenToc(mode, framerate, bandwidth, channels int) byte {
	return gen_toc(mode, framerate, bandwidth, channels)
}

// ExportFrameSizeSelect wraps frame_size_select.
func ExportFrameSizeSelect(application int, frameSize int32, variableDuration int, Fs int32) int32 {
	return int32(frame_size_select(application, opus_int32(frameSize), variableDuration, opus_int32(Fs)))
}

// ExportUserBitrateToBitrate drives user_bitrate_to_bitrate against a
// freshly-initialised Go encoder with user_bitrate_bps overridden to
// the requested value.
func ExportUserBitrateToBitrate(Fs int32, channels, application int, userBitrate int32, frameSize, maxDataBytes int) (int32, int) {
	st := &OpusEncoder{}
	ret := opus_encoder_init(st, opus_int32(Fs), channels, application)
	if ret != OPUS_OK {
		return 0, ret
	}
	st.user_bitrate_bps = opus_int32(userBitrate)
	return int32(user_bitrate_to_bitrate(st, frameSize, maxDataBytes)), OPUS_OK
}

// ─────────────────────────────────────────────────────────────────────
// 9f-B test shims: opus_encoder_ctl SET/GET parity helpers.
// ─────────────────────────────────────────────────────────────────────

// ExportOpusEncoderCtlSetAndSnapshot initialises a Go encoder, applies
// opus_encoder_ctl(request, value), and returns (initRet, ctlRet,
// snapshot). When value is not used by the selected request (e.g.
// OPUS_RESET_STATE) the argument is ignored.
func ExportOpusEncoderCtlSetAndSnapshot(Fs int32, channels, application, request int, value int32) (int, int, OpusEncoderStateSnapshot) {
	st := &OpusEncoder{}
	initRet := opus_encoder_init(st, opus_int32(Fs), channels, application)
	if initRet != OPUS_OK {
		return initRet, 0, OpusEncoderStateSnapshot{}
	}
	var ctlRet int
	switch request {
	case OPUS_RESET_STATE, OPUS_GET_LOOKAHEAD_REQUEST, OPUS_GET_SAMPLE_RATE_REQUEST, OPUS_GET_FINAL_RANGE_REQUEST:
		// RESET_STATE is arg-less; GET requests that only need a pointer
		// are wrapped here for snapshot purposes (their returned value
		// is tested separately via ExportOpusEncoderCtlGet*).
		if request == OPUS_RESET_STATE {
			ctlRet = opus_encoder_ctl(st, request)
		} else {
			var out opus_int32
			var outU opus_uint32
			if request == OPUS_GET_FINAL_RANGE_REQUEST {
				ctlRet = opus_encoder_ctl(st, request, &outU)
			} else {
				ctlRet = opus_encoder_ctl(st, request, &out)
			}
		}
	default:
		ctlRet = opus_encoder_ctl(st, request, opus_int32(value))
	}
	return initRet, ctlRet, snapshotOpusEncoder(st)
}

// ExportOpusEncoderCtlGetI32 initialises a Go encoder, optionally
// pre-applies a SET CTL, then runs a GET CTL expecting an opus_int32
// output. Returns (initRet, preSetRet, getRet, outValue).
func ExportOpusEncoderCtlGetI32(Fs int32, channels, application, preSetRequest int, preSetValue int32, getRequest int) (int, int, int, int32) {
	st := &OpusEncoder{}
	initRet := opus_encoder_init(st, opus_int32(Fs), channels, application)
	if initRet != OPUS_OK {
		return initRet, 0, 0, 0
	}
	preSetRet := 0
	if preSetRequest != 0 {
		preSetRet = opus_encoder_ctl(st, preSetRequest, opus_int32(preSetValue))
	}
	var out opus_int32
	getRet := opus_encoder_ctl(st, getRequest, &out)
	return initRet, preSetRet, getRet, int32(out)
}

// ExportOpusEncoderCtlGetU32 is the opus_uint32 variant.
func ExportOpusEncoderCtlGetU32(Fs int32, channels, application, preSetRequest int, preSetValue int32, getRequest int) (int, int, int, uint32) {
	st := &OpusEncoder{}
	initRet := opus_encoder_init(st, opus_int32(Fs), channels, application)
	if initRet != OPUS_OK {
		return initRet, 0, 0, 0
	}
	preSetRet := 0
	if preSetRequest != 0 {
		preSetRet = opus_encoder_ctl(st, preSetRequest, opus_int32(preSetValue))
	}
	var out opus_uint32
	getRet := opus_encoder_ctl(st, getRequest, &out)
	return initRet, preSetRet, getRet, uint32(out)
}

// Re-export CTL request constants for tests.
const (
	ExportOPUS_SET_APPLICATION_REQUEST              = OPUS_SET_APPLICATION_REQUEST
	ExportOPUS_GET_APPLICATION_REQUEST              = OPUS_GET_APPLICATION_REQUEST
	ExportOPUS_SET_BITRATE_REQUEST                  = OPUS_SET_BITRATE_REQUEST
	ExportOPUS_GET_BITRATE_REQUEST                  = OPUS_GET_BITRATE_REQUEST
	ExportOPUS_SET_MAX_BANDWIDTH_REQUEST            = OPUS_SET_MAX_BANDWIDTH_REQUEST
	ExportOPUS_GET_MAX_BANDWIDTH_REQUEST            = OPUS_GET_MAX_BANDWIDTH_REQUEST
	ExportOPUS_SET_VBR_REQUEST                      = OPUS_SET_VBR_REQUEST
	ExportOPUS_GET_VBR_REQUEST                      = OPUS_GET_VBR_REQUEST
	ExportOPUS_SET_BANDWIDTH_REQUEST                = OPUS_SET_BANDWIDTH_REQUEST
	ExportOPUS_GET_BANDWIDTH_REQUEST                = OPUS_GET_BANDWIDTH_REQUEST
	ExportOPUS_SET_COMPLEXITY_REQUEST               = OPUS_SET_COMPLEXITY_REQUEST
	ExportOPUS_GET_COMPLEXITY_REQUEST               = OPUS_GET_COMPLEXITY_REQUEST
	ExportOPUS_SET_INBAND_FEC_REQUEST               = OPUS_SET_INBAND_FEC_REQUEST
	ExportOPUS_GET_INBAND_FEC_REQUEST               = OPUS_GET_INBAND_FEC_REQUEST
	ExportOPUS_SET_PACKET_LOSS_PERC_REQUEST         = OPUS_SET_PACKET_LOSS_PERC_REQUEST
	ExportOPUS_GET_PACKET_LOSS_PERC_REQUEST         = OPUS_GET_PACKET_LOSS_PERC_REQUEST
	ExportOPUS_SET_DTX_REQUEST                      = OPUS_SET_DTX_REQUEST
	ExportOPUS_GET_DTX_REQUEST                      = OPUS_GET_DTX_REQUEST
	ExportOPUS_SET_VBR_CONSTRAINT_REQUEST           = OPUS_SET_VBR_CONSTRAINT_REQUEST
	ExportOPUS_GET_VBR_CONSTRAINT_REQUEST           = OPUS_GET_VBR_CONSTRAINT_REQUEST
	ExportOPUS_SET_FORCE_CHANNELS_REQUEST           = OPUS_SET_FORCE_CHANNELS_REQUEST
	ExportOPUS_GET_FORCE_CHANNELS_REQUEST           = OPUS_GET_FORCE_CHANNELS_REQUEST
	ExportOPUS_SET_SIGNAL_REQUEST                   = OPUS_SET_SIGNAL_REQUEST
	ExportOPUS_GET_SIGNAL_REQUEST                   = OPUS_GET_SIGNAL_REQUEST
	ExportOPUS_GET_LOOKAHEAD_REQUEST                = OPUS_GET_LOOKAHEAD_REQUEST
	ExportOPUS_GET_SAMPLE_RATE_REQUEST              = OPUS_GET_SAMPLE_RATE_REQUEST
	ExportOPUS_GET_FINAL_RANGE_REQUEST              = OPUS_GET_FINAL_RANGE_REQUEST
	ExportOPUS_SET_LSB_DEPTH_REQUEST                = OPUS_SET_LSB_DEPTH_REQUEST
	ExportOPUS_GET_LSB_DEPTH_REQUEST                = OPUS_GET_LSB_DEPTH_REQUEST
	ExportOPUS_SET_EXPERT_FRAME_DURATION_REQUEST    = OPUS_SET_EXPERT_FRAME_DURATION_REQUEST
	ExportOPUS_GET_EXPERT_FRAME_DURATION_REQUEST    = OPUS_GET_EXPERT_FRAME_DURATION_REQUEST
	ExportOPUS_SET_PREDICTION_DISABLED_REQUEST      = OPUS_SET_PREDICTION_DISABLED_REQUEST
	ExportOPUS_GET_PREDICTION_DISABLED_REQUEST      = OPUS_GET_PREDICTION_DISABLED_REQUEST
	ExportOPUS_SET_PHASE_INVERSION_DISABLED_REQUEST = OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST
	ExportOPUS_GET_PHASE_INVERSION_DISABLED_REQUEST = OPUS_GET_PHASE_INVERSION_DISABLED_REQUEST
	ExportOPUS_SET_VOICE_RATIO_REQUEST              = OPUS_SET_VOICE_RATIO_REQUEST
	ExportOPUS_GET_VOICE_RATIO_REQUEST              = OPUS_GET_VOICE_RATIO_REQUEST
	ExportOPUS_SET_FORCE_MODE_REQUEST               = OPUS_SET_FORCE_MODE_REQUEST
	ExportOPUS_SET_LFE_REQUEST                      = OPUS_SET_LFE_REQUEST
	ExportOPUS_GET_IN_DTX_REQUEST                   = OPUS_GET_IN_DTX_REQUEST
	ExportOPUS_RESET_STATE                          = OPUS_RESET_STATE
	ExportOPUS_BITRATE_MAX                          = OPUS_BITRATE_MAX
	ExportOPUS_AUTO                                 = OPUS_AUTO
	ExportOPUS_SIGNAL_VOICE                         = OPUS_SIGNAL_VOICE
	ExportOPUS_SIGNAL_MUSIC                         = OPUS_SIGNAL_MUSIC
)
