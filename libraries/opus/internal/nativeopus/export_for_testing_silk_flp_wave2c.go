package nativeopus

// Thin exports for SILK _FLP pitch-analysis mid-driver parity tests
// (Phase 8 Wave 2c: pitch_analysis_core_FLP + find_pitch_lags_FLP).

// ExportTestSilkPitchAnalysisCoreFLP invokes
// silk_pitch_analysis_core_FLP and returns the full set of outputs.
// `frame` length must be PE_FRAME_LENGTH_MS * Fs_kHz worth of samples
// (caller's responsibility; we pass through unchanged).
func ExportTestSilkPitchAnalysisCoreFLP(
	frame []float32,
	LTPCorrIn float32,
	prevLag int,
	searchThres1 float32,
	searchThres2 float32,
	Fs_kHz int,
	complexity int,
	nb_subfr int,
) (voicing int, pitchOut []int32, lagIndex int16, contourIndex int8, LTPCorrOut float32) {
	// The C version always zero-writes PE_MAX_NB_SUBFR entries when it
	// fails to find a candidate, so we must allocate at least that many.
	pitchOut32 := make([]opus_int, PE_MAX_NB_SUBFR)
	var lIdx opus_int16
	var cIdx opus_int8
	ltp := silk_float(LTPCorrIn)
	voicing = int(silk_pitch_analysis_core_FLP(
		frame, pitchOut32, &lIdx, &cIdx, &ltp,
		opus_int(prevLag),
		silk_float(searchThres1),
		silk_float(searchThres2),
		opus_int(Fs_kHz),
		opus_int(complexity),
		opus_int(nb_subfr),
		0,
	))
	pitchOut = make([]int32, nb_subfr)
	for i := 0; i < nb_subfr; i++ {
		pitchOut[i] = int32(pitchOut32[i])
	}
	lagIndex = int16(lIdx)
	contourIndex = int8(cIdx)
	LTPCorrOut = float32(ltp)
	return
}

// ExportFindPitchLagsInputs — scalar fields needed to exercise
// silk_find_pitch_lags_FLP. We don't require callers to build an
// entire encoder state; we fill in just the fields used by the
// function under test.
type ExportFindPitchLagsInputs struct {
	Fs_kHz                       int
	Nb_subfr                     int
	La_pitch                     int
	Frame_length                 int
	Ltp_mem_length               int
	Pitch_LPC_win_length         int
	PitchEstimationLPCOrder      int
	PitchEstimationComplexity    int
	PitchEstimationThreshold_Q16 int32
	SpeechActivity_Q8            int
	InputTilt_Q15                int
	PrevSignalType               int8
	SignalType                   int8
	FirstFrameAfterReset         int
	PrevLag                      int
	LTPCorrIn                    float32
}

// ExportFindPitchLagsOutputs — everything the driver mutates (modulo
// psEncCtrl.pitchL).
type ExportFindPitchLagsOutputs struct {
	Res          []float32
	PredGain     float32
	LTPCorr      float32
	PitchL       []int32
	LagIndex     int16
	ContourIndex int8
	SignalType   int8
}

// ExportTestSilkFindPitchLagsFLP — runs silk_find_pitch_lags_FLP. The
// caller passes `x` (speech buffer of length la_pitch+frame_length+
// ltp_mem_length); the function reads x[-ltp_mem_length .. buf_len]
// via a backing x_buf of the same shape, so we provide a buffer
// `bigX` of total length `ltp_mem_length + buf_len` and an xOff into
// it that gives `x = &bigX[xOff]`. `bigX` must include the LTP memory
// region before x.
func ExportTestSilkFindPitchLagsFLP(
	in ExportFindPitchLagsInputs,
	bigX []float32, xOff int,
) ExportFindPitchLagsOutputs {
	var psEnc silk_encoder_state_FLP
	var psEncCtrl silk_encoder_control_FLP

	psEnc.sCmn.fs_kHz = opus_int(in.Fs_kHz)
	psEnc.sCmn.nb_subfr = opus_int(in.Nb_subfr)
	psEnc.sCmn.la_pitch = opus_int(in.La_pitch)
	psEnc.sCmn.frame_length = opus_int(in.Frame_length)
	psEnc.sCmn.ltp_mem_length = opus_int(in.Ltp_mem_length)
	psEnc.sCmn.pitch_LPC_win_length = opus_int(in.Pitch_LPC_win_length)
	psEnc.sCmn.pitchEstimationLPCOrder = opus_int(in.PitchEstimationLPCOrder)
	psEnc.sCmn.pitchEstimationComplexity = opus_int(in.PitchEstimationComplexity)
	psEnc.sCmn.pitchEstimationThreshold_Q16 = opus_int32(in.PitchEstimationThreshold_Q16)
	psEnc.sCmn.speech_activity_Q8 = opus_int(in.SpeechActivity_Q8)
	psEnc.sCmn.input_tilt_Q15 = opus_int(in.InputTilt_Q15)
	psEnc.sCmn.prevSignalType = opus_int8(in.PrevSignalType)
	psEnc.sCmn.indices.signalType = opus_int8(in.SignalType)
	psEnc.sCmn.first_frame_after_reset = opus_int(in.FirstFrameAfterReset)
	psEnc.sCmn.prevLag = opus_int(in.PrevLag)
	psEnc.LTPCorr = silk_float(in.LTPCorrIn)

	buf_len := in.La_pitch + in.Frame_length + in.Ltp_mem_length
	res := make([]silk_float, buf_len)

	silk_find_pitch_lags_FLP_withBase(&psEnc, &psEncCtrl, res, bigX, opus_int(xOff), 0)

	out := ExportFindPitchLagsOutputs{
		Res:          make([]float32, buf_len),
		PredGain:     float32(psEncCtrl.predGain),
		LTPCorr:      float32(psEnc.LTPCorr),
		PitchL:       make([]int32, MAX_NB_SUBFR),
		LagIndex:     int16(psEnc.sCmn.indices.lagIndex),
		ContourIndex: int8(psEnc.sCmn.indices.contourIndex),
		SignalType:   int8(psEnc.sCmn.indices.signalType),
	}
	for i := 0; i < buf_len; i++ {
		out.Res[i] = float32(res[i])
	}
	for i := 0; i < MAX_NB_SUBFR; i++ {
		out.PitchL[i] = int32(psEncCtrl.pitchL[i])
	}
	return out
}
