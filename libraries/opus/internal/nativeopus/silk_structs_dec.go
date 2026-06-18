package nativeopus

// Port of libopus/silk/structs.h — decoder-side structs (PLC, CNG,
// decoder state, decoder control). Encoder structs and OSCE/DEEP_PLC
// sub-structs are omitted because the matching compile-time flags are
// disabled in our vendored config.

// SideInfoIndices — quantized indices describing a SILK frame.
// C: structs.h:129-141.
type SideInfoIndices struct {
	GainsIndices      [MAX_NB_SUBFR]opus_int8
	LTPIndex          [MAX_NB_SUBFR]opus_int8
	NLSFIndices       [MAX_LPC_ORDER + 1]opus_int8
	lagIndex          opus_int16
	contourIndex      opus_int8
	signalType        opus_int8
	quantOffsetType   opus_int8
	NLSFInterpCoef_Q2 opus_int8
	PERIndex          opus_int8
	LTP_scaleIndex    opus_int8
	Seed              opus_int8
}

// silk_PLC_struct — state for Packet Loss Concealment.
// C: structs.h:256-271.
type silk_PLC_struct struct {
	pitchL_Q8         opus_int32
	LTPCoef_Q14       [LTP_ORDER]opus_int16
	prevLPC_Q12       [MAX_LPC_ORDER]opus_int16
	last_frame_lost   opus_int
	rand_seed         opus_int32
	randScale_Q14     opus_int16
	conc_energy       opus_int32
	conc_energy_shift opus_int
	prevLTP_scale_Q14 opus_int16
	prevGain_Q16      [2]opus_int32
	fs_kHz            opus_int
	nb_subfr          opus_int
	subfr_length      opus_int
	enable_deep_plc   opus_int
}

// silk_CNG_struct — state for Comfort Noise Generation.
// C: structs.h:274-281.
type silk_CNG_struct struct {
	CNG_exc_buf_Q14   [MAX_FRAME_LENGTH]opus_int32
	CNG_smth_NLSF_Q15 [MAX_LPC_ORDER]opus_int16
	CNG_synth_state   [MAX_LPC_ORDER]opus_int32
	CNG_smth_Gain_Q16 opus_int32
	rand_seed         opus_int32
	fs_kHz            opus_int
}

// silk_decoder_state — main SILK decoder state.
// C: structs.h:286-341.
//
// Field order preserved exactly so the "reset from prev_gain_Q16 to end"
// semantics from silk_reset_decoder ports mechanically via resetFrom().
type silk_decoder_state struct {
	// --- Fields before SILK_DECODER_STATE_RESET_START (preserved on reset) ---
	// (Our config has ENABLE_OSCE off, so no pre-reset fields exist.)

	// --- Fields cleared by silk_reset_decoder (from prev_gain_Q16 onward) ---
	prev_gain_Q16           opus_int32
	exc_Q14                 [MAX_FRAME_LENGTH]opus_int32
	sLPC_Q14_buf            [MAX_LPC_ORDER]opus_int32
	outBuf                  [MAX_FRAME_LENGTH + 2*MAX_SUB_FRAME_LENGTH]opus_int16
	lagPrev                 opus_int
	LastGainIndex           opus_int8
	fs_kHz                  opus_int
	fs_API_hz               opus_int32
	nb_subfr                opus_int
	frame_length            opus_int
	subfr_length            opus_int
	ltp_mem_length          opus_int
	LPC_order               opus_int
	prevNLSF_Q15            [MAX_LPC_ORDER]opus_int16
	first_frame_after_reset opus_int
	pitch_lag_low_bits_iCDF []opus_uint8
	pitch_contour_iCDF      []opus_uint8

	nFramesDecoded   opus_int
	nFramesPerPacket opus_int

	ec_prevSignalType opus_int
	ec_prevLagIndex   opus_int16

	VAD_flags  [MAX_FRAMES_PER_PACKET]opus_int
	LBRR_flag  opus_int
	LBRR_flags [MAX_FRAMES_PER_PACKET]opus_int

	resampler_state silk_resampler_state_struct

	psNLSF_CB *silk_NLSF_CB_struct

	indices        SideInfoIndices
	sCNG           silk_CNG_struct
	lossCnt        opus_int
	prevSignalType opus_int
	arch           int

	sPLC silk_PLC_struct

	// Per-frame scratch buffers. Sized to the configuration-independent
	// upper bounds (max 16 kHz SILK internal rate × 20 ms frame). Each
	// silk_decode_core call overwrites its used prefix before reading,
	// so no cross-call zeroing is required. Outside the reset zone
	// because the values are ephemeral.
	scratch_sLTP     [LTP_MEM_LENGTH_MS * MAX_FS_KHZ]opus_int16 // max ltp_mem_length
	scratch_sLTP_Q15 [LTP_MEM_LENGTH_MS*MAX_FS_KHZ + MAX_FRAME_LENGTH]opus_int32
	scratch_res_Q14  [MAX_SUB_FRAME_LENGTH]opus_int32
	scratch_sLPC_Q14 [MAX_SUB_FRAME_LENGTH + MAX_LPC_ORDER]opus_int32

	// Excitation pulses for silk_decode_frame — rounded up to the shell
	// coder frame boundary (MAX_FRAME_LENGTH is already aligned to 16).
	scratch_pulses [MAX_FRAME_LENGTH]opus_int16
}

// reset_from_prev_gain_Q16 clears every field from prev_gain_Q16 onward
// — the C source expresses this as a single `silk_memset` whose pointer
// argument is the field `&psDec->SILK_DECODER_STATE_RESET_START`. The
// whole struct is mirror-reset by overwriting with a zero value of the
// struct type; we express the partial reset by zeroing field-by-field.
func (psDec *silk_decoder_state) reset_from_prev_gain_Q16() {
	psDec.prev_gain_Q16 = 0
	psDec.exc_Q14 = [MAX_FRAME_LENGTH]opus_int32{}
	psDec.sLPC_Q14_buf = [MAX_LPC_ORDER]opus_int32{}
	psDec.outBuf = [MAX_FRAME_LENGTH + 2*MAX_SUB_FRAME_LENGTH]opus_int16{}
	psDec.lagPrev = 0
	psDec.LastGainIndex = 0
	psDec.fs_kHz = 0
	psDec.fs_API_hz = 0
	psDec.nb_subfr = 0
	psDec.frame_length = 0
	psDec.subfr_length = 0
	psDec.ltp_mem_length = 0
	psDec.LPC_order = 0
	psDec.prevNLSF_Q15 = [MAX_LPC_ORDER]opus_int16{}
	psDec.first_frame_after_reset = 0
	psDec.pitch_lag_low_bits_iCDF = nil
	psDec.pitch_contour_iCDF = nil
	psDec.nFramesDecoded = 0
	psDec.nFramesPerPacket = 0
	psDec.ec_prevSignalType = 0
	psDec.ec_prevLagIndex = 0
	psDec.VAD_flags = [MAX_FRAMES_PER_PACKET]opus_int{}
	psDec.LBRR_flag = 0
	psDec.LBRR_flags = [MAX_FRAMES_PER_PACKET]opus_int{}
	psDec.resampler_state = silk_resampler_state_struct{}
	psDec.psNLSF_CB = nil
	psDec.indices = SideInfoIndices{}
	psDec.sCNG = silk_CNG_struct{}
	psDec.lossCnt = 0
	psDec.prevSignalType = 0
	psDec.arch = 0
	psDec.sPLC = silk_PLC_struct{}
}

// silk_decoder_control — per-frame decoder control parameters.
// C: structs.h:346-354.
type silk_decoder_control struct {
	pitchL        [MAX_NB_SUBFR]opus_int
	Gains_Q16     [MAX_NB_SUBFR]opus_int32
	PredCoef_Q12  [2][MAX_LPC_ORDER]opus_int16
	LTPCoef_Q14   [LTP_ORDER * MAX_NB_SUBFR]opus_int16
	LTP_scale_Q14 opus_int
}
