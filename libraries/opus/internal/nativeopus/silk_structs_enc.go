package nativeopus

// Port of libopus/silk/structs.h — encoder-side structs
// (silk_nsq_state, silk_VAD_state, stereo_enc_state,
//  silk_encoder_state), plus float-side helpers from
//  libopus/silk/float/structs_FLP.h
// (silk_shape_state_FLP, silk_encoder_control_FLP,
//  silk_encoder_state_FLP, silk_encoder). Field layouts mirror the C
// structs verbatim so the 1:1 transcription can use identical accessors.

// silk_nsq_state — Noise shaping quantization state. C: structs.h:56-69.
type silk_nsq_state struct {
	xq               [2 * MAX_FRAME_LENGTH]opus_int16
	sLTP_shp_Q14     [2 * MAX_FRAME_LENGTH]opus_int32
	sLPC_Q14         [MAX_SUB_FRAME_LENGTH + NSQ_LPC_BUF_LENGTH]opus_int32
	sAR2_Q14         [MAX_SHAPE_LPC_ORDER]opus_int32
	sLF_AR_shp_Q14   opus_int32
	sDiff_shp_Q14    opus_int32
	lagPrev          opus_int
	sLTP_buf_idx     opus_int
	sLTP_shp_buf_idx opus_int
	rand_seed        opus_int32
	prev_gain_Q16    opus_int32
	rewhite_flag     opus_int
}

// silk_VAD_state — Voice activity detector state. C: structs.h:74-85.
type silk_VAD_state struct {
	AnaState        [2]opus_int32
	AnaState1       [2]opus_int32
	AnaState2       [2]opus_int32
	XnrgSubfr       [VAD_N_BANDS]opus_int32
	NrgRatioSmth_Q8 [VAD_N_BANDS]opus_int32
	HPstate         opus_int16
	NL              [VAD_N_BANDS]opus_int32
	inv_NL          [VAD_N_BANDS]opus_int32
	NoiseLevelBias  [VAD_N_BANDS]opus_int32
	counter         opus_int32
}

// stereo_enc_state — encoder-side stereo predictor state. C: structs.h:111-121.
type stereo_enc_state struct {
	pred_prev_Q13   [2]opus_int16
	sMid            [2]opus_int16
	sSide           [2]opus_int16
	mid_side_amp_Q0 [4]opus_int32
	smth_width_Q14  opus_int16
	width_prev_Q14  opus_int16
	silent_side_len opus_int16
	predIx          [MAX_FRAMES_PER_PACKET][2][3]opus_int8
	mid_only_flags  [MAX_FRAMES_PER_PACKET]opus_int8
}

// silk_encoder_state — main SILK encoder state. C: structs.h:146-239.
// Field order and types match the C struct verbatim; no reordering.
type silk_encoder_state struct {
	In_HP_State                   [2]opus_int32
	variable_HP_smth1_Q15         opus_int32
	variable_HP_smth2_Q15         opus_int32
	sLP                           silk_LP_state
	sVAD                          silk_VAD_state
	sNSQ                          silk_nsq_state
	prev_NLSFq_Q15                [MAX_LPC_ORDER]opus_int16
	speech_activity_Q8            opus_int
	allow_bandwidth_switch        opus_int
	LBRRprevLastGainIndex         opus_int8
	prevSignalType                opus_int8
	prevLag                       opus_int
	pitch_LPC_win_length          opus_int
	max_pitch_lag                 opus_int
	API_fs_Hz                     opus_int32
	prev_API_fs_Hz                opus_int32
	maxInternal_fs_Hz             opus_int
	minInternal_fs_Hz             opus_int
	desiredInternal_fs_Hz         opus_int
	fs_kHz                        opus_int
	nb_subfr                      opus_int
	frame_length                  opus_int
	subfr_length                  opus_int
	ltp_mem_length                opus_int
	la_pitch                      opus_int
	la_shape                      opus_int
	shapeWinLength                opus_int
	TargetRate_bps                opus_int32
	PacketSize_ms                 opus_int
	PacketLoss_perc               opus_int
	frameCounter                  opus_int32
	Complexity                    opus_int
	nStatesDelayedDecision        opus_int
	useInterpolatedNLSFs          opus_int
	shapingLPCOrder               opus_int
	predictLPCOrder               opus_int
	pitchEstimationComplexity     opus_int
	pitchEstimationLPCOrder       opus_int
	pitchEstimationThreshold_Q16  opus_int32
	sum_log_gain_Q7               opus_int32
	NLSF_MSVQ_Survivors           opus_int
	first_frame_after_reset       opus_int
	controlled_since_last_payload opus_int
	warping_Q16                   opus_int
	useCBR                        opus_int
	prefillFlag                   opus_int
	pitch_lag_low_bits_iCDF       []opus_uint8
	pitch_contour_iCDF            []opus_uint8
	psNLSF_CB                     *silk_NLSF_CB_struct
	input_quality_bands_Q15       [VAD_N_BANDS]opus_int
	input_tilt_Q15                opus_int
	SNR_dB_Q7                     opus_int

	VAD_flags  [MAX_FRAMES_PER_PACKET]opus_int8
	LBRR_flag  opus_int8
	LBRR_flags [MAX_FRAMES_PER_PACKET]opus_int

	indices SideInfoIndices
	pulses  [MAX_FRAME_LENGTH]opus_int8

	arch int

	// Input/output buffering
	inputBuf         [MAX_FRAME_LENGTH + 2]opus_int16
	inputBufIx       opus_int
	nFramesPerPacket opus_int
	nFramesEncoded   opus_int

	nChannelsAPI      opus_int
	nChannelsInternal opus_int
	channelNb         opus_int

	// LTP scaling control
	frames_since_onset opus_int

	// Entropy coding
	ec_prevSignalType opus_int
	ec_prevLagIndex   opus_int16

	resampler_state silk_resampler_state_struct

	// DTX
	useDTX          opus_int
	inDTX           opus_int
	noSpeechCounter opus_int

	// Inband LBRR
	useInBandFEC       opus_int
	LBRR_enabled       opus_int
	LBRR_GainIncreases opus_int
	indices_LBRR       [MAX_FRAMES_PER_PACKET]SideInfoIndices
	pulses_LBRR        [MAX_FRAMES_PER_PACKET][MAX_FRAME_LENGTH]opus_int8
}

// silk_encoder_control — per-frame integer encoder control parameters.
// Not present as a standalone struct in structs.h in the reference
// distribution (the C encoder uses silk_encoder_control_FIX defined in
// main_FIX.h), so no mirror is needed here for the float-only build.
// This placeholder comment documents the intentional omission.

// silk_shape_state_FLP — Noise shaping analysis state (float).
// C: float/structs_FLP.h:39-43.
type silk_shape_state_FLP struct {
	LastGainIndex      opus_int8
	HarmShapeGain_smth silk_float
	Tilt_smth          silk_float
}

// silk_encoder_state_FLP — float-side encoder state.
// C: float/structs_FLP.h:48-55.
type silk_encoder_state_FLP struct {
	sCmn    silk_encoder_state
	sShape  silk_shape_state_FLP
	x_buf   [2*MAX_FRAME_LENGTH + LA_SHAPE_MAX]silk_float
	LTPCorr silk_float
}

// silk_encoder_control_FLP — per-frame float encoder control parameters.
// C: float/structs_FLP.h:60-86.
type silk_encoder_control_FLP struct {
	// Prediction and coding parameters.
	Gains     [MAX_NB_SUBFR]silk_float
	PredCoef  [2][MAX_LPC_ORDER]silk_float
	LTPCoef   [LTP_ORDER * MAX_NB_SUBFR]silk_float
	LTP_scale silk_float
	pitchL    [MAX_NB_SUBFR]opus_int

	// Noise shaping parameters.
	AR             [MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER]silk_float
	LF_MA_shp      [MAX_NB_SUBFR]silk_float
	LF_AR_shp      [MAX_NB_SUBFR]silk_float
	Tilt           [MAX_NB_SUBFR]silk_float
	HarmShapeGain  [MAX_NB_SUBFR]silk_float
	Lambda         silk_float
	input_quality  silk_float
	coding_quality silk_float

	// Measures.
	predGain      silk_float
	LTPredCodGain silk_float
	ResNrg        [MAX_NB_SUBFR]silk_float

	// CBR parameters.
	GainsUnq_Q16      [MAX_NB_SUBFR]opus_int32
	lastGainIndexPrev opus_int8
}

// silk_encoder — top-level multi-channel encoder super-struct.
// C: float/structs_FLP.h:91-103.
//
// The C layout places state_Fxx[ENCODER_NUM_CHANNELS] last so callers
// can allocate a smaller region when channels==1 by omitting the
// second state. We preserve the field order here for layout fidelity,
// but always hold ENCODER_NUM_CHANNELS entries in Go (a no-op: the
// second entry simply goes unused for mono).
type silk_encoder struct {
	sStereo                   stereo_enc_state
	nBitsUsedLBRR             opus_int32
	nBitsExceeded             opus_int32
	nChannelsAPI              opus_int
	nChannelsInternal         opus_int
	nPrevChannelsInternal     opus_int
	timeSinceSwitchAllowed_ms opus_int
	allowBandwidthSwitch      opus_int
	prev_decode_only_middle   opus_int
	state_Fxx                 [ENCODER_NUM_CHANNELS]silk_encoder_state_FLP
}
