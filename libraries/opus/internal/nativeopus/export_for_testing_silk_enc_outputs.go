package nativeopus

// Additional test shims for SILK encoder-side functions that produce
// a range-coded bitstream or mutate larger state structs.

// ExportTestProcessNLSFs runs silk_process_NLSFs on a caller-provided
// minimal state + input NLSF vector. Returns PredCoef_Q12 (both halves
// concatenated) plus the quantized NLSF and the chosen NLSF index vector.
func ExportTestProcessNLSFs(
	wb bool,
	speech_activity_Q8 int,
	useInterpolatedNLSFs int,
	NLSFInterpCoef_Q2 int,
	signalType int,
	nb_subfr int,
	NLSF_MSVQ_Survivors int,
	nlsf []int16,
	prev []int16,
) (predA, predB, nlsfOut []int16, indices []int8) {
	var s silk_encoder_state
	s.speech_activity_Q8 = opus_int(speech_activity_Q8)
	s.useInterpolatedNLSFs = opus_int(useInterpolatedNLSFs)
	s.indices.NLSFInterpCoef_Q2 = opus_int8(NLSFInterpCoef_Q2)
	s.indices.signalType = opus_int8(signalType)
	s.nb_subfr = opus_int(nb_subfr)
	s.NLSF_MSVQ_Survivors = opus_int(NLSF_MSVQ_Survivors)
	if wb {
		s.psNLSF_CB = &silk_NLSF_CB_WB
	} else {
		s.psNLSF_CB = &silk_NLSF_CB_NB_MB
	}
	s.predictLPCOrder = opus_int(s.psNLSF_CB.order)

	pNLSF := make([]opus_int16, len(nlsf))
	for i, v := range nlsf {
		pNLSF[i] = opus_int16(v)
	}
	prevQ := make([]opus_int16, MAX_LPC_ORDER)
	for i := 0; i < len(prev) && i < MAX_LPC_ORDER; i++ {
		prevQ[i] = opus_int16(prev[i])
	}

	var pc [2][MAX_LPC_ORDER]opus_int16
	silk_process_NLSFs(&s, &pc, pNLSF, prevQ)

	predA = make([]int16, MAX_LPC_ORDER)
	predB = make([]int16, MAX_LPC_ORDER)
	for i := 0; i < MAX_LPC_ORDER; i++ {
		predA[i] = int16(pc[0][i])
		predB[i] = int16(pc[1][i])
	}
	nlsfOut = make([]int16, len(pNLSF))
	for i, v := range pNLSF {
		nlsfOut[i] = int16(v)
	}
	indices = make([]int8, MAX_LPC_ORDER+1)
	for i := 0; i <= MAX_LPC_ORDER; i++ {
		indices[i] = int8(s.indices.NLSFIndices[i])
	}
	return
}

// ExportTestStereoLRToMS runs silk_stereo_LR_to_MS on two caller-provided
// channel buffers. The caller buffers must already include 2 samples of
// pre-history at indices [0..1] and the frame at [2..frame_length+1].
// Returns the mutated x1Buf and x2Buf (new M/S representation), the
// quantization indices and mid_only_flag, and the updated state fields
// that influence parity.
func ExportTestStereoLRToMS(
	x1Buf, x2Buf []int16,
	total_rate_bps int32,
	prev_speech_act_Q8, toMono, fs_kHz, frame_length int,
	stateIn StereoEncStateMirror,
) (x1Out, x2Out []int16, ix [2][3]int8, midOnly int8,
	midRate, sideRate int32, stateOut StereoEncStateMirror) {

	var st stereo_enc_state
	st.pred_prev_Q13[0] = opus_int16(stateIn.PredPrev0)
	st.pred_prev_Q13[1] = opus_int16(stateIn.PredPrev1)
	st.sMid[0] = opus_int16(stateIn.SMid0)
	st.sMid[1] = opus_int16(stateIn.SMid1)
	st.sSide[0] = opus_int16(stateIn.SSide0)
	st.sSide[1] = opus_int16(stateIn.SSide1)
	for i := 0; i < 4; i++ {
		st.mid_side_amp_Q0[i] = opus_int32(stateIn.MidSideAmp[i])
	}
	st.smth_width_Q14 = opus_int16(stateIn.SmthWidth)
	st.width_prev_Q14 = opus_int16(stateIn.WidthPrev)
	st.silent_side_len = opus_int16(stateIn.SilentSideLen)

	x1 := make([]opus_int16, len(x1Buf))
	x2 := make([]opus_int16, len(x2Buf))
	for i, v := range x1Buf {
		x1[i] = opus_int16(v)
	}
	for i, v := range x2Buf {
		x2[i] = opus_int16(v)
	}
	var ixInner [2][3]opus_int8
	var midOnlyInner opus_int8
	rates := make([]opus_int32, 2)

	silk_stereo_LR_to_MS(&st, x1, x2, &ixInner, &midOnlyInner, rates,
		opus_int32(total_rate_bps), opus_int(prev_speech_act_Q8),
		opus_int(toMono), opus_int(fs_kHz), opus_int(frame_length))

	x1Out = make([]int16, len(x1))
	x2Out = make([]int16, len(x2))
	for i, v := range x1 {
		x1Out[i] = int16(v)
	}
	for i, v := range x2 {
		x2Out[i] = int16(v)
	}
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			ix[i][j] = int8(ixInner[i][j])
		}
	}
	midOnly = int8(midOnlyInner)
	midRate = int32(rates[0])
	sideRate = int32(rates[1])
	stateOut.PredPrev0 = int16(st.pred_prev_Q13[0])
	stateOut.PredPrev1 = int16(st.pred_prev_Q13[1])
	stateOut.SMid0 = int16(st.sMid[0])
	stateOut.SMid1 = int16(st.sMid[1])
	stateOut.SSide0 = int16(st.sSide[0])
	stateOut.SSide1 = int16(st.sSide[1])
	for i := 0; i < 4; i++ {
		stateOut.MidSideAmp[i] = int32(st.mid_side_amp_Q0[i])
	}
	stateOut.SmthWidth = int16(st.smth_width_Q14)
	stateOut.WidthPrev = int16(st.width_prev_Q14)
	stateOut.SilentSideLen = int16(st.silent_side_len)
	return
}

// StereoEncStateMirror exposes stereo_enc_state's flat-value fields (no
// arrays of indices) for parity-test plumbing.
type StereoEncStateMirror struct {
	PredPrev0, PredPrev1 int16
	SMid0, SMid1         int16
	SSide0, SSide1       int16
	MidSideAmp           [4]int32
	SmthWidth, WidthPrev int16
	SilentSideLen        int16
}

// ExportTestEncodeIndices runs silk_encode_indices with a caller-supplied
// psEncC state + SideInfoIndices. Returns the final entropy-coded
// buffer as produced by the range encoder after this single call.
func ExportTestEncodeIndices(
	wb bool,
	nb_subfr int,
	fs_kHz int,
	signalType, quantOffsetType int,
	gainsIdx []int8,
	NLSFIdx []int8,
	lagIndex int16,
	contourIdx int,
	NLSFInterpCoef_Q2 int,
	PERIndex int,
	LTPIdx []int8,
	LTPscaleIdx int,
	seed int,
	ec_prevSignalType int,
	ec_prevLagIndex int16,
	encode_LBRR int,
	condCoding int,
	bufSize int,
) []byte {
	var s silk_encoder_state
	s.nb_subfr = opus_int(nb_subfr)
	s.fs_kHz = opus_int(fs_kHz)
	if wb {
		s.psNLSF_CB = &silk_NLSF_CB_WB
	} else {
		s.psNLSF_CB = &silk_NLSF_CB_NB_MB
	}
	s.predictLPCOrder = opus_int(s.psNLSF_CB.order)
	s.ec_prevSignalType = opus_int(ec_prevSignalType)
	s.ec_prevLagIndex = opus_int16(ec_prevLagIndex)

	// Use the NB pitch-lag/contour iCDFs for fs_kHz==8, WB for others.
	if fs_kHz == 8 && nb_subfr == 4 {
		s.pitch_lag_low_bits_iCDF = asByteSlice(silk_uniform4_iCDF[:])
		s.pitch_contour_iCDF = asByteSlice(silk_pitch_contour_NB_iCDF[:])
	} else if fs_kHz == 8 {
		s.pitch_lag_low_bits_iCDF = asByteSlice(silk_uniform4_iCDF[:])
		s.pitch_contour_iCDF = asByteSlice(silk_pitch_contour_10_ms_NB_iCDF[:])
	} else if nb_subfr == 4 {
		s.pitch_lag_low_bits_iCDF = asByteSlice(silk_uniform8_iCDF[:])
		s.pitch_contour_iCDF = asByteSlice(silk_pitch_contour_iCDF[:])
	} else {
		s.pitch_lag_low_bits_iCDF = asByteSlice(silk_uniform8_iCDF[:])
		s.pitch_contour_iCDF = asByteSlice(silk_pitch_contour_10_ms_iCDF[:])
	}

	idx := &s.indices
	idx.signalType = opus_int8(signalType)
	idx.quantOffsetType = opus_int8(quantOffsetType)
	for i := 0; i < len(gainsIdx) && i < MAX_NB_SUBFR; i++ {
		idx.GainsIndices[i] = opus_int8(gainsIdx[i])
	}
	for i := 0; i < len(NLSFIdx) && i <= MAX_LPC_ORDER; i++ {
		idx.NLSFIndices[i] = opus_int8(NLSFIdx[i])
	}
	idx.lagIndex = opus_int16(lagIndex)
	idx.contourIndex = opus_int8(contourIdx)
	idx.NLSFInterpCoef_Q2 = opus_int8(NLSFInterpCoef_Q2)
	idx.PERIndex = opus_int8(PERIndex)
	for i := 0; i < len(LTPIdx) && i < MAX_NB_SUBFR; i++ {
		idx.LTPIndex[i] = opus_int8(LTPIdx[i])
	}
	idx.LTP_scaleIndex = opus_int8(LTPscaleIdx)
	idx.Seed = opus_int8(seed)

	buf := make([]byte, bufSize)
	var enc ec_enc
	ec_enc_init(&enc, buf, opus_uint32(bufSize))
	silk_encode_indices(&s, &enc, 0, opus_int(encode_LBRR), opus_int(condCoding))
	ec_enc_done(&enc)
	n := int(enc.offs)
	out := make([]byte, n)
	copy(out, buf[:n])
	return out
}

// ExportTestSilkEncodePulses runs silk_encode_pulses and returns the encoded
// bytes.
func ExportTestSilkEncodePulses(signalType, quantOffsetType int, pulses []int8,
	frame_length int, bufSize int) []byte {
	p := make([]opus_int8, frame_length+SHELL_CODEC_FRAME_LENGTH)
	for i := 0; i < len(pulses) && i < frame_length; i++ {
		p[i] = opus_int8(pulses[i])
	}
	buf := make([]byte, bufSize)
	var enc ec_enc
	ec_enc_init(&enc, buf, opus_uint32(bufSize))
	silk_encode_pulses(&enc, opus_int(signalType), opus_int(quantOffsetType), p, opus_int(frame_length))
	ec_enc_done(&enc)
	n := int(enc.offs)
	out := make([]byte, n)
	copy(out, buf[:n])
	return out
}
